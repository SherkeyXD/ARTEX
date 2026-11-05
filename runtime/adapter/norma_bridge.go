package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/Autumn-27/artex/runtime"
	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

// NormaToolAdapter wraps an existing actool.CoreTool into a runtime.Tool.
type NormaToolAdapter struct {
	coreTool actool.CoreTool
}

// WrapNormaTool wraps a norma CoreTool into runtime.Tool.
func WrapNormaTool(ct actool.CoreTool) runtime.Tool {
	if ct == nil {
		return nil
	}
	return &NormaToolAdapter{coreTool: ct}
}

// WrapNormaTools wraps a slice of norma CoreTools into runtime.Tools.
func WrapNormaTools(cts []actool.CoreTool) []runtime.Tool {
	res := make([]runtime.Tool, 0, len(cts))
	for _, ct := range cts {
		if ct != nil {
			res = append(res, WrapNormaTool(ct))
		}
	}
	return res
}

func (a *NormaToolAdapter) Name() string {
	return a.coreTool.Name()
}

func (a *NormaToolAdapter) Description() string {
	return a.coreTool.Description()
}

func (a *NormaToolAdapter) ParametersSchema() json.RawMessage {
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

func (a *NormaToolAdapter) Execute(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	res, err := a.coreTool.Call(ctx, rawArgs, nil)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	for _, cb := range res.Content {
		if cb.Type == llm.BlockText {
			sb.WriteString(cb.Text)
		}
	}

	if res.IsError {
		return nil, fmt.Errorf("%s", sb.String())
	}
	return sb.String(), nil
}

// NormaLLMAdapter adapts norma's llm.Provider (including llmpool.Pool) into runtime.LLMProvider.
type NormaLLMAdapter struct {
	provider llm.Provider
}

// WrapNormaProvider creates a runtime.LLMProvider from a norma llm.Provider.
func WrapNormaProvider(p llm.Provider) runtime.LLMProvider {
	if p == nil {
		return nil
	}
	return &NormaLLMAdapter{provider: p}
}

// Stream adapts norma's streaming events into runtime.LLMStreamEvent stream.
func (a *NormaLLMAdapter) Stream(ctx context.Context, req runtime.CompletionRequest) iter.Seq2[runtime.LLMStreamEvent, error] {
	// Convert canonical messages to norma messages
	normaMsgs := make([]llm.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
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
					Type:     llm.BlockThinking,
					Thinking: p.Thinking,
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
	if req.SystemPrompt != "" {
		sysList = []string{req.SystemPrompt}
	}

	var temp *float64
	if req.Temperature > 0 {
		t := req.Temperature
		temp = &t
	}

	normaReq := llm.CompletionRequest{
		System:      sysList,
		Messages:    normaMsgs,
		Tools:       normaTools,
		Temperature: temp,
		MaxTokens:   req.MaxTokens,
		Thinking:    req.ThinkingType,
	}

	return func(yield func(runtime.LLMStreamEvent, error) bool) {
		var currentToolID string
		var currentToolName string
		var currentToolArgs strings.Builder

		for ev, err := range a.provider.Stream(ctx, normaReq) {
			if err != nil {
				yield(runtime.LLMStreamEvent{}, err)
				return
			}

			switch ev.Type {
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
						Arguments: json.RawMessage(currentToolArgs.String()),
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

			case llm.SEMessageDelta:
				if currentToolID != "" {
					tcData := &runtime.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: json.RawMessage(currentToolArgs.String()),
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

			case llm.SEMessageStop:
				if currentToolID != "" {
					tcData := &runtime.ToolCallData{
						ID:        currentToolID,
						Name:      currentToolName,
						Arguments: json.RawMessage(currentToolArgs.String()),
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
