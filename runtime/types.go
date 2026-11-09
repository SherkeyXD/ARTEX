package runtime

import (
	"encoding/json"
	"time"
)

// Role defines the message participant role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartType defines the type of a message content part.
type PartType string

const (
	PartText       PartType = "text"
	PartThinking   PartType = "thinking"
	PartToolCall   PartType = "tool_call"
	PartToolResult PartType = "tool_result"
)

// ToolCallData represents a single tool invocation request from the model.
type ToolCallData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultData represents the result of a tool execution.
type ToolResultData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	CharCount  int    `json:"char_count,omitempty"`
}

// Part represents a polymorphic chunk of content inside a Message.
type Part struct {
	Type       PartType        `json:"type"`
	Text       string          `json:"text,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	Signature  string          `json:"signature,omitempty"`
	ToolCall   *ToolCallData   `json:"tool_call,omitempty"`
	ToolResult *ToolResultData `json:"tool_result,omitempty"`
}

// TextPart builds a text part.
func TextPart(text string) Part {
	return Part{Type: PartText, Text: text}
}

// ThinkingPart builds a thinking part.
func ThinkingPart(thinking string) Part {
	return Part{Type: PartThinking, Thinking: thinking}
}

// ToolCallPart builds a tool call part.
func ToolCallPart(id, name string, args json.RawMessage) Part {
	return Part{
		Type: PartToolCall,
		ToolCall: &ToolCallData{
			ID:        id,
			Name:      name,
			Arguments: args,
		},
	}
}

// ToolResultPart builds a tool result part.
func ToolResultPart(toolCallID, name, content string, isError bool, durationMs int64) Part {
	return Part{
		Type: PartToolResult,
		ToolResult: &ToolResultData{
			ToolCallID: toolCallID,
			ToolName:   name,
			Content:    content,
			IsError:    isError,
			DurationMs: durationMs,
			CharCount:  len(content),
		},
	}
}

// Message is the canonical message format in the universal agent runtime.
type Message struct {
	ID        string    `json:"id,omitempty"`
	Role      Role      `json:"role"`
	Parts     []Part    `json:"parts"`
	CreatedAt time.Time `json:"created_at"`
}

// Text returns the concatenated text content of the message.
func (m *Message) Text() string {
	var s string
	for _, p := range m.Parts {
		if p.Type == PartText {
			s += p.Text
		}
	}
	return s
}

// Thinking returns the concatenated thinking content of the message.
func (m *Message) Thinking() string {
	var s string
	for _, p := range m.Parts {
		if p.Type == PartThinking {
			s += p.Thinking
		}
	}
	return s
}

// ToolCalls returns all tool calls contained in the message.
func (m *Message) ToolCalls() []*ToolCallData {
	var calls []*ToolCallData
	for _, p := range m.Parts {
		if p.Type == PartToolCall && p.ToolCall != nil {
			calls = append(calls, p.ToolCall)
		}
	}
	return calls
}

// ToolResults returns all tool results contained in the message.
func (m *Message) ToolResults() []*ToolResultData {
	var res []*ToolResultData
	for _, p := range m.Parts {
		if p.Type == PartToolResult && p.ToolResult != nil {
			res = append(res, p.ToolResult)
		}
	}
	return res
}

// NewUserMessage creates a new user message.
func NewUserMessage(text string) Message {
	return Message{
		Role:      RoleUser,
		Parts:     []Part{TextPart(text)},
		CreatedAt: time.Now(),
	}
}

// NewAssistantMessage creates a new assistant message.
func NewAssistantMessage(parts ...Part) Message {
	return Message{
		Role:      RoleAssistant,
		Parts:     parts,
		CreatedAt: time.Now(),
	}
}

// NewToolResultMessage creates a new tool response message.
func NewToolResultMessage(toolCallID, name, content string, isError bool, durationMs int64) Message {
	return Message{
		Role:      RoleTool,
		Parts:     []Part{ToolResultPart(toolCallID, name, content, isError, durationMs)},
		CreatedAt: time.Now(),
	}
}

// TokenUsage holds token accounting for one turn or entire session.
type TokenUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// Add sums another token usage.
func (u *TokenUsage) Add(other TokenUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
}

// Total returns total tokens consumed (input + output).
func (u TokenUsage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// TerminalReason identifies the high-level cause of agent completion.
type TerminalReason string

const (
	ReasonCompleted         TerminalReason = "completed"
	ReasonMaxTurns          TerminalReason = "max_turns"
	ReasonTimeout           TerminalReason = "timeout"
	ReasonModelError        TerminalReason = "model_error"
	ReasonBlockingLimit     TerminalReason = "blocking_limit"
	ReasonPromptTooLong     TerminalReason = "prompt_too_long"
	ReasonImageError        TerminalReason = "image_error"
	ReasonStopHookPrevented TerminalReason = "stop_hook_prevented"
	ReasonHookStopped       TerminalReason = "hook_stopped"
	ReasonAbortedStreaming  TerminalReason = "aborted_streaming"
	ReasonAbortedTools      TerminalReason = "aborted_tools"
)

// StopReason describes why a turn or loop finished.
type StopReason string

const (
	StopReasonComplete    StopReason = "complete"
	StopReasonMaxSteps    StopReason = "max_steps"
	StopReasonCancelled   StopReason = "cancelled"
	StopReasonToolError   StopReason = "tool_error"
	StopReasonInterrupted StopReason = "interrupted"
	StopReasonError       StopReason = "error"
)

// TurnResult represents the output of a single Agent turn execution.
type TurnResult struct {
	TurnIndex    int               `json:"turn_index"`
	FinalText    string            `json:"final_text"`
	ThinkingText string            `json:"thinking_text,omitempty"`
	ToolCalls    []*ToolResultData `json:"tool_calls,omitempty"`
	TokenUsage   TokenUsage        `json:"token_usage"`
	Duration     time.Duration     `json:"duration"`
	TTFT         time.Duration     `json:"ttft"`
	StopReason   StopReason        `json:"stop_reason"`
	Reason       TerminalReason    `json:"reason,omitempty"`
	Iterations   int               `json:"iterations"`
	Error        error             `json:"error,omitempty"`
}
