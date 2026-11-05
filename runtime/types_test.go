package runtime_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Autumn-27/artex/runtime"
)

func TestCanonicalMessage(t *testing.T) {
	msg := runtime.NewAssistantMessage(
		runtime.ThinkingPart("Let me plan the penetration test."),
		runtime.TextPart("Starting recon on target."),
		runtime.ToolCallPart("call-1", "nmap", json.RawMessage(`{"target":"127.0.0.1"}`)),
	)

	if msg.Role != runtime.RoleAssistant {
		t.Fatalf("expected role assistant, got %s", msg.Role)
	}

	if msg.Thinking() != "Let me plan the penetration test." {
		t.Fatalf("unexpected thinking: %s", msg.Thinking())
	}

	if msg.Text() != "Starting recon on target." {
		t.Fatalf("unexpected text: %s", msg.Text())
	}

	calls := msg.ToolCalls()
	if len(calls) != 1 || calls[0].Name != "nmap" {
		t.Fatalf("expected 1 tool call 'nmap', got %+v", calls)
	}
}

func TestTokenUsage(t *testing.T) {
	var u1 runtime.TokenUsage
	u1.Add(runtime.TokenUsage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 20})
	u1.Add(runtime.TokenUsage{InputTokens: 200, OutputTokens: 100, CacheWriteTokens: 30})

	if u1.InputTokens != 300 || u1.OutputTokens != 150 {
		t.Fatalf("unexpected token sum: %+v", u1)
	}
	if u1.Total() != 450 {
		t.Fatalf("unexpected total tokens: %d", u1.Total())
	}
}

func TestSessionSlidingWindow(t *testing.T) {
	s := runtime.NewSession("sess-1", "worker-1", "task-100")
	for i := 0; i < 10; i++ {
		s.AppendMessage(runtime.NewUserMessage("hello"))
	}

	if s.Len() != 10 {
		t.Fatalf("expected 10 messages, got %d", s.Len())
	}

	s.TruncateHead(4)
	if s.Len() != 4 {
		t.Fatalf("expected 4 messages after truncate, got %d", s.Len())
	}
}

func TestToolRegistry(t *testing.T) {
	reg := runtime.NewToolRegistry()
	tool := runtime.NewFuncTool("echo", "echoes input", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, rawArgs json.RawMessage) (any, error) {
		return string(rawArgs), nil
	})

	reg.Register(tool)

	got, ok := reg.Get("echo")
	if !ok || got.Name() != "echo" {
		t.Fatalf("failed to retrieve tool echo")
	}

	if len(reg.Names()) != 1 || reg.Names()[0] != "echo" {
		t.Fatalf("unexpected names: %+v", reg.Names())
	}
}
