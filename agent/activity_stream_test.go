package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
)

func TestLiveActivityArrivesBeforeProviderFinishes(t *testing.T) {
	for _, kind := range []string{"text", "thinking"} {
		t.Run(kind, func(t *testing.T) {
			seen := make(chan struct{})
			release := make(chan struct{})
			provider := &behaviorProvider{stream: func(ctx context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
				return func(yield func(llm.StreamEvent, error) bool) {
					eventType := llm.SETextDelta
					if kind == "thinking" {
						eventType = llm.SEThinkingDelta
					}
					if !yield(llm.StreamEvent{Type: eventType, Text: "first live fragment"}, nil) {
						return
					}
					select {
					case <-release:
					case <-ctx.Done():
						yield(llm.StreamEvent{}, ctx.Err())
						return
					}
					if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "final answer"}, nil) {
						return
					}
					yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
				}
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, _, err := RunUniversalAgent(ctx, UniversalAgentOptions{AgentKey: "chat", SessionID: "live", Ephemeral: true, Provider: provider, Input: "hello", MaxTurns: 2, Emit: func(activity db.Activity) {
					if activity.Kind == kind {
						select {
						case <-seen:
						default:
							close(seen)
						}
					}
				}})
				done <- err
			}()
			select {
			case <-seen:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("activity not emitted while provider blocked")
			}
			select {
			case err := <-done:
				t.Fatalf("provider finished before release: %v", err)
			default:
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLiveFragmentsFlushOnTimerDuringProviderPause(t *testing.T) {
	second := make(chan struct{})
	release := make(chan struct{})
	provider := &behaviorProvider{stream: func(ctx context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "first"}, nil) {
				return
			}
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: " second"}, nil) {
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
				yield(llm.StreamEvent{}, ctx.Err())
				return
			}
			yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := RunUniversalAgent(ctx, UniversalAgentOptions{AgentKey: "chat", Ephemeral: true, Provider: provider, Input: "hello", MaxTurns: 2, Emit: func(activity db.Activity) {
			if activity.Kind == "text" && activity.Detail == " second" {
				close(second)
			}
		}})
		done <- err
	}()
	select {
	case <-second:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("pending fragment not timer-flushed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestActivityStreamUnicodeBoundAndFinalIdentity(t *testing.T) {
	var rows []db.Activity
	stream := newActivityStream(func(activity db.Activity) { rows = append(rows, activity) })
	text := strings.Repeat("世界\n", 1000)
	stream.add("text", text)
	stream.flush()
	var joined strings.Builder
	for _, row := range rows {
		if !utf8.ValidString(row.Detail) || utf8.RuneCountInString(row.Detail) > activityFragmentRunes {
			t.Fatalf("invalid or oversized fragment")
		}
		joined.WriteString(row.Detail)
	}
	if joined.String() != text || stream.finalID(text) == "" {
		t.Fatal("stream data or final identity lost")
	}
	stream.boundary()
	stream.add("thinking", "reasoning")
	if stream.finalID(text) != "" {
		t.Fatal("thinking matched previous answer")
	}
}

func TestStreamingAuditRegisteredBeforeApprovalAndTerminalReusesTextID(t *testing.T) {
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, _ llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(llm.TextBlock("before tool"), behaviorCall("call", "echo", `{}`)), "tool_use", llm.Usage{}, nil
		}
		return behaviorMessage(llm.TextBlock("final answer")), "stop", llm.Usage{InputTokens: 4}, nil
	}}
	registered := false
	hooks := &dispatchHooks{check: func(_ string, _ []byte) (bool, string, []byte) {
		if !registered {
			t.Error("approval ran before tool-call audit capture")
		}
		return false, "", nil
	}}
	tool := runtime.NewFuncTool("echo", "echo", nil, func(context.Context, json.RawMessage) (any, error) { return "echo", nil })
	var rows []db.Activity
	_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "chat", Ephemeral: true, Provider: provider, Input: "hello", UniversalTools: []runtime.Tool{tool}, Hooks: hooks, NonStreaming: true, MaxTurns: 3, Emit: func(activity db.Activity) {
		if activity.Kind == "tool_use" {
			registered = true
		}
		rows = append(rows, activity)
	}})
	if err != nil || reason != runtime.ReasonCompleted {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
	var finalDraft db.Activity
	for _, row := range rows {
		if row.Kind == "text" && row.Detail == "final answer" {
			finalDraft = row
		}
	}
	terminal := rows[len(rows)-1]
	if finalDraft.ToolUseID == "" || terminal.Kind != "result" || terminal.ToolUseID != finalDraft.ToolUseID {
		t.Fatal("final answer cannot replace its live draft")
	}
	if terminal.InputTokens == nil || *terminal.InputTokens != 4 {
		t.Fatal("terminal usage lost")
	}
}

func TestCancellationFlushesPendingStreamOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &behaviorProvider{stream: func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "first"}, nil) {
				return
			}
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: " pending"}, nil) {
				return
			}
			cancel()
			yield(llm.StreamEvent{}, ctx.Err())
		}
	}}
	var rows []db.Activity
	_, reason, err := RunUniversalAgent(ctx, UniversalAgentOptions{AgentKey: "chat", Ephemeral: true, Provider: provider, Input: "hello", MaxTurns: 3, Emit: func(row db.Activity) { rows = append(rows, row) }})
	if !errors.Is(err, context.Canceled) || reason != runtime.ReasonAbortedStreaming {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
	var text strings.Builder
	for _, row := range rows {
		if row.Kind == "text" {
			text.WriteString(row.Detail)
		}
	}
	if text.String() != "first pending" || rows[len(rows)-1].Kind != "result" {
		t.Fatal("pending stream lost or duplicated on cancellation")
	}
}
