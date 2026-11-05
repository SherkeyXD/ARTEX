package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/intercept"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/Autumn-27/norma/harness"
)

// captureRuntimeTurn executes one turn using the Universal Agent Runtime (e.g. Pi Driver)
// and emits db.Activity records identically to captureRunSession.
func captureRuntimeTurn(
	ctx context.Context,
	runner *runtime.AgentRunner,
	session *runtime.Session,
	input string,
	emit func(db.Activity),
) (string, harness.TerminalReason, error) {
	ctx, auditTrace := intercept.WithTrace(ctx, input, nil)
	defer auditTrace.Finish()

	rec := func(r db.Activity) {
		if r.Kind == "text" || r.Kind == "tool_result" {
			auditTrace.Append(db.InterceptContextEntry{
				Kind:      r.Kind,
				Tool:      r.Tool,
				ToolUseID: r.ToolUseID,
				Text:      r.Detail,
				IsError:   r.IsError,
			})
		}
		if emit != nil {
			emit(r)
		}
	}

	res, err := runner.RunTurn(ctx, session, input)
	if err != nil {
		return "", harness.ReasonModelError, err
	}

	// Emit tool executions
	for _, tc := range res.ToolCalls {
		summary := firstLine(tc.Content, 200)
		meta, _ := json.Marshal(map[string]any{
			"duration_ms": tc.DurationMs,
			"char_count":  tc.CharCount,
		})
		rec(db.Activity{
			Kind:      "tool_result",
			Tool:      tc.ToolName,
			ToolUseID: tc.ToolCallID,
			Summary:   summary,
			Detail:    tc.Content,
			IsError:   tc.IsError,
			Metadata:  meta,
		})
	}

	// Emit final thinking if any
	if res.ThinkingText != "" {
		meta, _ := json.Marshal(map[string]any{
			"duration_ms": res.Duration.Milliseconds(),
			"char_count":  len(res.ThinkingText),
		})
		rec(db.Activity{
			Kind:     "thinking",
			Summary:  firstLine(strings.TrimSpace(res.ThinkingText), 200),
			Detail:   res.ThinkingText,
			Metadata: meta,
		})
	}

	// Emit final text
	if res.FinalText != "" {
		meta, _ := json.Marshal(map[string]any{
			"duration_ms": res.Duration.Milliseconds(),
			"char_count":  len(res.FinalText),
		})
		rec(db.Activity{
			Kind:     "text",
			Summary:  firstLine(strings.TrimSpace(res.FinalText), 200),
			Detail:   res.FinalText,
			Metadata: meta,
		})
	}

	var reason harness.TerminalReason
	switch res.StopReason {
	case runtime.StopReasonComplete:
		reason = harness.ReasonCompleted
	case runtime.StopReasonMaxSteps:
		reason = harness.ReasonMaxTurns
	case runtime.StopReasonCancelled:
		reason = harness.ReasonAbortedStreaming
	default:
		reason = harness.ReasonCompleted
	}

	return res.FinalText, reason, nil
}
