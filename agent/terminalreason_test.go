package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
)

func TestReasonHintCoversEveryTerminalReason(t *testing.T) {
	all := []runtime.TerminalReason{
		runtime.ReasonCompleted, runtime.ReasonBlockingLimit, runtime.ReasonImageError,
		runtime.ReasonModelError, runtime.ReasonAbortedStreaming, runtime.ReasonAbortedTools,
		runtime.ReasonPromptTooLong, runtime.ReasonStopHookPrevented, runtime.ReasonHookStopped,
		runtime.ReasonMaxTurns, runtime.ReasonTimeout,
	}
	for _, reason := range all {
		if strings.TrimSpace(reasonHint[reason]) == "" {
			t.Errorf("terminal reason %q has no explanation", reason)
		}
	}
}

func TestAbortCausePropagatesThroughRunContextChain(t *testing.T) {
	type key struct{}
	execCtx, cancelExec := context.WithCancelCause(context.Background())
	workCtx, cancelWork := context.WithCancelCause(execCtx)
	defer cancelWork(nil)
	valued := context.WithValue(workCtx, key{}, "task-1")
	runCtx, cancelRun := context.WithTimeoutCause(valued, time.Hour, AbortRunHardTimeout)
	defer cancelRun()

	cancelExec(AbortPausedByUser)
	code, _, text, ok := AbortReason(runCtx)
	if !ok || code != "paused_by_user" {
		t.Fatalf("code=%q ok=%v, want paused_by_user", code, ok)
	}
	if !strings.Contains(text, "frontier") {
		t.Fatalf("cause detail did not propagate: %q", text)
	}
}

func TestAbortReasonFallbacks(t *testing.T) {
	if _, _, _, ok := AbortReason(context.Background()); ok {
		t.Fatal("live context must not report an abort reason")
	}
	plain, cancel := context.WithCancel(context.Background())
	cancel()
	if code, _, _, ok := AbortReason(plain); !ok || code != "canceled_no_cause" {
		t.Fatalf("code=%q ok=%v, want canceled_no_cause", code, ok)
	}
	timed, cancelTimed := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelTimed()
	<-timed.Done()
	if code, _, _, _ := AbortReason(timed); code != "deadline_exceeded" {
		t.Fatalf("code=%q, want deadline_exceeded", code)
	}
}

func TestTerminalTextAbortedNamesCauseAndHangingTool(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortKilledByPlanner)
	trace := &runTrace{startedAt: time.Now().Add(-90 * time.Second)}
	trace.start("tu_1", "Bash", `{"command":"nmap -p- 10.0.0.1"}`)
	term := &TerminalResult{
		Reason: runtime.ReasonAbortedTools,
		Err:    context.Canceled,
		Turns:  7,
		Usage:  llm.Usage{InputTokens: 1200, OutputTokens: 340},
	}
	summary, detail := terminalText(ctx, term, trace)
	if !strings.Contains(summary, "规划者") || strings.Contains(summary, "\n") {
		t.Fatalf("unexpected summary: %q", summary)
	}
	for _, want := range []string{"killed_by_planner", "aborted_tools", "7 轮", "Bash", "未返回结果", "1200"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestTerminalTextDirectContextCancellationKeepsCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortChatStoppedByUser)
	summary, detail := terminalText(ctx, &TerminalResult{Err: context.Canceled}, &runTrace{startedAt: time.Now()})
	if !strings.Contains(summary, "用户停止") || !strings.Contains(detail, "chat_stopped_by_user") {
		t.Fatalf("direct context cancellation lost cause: %q\n%s", summary, detail)
	}
	if !strings.Contains(detail, "context_canceled") {
		t.Fatalf("missing synthesized terminal label: %s", detail)
	}
}

func TestTerminalTextAbortedPreservesPartialOutput(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(AbortTaskDeleted)
	term := &TerminalResult{
		Reason: runtime.ReasonAbortedStreaming,
		Err:    context.Canceled,
		Text:   "partial markdown output before stream aborted",
	}
	_, detail := terminalText(ctx, term, &runTrace{startedAt: time.Now()})
	if !strings.Contains(detail, "partial markdown output before stream aborted") {
		t.Fatalf("detail lost partial output: %s", detail)
	}
	if !strings.Contains(detail, "task_deleted") {
		t.Fatalf("detail lost named cause: %s", detail)
	}
}

func TestTerminalTextBudgetHitSummary(t *testing.T) {
	trace := &runTrace{startedAt: time.Now().Add(-12 * time.Second)}
	term := &TerminalResult{Reason: runtime.ReasonMaxTurns, Turns: 10}
	summary, _ := terminalText(context.Background(), term, trace)
	if !strings.Contains(summary, "max_turns") || !strings.Contains(summary, "已收尾") {
		t.Fatalf("unexpected budget summary: %q", summary)
	}
	if !strings.Contains(summary, "10 轮") {
		t.Fatalf("summary missing turn count: %q", summary)
	}
}

func TestTerminalTextNonAbortReasons(t *testing.T) {
	trace := &runTrace{startedAt: time.Now().Add(-5 * time.Second)}
	term := &TerminalResult{
		Reason: runtime.ReasonModelError,
		Err:    context.DeadlineExceeded,
	}
	summary, detail := terminalText(context.Background(), term, trace)
	if !strings.Contains(summary, "model_error") {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if !strings.Contains(detail, "context deadline exceeded") {
		t.Fatalf("detail missing underlying error: %s", detail)
	}
}
