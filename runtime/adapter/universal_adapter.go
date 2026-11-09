package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
	"github.com/SherkeyXD/ARTEX/sidequestion"
)

// UniversalToolAdapter wraps an existing tool into a universal runtime.Tool.
type UniversalToolAdapter struct {
	coreTool actool.CoreTool
}

// ToUniversalTool converts a CoreTool into a universal runtime.Tool.
func ToUniversalTool(ct actool.CoreTool) runtime.Tool {
	if ct == nil {
		return nil
	}
	return ct
}

// ToUniversalTools converts a slice of CoreTools into universal runtime.Tools.
func ToUniversalTools(cts []actool.CoreTool) []runtime.Tool {
	res := make([]runtime.Tool, 0, len(cts))
	for _, ct := range cts {
		if ct != nil {
			res = append(res, ToUniversalTool(ct))
		}
	}
	return res
}

// Backward-compatible aliases for legacy callers
var (
	WrapNormaTool  = ToUniversalTool
	WrapNormaTools = ToUniversalTools
)

type NormaToolAdapter = UniversalToolAdapter

func (a *UniversalToolAdapter) Name() string {
	return a.coreTool.Name()
}

func (a *UniversalToolAdapter) Description() string {
	return a.coreTool.Description()
}

func (a *UniversalToolAdapter) ParametersSchema() json.RawMessage {
	schemaMap := a.coreTool.InputSchema()
	if schemaMap == nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	b, err := json.Marshal(schemaMap)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b
}

func (a *UniversalToolAdapter) Execute(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	execution := runtime.ToolExecutionFrom(ctx)
	toolContext := &actool.ToolContext{
		WorkingDir: execution.WorkingDir, AgentID: execution.AgentID,
		ToolUseID: execution.ToolUseID, OutputDir: execution.OutputDir,
		MaxOutputChars: execution.MaxOutputChars,
	}
	res, err := a.coreTool.Call(ctx, rawArgs, toolContext)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	for _, cb := range res.Content {
		if string(cb.Type) == string(llm.BlockText) {
			sb.WriteString(cb.Text)
		}
	}

	if res.IsError {
		return nil, fmt.Errorf("%s", sb.String())
	}
	content := actool.CaptureOnce(toolContext, runtime.SanitizeToolOutput(sb.String()))
	captured := strings.Contains(content, "<persisted-output>") || strings.Contains(content, "characters truncated]")
	if len(res.Extra) > 0 || captured {
		var messages []runtime.Message
		for _, message := range res.Extra {
			parts := make([]runtime.Part, 0, len(message.Content))
			for _, block := range message.Content {
				if string(block.Type) == string(llm.BlockText) {
					parts = append(parts, runtime.TextPart(block.Text))
				}
			}
			role := runtime.RoleUser
			if string(message.Role) == string(llm.RoleAssistant) {
				role = runtime.RoleAssistant
			}
			messages = append(messages, runtime.Message{Role: role, Parts: parts})
		}
		return runtime.ToolOutput{Content: content, Messages: messages, Captured: captured}, nil
	}
	return content, nil
}

// UniversalLLMAdapter adapts an external llm.Provider (including llmpool.Pool) into runtime.LLMProvider.
type UniversalLLMAdapter struct {
	provider llm.Provider
}

// ToUniversalProvider creates a runtime.LLMProvider from an llm.Provider.
func ToUniversalProvider(p llm.Provider) runtime.LLMProvider {
	if p == nil {
		return nil
	}
	return &UniversalLLMAdapter{provider: p}
}

// Backward-compatible aliases for legacy callers
var WrapNormaProvider = ToUniversalProvider

type NormaLLMAdapter = UniversalLLMAdapter

// ToProviderMessages converts canonical runtime messages into legacy provider messages.
func ToProviderMessages(msgs []runtime.Message) []llm.Message {
	normaMsgs := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		normaRole := llm.RoleUser
		if m.Role == runtime.RoleAssistant {
			normaRole = llm.RoleAssistant
		}

		blocks := make([]llm.ContentBlock, 0, len(m.Parts))
		for _, p := range m.Parts {
			switch p.Type {
			case runtime.PartText:
				blocks = append(blocks, llm.TextBlock(p.Text))
			case runtime.PartThinking:
				blocks = append(blocks, llm.ContentBlock{
					Type:      llm.BlockThinking,
					Thinking:  p.Thinking,
					Signature: p.Signature,
				})
			case runtime.PartToolCall:
				if p.ToolCall != nil {
					blocks = append(blocks, llm.ContentBlock{
						Type:  llm.BlockToolUse,
						ID:    p.ToolCall.ID,
						Name:  p.ToolCall.Name,
						Input: p.ToolCall.Arguments,
					})
				}
			case runtime.PartToolResult:
				if p.ToolResult != nil {
					blocks = append(blocks, llm.ToolResultText(p.ToolResult.ToolCallID, p.ToolResult.Content, p.ToolResult.IsError))
				}
			}
		}

		normaMsgs = append(normaMsgs, llm.Message{
			Role:    normaRole,
			Content: blocks,
		})
	}
	return normaMsgs
}

