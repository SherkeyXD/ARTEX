package runtime

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	sessionFormatVersion  = 1
	snapshotEveryRecords  = 64
	snapshotEveryBytes    = 4 << 20
	maxJournalRecordBytes = 64 << 20
)

type checkedRecord struct {
	Data     json.RawMessage `json:"data"`
	Checksum string          `json:"checksum"`
}
type sessionSnapshot struct {
	Version  int      `json:"version"`
	Sequence uint64   `json:"sequence"`
	Session  *Session `json:"session"`
}
type sessionDelta struct {
	Version      int            `json:"version"`
	Sequence     uint64         `json:"sequence"`
	ID           string         `json:"id"`
	AgentID      string         `json:"agent_id"`
	TaskID       string         `json:"task_id"`
	HistoryStart *int           `json:"history_start,omitempty"`
	Messages     []Message      `json:"messages,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Usage        *TokenUsage    `json:"usage,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// A checkpoint fsyncs only changed messages/metadata. A snapshot is written
// periodically; old journal entries remain replay-safe until rotation succeeds.
type sessionJournal struct {
	mu          sync.Mutex
	path        string
	sequence    uint64
	records     int
	bytes       int64
	initialized bool
	legacy      bool
	failed      error
}

func checkedJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return json.Marshal(checkedRecord{Data: data, Checksum: hex.EncodeToString(digest[:])})
}

func decodeChecked(data []byte, value any) error {
	var record checkedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	digest := sha256.Sum256(record.Data)
	if len(record.Data) == 0 || record.Checksum != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("session checksum mismatch")
	}
	return json.Unmarshal(record.Data, value)
}

