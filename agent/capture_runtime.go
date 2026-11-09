package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/intercept"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/adapter"
	"github.com/SherkeyXD/ARTEX/sidequestion"
)

// captureRuntimeTurn executes one turn using the Universal Agent Runtime (e.g. Pi Driver)
// and emits db.Activity records identically to captureRunSession.
func captureRuntimeTurn(
	ctx context.Context,
	cfg runtime.AgentConfig,
	session *runtime.Session,
	chain *runtime.MiddlewareChain,
	input string,
	emit func(db.Activity),
) (string, runtime.TerminalReason, error) {
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

	sink := runtime.EventSinkFunc(func(ctx context.Context, ev runtime.Event) {
		switch ev.Type {
		case runtime.EventToolCall:
			if ev.ToolCall != nil {
				in := string(ev.ToolCall.Arguments)
				auditTrace.Start(ev.ToolCall.ID, ev.ToolCall.Name, ev.ToolCall.Arguments)
				rec(db.Activity{
					Kind:      "tool_use",
					Tool:      ev.ToolCall.Name,
					ToolUseID: ev.ToolCall.ID,
					Summary:   ev.ToolCall.Name + " " + firstLine(in, 200),
					Detail:    in,
				})
			}
		case runtime.EventToolEnd:
			if ev.ToolResult != nil {
				auditTrace.Complete(ev.ToolResult.ToolCallID, ev.ToolResult.Content, ev.ToolResult.IsError)
				meta, _ := json.Marshal(map[string]any{
					"duration_ms": ev.ToolResult.DurationMs,
					"char_count":  ev.ToolResult.CharCount,
				})
				rec(db.Activity{
					Kind:      "tool_result",
					Tool:      ev.ToolResult.ToolName,
					ToolUseID: ev.ToolResult.ToolCallID,
					Summary:   firstLine(ev.ToolResult.Content, 200),
					Detail:    ev.ToolResult.Content,
					IsError:   ev.ToolResult.IsError,
					Metadata:  meta,
				})
			}
		}
	})

	runner, err := runtime.NewAgentRunner(cfg, nil, chain, sink)
	if err != nil {
		return "", runtime.ReasonModelError, fmt.Errorf("create universal agent runner: %w", err)
	}

	res, err := runner.RunTurn(ctx, session, input)
	if err != nil {
		return "", runtime.ReasonModelError, err
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

	// Update side-question capture with final session messages
	sidequestion.Finish(ctx, adapter.ToProviderMessages(session.Messages()))

	var reason runtime.TerminalReason
	switch res.StopReason {
	case runtime.StopReasonComplete:
		reason = runtime.ReasonCompleted
	case runtime.StopReasonMaxSteps:
		reason = runtime.ReasonMaxTurns
	case runtime.StopReasonCancelled:
		reason = runtime.ReasonAbortedStreaming
	default:
		reason = runtime.ReasonCompleted
	}

	return res.FinalText, reason, nil
}
