package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalSession(t *testing.T) (*Session, func(), string) {
	t.Helper()
	dir := t.TempDir()
	session, release, err := OpenSession(context.Background(), dir, "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Checkpoint(); err != nil {
		release()
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		release()
		t.Fatal(files)
	}
	return session, release, files[0]
}

func reopenJournal(t *testing.T, path string) (*Session, func()) {
	t.Helper()
	session, release, err := OpenSession(context.Background(), filepath.Dir(path), "session", "agent", "task")
	if err != nil {
		t.Fatal(err)
	}
	return session, release
}

func TestCheckpointAppendsOnlyChangedSuffix(t *testing.T) {
	session, release, path := journalSession(t)
	session.AppendMessage(NewUserMessage(strings.Repeat("unchanged prefix", 2000)))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path + ".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	snapshotBefore, _ := os.ReadFile(path)
	session.AppendMessage(NewAssistantMessage(TextPart("new small reply")))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path + ".jsonl")
	if !bytes.HasPrefix(after, before) || bytes.Contains(after[len(before):], []byte("unchanged prefix")) || len(after)-len(before) > 1000 {
		t.Fatalf("checkpoint rewrote history: new record size=%d", len(after)-len(before))
	}
	snapshotAfter, _ := os.ReadFile(path)
	if !bytes.Equal(snapshotBefore, snapshotAfter) {
		t.Fatal("every checkpoint rewrote full snapshot")
	}
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(path + ".jsonl")
	if !bytes.Equal(after, unchanged) {
		t.Fatal("unchanged checkpoint appended a record")
	}
	release()
	restored, release := reopenJournal(t, path)
	defer release()
	if restored.Len() != 2 || restored.Messages()[1].Text() != "new small reply" {
		t.Fatalf("journal resume=%+v", restored.Messages())
	}
}

func TestJournalPersistsApprovedArgumentsMetadataUsageAndCompaction(t *testing.T) {
	session, release, path := journalSession(t)
	session.AppendMessage(NewUserMessage("work"), NewAssistantMessage(ToolCallPart("call", "tool", json.RawMessage(`{"original":true}`))))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	session.UpdateToolCall(&ToolCallData{ID: "call", Name: "tool", Arguments: json.RawMessage(`{"approved":true}`)})
	session.SetMetadata("todos", []string{"one", "two"})
	session.RecordUsage(TokenUsage{InputTokens: 7, OutputTokens: 3})
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release = reopenJournal(t, path)
	if string(session.Messages()[1].ToolCalls()[0].Arguments) != `{"approved":true}` || session.Usage().InputTokens != 7 {
		t.Fatal("approved arguments or usage lost")
	}
	if _, ok := session.GetMetadata("todos"); !ok {
		t.Fatal("metadata lost")
	}
	session.ReplaceMessages([]Message{NewUserMessage("summary"), NewAssistantMessage(TextPart("tail"))})
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release = reopenJournal(t, path)
	defer release()
	if session.Len() != 2 || session.Messages()[0].Text() != "summary" || session.Usage().OutputTokens != 3 {
		t.Fatal("compaction not replayed")
	}
}

