package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	actool "github.com/Autumn-27/norma/tool"
	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
)

type behaviorProvider struct {
	complete func(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error)
	stream   func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]
}

func (p *behaviorProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return p.complete(ctx, req)
}
func (p *behaviorProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	if p.stream != nil {
		return p.stream(ctx, req)
	}
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, errors.New("unexpected streaming request"))
	}
}
func behaviorMessage(blocks ...llm.ContentBlock) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: blocks}
}
func behaviorCall(id, name, input string) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}
}

func TestUniversalRuntimeResumesChatAndBindsSession(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")
	dir := t.TempDir()
	count := 0
	provider := &behaviorProvider{complete: func(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		id, _ := llm.SessionIDFrom(ctx)
		if id != "conversation" {
			t.Fatalf("session context = %q", id)
		}
		want := 1
		if count == 1 {
			want = 3
		}
		if len(req.Messages) != want {
			t.Fatalf("call %d has %d messages, want %d", count, len(req.Messages), want)
		}
		if count == 1 && (req.Messages[0].Text() != "remember this" || req.Messages[1].Text() != "remembered") {
			t.Fatalf("history lost: %+v", req.Messages)
		}
		count++
		return behaviorMessage(llm.TextBlock("remembered")), "stop", llm.Usage{InputTokens: 10, OutputTokens: 2}, nil
	}}
	// Reconstructing the ChatAgent exercises persistence rather than instance memory.
	var results []db.Activity
	for i, key := range []string{"chat", "chat", "other-agent"} {
		chat := NewChatAgent(provider, "mock", dir, 64000)
		chat.SetNonStreaming(func() bool { return true })
		input := "remember this"
		if i == 1 {
			input = "what did I say?"
		}
		if _, err := chat.Chat(context.Background(), key, "conversation", input, 3, time.Second, false, func(activity db.Activity) {
			if activity.Kind == "result" {
				results = append(results, activity)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range results {
		if result.InputTokens == nil || *result.InputTokens != 10 {
			t.Fatalf("resumed run double-counted usage: %+v", result)
		}
	}
}

func TestUniversalRuntimeLegacyToolsUseWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("isolated content"), 0o600); err != nil {
		t.Fatal(err)
	}
	count := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		count++
		if count == 1 {
			return behaviorMessage(behaviorCall("read", "Read", `{"file_path":"input.txt"}`), behaviorCall("cwd", "Bash", `{"command":"pwd"}`)), "tool_use", llm.Usage{}, nil
		}
		var read, cwd string
		for _, message := range req.Messages {
			for _, block := range message.Content {
				if block.Type == llm.BlockToolResult {
					for _, content := range block.Content {
						if block.ToolUseID == "read" {
							read += content.Text
						}
						if block.ToolUseID == "cwd" {
							cwd += content.Text
						}
					}
				}
			}
		}
		if !strings.Contains(read, "isolated content") || strings.TrimSpace(cwd) != dir {
			t.Fatalf("Read=%q, Bash cwd=%q, want %q", read, cwd, dir)
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "tools", WorkingDir: dir, Provider: provider, Input: "read", Tools: []actool.CoreTool{actool.NewRead(), actool.NewBash()}, MaxTurns: 3, NonStreaming: true})
	if err != nil || reason != runtime.ReasonCompleted {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
}

func TestUniversalRuntimeStepBudgetSettlesWithRestrictedTools(t *testing.T) {
	dir := t.TempDir()
	persisted, forbidden, calls := 0, 0, 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(behaviorCall("first", "probe", `{}`)), "tool_use", llm.Usage{}, nil
		}
		if calls == 2 {
			if req.Messages[len(req.Messages)-1].Text() != "persist discovered facts" {
				t.Fatal("missing settlement prompt")
			}
			for _, tool := range req.Tools {
				if tool.Name == "Bash" {
					t.Fatal("Bash advertised during settlement")
				}
			}
			return behaviorMessage(behaviorCall("forbidden", "Bash", `{}`), behaviorCall("save", "persist", `{}`)), "tool_use", llm.Usage{}, nil
		}
		return behaviorMessage(llm.TextBlock("facts saved")), "stop", llm.Usage{}, nil
	}}
	tools := []runtime.Tool{
		runtime.NewFuncTool("probe", "probe", nil, func(context.Context, json.RawMessage) (any, error) { return "discovered", nil }),
		runtime.NewFuncTool("persist", "persist", nil, func(context.Context, json.RawMessage) (any, error) { persisted++; return "saved", nil }),
		runtime.NewFuncTool("Bash", "shell", nil, func(context.Context, json.RawMessage) (any, error) { forbidden++; return "bad", nil }),
	}
	text, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "settle", WorkingDir: dir, Provider: provider, Input: "probe", UniversalTools: tools, MaxTurns: 1, NonStreaming: true, Settlement: &runtime.Settlement{Prompt: "persist discovered facts", DisabledTools: []string{"Bash"}, MaxTurns: 3}})
	if err != nil || reason != runtime.ReasonMaxTurns || text != "facts saved" || persisted != 1 || forbidden != 0 {
		t.Fatalf("text=%q reason=%s err=%v persisted=%d forbidden=%d", text, reason, err, persisted, forbidden)
	}
}

