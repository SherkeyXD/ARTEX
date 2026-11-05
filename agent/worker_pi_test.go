package agent

import (
	"context"
	"iter"
	"testing"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

type mockNormaProvider struct{}

func (m *mockNormaProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if !yield(llm.StreamEvent{
			Type: llm.SEThinkingDelta,
			Text: "Analyzing intent with Pi harness...",
		}, nil) {
			return
		}
		if !yield(llm.StreamEvent{
			Type: llm.SETextDelta,
			Text: "Worker finished intent execution successfully.",
		}, nil) {
			return
		}
		if !yield(llm.StreamEvent{
			Type: llm.SEMessageDelta,
			Usage: llm.Usage{
				InputTokens:  150,
				OutputTokens: 30,
			},
		}, nil) {
			return
		}
		yield(llm.StreamEvent{
			Type: llm.SEMessageStop,
		}, nil)
	}
}

func (m *mockNormaProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "", llm.Usage{}, nil
}

func (m *mockNormaProvider) Model() string {
	return "mock-model"
}

func TestWorkerPiHarnessDualTrack(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")

	if ActiveHarness() != HarnessPi {
		t.Fatalf("expected ActiveHarness to be pi")
	}

	mockProv := &mockNormaProvider{}
	worker := NewWorker(mockProv, "mock-model", t.TempDir(), 8000, 5)

	intent := &db.Node{
		ID:   101,
		Kind: "intent",
	}

	var emittedActivities []db.Activity
	emitWrap := func(a db.Activity) {
		emittedActivities = append(emittedActivities, a)
	}

	reason, err := worker.runWorkerWithPi(
		context.Background(),
		"worker-1",
		1,
		nil,
		intent,
		emitWrap,
		"Execute recon intent",
		nil,
		"You are a pentest worker.",
	)
	if err != nil {
		t.Fatalf("worker execute with pi harness failed: %v", err)
	}

	if reason != harness.ReasonCompleted {
		t.Fatalf("expected ReasonCompleted, got: %v", reason)
	}

	var hasThinking, hasText bool
	for _, act := range emittedActivities {
		if act.Kind == "thinking" {
			hasThinking = true
		}
		if act.Kind == "text" {
			hasText = true
		}
	}

	if !hasThinking || !hasText {
		t.Fatalf("expected thinking and text activities to be emitted, got: %+v", emittedActivities)
	}
}
