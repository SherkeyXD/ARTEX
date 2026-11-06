package runtime

import (
	"sync"
	"time"
)

// Session represents an ongoing agent interaction context.
// Inspired by Google ADK's Session state machine.
type Session struct {
	mu           sync.RWMutex
	ID           string         `json:"id"`
	AgentID      string         `json:"agent_id"`
	TaskID       string         `json:"task_id,omitempty"`
	History      []Message      `json:"history"`
	Metadata     map[string]any `json:"metadata"`
	TokenUsage   TokenUsage     `json:"token_usage"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// NewSession creates an empty initialized session.
func NewSession(id, agentID, taskID string) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		AgentID:   agentID,
		TaskID:    taskID,
		History:   make([]Message, 0),
		Metadata:  make(map[string]any),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AppendMessage appends one or more messages to the conversation history.
func (s *Session) AppendMessage(msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, msgs...)
	s.UpdatedAt = time.Now()
}

// Messages returns a copy of the current message history.
func (s *Session) Messages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Message, len(s.History))
	copy(res, s.History)
	return res
}

// Len returns the number of messages in the session.
func (s *Session) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.History)
}

// RecordUsage accumulates token usage into the session.
func (s *Session) RecordUsage(usage TokenUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TokenUsage.Add(usage)
	s.UpdatedAt = time.Now()
}

// Usage returns the cumulative token usage.
func (s *Session) Usage() TokenUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TokenUsage
}

// SetMetadata sets a key-value in session metadata.
func (s *Session) SetMetadata(key string, val any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	s.Metadata[key] = val
	s.UpdatedAt = time.Now()
}

// GetMetadata retrieves a metadata value.
func (s *Session) GetMetadata(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Metadata == nil {
		return nil, false
	}
	v, ok := s.Metadata[key]
	return v, ok
}

// TruncateHead keeps only the last keepCount messages (sliding window context management).
func (s *Session) TruncateHead(keepCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if keepCount <= 0 {
		s.History = make([]Message, 0)
		return
	}
	if len(s.History) > keepCount {
		s.History = s.History[len(s.History)-keepCount:]
		s.UpdatedAt = time.Now()
	}
}
