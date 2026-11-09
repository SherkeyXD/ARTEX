package agent

import (
	"context"
	"iter"

	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/harness"
	normallm "github.com/Autumn-27/norma/llm"
	"github.com/SherkeyXD/ARTEX/guard"
	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/sidequestion"
)

func nativeSettlement(config *harness.Settlement) *runtime.Settlement {
	if config == nil {
		return nil
	}
	settlement := &runtime.Settlement{Prompt: config.Prompt, DisabledTools: append([]string(nil), config.DisabledTools...), MaxTurns: config.MaxTurns}
	if config.PromptByReason != nil {
		settlement.PromptByReason = make(map[runtime.TerminalReason]string)
		for reason, prompt := range config.PromptByReason {
			settlement.PromptByReason[runtime.TerminalReason(reason)] = prompt
		}
	}
	return settlement
}

func nativeCompaction(config *compaction.Config) *runtime.CompactionConfig {
	if config == nil {
		return nil
	}
	return &runtime.CompactionConfig{ContextWindow: config.ContextWindow, MaxOutputTokens: config.MaxOutputTokens, KeepRecent: config.KeepRecent}
}

func toNormaFormat(f llm.Format) normallm.Format {
	return normallm.Format(f)
}

func toNativeFormat(f normallm.Format) llm.Format {
	return llm.Format(f)
}

func toNormaBlock(b llm.ContentBlock) normallm.ContentBlock {
	return normallm.ContentBlock{
		Type:      normallm.BlockType(b.Type),
		Text:      b.Text,
		Thinking:  b.Thinking,
		Signature: b.Signature,
		ID:        b.ID,
		Name:      b.Name,
		Input:     b.Input,
		ToolUseID: b.ToolUseID,
		Content:   toNormaBlocks(b.Content),
		IsError:   b.IsError,
	}
}

func toNativeBlock(b normallm.ContentBlock) llm.ContentBlock {
	return llm.ContentBlock{
		Type:      llm.BlockType(b.Type),
		Text:      b.Text,
		Thinking:  b.Thinking,
		Signature: b.Signature,
		ID:        b.ID,
		Name:      b.Name,
		Input:     b.Input,
		ToolUseID: b.ToolUseID,
		Content:   toNativeBlocks(b.Content),
		IsError:   b.IsError,
	}
}

func toNormaBlocks(bs []llm.ContentBlock) []normallm.ContentBlock {
	if bs == nil {
		return nil
	}
	out := make([]normallm.ContentBlock, len(bs))
	for i, b := range bs {
		out[i] = toNormaBlock(b)
	}
	return out
}

func toNativeBlocks(bs []normallm.ContentBlock) []llm.ContentBlock {
	if bs == nil {
		return nil
	}
	out := make([]llm.ContentBlock, len(bs))
	for i, b := range bs {
		out[i] = toNativeBlock(b)
	}
	return out
}

func toNormaMessage(m llm.Message) normallm.Message {
	return normallm.Message{
		Role:    normallm.Role(m.Role),
		Content: toNormaBlocks(m.Content),
	}
}

func toNativeMessage(m normallm.Message) llm.Message {
	return llm.Message{
		Role:    llm.Role(m.Role),
		Content: toNativeBlocks(m.Content),
	}
}

func toNormaMessages(ms []llm.Message) []normallm.Message {
	if ms == nil {
		return nil
	}
	out := make([]normallm.Message, len(ms))
	for i, m := range ms {
		out[i] = toNormaMessage(m)
	}
	return out
}

func toNativeMessages(ms []normallm.Message) []llm.Message {
	if ms == nil {
		return nil
	}
	out := make([]llm.Message, len(ms))
	for i, m := range ms {
		out[i] = toNativeMessage(m)
	}
	return out
}

func toNormaToolSchema(ts llm.ToolSchema) normallm.ToolSchema {
	return normallm.ToolSchema{
		Name:        ts.Name,
		Description: ts.Description,
		InputSchema: ts.InputSchema,
	}
}

func toNativeToolSchema(ts normallm.ToolSchema) llm.ToolSchema {
	return llm.ToolSchema{
		Name:        ts.Name,
		Description: ts.Description,
		InputSchema: ts.InputSchema,
	}
}

func toNormaRequest(req llm.CompletionRequest) normallm.CompletionRequest {
	var tools []normallm.ToolSchema
	if req.Tools != nil {
		tools = make([]normallm.ToolSchema, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = toNormaToolSchema(t)
		}
	}
	return normallm.CompletionRequest{
		System:          req.System,
		DynamicBoundary: req.DynamicBoundary,
		Messages:        toNormaMessages(req.Messages),
		Tools:           tools,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		Stop:            req.Stop,
		Thinking:        req.Thinking,
	}
}

func toNativeRequest(req normallm.CompletionRequest) llm.CompletionRequest {
	var tools []llm.ToolSchema
	if req.Tools != nil {
		tools = make([]llm.ToolSchema, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = toNativeToolSchema(t)
		}
	}
	return llm.CompletionRequest{
		System:          req.System,
		DynamicBoundary: req.DynamicBoundary,
		Messages:        toNativeMessages(req.Messages),
		Tools:           tools,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		Stop:            req.Stop,
		Thinking:        req.Thinking,
	}
}

