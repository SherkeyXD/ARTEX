package pi_test

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/harness/pi"
)

type mockLLMProvider struct {
	round int
}

func (m *mockLLMProvider) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	m.round++
	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		time.Sleep(5 * time.Millisecond) // simulate network TTFT

		if m.round == 1 {
			// Round 1: Model thinks, then calls "port_scan" tool
			if !yield(runtime.LLMStreamEvent{
				Type:     runtime.LLMEventThinking,
				Thinking: "Thinking about reconnaissance on 127.0.0.1...",
			}, nil) {
				return
			}

			if !yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventToolCall,
				ToolCall: &runtime.ToolCallData{
					ID:        "call_123",
					Name:      "port_scan",
					Arguments: json.RawMessage(`{"host":"127.0.0.1","port":80}`),
				},
			}, nil) {
				return
			}

			yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventUsage,
				Usage: &runtime.TokenUsage{
					InputTokens:  120,
					OutputTokens: 45,
				},
			}, nil)
		} else {
			// Round 2: Model receives tool result and produces final answer
			if !yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventText,
				Text: "Target 127.0.0.1 has port 80 OPEN (HTTP nginx).",
			}, nil) {
				return
			}

			yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventUsage,
				Usage: &runtime.TokenUsage{
					InputTokens:  180,
					OutputTokens: 25,
				},
			}, nil)
		}
	}
}

func TestPiDriverExecutionLoop(t *testing.T) {
	ctx := context.Background()
	driver := pi.NewDriver()

	session := runtime.NewSession("test-sess", "worker-recon", "task-1")
	session.AppendMessage(runtime.NewUserMessage("Please scan 127.0.0.1"))

	mockProvider := &mockLLMProvider{}

	scanTool := runtime.NewFuncTool("port_scan", "scans a port", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, rawArgs json.RawMessage) (any, error) {
		time.Sleep(10 * time.Millisecond) // simulate tool runtime
		return map[string]any{"status": "open", "service": "http"}, nil
	})

	var emittedEvents []runtime.Event
	sink := runtime.EventSinkFunc(func(ctx context.Context, ev runtime.Event) {
		emittedEvents = append(emittedEvents, ev)
	})

	opts := runtime.TurnOptions{
		SystemPrompt:    "You are a pentest agent.",
		Tools:           []runtime.Tool{scanTool},
		Provider:        mockProvider,
		MaxIterations:   5,
		ToolOutputLimit: 1000,
	}

	result, err := driver.ExecuteTurn(ctx, session, opts, runtime.NewMiddlewareChain(), sink)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	// 1. Verify StopReason and final text
	if result.StopReason != runtime.StopReasonComplete {
		t.Fatalf("expected StopReasonComplete, got %s", result.StopReason)
	}

	if result.FinalText != "Target 127.0.0.1 has port 80 OPEN (HTTP nginx)." {
		t.Fatalf("unexpected final text: %s", result.FinalText)
	}

	// 2. Verify thinking
	if result.ThinkingText != "Thinking about reconnaissance on 127.0.0.1..." {
		t.Fatalf("unexpected thinking text: %s", result.ThinkingText)
	}

	// 3. Verify tool call result
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ToolName != "port_scan" || tc.IsError {
		t.Fatalf("unexpected tool call result: %+v", tc)
	}
	if tc.DurationMs <= 0 {
		t.Fatalf("expected positive duration_ms, got %d", tc.DurationMs)
	}

	// 4. Verify TTFT
	if result.TTFT <= 0 {
		t.Fatalf("expected positive TTFT, got %v", result.TTFT)
	}

	// 5. Verify Token Accounting
	if result.TokenUsage.InputTokens != 300 || result.TokenUsage.OutputTokens != 70 {
		t.Fatalf("unexpected token usage: %+v", result.TokenUsage)
	}

	// 6. Verify Session History structure
	// Expected: [UserMessage, AssistantMessage(Thinking+ToolCall), ToolResultMessage, AssistantMessage(Text)]
	msgs := session.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages in session history, got %d", len(msgs))
	}
	if msgs[0].Role != runtime.RoleUser {
		t.Fatalf("expected msg[0] RoleUser, got %s", msgs[0].Role)
	}
	if msgs[1].Role != runtime.RoleAssistant || len(msgs[1].ToolCalls()) != 1 {
		t.Fatalf("expected msg[1] RoleAssistant with 1 tool call, got %+v", msgs[1])
	}
	if msgs[2].Role != runtime.RoleTool {
		t.Fatalf("expected msg[2] RoleTool, got %s", msgs[2].Role)
	}
	if msgs[3].Role != runtime.RoleAssistant || msgs[3].Text() == "" {
		t.Fatalf("expected msg[3] RoleAssistant with final text, got %+v", msgs[3])
	}

	// 7. Verify Events emitted
	var hasFirstToken, hasToolStart, hasToolEnd bool
	for _, ev := range emittedEvents {
		if ev.Type == runtime.EventFirstToken {
			hasFirstToken = true
		}
		if ev.Type == runtime.EventToolStart {
			hasToolStart = true
		}
		if ev.Type == runtime.EventToolEnd {
			hasToolEnd = true
		}
	}
	if !hasFirstToken || !hasToolStart || !hasToolEnd {
		t.Fatalf("missing expected events in stream: firstToken=%v, toolStart=%v, toolEnd=%v",
			hasFirstToken, hasToolStart, hasToolEnd)
	}
}

func TestPiDriverToolOutputSanitization(t *testing.T) {
	ctx := context.Background()
	driver := pi.NewDriver()

	session := runtime.NewSession("test-sanitizer-sess", "worker-recon", "task-2")
	session.AppendMessage(runtime.NewUserMessage("Run recon tool"))

	mockProvider := &mockLLMProvider{}

	toolWithColorsAndProgress := runtime.NewFuncTool("port_scan", "runs scan", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, rawArgs json.RawMessage) (any, error) {
		return "\x1b[31;1m[CRITICAL]\x1b[0m \x1b[32m[CVE-2024-9999]\x1b[0m Found vulnerability\r\nScanning: 50%\rScanning: 100%\nScan complete.\n", nil
	})

	opts := runtime.TurnOptions{
		SystemPrompt:    "You are a pentest agent.",
		Tools:           []runtime.Tool{toolWithColorsAndProgress},
		Provider:        mockProvider,
		MaxIterations:   5,
		ToolOutputLimit: 1000,
	}

	result, err := driver.ExecuteTurn(ctx, session, opts, runtime.NewMiddlewareChain(), nil)
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call result, got %d", len(result.ToolCalls))
	}

	tcResult := result.ToolCalls[0].Content
	expected := "[CRITICAL] [CVE-2024-9999] Found vulnerability\nScanning: 100%\nScan complete.\n"
	if tcResult != expected {
		t.Fatalf("expected sanitized output:\n%q\ngot:\n%q", expected, tcResult)
	}
}
