package agent

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/Autumn-27/norma/llm"
)

type mockUniversalProvider struct{}

func (m *mockUniversalProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if !yield(llm.StreamEvent{
			Type: llm.SEThinkingDelta,
			Text: "Analyzing turn with Universal Agent Runtime...",
		}, nil) {
			return
		}
		if !yield(llm.StreamEvent{
			Type: llm.SETextDelta,
			Text: "Agent turn executed successfully.",
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

func (m *mockUniversalProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "", llm.Usage{}, nil
}

func (m *mockUniversalProvider) Model() string {
	return "mock-model"
}

func TestActiveHarnessDefaultAndFallback(t *testing.T) {
	// Default without env should be pi
	t.Setenv("ARTEX_HARNESS", "")
	if ActiveHarness() != HarnessPi {
		t.Fatalf("expected default harness to be %q, got %q", HarnessPi, ActiveHarness())
	}

	// Explicit pi
	t.Setenv("ARTEX_HARNESS", "pi")
	if ActiveHarness() != HarnessPi {
		t.Fatalf("expected harness to be %q, got %q", HarnessPi, ActiveHarness())
	}

	// Legacy norma fallback
	t.Setenv("ARTEX_HARNESS", "norma")
	if ActiveHarness() != HarnessNorma {
		t.Fatalf("expected harness to be %q, got %q", HarnessNorma, ActiveHarness())
	}

	// Legacy alias
	t.Setenv("ARTEX_HARNESS", "legacy")
	if ActiveHarness() != HarnessNorma {
		t.Fatalf("expected harness to be %q, got %q", HarnessNorma, ActiveHarness())
	}
}

func TestWorkerUniversalRunner(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")

	mockProv := &mockUniversalProvider{}
	worker := NewWorker(mockProv, "mock-model", t.TempDir(), 8000, 5)

	intent := &db.Node{
		ID:   101,
		Kind: "intent",
	}

	var emittedActivities []db.Activity
	emitWrap := func(a db.Activity) {
		emittedActivities = append(emittedActivities, a)
	}

	reason, err := worker.runWorkerWithUniversalRuntime(
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
		t.Fatalf("worker execute failed: %v", err)
	}

	if reason != runtime.ReasonCompleted {
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

func TestRunUniversalAgentPlanner(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")

	mockProv := &mockUniversalProvider{}
	var emittedActivities []db.Activity
	emitWrap := func(a db.Activity) {
		emittedActivities = append(emittedActivities, a)
	}

	output, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{
		AgentKey:        "planner",
		SessionID:       "planner-1",
		TaskID:          1,
		Model:           "mock-model",
		Provider:        mockProv,
		SystemPrompt:    "You are a planner.",
		Input:           "Plan next steps",
		MaxTurns:        3,
		MaxTokens:       1000,
		ToolOutputLimit: 4000,
		WorkingDir:      t.TempDir(),
		Emit:            emitWrap,
	})
	if err != nil {
		t.Fatalf("RunUniversalAgent planner turn failed: %v", err)
	}

	if reason != runtime.ReasonCompleted {
		t.Fatalf("expected ReasonCompleted, got: %v", reason)
	}
	if output != "Agent turn executed successfully." {
		t.Fatalf("unexpected output: %s", output)
	}

	if len(emittedActivities) == 0 {
		t.Fatalf("expected emitted activities")
	}
}

func TestChatAgentWithUniversalRuntime(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")

	mockProv := &mockUniversalProvider{}
	chatAgent := NewChatAgent(mockProv, "mock-model", t.TempDir(), 8000)

	var emittedActivities []db.Activity
	emitWrap := func(a db.Activity) {
		emittedActivities = append(emittedActivities, a)
	}

	resp, err := chatAgent.Chat(
		context.Background(),
		"chatagent",
		"session-test-1",
		"Hello agent",
		3,
		10*time.Second,
		false,
		emitWrap,
	)
	if err != nil {
		t.Fatalf("chatAgent.Chat with universal runtime failed: %v", err)
	}

	if resp != "Agent turn executed successfully." {
		t.Fatalf("expected output 'Agent turn executed successfully.', got %q", resp)
	}
}

func TestSeedGoalsWithUniversalRuntime(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")

	mockProv := &mockUniversalProvider{}
	var emittedActivities []db.Activity
	emitWrap := func(a db.Activity) {
		emittedActivities = append(emittedActivities, a)
	}

	// DecomposeGoalsWithProvider with nil stores
	_ = DecomposeGoalsWithProvider(
		context.Background(),
		mockProv,
		t.TempDir(),
		"Test goal: discover all subdomains",
		"In-scope: example.com",
		nil,
		nil,
		1,
		false,
		1000,
		emitWrap,
	)

	if len(emittedActivities) == 0 {
		t.Fatalf("expected emitted activities during goal seeding")
	}
}
