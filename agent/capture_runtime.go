package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/intercept"
	"github.com/SherkeyXD/ARTEX/llm"
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
	started := time.Now()
	ctx, auditTrace := intercept.WithTrace(ctx, input, approvalHistory(adapter.ToProviderMessages(session.Messages())))
	defer auditTrace.Finish()
	defer func() { sidequestion.Finish(ctx, adapter.ToProviderMessages(session.Messages())) }()

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
		case runtime.EventUsage:
			if ev.TokenUsage != nil {
				u := ev.TokenUsage
				rec(db.Activity{Kind: "usage", InputTokens: &u.InputTokens, OutputTokens: &u.OutputTokens, CacheReadTokens: &u.CacheReadTokens, CacheWriteTokens: &u.CacheWriteTokens})
			}
		}
	})

	runner, err := runtime.NewAgentRunner(cfg, nil, chain, sink)
	if err != nil {
		return "", runtime.ReasonModelError, fmt.Errorf("create universal agent runner: %w", err)
	}

	res, err := runner.RunTurn(ctx, session, input)
	if res == nil {
		res = &runtime.TurnResult{Reason: runtime.ReasonModelError, Error: err}
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

	reason := res.Reason
	if reason == "" {
		switch res.StopReason {
		case runtime.StopReasonComplete:
			reason = runtime.ReasonCompleted
		case runtime.StopReasonMaxSteps:
			reason = runtime.ReasonMaxTurns
		case runtime.StopReasonCancelled:
			reason = runtime.ReasonAbortedStreaming
		default:
			if err != nil {
				reason = runtime.ReasonModelError
			} else {
				reason = runtime.ReasonCompleted
			}
		}
	}
	meta, _ := json.Marshal(map[string]any{"total_ms": time.Since(started).Milliseconds(), "ttft_ms": res.TTFT.Milliseconds(), "terminal_reason": reason, "turns": res.Iterations})
	detail := res.FinalText
	if detail == "" {
		detail = "运行结束：" + string(reason)
	}
	if err != nil {
		detail += "\n\n" + err.Error()
	}
	if reason == runtime.ReasonAbortedStreaming || reason == runtime.ReasonAbortedTools {
		if _, short, why, ok := AbortReason(ctx); ok {
			detail += "\n\n中断原因：" + short + "；" + why
		}
	}
	// Activity totals are aggregated across runs by the DB. Persist this run's
	// usage here; the resumed session keeps its separate cumulative counter.
	u := res.TokenUsage
	rec(db.Activity{Kind: "result", Summary: firstLine(detail, 400), Detail: detail, IsError: err != nil, Metadata: meta, InputTokens: &u.InputTokens, OutputTokens: &u.OutputTokens, CacheReadTokens: &u.CacheReadTokens, CacheWriteTokens: &u.CacheWriteTokens})

	return res.FinalText, reason, err
}

// Preserve the recorded session's visible messages, excluding thinking blocks.
// This is audit context; the judge receives only bounded, paired execution
// evidence selected from it, never assistant prose or thinking blocks.
func approvalHistory(messages []llm.Message) []db.InterceptContextEntry {
	var entries []db.InterceptContextEntry
	for _, message := range messages {
		for _, block := range message.Content {
			entry := db.InterceptContextEntry{Kind: string(message.Role)}
			switch block.Type {
			case llm.BlockText:
				entry.Text = block.Text
			case llm.BlockToolUse:
				entry.Kind, entry.Tool, entry.ToolUseID, entry.Text = "tool_use", block.Name, block.ID, string(block.Input)
			case llm.BlockToolResult:
				entry.Kind, entry.ToolUseID, entry.Text, entry.IsError = "tool_result", block.ToolUseID, blocksText(block.Content), block.IsError
			default:
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func blocksText(blocks []llm.ContentBlock) string {
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type == llm.BlockText && bl.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(bl.Text)
		}
	}
	return b.String()
}

// firstLine returns a single-line, rune-capped preview for the summary column.
func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if before, _, found := strings.Cut(s, "\n"); found {
		s = before
	}
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}