func TestUniversalRuntimeTimeoutInterruptsToolThenSettles(t *testing.T) {
	calls := 0
	provider := &behaviorProvider{complete: func(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(behaviorCall("slow", "slow", `{}`)), "tool_use", llm.Usage{}, nil
		}
		if ctx.Err() != nil || req.Messages[len(req.Messages)-1].Text() != "task deadline reached" {
			t.Fatalf("settlement context/prompt: %v %+v", ctx.Err(), req.Messages)
		}
		return behaviorMessage(llm.TextBlock("settled")), "stop", llm.Usage{}, nil
	}}
	tool := runtime.NewFuncTool("slow", "slow", nil, func(ctx context.Context, _ json.RawMessage) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	text, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "timeout", WorkingDir: t.TempDir(), Provider: provider, Input: "work", UniversalTools: []runtime.Tool{tool}, NonStreaming: true, MaxDuration: 30 * time.Millisecond, Settlement: &runtime.Settlement{Prompt: "ordinary wrapup", PromptByReason: map[runtime.TerminalReason]string{runtime.ReasonTimeout: "task deadline reached"}, MaxTurns: 2}})
	if err != nil || reason != runtime.ReasonTimeout || text != "settled" || calls != 2 {
		t.Fatalf("text=%q reason=%s err=%v calls=%d", text, reason, err, calls)
	}
}

func TestUniversalRuntimeCancellationPreservesStageOutputAndUsage(t *testing.T) {
	for _, toolStage := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "tool"}[toolStage], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var activities []db.Activity
			provider := &behaviorProvider{stream: func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
				return func(yield func(llm.StreamEvent, error) bool) {
					if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "partial answer"}, nil) {
						return
					}
					if !yield(llm.StreamEvent{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: 7}}, nil) {
						return
					}
					if toolStage {
						if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: "cancel", ToolName: "cancel"}, nil) {
							return
						}
						if !yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: `{}`}, nil) {
							return
						}
						yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
					} else {
						cancel()
						yield(llm.StreamEvent{}, ctx.Err())
					}
				}
			}}
			tool := runtime.NewFuncTool("cancel", "cancel", nil, func(context.Context, json.RawMessage) (any, error) { cancel(); return nil, ctx.Err() })
			text, reason, err := RunUniversalAgent(ctx, UniversalAgentOptions{AgentKey: "worker", SessionID: "cancel", WorkingDir: t.TempDir(), Provider: provider, Input: "work", UniversalTools: []runtime.Tool{tool}, MaxTurns: 2, Settlement: &runtime.Settlement{Prompt: "must not settle"}, Emit: func(activity db.Activity) { activities = append(activities, activity) }})
			want := runtime.ReasonAbortedStreaming
			if toolStage {
				want = runtime.ReasonAbortedTools
			}
			if !errors.Is(err, context.Canceled) || reason != want || text != "partial answer" {
				t.Fatalf("text=%q reason=%s err=%v", text, reason, err)
			}
			last := activities[len(activities)-1]
			if last.Kind != "result" || last.InputTokens == nil || *last.InputTokens != 7 || !strings.Contains(last.Detail, "partial answer") {
				t.Fatalf("terminal activity lost: %+v", last)
			}
		})
	}
}

