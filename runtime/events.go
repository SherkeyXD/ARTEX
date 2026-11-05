package runtime

import (
	"context"
	"time"
)

// EventType defines the kind of runtime event emitted during execution.
type EventType string

const (
	EventTurnStart        EventType = "turn_start"
	EventModelCallStart   EventType = "model_call_start"
	EventFirstToken       EventType = "first_token"
	EventThinkingChunk    EventType = "thinking_chunk"
	EventTextChunk        EventType = "text_chunk"
	EventToolCall         EventType = "tool_call"
	EventToolStart        EventType = "tool_start"
	EventToolEnd          EventType = "tool_end"
	EventTurnEnd          EventType = "turn_end"
	EventError            EventType = "error"
)

// Event is the event structure emitted by the agent runtime and harness drivers.
type Event struct {
	Type        EventType      `json:"type"`
	Timestamp   time.Time      `json:"timestamp"`
	SessionID   string         `json:"session_id,omitempty"`
	AgentID     string         `json:"agent_id,omitempty"`
	TurnIndex   int            `json:"turn_index,omitempty"`
	Text        string         `json:"text,omitempty"`
	Thinking    string         `json:"thinking,omitempty"`
	ToolCall    *ToolCallData  `json:"tool_call,omitempty"`
	ToolResult  *ToolResultData`json:"tool_result,omitempty"`
	TokenUsage  *TokenUsage    `json:"token_usage,omitempty"`
	DurationMs  int64          `json:"duration_ms,omitempty"`
	TTFTMs      int64          `json:"ttft_ms,omitempty"`
	Error       string         `json:"error,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// EventSink consumes events from runtime execution.
type EventSink interface {
	Emit(ctx context.Context, ev Event)
}

// EventSinkFunc converts a function into an EventSink.
type EventSinkFunc func(ctx context.Context, ev Event)

func (fn EventSinkFunc) Emit(ctx context.Context, ev Event) {
	if fn != nil {
		fn(ctx, ev)
	}
}

// MultiEventSink broadcasts each event to multiple downstream sinks.
type MultiEventSink struct {
	sinks []EventSink
}

// NewMultiEventSink constructs a multiplexing sink.
func NewMultiEventSink(sinks ...EventSink) *MultiEventSink {
	return &MultiEventSink{sinks: sinks}
}

// Add adds another sink.
func (m *MultiEventSink) Add(s ...EventSink) {
	m.sinks = append(m.sinks, s...)
}

// Emit broadcasts the event.
func (m *MultiEventSink) Emit(ctx context.Context, ev Event) {
	for _, s := range m.sinks {
		if s != nil {
			s.Emit(ctx, ev)
		}
	}
}

// ChannelEventSink sends events to a Go channel.
type ChannelEventSink struct {
	ch chan<- Event
}

// NewChannelEventSink creates a channel sink.
func NewChannelEventSink(ch chan<- Event) *ChannelEventSink {
	return &ChannelEventSink{ch: ch}
}

func (c *ChannelEventSink) Emit(ctx context.Context, ev Event) {
	select {
	case <-ctx.Done():
	case c.ch <- ev:
	}
}
