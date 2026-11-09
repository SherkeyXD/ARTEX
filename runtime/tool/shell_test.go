//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestInteractiveShellRoundTripAndIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager := NewShellManager(ctx, t.TempDir(), nil)
	defer manager.Close()
	snapshot, err := manager.Open(ctx, ShellOpenOptions{Command: `stty -echo; printf 'ready>'; IFS= read -r line; printf '\nreply:%s\n' "$line"; IFS= read -r line`, QuietMS: 20, PromptRegex: `ready>`, Watch: []string{"ready"}})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Running || !strings.Contains(snapshot.Output, "ready>") || !snapshot.PromptDetected || len(snapshot.Matches) != 1 {
		t.Fatalf("initial PTY state=%+v", snapshot)
	}
	other := NewShellManager(ctx, t.TempDir(), nil)
	defer other.Close()
	if _, err := other.Read(ctx, snapshot.SessionID, false); err == nil {
		t.Fatal("shell leaked into another run")
	}
	reply, err := manager.Send(ctx, snapshot.SessionID, "hello", "enter")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Output, "reply:hello") {
		t.Fatalf("PTY input not received: %+v", reply)
	}
	screen, err := manager.Read(ctx, snapshot.SessionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.Screen, "reply:hello") {
		t.Fatalf("screen renderer lost output: %+v", screen)
	}
	if len(manager.List()) != 1 {
		t.Fatal("shell missing from list")
	}
	if err := manager.CloseSession(snapshot.SessionID); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 0 {
		t.Fatal("closed shell still listed")
	}
}

func TestShellToolsUseRunManagerAndCleanUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager := NewShellManager(ctx, t.TempDir(), nil)
	ctx = WithShellManager(ctx, manager)
	result, err := NewShellOpen().Call(ctx, json.RawMessage(`{"command":"stty -echo; printf ready; read line","quiet_ms":20}`), nil)
	if err != nil || result.IsError {
		t.Fatalf("open=%+v err=%v", result, err)
	}
	var snapshot ShellSnapshot
	if err := json.Unmarshal([]byte(result.Flatten()), &snapshot); err != nil {
		t.Fatal(err)
	}
	session, err := manager.get(snapshot.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	select {
	case <-session.done:
	default:
		t.Fatal("Close returned before joining process")
	}
	select {
	case <-session.readerDone:
	default:
		t.Fatal("Close returned before joining PTY reader")
	}
	if _, err := manager.Open(ctx, ShellOpenOptions{Command: "true"}); err == nil {
		t.Fatal("closed manager accepted a new shell")
	}
}

func TestShellCancellationStopsChildProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := NewShellManager(ctx, t.TempDir(), nil)
	defer manager.Close()
	snapshot, err := manager.Open(ctx, ShellOpenOptions{Command: "sleep 30", Mode: "dispatch"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.get(snapshot.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not terminate shell process group")
	}
}
