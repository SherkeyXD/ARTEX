package pi_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/harness/pi"
)

type loopProvider struct {
	calls  int
	stopAt int
	cancel context.CancelFunc
}

func (p *loopProvider) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		p.calls++
		if p.cancel != nil && p.calls == 2 {
			p.cancel()
			yield(runtime.LLMStreamEvent{}, ctx.Err())
			return
		}
		if p.stopAt > 0 && p.calls == p.stopAt {
			yield(runtime.LLMStreamEvent{Type: runtime.LLMEventText, Text: "done"}, nil)
			return
		}
		yield(runtime.LLMStreamEvent{Type: runtime.LLMEventToolCall, ToolCall: &runtime.ToolCallData{ID: "call", Name: "echo", Arguments: json.RawMessage(`{}`)}}, nil)
	}
}

func budgetTools() []runtime.Tool {
	return []runtime.Tool{runtime.NewFuncTool("echo", "echo", nil, func(context.Context, json.RawMessage) (any, error) { return "echo", nil })}
}

func TestZeroIterationBudgetIsUnlimited(t *testing.T) {
	provider := &loopProvider{stopAt: 18}
	result, err := pi.NewDriver().ExecuteTurn(context.Background(), runtime.NewSession("s", "a", "t"), runtime.TurnOptions{Provider: provider, Tools: budgetTools()}, nil, nil)
	if err != nil || result.Reason != runtime.ReasonCompleted || provider.calls != 18 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls)
	}
}

func TestSettlementHasSeparateFiniteIterationBudget(t *testing.T) {
	provider := &loopProvider{}
	result, err := pi.NewDriver().ExecuteTurn(context.Background(), runtime.NewSession("s", "a", "t"), runtime.TurnOptions{Provider: provider, Tools: budgetTools(), MaxIterations: 1, Settlement: &runtime.Settlement{Prompt: "settle", MaxTurns: 2}}, nil, nil)
	if err != nil || result.Reason != runtime.ReasonMaxTurns || provider.calls != 3 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls)
	}
}

func TestCallerCanCancelSettlement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &loopProvider{cancel: cancel}
	result, err := pi.NewDriver().ExecuteTurn(ctx, runtime.NewSession("s", "a", "t"), runtime.TurnOptions{Provider: provider, Tools: budgetTools(), MaxIterations: 1, Settlement: &runtime.Settlement{Prompt: "settle", MaxTurns: 10, MaxDuration: time.Second}}, nil, nil)
	if !errors.Is(err, context.Canceled) || result.Reason != runtime.ReasonAbortedStreaming || provider.calls != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, provider.calls)
	}
}