func (j *sessionJournal) load(id, agentID, taskID string) (*Session, error) {
	session := NewSession(id, agentID, taskID)
	data, err := os.ReadFile(j.path)
	if err == nil {
		var header map[string]json.RawMessage
		if err := json.Unmarshal(data, &header); err != nil {
			return nil, fmt.Errorf("load session snapshot: %w", err)
		}
		if _, ok := header["checksum"]; ok {
			var snapshot sessionSnapshot
			if err := decodeChecked(data, &snapshot); err != nil {
				return nil, fmt.Errorf("load session snapshot: %w", err)
			}
			if snapshot.Version != sessionFormatVersion || snapshot.Session == nil {
				return nil, fmt.Errorf("unsupported session snapshot")
			}
			session = snapshot.Session
			j.sequence = snapshot.Sequence
		} else {
			// Migrate the existing plain JSON snapshot on the next checkpoint.
			if err := json.Unmarshal(data, session); err != nil {
				return nil, fmt.Errorf("load legacy session: %w", err)
			}
			j.legacy = true
		}
		j.initialized = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if session.ID != id || session.AgentID != agentID || session.TaskID != taskID {
		return nil, fmt.Errorf("session identity mismatch")
	}
	if err := j.replay(session); err != nil {
		return nil, err
	}
	session.historyDirty = -1
	session.metadataDirty = nil
	session.usageDirty = false
	return session, nil
}

func readJournalLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxJournalRecordBytes {
			return nil, fmt.Errorf("session journal record exceeds 64 MiB")
		}
		line = append(line, part...)
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

func (j *sessionJournal) replay(session *Session) error {
	file, err := os.OpenFile(j.path+".jsonl", os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	var offset int64
	baseSequence := j.sequence
	var previousSequence uint64
	for {
		line, err := readJournalLine(reader)
		if err == io.EOF {
			// An interrupted append is not a committed record. Discard only the
			// unterminated suffix before a future append; intact records stay strict.
			if len(line) > 0 {
				if err := file.Truncate(offset); err != nil {
					return err
				}
				if err := file.Sync(); err != nil {
					return err
				}
			}
			break
		}
		if err != nil {
			return fmt.Errorf("read session journal: %w", err)
		}
		var delta sessionDelta
		if err := decodeChecked(bytes.TrimSuffix(line, []byte{'\n'}), &delta); err != nil {
			return fmt.Errorf("session journal at byte %d: %w", offset, err)
		}
		if delta.Version != sessionFormatVersion || delta.ID != session.ID || delta.AgentID != session.AgentID || delta.TaskID != session.TaskID {
			return fmt.Errorf("session journal identity/version mismatch")
		}
		if delta.Sequence == 0 || delta.Sequence <= previousSequence {
			return fmt.Errorf("session journal sequence is not increasing")
		}
		previousSequence = delta.Sequence
		if delta.Sequence > baseSequence {
			if delta.Sequence != j.sequence+1 {
				return fmt.Errorf("session journal sequence gap: got %d after %d", delta.Sequence, j.sequence)
			}
			if !j.initialized {
				return fmt.Errorf("session journal has no base snapshot")
			}
			if err := applyDelta(session, delta); err != nil {
				return err
			}
			j.sequence = delta.Sequence
			j.records++
		}
		offset += int64(len(line))
	}
	j.bytes = offset
	return nil
}

func applyDelta(session *Session, delta sessionDelta) error {
	if delta.HistoryStart != nil {
		start := *delta.HistoryStart
		if start < 0 || start > len(session.History) {
			return fmt.Errorf("invalid session history replacement offset %d", start)
		}
		session.History = append(session.History[:start], delta.Messages...)
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	for key, value := range delta.Metadata {
		session.Metadata[key] = value
	}
	if delta.Usage != nil {
		session.TokenUsage = *delta.Usage
	}
	session.UpdatedAt = delta.UpdatedAt
	return nil
}

func (j *sessionJournal) checkpoint(session *Session) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	session.mu.Lock()
	defer session.mu.Unlock()
	if j.failed != nil {
		return j.failed
	}
	if !j.initialized || j.legacy {
		if err := j.snapshot(session); err != nil {
			return err
		}
		j.initialized = true
		j.legacy = false
		session.clearDirty()
		return nil
	}
	if session.historyDirty >= 0 || len(session.metadataDirty) > 0 || session.usageDirty {
		delta := sessionDelta{Version: sessionFormatVersion, Sequence: j.sequence + 1, ID: session.ID, AgentID: session.AgentID, TaskID: session.TaskID, UpdatedAt: session.UpdatedAt}
		if session.historyDirty >= 0 {
			start := session.historyDirty
			delta.HistoryStart = &start
			delta.Messages = session.History[start:]
		}
		if len(session.metadataDirty) > 0 {
			delta.Metadata = make(map[string]any)
			for key := range session.metadataDirty {
				delta.Metadata[key] = session.Metadata[key]
			}
		}
		if session.usageDirty {
			usage := session.TokenUsage
			delta.Usage = &usage
		}
		data, err := checkedJSON(delta)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if len(data) > maxJournalRecordBytes {
			return fmt.Errorf("session journal record exceeds 64 MiB")
		}
		if err := j.append(data); err != nil {
			return err
		}
		j.sequence++
		j.records++
		j.bytes += int64(len(data))
		session.clearDirty()
	}
	if j.records >= snapshotEveryRecords || j.bytes >= snapshotEveryBytes {
		if err := j.snapshot(session); err != nil {
			return err
		}
		// Snapshot rename is durable before journal removal. If interrupted here,
		// replay skips entries already included in the snapshot's sequence.
		if err := atomicSessionFile(j.path+".jsonl", nil); err != nil {
			return err
		}
		j.records = 0
		j.bytes = 0
	}
	return nil
}

func (s *Session) clearDirty() { s.historyDirty = -1; s.metadataDirty = nil; s.usageDirty = false }

func (j *sessionJournal) snapshot(session *Session) error {
	data, err := checkedJSON(sessionSnapshot{Version: sessionFormatVersion, Sequence: j.sequence, Session: session})
	if err != nil {
		return err
	}
	return atomicSessionFile(j.path, data)
}

func (j *sessionJournal) append(data []byte) error {
	_, statErr := os.Stat(j.path + ".jsonl")
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return statErr
	}
	file, err := os.OpenFile(j.path+".jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		if rollback := file.Truncate(info.Size()); rollback != nil {
			j.failed = fmt.Errorf("session journal rollback failed: %w", rollback)
		} else if syncErr := file.Sync(); syncErr != nil {
			j.failed = fmt.Errorf("session journal rollback sync failed: %w", syncErr)
		}
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		j.failed = closeErr
		return closeErr
	}
	if created {
		if err := syncSessionDirectory(filepath.Dir(j.path)); err != nil {
			j.failed = err
			return err
		}
	}
	return nil
}

func atomicSessionFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncSessionDirectory(filepath.Dir(path))
}
