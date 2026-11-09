package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SherkeyXD/ARTEX/runtime"
)

func init() {
	runtime.RegisterDriver(NewDriver())
}

// Driver implements runtime.HarnessDriver for the native Pi execution loop.
type Driver struct{}

// NewDriver creates an instance of the Pi HarnessDriver.
func NewDriver() *Driver {
	return &Driver{}
}

func (d *Driver) ID() string      { return "pi" }
func (d *Driver) Version() string { return "0.1.0-native" }

// ExecuteTurn executes a multi-step agent interaction loop until completion or max iterations.
func (d *Driver) ExecuteTurn(
	ctx context.Context,
	session *runtime.Session,
	opts runtime.TurnOptions,
	middlewares *runtime.MiddlewareChain,
	sink runtime.EventSink,
) (*runtime.TurnResult, error) {
	if opts.Provider == nil {
		return nil, fmt.Errorf("pi harness: LLMProvider is required")
	}

	maxIters := opts.MaxIterations
	if maxIters <= 0 {
		maxIters = 15
	}

	toolLimit := opts.ToolOutputLimit
	if toolLimit <= 0 {
		toolLimit = 4000 // default 4K chars context limit (prevents 30K explosion)
	}

	// Index tools by name
	toolMap := make(map[string]runtime.Tool, len(opts.Tools))
	toolDefs := make([]runtime.ToolDefinition, 0, len(opts.Tools))
	for _, t := range opts.Tools {
		if t != nil && t.Name() != "" {
			toolMap[t.Name()] = t
			toolDefs = append(toolDefs, runtime.ToolDefinition{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.ParametersSchema(),
			})
		}
	}

	turnStart := time.Now()
	var totalUsage runtime.TokenUsage
	var collectedToolResults []*runtime.ToolResultData
	var allThinking strings.Builder
	var lastText string
	var firstTokenOnce bool
	var ttft time.Duration

	tc := &runtime.TurnContext{
		SessionID: session.ID,
		AgentID:   session.AgentID,
		TaskID:    session.TaskID,
		TurnIndex: session.Len() / 2,
	}

	for iterCount := 1; iterCount <= maxIters; iterCount++ {
		select {
		case <-ctx.Done():
			return &runtime.TurnResult{
				FinalText:    lastText,
				ThinkingText: allThinking.String(),
				ToolCalls:    collectedToolResults,
				TokenUsage:   totalUsage,
				Duration:     time.Since(turnStart),
				TTFT:         ttft,
				StopReason:   runtime.StopReasonCancelled,
				Error:        ctx.Err(),
			}, ctx.Err()
		default:
		}

		// 1. Prepare completion request
		req := runtime.CompletionRequest{
			SystemPrompt:    opts.SystemPrompt,
			Messages:        session.Messages(),
			Tools:           toolDefs,
			Temperature:     opts.Temperature,
			MaxTokens:       opts.MaxTokens,
			ReasoningEffort: opts.ReasoningEffort,
			ThinkingType:    opts.ThinkingType,
			Model:           opts.ModelName,
		}

		// 2. Lifecycle: OnModelCallStart
		if middlewares != nil {
			middlewares.OnModelCallStart(ctx, tc)
		}
		if sink != nil {
			sink.Emit(ctx, runtime.Event{
				Type:      runtime.EventModelCallStart,
				Timestamp: time.Now(),
				SessionID: session.ID,
				TurnIndex: tc.TurnIndex,
			})
		}

		modelCallStart := time.Now()
		var iterText strings.Builder
		var iterThinking strings.Builder
		var pendingToolCalls []*runtime.ToolCallData

		// 3. Stream from LLMProvider
		for ev, err := range opts.Provider.Stream(ctx, req) {
			if err != nil {
				return nil, fmt.Errorf("pi harness stream error on iter %d: %w", iterCount, err)
			}

			// Capture TTFT on first token/chunk
			if !firstTokenOnce {
				firstTokenOnce = true
				ttft = time.Since(modelCallStart)
				if middlewares != nil {
					middlewares.OnFirstToken(ctx, tc, ttft)
				}
				if sink != nil {
					sink.Emit(ctx, runtime.Event{
						Type:       runtime.EventFirstToken,
						Timestamp:  time.Now(),
						SessionID:  session.ID,
						TurnIndex:  tc.TurnIndex,
						TTFTMs:     ttft.Milliseconds(),
					})
				}
			}

			switch ev.Type {
			case runtime.LLMEventThinking:
				if ev.Thinking != "" {
					iterThinking.WriteString(ev.Thinking)
					allThinking.WriteString(ev.Thinking)
					if middlewares != nil {
						middlewares.OnThinkingChunk(ctx, tc, ev.Thinking)
					}
					if sink != nil {
						sink.Emit(ctx, runtime.Event{
							Type:      runtime.EventThinkingChunk,
							Timestamp: time.Now(),
							SessionID: session.ID,
							TurnIndex: tc.TurnIndex,
							Thinking:  ev.Thinking,
						})
					}
				}

			case runtime.LLMEventText:
				if ev.Text != "" {
					iterText.WriteString(ev.Text)
					if sink != nil {
						sink.Emit(ctx, runtime.Event{
							Type:      runtime.EventTextChunk,
							Timestamp: time.Now(),
							SessionID: session.ID,
							TurnIndex: tc.TurnIndex,
							Text:      ev.Text,
						})
					}
				}

			case runtime.LLMEventToolCall:
				if ev.ToolCall != nil {
					pendingToolCalls = append(pendingToolCalls, ev.ToolCall)
					if sink != nil {
						sink.Emit(ctx, runtime.Event{
							Type:      runtime.EventToolCall,
							Timestamp: time.Now(),
							SessionID: session.ID,
							TurnIndex: tc.TurnIndex,
							ToolCall:  ev.ToolCall,
						})
					}
				}

			case runtime.LLMEventUsage:
				if ev.Usage != nil {
					totalUsage.Add(*ev.Usage)
				}
			}
		}

		currentText := iterText.String()
		currentThinking := iterThinking.String()
		if currentText != "" {
			lastText = currentText
		}

		// 4. If no tool calls, model provided the final text response!
		if len(pendingToolCalls) == 0 {
			var parts []runtime.Part
			if currentThinking != "" {
				parts = append(parts, runtime.ThinkingPart(currentThinking))
			}
			parts = append(parts, runtime.TextPart(currentText))

			session.AppendMessage(runtime.NewAssistantMessage(parts...))

			return &runtime.TurnResult{
				TurnIndex:    tc.TurnIndex,
				FinalText:    currentText,
				ThinkingText: allThinking.String(),
				ToolCalls:    collectedToolResults,
				TokenUsage:   totalUsage,
				Duration:     time.Since(turnStart),
				TTFT:         ttft,
				StopReason:   runtime.StopReasonComplete,
			}, nil
		}

		// 5. Model requested tool calls -> Record assistant turn first
		var assistantParts []runtime.Part
		if currentThinking != "" {
			assistantParts = append(assistantParts, runtime.ThinkingPart(currentThinking))
		}
		if currentText != "" {
			assistantParts = append(assistantParts, runtime.TextPart(currentText))
		}
		for _, tcData := range pendingToolCalls {
			assistantParts = append(assistantParts, runtime.ToolCallPart(tcData.ID, tcData.Name, tcData.Arguments))
		}
		session.AppendMessage(runtime.NewAssistantMessage(assistantParts...))

		// 6. Execute requested tools
		for _, call := range pendingToolCalls {
			var toolResult *runtime.ToolResultData
			targetTool, exists := toolMap[call.Name]

			// Run Middleware: Guard / Approval Interception
			proceed := true
			reason := ""
			var interceptErr error
			if middlewares != nil {
				proceed, reason, interceptErr = middlewares.OnBeforeToolExec(ctx, tc, call)
			}

			if interceptErr != nil {
				return nil, fmt.Errorf("tool interception error for %s: %w", call.Name, interceptErr)
			}

			if !proceed {
				// Tool execution denied by approval guard!
				denialMsg := fmt.Sprintf("[拦截规则拦截] %s", reason)
				toolResult = &runtime.ToolResultData{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Content:    denialMsg,
					IsError:    true,
					CharCount:  len(denialMsg),
				}
				if sink != nil {
					sink.Emit(ctx, runtime.Event{
						Type:       runtime.EventToolEnd,
						Timestamp:  time.Now(),
						SessionID:  session.ID,
						TurnIndex:  tc.TurnIndex,
						ToolResult: toolResult,
					})
				}
			} else if !exists {
				errMsg := fmt.Sprintf("未找到指定工具 %q", call.Name)
				toolResult = &runtime.ToolResultData{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Content:    errMsg,
					IsError:    true,
					CharCount:  len(errMsg),
				}
				if sink != nil {
					sink.Emit(ctx, runtime.Event{
						Type:       runtime.EventToolEnd,
						Timestamp:  time.Now(),
						SessionID:  session.ID,
						TurnIndex:  tc.TurnIndex,
						ToolResult: toolResult,
					})
				}
			} else {
				// Tool execution approved!
				if sink != nil {
					sink.Emit(ctx, runtime.Event{
						Type:      runtime.EventToolStart,
						Timestamp: time.Now(),
						SessionID: session.ID,
						TurnIndex: tc.TurnIndex,
						ToolCall:  call,
					})
				}

				toolExecStart := time.Now()
				rawOutput, execErr := targetTool.Execute(ctx, call.Arguments)
				toolDuration := time.Since(toolExecStart)

				var outputStr string
				isErr := execErr != nil
				if execErr != nil {
					outputStr = fmt.Sprintf("执行错误: %v", execErr)
				} else {
					switch v := rawOutput.(type) {
					case string:
						outputStr = v
					case []byte:
						outputStr = string(v)
					default:
						b, jsonErr := json.Marshal(v)
						if jsonErr != nil {
							outputStr = fmt.Sprintf("%v", v)
						} else {
							outputStr = string(b)
						}
					}
				}

				// Apply Context Truncation to prevent token blowup
				origLen := len(outputStr)
				if origLen > toolLimit {
					outputStr = outputStr[:toolLimit] + fmt.Sprintf("\n... [输出截断: 共 %d 字符，已保留前 %d 字符]", origLen, toolLimit)
				}

				toolResult = &runtime.ToolResultData{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Content:    outputStr,
					IsError:    isErr,
					DurationMs: toolDuration.Milliseconds(),
					CharCount:  origLen,
				}

				// Lifecycle: OnAfterToolExec
				if middlewares != nil {
					middlewares.OnAfterToolExec(ctx, tc, toolResult, toolDuration)
				}

				if sink != nil {
					sink.Emit(ctx, runtime.Event{
						Type:       runtime.EventToolEnd,
						Timestamp:  time.Now(),
						SessionID:  session.ID,
						TurnIndex:  tc.TurnIndex,
						ToolResult: toolResult,
						DurationMs: toolDuration.Milliseconds(),
					})
				}
			}

			collectedToolResults = append(collectedToolResults, toolResult)

			// Append tool response into conversation history
			session.AppendMessage(runtime.NewToolResultMessage(
				toolResult.ToolCallID,
				toolResult.ToolName,
				toolResult.Content,
				toolResult.IsError,
				toolResult.DurationMs,
			))
		}
	}

	// Loop reached max iterations
	return &runtime.TurnResult{
		TurnIndex:    tc.TurnIndex,
		FinalText:    lastText,
		ThinkingText: allThinking.String(),
		ToolCalls:    collectedToolResults,
		TokenUsage:   totalUsage,
		Duration:     time.Since(turnStart),
		TTFT:         ttft,
		StopReason:   runtime.StopReasonMaxSteps,
	}, nil
}
