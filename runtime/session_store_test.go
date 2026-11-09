package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStoreColdResumeAndIsolation(t *testing.T) {
	dir := t.TempDir()
	session, release, err := OpenSession(context.Background(), dir, "../conversation", "chat", "task")
	if err != nil {
		t.Fatal(err)
	}
	part := ThinkingPart("reasoning")
	part.Signature = "signed"
	session.AppendMessage(NewUserMessage("remember"), NewAssistantMessage(part, TextPart("answer")))
	session.RecordUsage(TokenUsage{InputTokens: 10, OutputTokens: 2})
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release, err = OpenSession(context.Background(), dir, "../conversation", "chat", "task")
	if err != nil {
		t.Fatal(err)
	}
	if session.Len() != 2 || session.Messages()[1].Parts[0].Signature != "signed" || session.Usage().InputTokens != 10 {
		t.Fatalf("cold resume lost state: %+v", session)
	}
	release()
	for _, identity := range [][3]string{{"other", "chat", "task"}, {"../conversation", "other", "task"}, {"../conversation", "chat", "other"}} {
		isolated, release, err := OpenSession(context.Background(), dir, identity[0], identity[1], identity[2])
		if err != nil {
			t.Fatal(err)
		}
		if isolated.Len() != 0 {
			t.Fatal("session identity leaked history")
		}
		release()
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("hashed session files=%v err=%v", files, err)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session permissions=%o", info.Mode().Perm())
	}
}

func TestSessionStoreWaitCanBeCancelled(t *testing.T) {
	dir := t.TempDir()
	_, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = OpenSession(ctx, dir, "session", "agent", "task")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting for same session: %v", err)
	}
	release()
	session, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	session.AppendMessage(NewUserMessage("still works"))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStoreSerializesReadExecuteSave(t *testing.T) {
	dir := t.TempDir()
	first, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		second, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
		if err != nil {
			done <- err
			return
		}
		defer release()
		if second.Len() != 1 {
			done <- errors.New("second runner read a stale snapshot")
			return
		}
		second.AppendMessage(NewAssistantMessage(TextPart("second")))
		done <- second.Checkpoint()
	}()
	first.AppendMessage(NewUserMessage("first"))
	if err := first.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	last, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if last.Len() != 2 {
		t.Fatalf("concurrent turn lost: %+v", last.Messages())
	}
}
