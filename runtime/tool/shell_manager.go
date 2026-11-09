package tool

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/hinshun/vt10x"
)

type ShellOpenOptions struct {
	Command        string   `json:"command"`
	Mode           string   `json:"mode"`
	Watch          []string `json:"watch"`
	PromptRegex    string   `json:"prompt_regex"`
	Rows           int      `json:"rows"`
	Cols           int      `json:"cols"`
	QuietMS        int      `json:"quiet_ms"`
	StartupGraceMS int      `json:"startup_grace_ms"`
}

type ShellSnapshot struct {
	SessionID      string   `json:"session_id"`
	Output         string   `json:"output"`
	Screen         string   `json:"screen,omitempty"`
	Running        bool     `json:"running"`
	ExitError      string   `json:"exit_error,omitempty"`
	Matches        []string `json:"matches,omitempty"`
	PromptDetected bool     `json:"prompt_detected,omitempty"`
	Truncated      bool     `json:"truncated,omitempty"`
}

// ShellManager owns PTYs for one run. Close joins the processes and readers;
// a shell from another run is never addressable through this manager.
type ShellManager struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	workingDir string
	env        []string
	sessions   map[string]*shellSession
	closed     bool
}

type shellSession struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	file       io.ReadWriteCloser
	cmd        *exec.Cmd
	terminal   vt10x.Terminal
	output     strings.Builder
	truncated  bool
	lastOutput time.Time
	quiet      time.Duration
	watch      []string
	prompt     *regexp.Regexp
	running    bool
	exitError  string
	changed    chan struct{}
	done       chan struct{}
	readerDone chan struct{}
	closeOnce  sync.Once
}

func NewShellManager(ctx context.Context, workingDir string, env []string) *ShellManager {
	ctx, cancel := context.WithCancel(ctx)
	return &ShellManager{ctx: ctx, cancel: cancel, workingDir: workingDir, env: append([]string(nil), env...), sessions: make(map[string]*shellSession)}
}

func (m *ShellManager) Open(ctx context.Context, args ShellOpenOptions) (ShellSnapshot, error) {
	if strings.TrimSpace(args.Command) == "" {
		return ShellSnapshot{}, fmt.Errorf("shell command cannot be empty")
	}
	if args.Mode == "" {
		args.Mode = "interactive"
	}
	switch args.Mode {
	case "interactive", "hands-free", "dispatch", "monitor":
	default:
		return ShellSnapshot{}, fmt.Errorf("invalid shell mode %q", args.Mode)
	}
	if args.Rows == 0 {
		args.Rows = 24
	}
	if args.Cols == 0 {
		args.Cols = 80
	}
	if args.Rows < 1 || args.Rows > 200 || args.Cols < 1 || args.Cols > 300 {
		return ShellSnapshot{}, fmt.Errorf("terminal dimensions out of range")
	}
	if args.QuietMS < 0 || args.QuietMS > 5000 || args.StartupGraceMS < 0 || args.StartupGraceMS > 10000 {
		return ShellSnapshot{}, fmt.Errorf("shell wait options out of range")
	}
	quiet := time.Duration(args.QuietMS) * time.Millisecond
	if quiet == 0 {
		quiet = 100 * time.Millisecond
	}
	var prompt *regexp.Regexp
	if args.PromptRegex != "" {
		var err error
		prompt, err = regexp.Compile(args.PromptRegex)
		if err != nil {
			return ShellSnapshot{}, err
		}
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return ShellSnapshot{}, fmt.Errorf("shell manager closed")
	}
	if len(m.sessions) >= 16 {
		m.mu.Unlock()
		return ShellSnapshot{}, fmt.Errorf("run has reached the 16-shell limit; close a shell first")
	}
	file, cmd, err := startShell(m.ctx, args.Command, m.workingDir, m.env, args.Rows, args.Cols)
	if err != nil {
		m.mu.Unlock()
		return ShellSnapshot{}, err
	}
	id := llm.NewSessionID()
	session := &shellSession{file: file, cmd: cmd, terminal: vt10x.New(vt10x.WithSize(args.Cols, args.Rows), vt10x.WithWriter(file)), quiet: quiet, watch: append([]string(nil), args.Watch...), prompt: prompt, running: true, lastOutput: time.Now(), changed: make(chan struct{}, 1), done: make(chan struct{}), readerDone: make(chan struct{})}
	m.sessions[id] = session
	go session.readOutput()
	go func() {
		err := cmd.Wait()
		session.mu.Lock()
		session.running = false
		if err != nil {
			session.exitError = err.Error()
		}
		session.mu.Unlock()
		close(session.done)
		session.signal()
	}()
	// Unlock while waiting; Close must be able to interrupt startup reads.
	m.mu.Unlock()
	wait := 2 * time.Second
	if args.Mode == "dispatch" {
		wait = 0
	}
	if args.StartupGraceMS > 0 {
		wait = time.Duration(args.StartupGraceMS) * time.Millisecond
	}
	snapshot, err := session.read(ctx, id, false, wait)
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		session.close()
	}
	return snapshot, err
}