func TestUniversalRuntimeMainSegmentsHaveSeparateHistory(t *testing.T) {
	t.Setenv("ARTEX_HARNESS", "pi")
	count := 0
	provider := &behaviorProvider{complete: func(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		want := 1
		if count == 1 {
			want = 3
		}
		if len(req.Messages) != want {
			t.Fatalf("main segment %d has %d messages, want %d", count, len(req.Messages), want)
		}
		count++
		return behaviorMessage(llm.TextBlock("main answer")), "stop", llm.Usage{}, nil
	}}
	main := NewMainAgent(provider, "mock", t.TempDir(), 64000, 3)
	main.SetNonStreaming(func() bool { return true })
	for _, segment := range []int{0, 0, 1} {
		if _, err := main.Chat(context.Background(), 1, segment, nil, nil, "goal", "message", nil, nil, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUniversalRuntimePreservesSpilledOutputAndToolIdentity(t *testing.T) {
	dir := t.TempDir()
	full := strings.Repeat("tool-output\n", 100)
	count := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		count++
		if count == 1 {
			return behaviorMessage(behaviorCall("capture-id", "capture", `{}`)), "tool_use", llm.Usage{}, nil
		}
		output := req.Messages[len(req.Messages)-1].Content[0].Content[0].Text
		if !strings.Contains(output, "<persisted-output>") || !strings.Contains(output, "cmd-output") {
			t.Fatalf("spill pointer truncated: %q", output)
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	tool := actool.Build(actool.Spec{Name: "capture", Schema: map[string]any{"type": "object"}, Run: func(_ context.Context, _ json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
		if tc == nil || tc.ToolUseID != "capture-id" || tc.AgentID != "worker" || tc.WorkingDir != dir {
			t.Fatalf("tool context lost: %+v", tc)
		}
		return actool.Text(actool.Capture(tc, full)), nil
	}})
	_, _, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "spill", WorkingDir: dir, Provider: provider, Input: "read", Tools: []actool.CoreTool{tool}, NonStreaming: true, ToolOutputLimit: 80, MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "cmd-output", "*.txt"))
	if len(files) != 1 {
		t.Fatalf("spill files=%v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || string(data) != full {
		t.Fatal("full output was not preserved")
	}
}

func TestUniversalRuntimeThinkingSignatureSurvivesToolRound(t *testing.T) {
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(llm.ContentBlock{Type: llm.BlockThinking, Thinking: "reasoning", Signature: "signed-reasoning"}, behaviorCall("call", "echo", `{}`)), "tool_use", llm.Usage{}, nil
		}
		if got := req.Messages[1].Content[0].Signature; got != "signed-reasoning" {
			t.Fatalf("thinking signature=%q", got)
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	tool := runtime.NewFuncTool("echo", "echo", nil, func(context.Context, json.RawMessage) (any, error) { return "echo", nil })
	_, _, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "chat", SessionID: "thinking", WorkingDir: t.TempDir(), Provider: provider, Input: "hello", UniversalTools: []runtime.Tool{tool}, NonStreaming: true, MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUniversalRuntimeReactiveCompactionRetriesOnce(t *testing.T) {
	dir := t.TempDir()
	session, release, err := runtime.OpenSession(context.Background(), dir, "compact", "worker", "0")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		session.AppendMessage(runtime.NewUserMessage(strings.Repeat("historical fact ", 60)), runtime.NewAssistantMessage(runtime.TextPart("fact recorded")))
	}
	if err := session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	release()
	mainCalls, summaryCalls, originalSize := 0, 0, 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		if req.Thinking == "disabled" {
			summaryCalls++
			return behaviorMessage(llm.TextBlock("prior goals and facts")), "stop", llm.Usage{InputTokens: 20, OutputTokens: 3}, nil
		}
		mainCalls++
		if mainCalls == 1 {
			originalSize = len(req.Messages)
			return llm.Message{}, "", llm.Usage{}, errors.New("context_length_exceeded")
		}
		if len(req.Messages) >= originalSize || !strings.Contains(req.Messages[0].Text(), "prior goals") {
			t.Fatal("overflow retry did not use compacted history")
		}
		return behaviorMessage(llm.TextBlock("continued")), "stop", llm.Usage{InputTokens: 7, OutputTokens: 1}, nil
	}}
	_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "compact", SessionDir: dir, Provider: provider, Input: "continue", NonStreaming: true, Compaction: &runtime.CompactionConfig{ContextWindow: 64000, KeepRecent: 3}, MaxTurns: 3})
	if err != nil || reason != runtime.ReasonCompleted || mainCalls != 2 || summaryCalls != 1 {
		t.Fatalf("reason=%s err=%v calls=%d/%d", reason, err, mainCalls, summaryCalls)
	}
	session, release, err = runtime.OpenSession(context.Background(), dir, "compact", "worker", "0")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if session.Usage().InputTokens != 27 {
		t.Fatalf("summary tokens not accounted: %+v", session.Usage())
	}
}