func toNormaUsage(u llm.Usage) normallm.Usage {
	return normallm.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

func toNativeUsage(u normallm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

func toNormaStreamEvent(ev llm.StreamEvent) normallm.StreamEvent {
	return normallm.StreamEvent{
		Type:       normallm.StreamEventType(ev.Type),
		Text:       ev.Text,
		ToolID:     ev.ToolID,
		ToolName:   ev.ToolName,
		StopReason: ev.StopReason,
		Usage:      toNormaUsage(ev.Usage),
	}
}

func toNativeStreamEvent(ev normallm.StreamEvent) llm.StreamEvent {
	return llm.StreamEvent{
		Type:       llm.StreamEventType(ev.Type),
		Text:       ev.Text,
		ToolID:     ev.ToolID,
		ToolName:   ev.ToolName,
		StopReason: ev.StopReason,
		Usage:      toNativeUsage(ev.Usage),
	}
}

type normaProviderAdapter struct {
	inner llm.Provider
}

func (a normaProviderAdapter) Stream(ctx context.Context, req normallm.CompletionRequest) iter.Seq2[normallm.StreamEvent, error] {
	nativeReq := toNativeRequest(req)
	return func(yield func(normallm.StreamEvent, error) bool) {
		for ev, err := range a.inner.Stream(ctx, nativeReq) {
			if !yield(toNormaStreamEvent(ev), err) {
				return
			}
		}
	}
}

func (a normaProviderAdapter) Complete(ctx context.Context, req normallm.CompletionRequest) (normallm.Message, string, normallm.Usage, error) {
	nativeReq := toNativeRequest(req)
	msg, s, usage, err := a.inner.Complete(ctx, nativeReq)
	return toNormaMessage(msg), s, toNormaUsage(usage), err
}

func toNormaProvider(p llm.Provider) normallm.Provider {
	if p == nil {
		return nil
	}
	return normaProviderAdapter{inner: p}
}

type nativeProviderAdapter struct {
	inner normallm.Provider
}

func (a nativeProviderAdapter) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	normaReq := toNormaRequest(req)
	return func(yield func(llm.StreamEvent, error) bool) {
		for ev, err := range a.inner.Stream(ctx, normaReq) {
			if !yield(toNativeStreamEvent(ev), err) {
				return
			}
		}
	}
}

func (a nativeProviderAdapter) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	normaReq := toNormaRequest(req)
	msg, s, usage, err := a.inner.Complete(ctx, normaReq)
	return toNativeMessage(msg), s, toNativeUsage(usage), err
}

func toNativeProvider(p normallm.Provider) llm.Provider {
	if p == nil {
		return nil
	}
	return nativeProviderAdapter{inner: p}
}

func toNormaQueryDeps(d sidequestion.QueryDeps) harness.QueryDeps {
	var out harness.QueryDeps
	out.NewID = d.NewID
	if d.CallModel != nil {
		out.CallModel = func(ctx context.Context, req normallm.CompletionRequest) iter.Seq2[normallm.StreamEvent, error] {
			return func(yield func(normallm.StreamEvent, error) bool) {
				for ev, err := range d.CallModel(ctx, toNativeRequest(req)) {
					if !yield(toNormaStreamEvent(ev), err) {
						return
					}
				}
			}
		}
	}
	if d.CallModelSync != nil {
		out.CallModelSync = func(ctx context.Context, req normallm.CompletionRequest) (normallm.Message, string, normallm.Usage, error) {
			msg, finish, usage, err := d.CallModelSync(ctx, toNativeRequest(req))
			return toNormaMessage(msg), finish, toNormaUsage(usage), err
		}
	}
	return out
}

func toNativeQueryDeps(d harness.QueryDeps) sidequestion.QueryDeps {
	var out sidequestion.QueryDeps
	out.NewID = d.NewID
	if d.CallModel != nil {
		out.CallModel = func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
			return func(yield func(llm.StreamEvent, error) bool) {
				for ev, err := range d.CallModel(ctx, toNormaRequest(req)) {
					if !yield(toNativeStreamEvent(ev), err) {
						return
					}
				}
			}
		}
	}
	if d.CallModelSync != nil {
		out.CallModelSync = func(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
			msg, finish, usage, err := d.CallModelSync(ctx, toNormaRequest(req))
			return toNativeMessage(msg), finish, toNativeUsage(usage), err
		}
	}
	return out
}

type legacyHookRunner struct {
	inner guard.HookRunner
}

func (l legacyHookRunner) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if l.inner == nil {
		return false, "", nil
	}
	return l.inner.PreToolUse(ctx, name, input)
}

func (l legacyHookRunner) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if l.inner != nil {
		l.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (l legacyHookRunner) Stop(ctx context.Context, messages []normallm.Message) (bool, []string, string) {
	return false, nil, ""
}

// AdaptGuardHooks adapts a guard.HookRunner to harness.HookRunner for legacy Norma agentcore sessions.
func AdaptGuardHooks(h guard.HookRunner) harness.HookRunner {
	if h == nil {
		return nil
	}
	if hr, ok := h.(harness.HookRunner); ok {
		return hr
	}
	return legacyHookRunner{inner: h}
}
