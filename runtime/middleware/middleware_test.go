package middleware_test

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/Autumn-27/artex/runtime"
	"github.com/Autumn-27/artex/runtime/harness/pi"
	"github.com/Autumn-27/artex/runtime/middleware"
)

type mockLLMWithTool struct {
	round int
}

func (m *mockLLMWithTool) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	m.round++
	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		time.Sleep(2 * time.Millisecond)
		if m.round == 1 {
			if !yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventToolCall,
				ToolCall: &runtime.ToolCallData{
					ID:        "call_1",
					Name:      "rm_rf",
					Arguments: json.RawMessage(`{"path":"/etc"}`),
				},
			}, nil) {
				return
			}
		} else {
			yield(runtime.LLMStreamEvent{
				Type: runtime.LLMEventText,
				Text: "Action was blocked, safe state maintained.",
			}, nil)
		}
	}
}

func TestGuardMiddlewareBlocksDangerousTool(t *testing.T) {
	ctx := context.Background()
	driver := pi.NewDriver()

	// Guard that blocks rm_rf
	guard := middleware.NewGuardMiddleware(func(ctx context.Context, agentID, toolName string, args json.RawMessage) (bool, string, error) {
		if toolName == "rm_rf" {
			return false, "高危删除指令被安全策略拦截", nil
		}
		return true, "", nil
	})

	var recordedSteps []middleware.StepMetric
	metrics := middleware.NewMetricsRecorder(func(metric middleware.StepMetric) {
		recordedSteps = append(recordedSteps, metric)
	})

	chain := runtime.NewMiddlewareChain(guard, metrics)

	session := runtime.NewSession("sess-guard", "worker-1", "task-1")
	session.AppendMessage(runtime.NewUserMessage("Delete /etc please"))

	opts := runtime.TurnOptions{
		Provider: &mockLLMWithTool{},
		Tools: []runtime.Tool{
			runtime.NewFuncTool("rm_rf", "deletes path", json.RawMessage(`{}`), func(ctx context.Context, rawArgs json.RawMessage) (any, error) {
				t.Fatalf("rm_rf tool MUST NOT be executed!")
				return nil, nil
			}),
		},
		MaxIterations: 3,
	}

	result, err := driver.ExecuteTurn(ctx, session, opts, chain, nil)
	if err != nil {
		t.Fatalf("turn failed: %v", err)
	}

	if result.StopReason != runtime.StopReasonComplete {
		t.Fatalf("expected StopReasonComplete, got %s", result.StopReason)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 intercepted tool call, got %d", len(result.ToolCalls))
	}

	tc := result.ToolCalls[0]
	if !tc.IsError || tc.Content != "[拦截规则拦截] 高危删除指令被安全策略拦截" {
		t.Fatalf("unexpected intercepted message: %+v", tc)
	}

	if metrics.TTFT() <= 0 {
		t.Fatalf("expected positive TTFT recorded in metrics, got %v", metrics.TTFT())
	}
}