type behaviorHooks struct {
	postCalls int
	postInput string
}

func (h *behaviorHooks) PreToolUse(_ context.Context, _ string, _ []byte) (bool, string, []byte) {
	return false, "", []byte(`{"value":"rewritten"}`)
}
func (h *behaviorHooks) PostToolUse(_ context.Context, _ string, input, result []byte, isErr bool) {
	h.postCalls++
	h.postInput = string(input)
}

func TestUniversalRuntimeHooksRewriteArgumentsAndReceiveResult(t *testing.T) {
	hooks := &behaviorHooks{}
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(behaviorCall("call", "echo", `{"value":"original"}`)), "tool_use", llm.Usage{}, nil
		}
		if got := string(req.Messages[1].Content[0].Input); got != `{"value":"rewritten"}` {
			t.Fatalf("history did not retain rewritten arguments: %q", got)
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	tool := runtime.NewFuncTool("echo", "echo", nil, func(_ context.Context, input json.RawMessage) (any, error) {
		if string(input) != `{"value":"rewritten"}` {
			t.Fatalf("tool arguments=%s", input)
		}
		return "echo", nil
	})
	_, _, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "hooks", WorkingDir: t.TempDir(), Provider: provider, Input: "hello", UniversalTools: []runtime.Tool{tool}, NonStreaming: true, MaxTurns: 3, Hooks: hooks})
	if err != nil || hooks.postCalls != 1 || hooks.postInput != `{"value":"rewritten"}` {
		t.Fatalf("err=%v hooks=%+v", err, hooks)
	}
}

func TestUniversalRuntimePreservesInjectedToolInstructions(t *testing.T) {
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(behaviorCall("skill", "skill", `{}`)), "tool_use", llm.Usage{}, nil
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role != llm.RoleUser || last.Text() != "skill instructions" {
			t.Fatalf("injected instructions lost: %+v", last)
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	tool := runtime.NewFuncTool("skill", "skill", nil, func(context.Context, json.RawMessage) (any, error) {
		return runtime.ToolOutput{Content: "loaded", Messages: []runtime.Message{runtime.NewUserMessage("skill instructions")}}, nil
	})
	_, _, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "skill", WorkingDir: t.TempDir(), Provider: provider, Input: "hello", UniversalTools: []runtime.Tool{tool}, NonStreaming: true, MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
}