// Stream adapts streaming events into runtime.LLMStreamEvent stream.
func (a *UniversalLLMAdapter) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	ctx = sidequestion.WithAttempt(ctx)
	// Convert canonical messages to provider messages
	normaMsgs := ToProviderMessages(req.Messages)

	// Convert tools
	normaTools := make([]llm.ToolSchema, 0, len(req.Tools))
	for _, t := range req.Tools {
		var schemaMap map[string]any
		if len(t.Parameters) > 0 {
			_ = json.Unmarshal(t.Parameters, &schemaMap)
		}
		if schemaMap == nil {
			schemaMap = map[string]any{"type": "object"}
		}
		normaTools = append(normaTools, llm.ToolSchema{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schemaMap,
		})
	}

	var sysList []string
	if len(req.SystemPrompts) > 0 {
		sysList = req.SystemPrompts
	} else if req.SystemPrompt != "" {
		sysList = []string{req.SystemPrompt}
	}

	var temp *float64
	if req.Temperature > 0 {
		t := req.Temperature
		temp = &t
	}

	normaReq := llm.CompletionRequest{
		System:          sysList,
		DynamicBoundary: req.DynamicBoundary,
		Messages:        normaMsgs,
		Tools:           normaTools,
		Temperature:     temp,
		MaxTokens:       req.MaxTokens,
		Thinking:        req.ThinkingType,
	}

	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		var currentToolID string
		var currentToolName string
		var currentToolArgs strings.Builder

		for ev, err := range a.events(ctx, normaReq, req.NonStreaming) {
			if err != nil {
				yield(runtime.LLMStreamEvent{}, err)
				return
			}

			switch ev.Type {
			case llm.SEThinkingSignature:
				if !yield(runtime.LLMStreamEvent{Type: runtime.LLMEventThinkingSignature, Signature: ev.Text}, nil) {
					return
				}
			case llm.SEThinkingDelta:
				if !yield(runtime.LLMStreamEvent{
					Type:     runtime.LLMEventThinking,
					Thinking: ev.Text,
				}, nil) {
					return
				}

			case llm.SETextDelta:
				if !yield(runtime.LLMStreamEvent{
					Type: runtime.LLMEventText,
					Text: ev.Text,
				}, nil) {
					return
				}

			case llm.SEToolUseStart:
				if currentToolID != "" {
					tcData := &runtime.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: toolInput(currentToolArgs.String()),
					}
					if !yield(runtime.LLMStreamEvent{
						Type:     runtime.LLMEventToolCall,
						ToolCall: tcData,
					}, nil) {
						return
					}
				}
				currentToolID = ev.ToolID
				currentToolName = ev.ToolName
				currentToolArgs.Reset()

			case llm.SEToolInputJSON:
				currentToolArgs.WriteString(ev.Text)

			case llm.SEMessageStart, llm.SEMessageDelta:
				if currentToolID != "" {
					tcData := &runtime.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: toolInput(currentToolArgs.String()),
					}
					currentToolID = ""
					if !yield(runtime.LLMStreamEvent{
						Type:     runtime.LLMEventToolCall,
						ToolCall: tcData,
					}, nil) {
						return
					}
				}

				if ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0 || ev.Usage.CacheReadTokens > 0 || ev.Usage.CacheWriteTokens > 0 {
					usage := &runtime.TokenUsage{
						InputTokens:      ev.Usage.InputTokens,
						OutputTokens:     ev.Usage.OutputTokens,
						CacheReadTokens:  ev.Usage.CacheReadTokens,
						CacheWriteTokens: ev.Usage.CacheWriteTokens,
					}
					if !yield(runtime.LLMStreamEvent{
						Type:  runtime.LLMEventUsage,
						Usage: usage,
					}, nil) {
						return
					}
				}
				if ev.StopReason != "" {
					if !yield(runtime.LLMStreamEvent{Type: runtime.LLMEventFinish, FinishReason: ev.StopReason}, nil) {
						return
					}
				}

			case llm.SEMessageStop:
				if currentToolID != "" {
					tcData := &runtime.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: toolInput(currentToolArgs.String()),
					}
					currentToolID = ""
					if !yield(runtime.LLMStreamEvent{
						Type:     runtime.LLMEventToolCall,
						ToolCall: tcData,
					}, nil) {
						return
					}
				}
			}
		}
	}
}

// events uses the provider's real non-streaming request when selected, then
// normalizes the completed message through the same event adapter.
func (a *UniversalLLMAdapter) events(ctx context.Context, req llm.CompletionRequest, nonStreaming bool) iter.Seq2[llm.StreamEvent, error] {
	if !nonStreaming {
		return a.provider.Stream(ctx, req)
	}
	return func(yield func(llm.StreamEvent, error) bool) {
		message, stop, usage, err := a.provider.Complete(ctx, req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		for _, block := range message.Content {
			switch block.Type {
			case llm.BlockText:
				if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: block.Text}, nil) {
					return
				}
			case llm.BlockThinking:
				if !yield(llm.StreamEvent{Type: llm.SEThinkingDelta, Text: block.Thinking}, nil) {
					return
				}
				if !yield(llm.StreamEvent{Type: llm.SEThinkingSignature, Text: block.Signature}, nil) {
					return
				}
			case llm.BlockToolUse:
				if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: block.ID, ToolName: block.Name}, nil) {
					return
				}
				input := string(block.Input)
				if input == "" {
					input = "{}"
				}
				if !yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: input}, nil) {
					return
				}
			}
		}
		if !yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stop, Usage: usage}, nil) {
			return
		}
		yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	}
}

func toolInput(input string) json.RawMessage {
	if strings.TrimSpace(input) == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(input)
}