func (s *shellSession) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *shellSession) readOutput() {
	defer close(s.readerDone)
	buf := make([]byte, 8192)
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.terminal.Write(buf[:n])
			if s.output.Len()+n > 1<<20 {
				tail := s.output.String() + string(buf[:n])
				if len(tail) > 1<<20 {
					tail = tail[len(tail)-(1<<20):]
				}
				s.output.Reset()
				s.output.WriteString(tail)
				s.truncated = true
			} else {
				s.output.Write(buf[:n])
			}
			s.lastOutput = time.Now()
			s.mu.Unlock()
			s.signal()
		}
		if err != nil {
			return
		}
	}
}

func (m *ShellManager) get(id string) (*shellSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if m.closed || s == nil {
		return nil, fmt.Errorf("shell session %q not found in this run", id)
	}
	return s, nil
}

func (m *ShellManager) Send(ctx context.Context, id, input, key string) (ShellSnapshot, error) {
	s, err := m.get(id)
	if err != nil {
		return ShellSnapshot{}, err
	}
	keyBytes, err := shellKey(key)
	if err != nil {
		return ShellSnapshot{}, err
	}
	if len(input)+len(keyBytes) == 0 {
		return ShellSnapshot{}, fmt.Errorf("shell_send requires input or key")
	}
	if len(input) > 64<<10 {
		return ShellSnapshot{}, fmt.Errorf("shell input exceeds 64 KiB")
	}
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running {
		return ShellSnapshot{}, fmt.Errorf("shell has exited")
	}
	if ctx.Err() != nil {
		return ShellSnapshot{}, ctx.Err()
	}
	s.writeMu.Lock()
	_, err = s.file.Write([]byte(input + keyBytes))
	s.writeMu.Unlock()
	if err != nil {
		return ShellSnapshot{}, err
	}
	return s.read(ctx, id, false, 2*time.Second)
}

func (m *ShellManager) Read(ctx context.Context, id string, screen bool) (ShellSnapshot, error) {
	s, err := m.get(id)
	if err != nil {
		return ShellSnapshot{}, err
	}
	return s.read(ctx, id, screen, 2*time.Second)
}

func (s *shellSession) read(ctx context.Context, id string, screen bool, maxWait time.Duration) (ShellSnapshot, error) {
	deadline := time.NewTimer(maxWait)
	defer deadline.Stop()
	for maxWait > 0 {
		if ctx.Err() != nil {
			return ShellSnapshot{}, ctx.Err()
		}
		s.mu.Lock()
		ready := s.output.Len() > 0 && time.Since(s.lastOutput) >= s.quiet
		running := s.running
		s.mu.Unlock()
		if ready || !running {
			break
		}
		timer := time.NewTimer(s.quiet)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ShellSnapshot{}, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			maxWait = 0
		case <-s.changed:
			timer.Stop()
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	output := s.output.String()
	s.output.Reset()
	result := ShellSnapshot{SessionID: id, Output: output, Running: s.running, ExitError: s.exitError, Truncated: s.truncated}
	s.truncated = false
	if screen {
		result.Screen = s.terminal.String()
	}
	for _, word := range s.watch {
		if word != "" && strings.Contains(output, word) {
			result.Matches = append(result.Matches, word)
		}
	}
	if s.prompt != nil {
		result.PromptDetected = s.prompt.MatchString(output)
	}
	return result, nil
}

func (s *shellSession) close() {
	s.closeOnce.Do(func() { killShell(s.cmd); _ = s.file.Close(); <-s.done; <-s.readerDone })
}

func (m *ShellManager) CloseSession(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("shell session %q not found", id)
	}
	s.close()
	return nil
}

func (m *ShellManager) List() []ShellSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ShellSnapshot, 0, len(m.sessions))
	for id, s := range m.sessions {
		s.mu.Lock()
		out = append(out, ShellSnapshot{SessionID: id, Running: s.running, ExitError: s.exitError})
		s.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

func (m *ShellManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	sessions := m.sessions
	m.sessions = make(map[string]*shellSession)
	m.mu.Unlock()
	m.cancel()
	for _, s := range sessions {
		s.close()
	}
}
