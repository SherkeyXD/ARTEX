package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/llm"
	tool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

type reviewProvider struct {
	streams, completes int
	requireDeadline    bool
	loop               bool
}

func (p *reviewProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.completes++
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("complete")}}, "stop", llm.Usage{}, nil
}
func (p *reviewProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(y func(llm.StreamEvent, error) bool) {
		p.streams++
		if p.requireDeadline {
			if _, ok := ctx.Deadline(); !ok {
				y(llm.StreamEvent{}, errors.New("configured run deadline was not propagated"))
				return
			}
		}
		if p.loop && p.streams == 1 {
			if !y(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: "one", ToolName: "echo"}, nil) {
				return
			}
			if !y(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: `{}`}, nil) {
				return
			}
		} else {
			if !y(llm.StreamEvent{Type: llm.SETextDelta, Text: "stream"}, nil) {
				return
			}
		}
		if !y(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "stop"}, nil) {
			return
		}
		y(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	}
}
func TestNativeRegressionChatUsesNonStreaming(t *testing.T) {
	p := &reviewProvider{}
	c := NewChatAgent(p, "mock", t.TempDir(), 64000)
	c.SetNonStreaming(func() bool { return true })
	_, err := c.Chat(context.Background(), "chat", "mode", "hello", 2, time.Second, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.streams != 0 || p.completes != 1 {
		t.Fatalf("Stream=%d Complete=%d; expected Complete only", p.streams, p.completes)
	}
}
func TestNativeRegressionChatPropagatesDeadline(t *testing.T) {
	p := &reviewProvider{requireDeadline: true}
	c := NewChatAgent(p, "mock", t.TempDir(), 64000)
	_, err := c.Chat(context.Background(), "chat", "deadline", "hello", 2, time.Second, false, nil)
	if err != nil {
		t.Fatal(err)
	}
}
func TestNativeRegressionMainUsesNonStreaming(t *testing.T) {
	p := &reviewProvider{}
	c := NewMainAgent(p, "mock", t.TempDir(), 64000, 2)
	c.SetNonStreaming(func() bool { return true })
	_, err := c.Chat(context.Background(), 1, 0, nil, nil, "goal", "hello", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.streams != 0 || p.completes != 1 {
		t.Fatalf("Stream=%d Complete=%d; expected Complete only", p.streams, p.completes)
	}
}
func TestNativeRegressionWorkerSettlesBudget(t *testing.T) {
	p := &reviewProvider{loop: true}
	w := NewWorker(p, "mock", t.TempDir(), 64000, 1)
	base := tool.Build(tool.Spec{Name: "echo", Run: func(context.Context, json.RawMessage, *tool.ToolContext) (tool.Result, error) {
		return tool.Text("fact"), nil
	}})
	_, err := w.runWorkerWithUniversalRuntime(context.Background(), "worker", 1, nil, &db.Node{ID: 1}, nil, "work", []tool.CoreTool{base}, "system", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.streams != 2 {
		t.Fatalf("model calls=%d, expected exploration plus settlement", p.streams)
	}
}
func TestNativeRegressionDecoratedExecuteHonorsDefaultsAndSchema(t *testing.T) {
	var seen map[string]any
	base := tool.Build(tool.Spec{Name: "x", Schema: map[string]any{"type": "object"}, Run: func(_ context.Context, in json.RawMessage, _ *tool.ToolContext) (tool.Result, error) {
		json.Unmarshal(in, &seen)
		return tool.Text("ok"), nil
	}})
	schema := map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "default": 3}}}
	wrapped := DecorateTool(base, "custom", schema)
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if seen["limit"] != float64(3) {
		t.Errorf("Execute bypassed default injection: input=%v", seen)
	}
	if !strings.Contains(string(wrapped.ParametersSchema()), "limit") {
		t.Errorf("ParametersSchema bypassed DB override: %s", wrapped.ParametersSchema())
	}
}

type reviewGateProvider struct{ calls int }

func (p *reviewGateProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "", llm.Usage{}, nil
}
func (p *reviewGateProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(y func(llm.StreamEvent, error) bool) {
		p.calls++
		if p.calls == 1 {
			if !y(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: "restricted", ToolName: "mcp__gated__action"}, nil) {
				return
			}
			if !y(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: `{}`}, nil) {
				return
			}
		} else {
			if !y(llm.StreamEvent{Type: llm.SETextDelta, Text: "done"}, nil) {
				return
			}
		}
		if !y(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "stop"}, nil) {
			return
		}
		y(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	}
}
func TestNativeRegressionLockedToolCannotExecute(t *testing.T) {
	prior := ToolAugment
	defer func() { ToolAugment = prior }()
	executed := 0
	unlock := tool.NewUnlockSet()
	ToolAugment = func(context.Context, string) ([]tool.CoreTool, DeferredInfo, func()) {
		base := tool.Build(tool.Spec{Name: "mcp__gated__action", Run: func(context.Context, json.RawMessage, *tool.ToolContext) (tool.Result, error) {
			executed++
			return tool.Text("ran"), nil
		}})
		return []tool.CoreTool{base}, DeferredInfo{Deferred: []string{base.Name()}, Unlock: unlock}, func() {}
	}
	c := NewChatAgent(&reviewGateProvider{}, "mock", t.TempDir(), 64000)
	_, err := c.Chat(context.Background(), "chat", "gate", "hello", 3, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 {
		t.Fatalf("locked deferred tool executed %d times; unlock=%v", executed, unlock.Has("mcp__gated__action"))
	}
}