func TestJournalRecoversInterruptedAppendWithoutLosingCommittedRecords(t *testing.T) {
	session, release, path := journalSession(t)
	session.AppendMessage(NewUserMessage("committed"))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	committed, _ := os.ReadFile(path + ".jsonl")
	file, err := os.OpenFile(path+".jsonl", os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(`{"data":{"sequence":2,"messages":[`)
	file.Close()
	session, release = reopenJournal(t, path)
	if session.Len() != 1 || session.Messages()[0].Text() != "committed" {
		t.Fatal("recovery lost committed history")
	}
	trimmed, _ := os.ReadFile(path + ".jsonl")
	if !bytes.Equal(committed, trimmed) {
		t.Fatal("partial append not repaired")
	}
	session.AppendMessage(NewAssistantMessage(TextPart("after recovery")))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release = reopenJournal(t, path)
	defer release()
	if session.Len() != 2 {
		t.Fatal("subsequent append invalid after recovery")
	}
}

func TestJournalRejectsCommittedCorruptionAndSequenceGaps(t *testing.T) {
	for _, failure := range []string{"checksum", "gap", "identity", "snapshot"} {
		t.Run(failure, func(t *testing.T) {
			session, release, path := journalSession(t)
			session.AppendMessage(NewUserMessage("committed"))
			if err := session.Checkpoint(); err != nil {
				t.Fatal(err)
			}
			release()
			if failure == "snapshot" {
				data, _ := os.ReadFile(path)
				data = bytes.Replace(data, []byte("agent"), []byte("AGENT"), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				data, _ := os.ReadFile(path + ".jsonl")
				if failure == "checksum" {
					data = bytes.Replace(data, []byte("committed"), []byte("CORRUPTED"), 1)
				} else {
					var delta sessionDelta
					if err := decodeChecked(bytes.TrimSuffix(data, []byte{'\n'}), &delta); err != nil {
						t.Fatal(err)
					}
					if failure == "gap" {
						delta.Sequence = 3
					} else {
						delta.AgentID = "other"
					}
					data, _ = checkedJSON(delta)
					data = append(data, '\n')
				}
				if err := os.WriteFile(path+".jsonl", data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, release, err := OpenSession(context.Background(), filepath.Dir(path), "session", "agent", "task")
			if err == nil {
				release()
				t.Fatal("committed corruption silently accepted")
			}
		})
	}
}

func TestSnapshotRotationAndCrashBeforeJournalRemoval(t *testing.T) {
	session, release, path := journalSession(t)
	for i := 0; i < snapshotEveryRecords-1; i++ {
		session.AppendMessage(NewUserMessage("message"))
		if err := session.Checkpoint(); err != nil {
			t.Fatal(err)
		}
	}
	oldJournal, _ := os.ReadFile(path + ".jsonl")
	session.AppendMessage(NewUserMessage("last snapshot message"))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	rotated, _ := os.ReadFile(path + ".jsonl")
	if len(rotated) != 0 {
		t.Fatal("journal not rotated")
	}
	var snapshot sessionSnapshot
	data, _ := os.ReadFile(path)
	if err := decodeChecked(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Sequence != snapshotEveryRecords {
		t.Fatalf("snapshot sequence=%d", snapshot.Sequence)
	}
	release()
	// Simulate a crash after durable snapshot rename but before journal cleanup.
	if err := os.WriteFile(path+".jsonl", oldJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	session, release = reopenJournal(t, path)
	if session.Len() != snapshotEveryRecords {
		t.Fatalf("snapshot history replayed twice: len=%d", session.Len())
	}
	session.AppendMessage(NewAssistantMessage(TextPart("new tail")))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release = reopenJournal(t, path)
	defer release()
	if session.Len() != snapshotEveryRecords+1 {
		t.Fatal("post-rotation history lost")
	}
}

func TestLegacySnapshotMigratesWithoutLosingHistory(t *testing.T) {
	session, release, path := journalSession(t)
	session.AppendMessage(NewUserMessage("legacy"))
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	session, release = reopenJournal(t, path)
	if session.Len() != 1 {
		t.Fatal("legacy history lost")
	}
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	var snapshot sessionSnapshot
	if err := decodeChecked(data, &snapshot); err != nil {
		t.Fatalf("legacy not migrated: %v", err)
	}
	session.AppendMessage(NewAssistantMessage(TextPart("new")))
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	session, release = reopenJournal(t, path)
	defer release()
	if session.Len() != 2 {
		t.Fatal("legacy continuation lost")
	}
}

func TestFailedJournalWriteCanRetryWithoutClearingDirtyState(t *testing.T) {
	session, release, path := journalSession(t)
	defer release()
	if err := os.Mkdir(path+".jsonl", 0o700); err != nil {
		t.Fatal(err)
	}
	session.AppendMessage(NewUserMessage("retry this write"))
	if err := session.Checkpoint(); err == nil {
		t.Fatal("write should fail with a directory at journal path")
	}
	if session.historyDirty != 0 {
		t.Fatal("failed write cleared dirty history")
	}
	if err := os.Remove(path + ".jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path + ".jsonl")
	if !bytes.Contains(data, []byte("retry this write")) {
		t.Fatal("retry omitted pending change")
	}
}

func BenchmarkIncrementalSessionCheckpoint(b *testing.B) {
	dir := b.TempDir()
	session, release, err := OpenSession(context.Background(), dir, "bench", "agent", "task")
	if err != nil {
		b.Fatal(err)
	}
	defer release()
	session.AppendMessage(NewUserMessage(strings.Repeat("history", 100000)))
	if err := session.Checkpoint(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		session.AppendMessage(NewAssistantMessage(TextPart("short continuation")))
		if err := session.Checkpoint(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFullSessionSnapshot(b *testing.B) {
	session := NewSession("bench", "agent", "task")
	session.AppendMessage(NewUserMessage(strings.Repeat("history", 100000)))
	store := sessionJournal{path: filepath.Join(b.TempDir(), "snapshot.json")}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		session.AppendMessage(NewAssistantMessage(TextPart("short continuation")))
		if err := store.snapshot(session); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSnapshotRotatesWhenJournalByteBudgetIsReached(t *testing.T) {
	session, release, path := journalSession(t)
	defer release()
	for range 2 {
		session.AppendMessage(NewUserMessage(strings.Repeat("x", 3<<20)))
		if err := session.Checkpoint(); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path + ".jsonl")
	if len(data) != 0 {
		t.Fatal("large journal not rotated")
	}
	data, _ = os.ReadFile(path)
	var snapshot sessionSnapshot
	if err := decodeChecked(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Session.Len() != 2 || snapshot.Sequence != 2 {
		t.Fatal("byte-budget snapshot lost messages")
	}
}
