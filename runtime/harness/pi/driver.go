package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SherkeyXD/ARTEX/runtime"
)

func init() { runtime.RegisterDriver(NewDriver()) }

type Driver struct{}

func NewDriver() *Driver          { return &Driver{} }
func (d *Driver) ID() string      { return "pi" }
func (d *Driver) Version() string { return "0.2.0-native" }

// ExecuteTurn owns the run budget; settlement uses the live caller context with
// its own budget. A caller cancellation always stops both phases.
func (d *Driver) ExecuteTurn(ctx context.Context, session *runtime.Session, opts runtime.TurnOptions, middlewares *runtime.MiddlewareChain, sink runtime.EventSink) (*runtime.TurnResult, error) {
	started := time.Now()
	result := &runtime.TurnResult{TurnIndex: session.Len() / 2}
	var thinking strings.Builder
	finish := func(reason runtime.TerminalReason, err error) (*runtime.TurnResult, error) {
		result.Reason, result.Error = reason, err
		result.ThinkingText, result.Duration = thinking.String(), time.Since(started)
		switch reason {
		case runtime.ReasonCompleted:
			result.StopReason = runtime.StopReasonComplete
		case runtime.ReasonMaxTurns:
			result.StopReason = runtime.StopReasonMaxSteps
		case runtime.ReasonAbortedStreaming, runtime.ReasonAbortedTools, runtime.ReasonTimeout:
			result.StopReason = runtime.StopReasonCancelled
		default:
			result.StopReason = runtime.StopReasonError
		}
		return result, err
	}
	if opts.Provider == nil {
		return finish(runtime.ReasonModelError, fmt.Errorf("pi harness: LLMProvider is required"))
	}
	phaseCtx, cancel := context.WithCancel(ctx)
	if opts.MaxDuration > 0 {
		cancel()
		phaseCtx, cancel = context.WithTimeout(ctx, opts.MaxDuration)
	}
	defer cancel()
	var cancelSettlement context.CancelFunc
	defer func() {
		if cancelSettlement != nil {
			cancelSettlement()
		}
	}()
	settling, phaseIterations, phaseLimit := false, 0, opts.MaxIterations
	toolLimit := opts.ToolOutputLimit
	if toolLimit <= 0 {
		toolLimit = 4000
	}
	toolMap := make(map[string]runtime.Tool)
	toolDefs := make([]runtime.ToolDefinition, 0, len(opts.Tools))
	for _, tool := range opts.Tools {
		if tool == nil || tool.Name() == "" {
			continue
		}
		toolMap[tool.Name()] = tool
		if visible, ok := tool.(runtime.SchemaVisibility); ok && !visible.SchemaVisible() {
			continue
		}
		toolDefs = append(toolDefs, runtime.ToolDefinition{Name: tool.Name(), Description: tool.Description(), Parameters: tool.ParametersSchema()})
	}
	tc := &runtime.TurnContext{SessionID: session.ID, AgentID: session.AgentID, TaskID: session.TaskID, TurnIndex: result.TurnIndex}
	emit := func(event runtime.Event) {
		if sink == nil {
			return
		}
		event.Timestamp, event.SessionID, event.TurnIndex = time.Now(), session.ID, tc.TurnIndex
		sink.Emit(ctx, event)
	}
	summaryProvider := usageObserver{inner: opts.Provider, observe: func(usage runtime.TokenUsage) {
		result.TokenUsage.Add(usage)
		total := result.TokenUsage
		emit(runtime.Event{Type: runtime.EventUsage, TokenUsage: &total})
	}}
	beginSettlement := func(reason runtime.TerminalReason) (bool, error) {
		result.Reason = reason
		if opts.Settlement == nil {
			return false, nil
		}
		prompt := opts.Settlement.Prompt
		if override := opts.Settlement.PromptByReason[reason]; override != "" {
			prompt = override
		}
		if strings.TrimSpace(prompt) == "" {
			return false, nil
		}
		cancel()
		budget := opts.Settlement.MaxDuration
		if budget <= 0 {
			budget = 2 * time.Minute
		}
		phaseCtx, cancelSettlement = context.WithTimeout(ctx, budget)
		settling, phaseIterations = true, 0
		phaseLimit = opts.Settlement.MaxTurns
		if phaseLimit <= 0 {
			phaseLimit = 10
		}
		disabled := make(map[string]bool)
		for _, name := range opts.Settlement.DisabledTools {
			disabled[name] = true
			delete(toolMap, name)
		}
		filtered := make([]runtime.ToolDefinition, 0, len(toolDefs))
		for _, tool := range toolDefs {
			if !disabled[tool.Name] {
				filtered = append(filtered, tool)
			}
		}
		toolDefs = filtered
		session.AppendMessage(runtime.NewUserMessage(prompt))
		return true, session.Checkpoint()
	}
	// A phase timeout starts settlement; caller cancellation retains its stage.
	handleError := func(stage runtime.TerminalReason, err error) (bool, *runtime.TurnResult, error) {
		if ctx.Err() != nil {
			res, e := finish(stage, ctx.Err())
			return false, res, e
		}
		if phaseCtx.Err() != nil && !settling {
			resume, saveErr := beginSettlement(runtime.ReasonTimeout)
			if saveErr != nil {
				res, e := finish(runtime.ReasonModelError, saveErr)
				return false, res, e
			}
			if resume {
				return true, nil, nil
			}
			res, e := finish(runtime.ReasonTimeout, nil)
			return false, res, e
		}
		reason := result.Reason
		if reason == "" {
			reason = runtime.ReasonModelError
		}
		if runtime.IsContextOverflow(err) {
			reason = runtime.ReasonPromptTooLong
		}
		res, e := finish(reason, err)
		return false, res, e
	}
	recordTool := func(call *runtime.ToolCallData, content string, isError bool, duration time.Duration, captured ...bool) error {
		content = runtime.SanitizeToolOutput(content)
		count := utf8.RuneCountInString(content)
		if count > toolLimit && !(len(captured) > 0 && captured[0]) && !strings.Contains(content, "<persisted-output>") {
			content = string([]rune(content)[:toolLimit]) + fmt.Sprintf("\n... [输出截断: 共 %d 字符，已保留前 %d 字符]", count, toolLimit)
		}
		toolResult := &runtime.ToolResultData{ToolCallID: call.ID, ToolName: call.Name, Content: content, IsError: isError, DurationMs: duration.Milliseconds(), CharCount: count}
		result.ToolCalls = append(result.ToolCalls, toolResult)
		session.AppendMessage(runtime.NewToolResultMessage(call.ID, call.Name, content, isError, duration.Milliseconds()))
		if middlewares != nil {
			middlewares.OnAfterToolExec(ctx, tc, toolResult, duration)
		}
		emit(runtime.Event{Type: runtime.EventToolEnd, ToolResult: toolResult, DurationMs: duration.Milliseconds()})
		return session.Checkpoint()
	}
	for {
		if phaseCtx.Err() != nil {
			resume, res, err := handleError(runtime.ReasonAbortedStreaming, phaseCtx.Err())
			if resume {
				continue
			}
			return res, err
		}
		if phaseLimit > 0 && phaseIterations >= phaseLimit {
			if settling {
				return finish(result.Reason, nil)
			}
			resume, err := beginSettlement(runtime.ReasonMaxTurns)
			if err != nil {
				return finish(runtime.ReasonModelError, err)
			}
			if resume {
				continue
			}
			return finish(runtime.ReasonMaxTurns, nil)
		}
		req := runtime.CompletionRequest{SystemPrompt: opts.SystemPrompt, SystemPrompts: opts.SystemPrompts, DynamicBoundary: opts.DynamicBoundary, NonStreaming: opts.NonStreaming, Messages: session.Messages(), Tools: toolDefs, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens, ReasoningEffort: opts.ReasoningEffort, ThinkingType: opts.ThinkingType, Model: opts.ModelName}
		if _, err := runtime.CompactSession(phaseCtx, session, summaryProvider, req, opts.Compaction, false); err != nil {
			resume, res, e := handleError(runtime.ReasonAbortedStreaming, err)
			if resume {
				continue
			}
			return res, e
		}
		req.Messages = session.Messages()
		phaseIterations++
		result.Iterations++
		if middlewares != nil {
			middlewares.OnModelCallStart(phaseCtx, tc)
		}
		emit(runtime.Event{Type: runtime.EventModelCallStart})
		var text, iterationThinking strings.Builder
		var signature string
		var calls []*runtime.ToolCallData
		expansionErrors := make(map[string]error)
		var streamErr error
		for attempt := 0; attempt < 2; attempt++ {
			for event, err := range opts.Provider.Stream(phaseCtx, req) {
				if err != nil {
					streamErr = err
					break
				}
				if result.TTFT == 0 && (event.Type == runtime.LLMEventText || event.Type == runtime.LLMEventThinking || event.Type == runtime.LLMEventToolCall) {
					result.TTFT = time.Since(started)
					if middlewares != nil {
						middlewares.OnFirstToken(phaseCtx, tc, result.TTFT)
					}
					emit(runtime.Event{Type: runtime.EventFirstToken, TTFTMs: result.TTFT.Milliseconds()})
				}
				switch event.Type {
				case runtime.LLMEventText:
					text.WriteString(event.Text)
					emit(runtime.Event{Type: runtime.EventTextChunk, Text: event.Text})
				case runtime.LLMEventThinking:
					iterationThinking.WriteString(event.Thinking)
					thinking.WriteString(event.Thinking)
					if middlewares != nil {
						middlewares.OnThinkingChunk(phaseCtx, tc, event.Thinking)
					}
					emit(runtime.Event{Type: runtime.EventThinkingChunk, Thinking: event.Thinking})
				case runtime.LLMEventThinkingSignature:
					signature = event.Signature
				case runtime.LLMEventToolCall:
					if event.ToolCall != nil {
						if dispatcher, ok := toolMap[event.ToolCall.Name].(runtime.ToolCallExpander); ok {
							if err := dispatcher.ExpandCall(event.ToolCall); err != nil {
								expansionErrors[event.ToolCall.ID] = err
							}
						}
						calls = append(calls, event.ToolCall)
						emit(runtime.Event{Type: runtime.EventToolCall, ToolCall: event.ToolCall})
					}
				case runtime.LLMEventUsage:
					if event.Usage != nil {
						result.TokenUsage.Add(*event.Usage)
						usage := result.TokenUsage
						emit(runtime.Event{Type: runtime.EventUsage, TokenUsage: &usage})
					}
				}
			}
			if streamErr == nil {
				streamErr = phaseCtx.Err()
			}
			if attempt == 0 && runtime.IsContextOverflow(streamErr) && text.Len() == 0 && iterationThinking.Len() == 0 && len(calls) == 0 {
				changed, compactErr := runtime.CompactSession(phaseCtx, session, summaryProvider, req, opts.Compaction, true)
				if compactErr != nil {
					streamErr = compactErr
					break
				}
				if changed {
					req.Messages = session.Messages()
					streamErr = nil
					continue
				}
			}
			break
		}
		parts := make([]runtime.Part, 0, len(calls)+2)
		if iterationThinking.Len() > 0 {
			part := runtime.ThinkingPart(iterationThinking.String())
			part.Signature = signature
			parts = append(parts, part)
		}
		if text.Len() > 0 {
			parts = append(parts, runtime.TextPart(text.String()))
			result.FinalText = text.String()
		}
		for _, call := range calls {
			parts = append(parts, runtime.ToolCallPart(call.ID, call.Name, call.Arguments))
		}
		if len(parts) > 0 {
			session.AppendMessage(runtime.NewAssistantMessage(parts...))
		}
		if err := session.Checkpoint(); err != nil {
			return finish(runtime.ReasonModelError, err)
		}
		if streamErr != nil {
			for _, call := range calls {
				if err := recordTool(call, "模型输出中断，工具未执行", true, 0); err != nil {
					return finish(runtime.ReasonModelError, err)
				}
			}
			resume, res, err := handleError(runtime.ReasonAbortedStreaming, streamErr)
			if resume {
				continue
			}
			return res, err
		}
		if len(calls) == 0 {
			result.FinalText = text.String()
			reason := result.Reason
			if reason == "" {
				reason = runtime.ReasonCompleted
			}
			return finish(reason, nil)
		}
		var extra []runtime.Message
		var toolPhaseErr error
		for _, call := range calls {
			if phaseCtx.Err() != nil {
				toolPhaseErr = phaseCtx.Err()
				if err := recordTool(call, "执行中断，工具未执行", true, 0); err != nil {
					return finish(runtime.ReasonModelError, err)
				}
				continue
			}
			proceed, reason := true, ""
			var gateErr error
			if middlewares != nil {
				proceed, reason, gateErr = middlewares.OnBeforeToolExec(phaseCtx, tc, call)
			}
			if phaseCtx.Err() != nil {
				gateErr = phaseCtx.Err()
			}
			session.UpdateToolCall(call)
			var content string
			var callErr error
			var duration time.Duration
			var captured bool
			switch {
			case gateErr != nil:
				callErr = gateErr
				content = gateErr.Error()
			case expansionErrors[call.ID] != nil:
				callErr = expansionErrors[call.ID]
				content = callErr.Error()
			case !proceed:
				callErr = fmt.Errorf("tool blocked: %s", reason)
				content = "[拦截规则拦截] " + reason
			case toolMap[call.Name] == nil:
				callErr = fmt.Errorf("未找到指定工具 %q", call.Name)
				content = callErr.Error()
			default:
				emit(runtime.Event{Type: runtime.EventToolStart, ToolCall: call})
				callCtx := runtime.WithToolExecution(phaseCtx, runtime.ToolExecutionContext{WorkingDir: opts.WorkingDir, AgentID: session.AgentID, ToolUseID: call.ID, OutputDir: opts.ToolOutputDir, MaxOutputChars: toolLimit})
				start := time.Now()
				output, err := toolMap[call.Name].Execute(callCtx, call.Arguments)
				duration, callErr = time.Since(start), err
				if err != nil {
					content = "执行错误: " + err.Error()
				} else {
					switch value := output.(type) {
					case runtime.ToolOutput:
						content = value.Content
						captured = value.Captured
						extra = append(extra, value.Messages...)
					case string:
						content = value
					case []byte:
						content = string(value)
					default:
						data, err := json.Marshal(output)
						if err != nil {
							content = fmt.Sprint(output)
						} else {
							content = string(data)
						}
					}
				}
			}
			if err := recordTool(call, content, callErr != nil, duration, captured); err != nil {
				return finish(runtime.ReasonModelError, err)
			}
			if phaseCtx.Err() != nil {
				toolPhaseErr = phaseCtx.Err()
			}
			if gateErr != nil && toolPhaseErr == nil {
				toolPhaseErr = gateErr
			}
		}
		if len(extra) > 0 {
			session.AppendMessage(extra...)
			if err := session.Checkpoint(); err != nil {
				return finish(runtime.ReasonModelError, err)
			}
		}
		if toolPhaseErr != nil {
			resume, res, err := handleError(runtime.ReasonAbortedTools, toolPhaseErr)
			if resume {
				continue
			}
			return res, err
		}
	}
}

type usageObserver struct {
	inner   runtime.LLMProvider
	observe func(runtime.TokenUsage)
}

func (p usageObserver) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		for event, err := range p.inner.Stream(ctx, req) {
			if event.Usage != nil {
				p.observe(*event.Usage)
			}
			if !yield(event, err) {
				return
			}
		}
	}
}
