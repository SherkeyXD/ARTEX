package runtime

import (
	"context"
	"encoding/json"
	"iter"
)

// LLMStreamEventType defines events emitted by the LLM stream.
type LLMStreamEventType string

const (
	LLMEventText              LLMStreamEventType = "text"
	LLMEventThinking          LLMStreamEventType = "thinking"
	LLMEventThinkingSignature LLMStreamEventType = "thinking_signature"
	LLMEventToolCall          LLMStreamEventType = "tool_call"
	LLMEventUsage             LLMStreamEventType = "usage"
	LLMEventFinish            LLMStreamEventType = "finish"
)

// LLMStreamEvent is a single chunk/event emitted by an LLMProvider during streaming.
type LLMStreamEvent struct {
	Type         LLMStreamEventType
	Text         string
	Thinking     string
	Signature    string
	ToolCall     *ToolCallData
	Usage        *TokenUsage
	FinishReason string
}

// ToolDefinition is passed to the LLM for function calling.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// CompletionRequest is the universal model request format.
type CompletionRequest struct {
	SystemPrompt    string           `json:"system_prompt,omitempty"`
	SystemPrompts   []string         `json:"system_prompts,omitempty"`
	DynamicBoundary int              `json:"dynamic_boundary,omitempty"`
	NonStreaming    bool             `json:"non_streaming,omitempty"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	Temperature     float64          `json:"temperature,omitempty"`
	MaxTokens       int              `json:"max_tokens,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	ThinkingType    string           `json:"thinking_type,omitempty"`
	Model           string           `json:"model,omitempty"`
}

// LLMProvider is the universal LLM provider interface.
type LLMProvider interface {
	// Stream generates a stream of events from the model using Go 1.23+ iter.Seq2.
	Stream(ctx context.Context, req CompletionRequest) iter.Seq2[LLMStreamEvent, error]
}
