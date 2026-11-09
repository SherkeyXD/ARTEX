package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type sessionLock struct {
	token chan struct{}
	users int
}

var sessionLocks = struct {
	sync.Mutex
	entries map[string]*sessionLock
}{entries: make(map[string]*sessionLock)}

// OpenSession serializes a read/execute/save cycle for one session in this process.
// Identifiers are hashed so user-controlled session names never become paths.
// An empty directory explicitly selects an ephemeral session.
func OpenSession(ctx context.Context, dir, id, agentID, taskID string) (*Session, func(), error) {
	if dir == "" {
		return NewSession(id, agentID, taskID), func() {}, nil
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}
	key, _ := json.Marshal([]string{id, agentID, taskID})
	hash := sha256.Sum256(key)
	path := filepath.Join(root, hex.EncodeToString(hash[:])+".json")
	sessionLocks.Lock()
	lock := sessionLocks.entries[path]
	if lock == nil {
		lock = &sessionLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		sessionLocks.entries[path] = lock
	}
	lock.users++
	sessionLocks.Unlock()
	drop := func() {
		sessionLocks.Lock()
		defer sessionLocks.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(sessionLocks.entries, path)
		}
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, nil, ctx.Err()
	case <-lock.token:
	}
	release := func() { lock.token <- struct{}{}; drop() }
	if ctx.Err() != nil {
		release()
		return nil, nil, ctx.Err()
	}
	session := NewSession(id, agentID, taskID)
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, session); err != nil {
			release()
			return nil, nil, fmt.Errorf("load session: %w", err)
		}
		if session.ID != id || session.AgentID != agentID || session.TaskID != taskID {
			release()
			return nil, nil, fmt.Errorf("session identity mismatch")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		release()
		return nil, nil, err
	}
	session.SetCheckpoint(func(s *Session) error { return saveSession(path, s) })
	return session, release, nil
}

func saveSession(path string, session *Session) error {
	session.mu.RLock()
	data, err := json.Marshal(session)
	session.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
