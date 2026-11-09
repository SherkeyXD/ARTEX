package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
)

// Result is the outcome of a tool call.
type Result struct {
	Content []llm.ContentBlock
	IsError bool
	Extra   []llm.Message
}

// Text builds a successful Result containing one text block.
func Text(s string) Result {
	return Result{
		Content: []llm.ContentBlock{llm.TextBlock(s)},
	}
}

// Errorf builds an error Result containing one text block marked as error.
// Signature matches actool.Errorf(s string) for full compatibility.
func Errorf(s string) Result {
	return Result{
		Content: []llm.ContentBlock{llm.TextBlock(s)},
		IsError: true,
	}
}

// Flatten joins all text content blocks into a single string.
func (r Result) Flatten() string {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type == llm.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// ToolContext carries execution metadata into a tool invocation.
type ToolContext struct {
	WorkingDir     string
	AgentID        string
	ToolUseID      string
	MaxOutputChars int
	OutputDir      string
	Env            []string
}

type toolContextKey struct{}

// WithToolContext attaches a ToolContext to the context.
func WithToolContext(ctx context.Context, tc *ToolContext) context.Context {
	return context.WithValue(ctx, toolContextKey{}, tc)
}

// ToolContextFrom retrieves the ToolContext from context, or nil if unset.
func ToolContextFrom(ctx context.Context) *ToolContext {
	if tc, ok := ctx.Value(toolContextKey{}).(*ToolContext); ok && tc != nil {
		return tc
	}
	exec := runtime.ToolExecutionFrom(ctx)
	if exec.WorkingDir != "" || exec.AgentID != "" || exec.ToolUseID != "" || exec.OutputDir != "" || exec.MaxOutputChars > 0 {
		return &ToolContext{
			WorkingDir:     exec.WorkingDir,
			AgentID:        exec.AgentID,
			ToolUseID:      exec.ToolUseID,
			OutputDir:      exec.OutputDir,
			MaxOutputChars: exec.MaxOutputChars,
		}
	}
	return nil
}

// CoreTool is the universal tool interface for ARTEX.
// It simultaneously satisfies runtime.Tool (for Universal Agent Runtime)
// and legacy CoreTool methods for seamless drop-in compatibility.
type CoreTool interface {
	runtime.Tool
	InputSchema() map[string]any
	Call(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error)
}

// Spec declares a tool's identity, parameter schema, and execution handler.
type Spec struct {
	Name        string
	Description string
	Prompt      string
	Schema      map[string]any
	ReadOnly    func(json.RawMessage) bool
	Concurrent  func(json.RawMessage) bool
	Run         func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error)
	RawInput    bool
}

type builtTool struct {
	spec      Spec
	rawSchema json.RawMessage
}

// Build constructs a CoreTool from a Spec with fail-safe defaults.
func Build(s Spec) CoreTool {
	schema := s.Schema
	if schema == nil {
		schema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		raw = json.RawMessage(`{"type":"object"}`)
	}
	return &builtTool{
		spec:      s,
		rawSchema: raw,
	}
}

func (b *builtTool) Name() string {
	return b.spec.Name
}

func (b *builtTool) Description() string {
	return b.spec.Description
}

func (b *builtTool) InputSchema() map[string]any {
	return b.spec.Schema
}

func (b *builtTool) ParametersSchema() json.RawMessage {
	return b.rawSchema
}

func (b *builtTool) Call(ctx context.Context, input json.RawMessage, tc *ToolContext) (res Result, err error) {
	if tc == nil {
		tc = ToolContextFrom(ctx)
	}
	if tc != nil {
		ctx = WithToolContext(ctx, tc)
	}
	defer func() {
		if r := recover(); r != nil {
			res = Errorf(fmt.Sprintf("tool panic: %v", r))
			err = nil
		}
	}()
	if b.spec.Run == nil {
		return Errorf(fmt.Sprintf("tool %q has no run handler", b.spec.Name)), nil
	}
	return b.spec.Run(ctx, input, tc)
}

func (b *builtTool) Execute(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	tc := ToolContextFrom(ctx)
	res, err := b.Call(ctx, rawArgs, tc)
	if err != nil {
		return nil, err
	}
	text := res.Flatten()
	if res.IsError {
		return nil, fmt.Errorf("%s", text)
	}
	if strings.Contains(text, "<persisted-output>") || strings.Contains(text, "characters truncated]") {
		return runtime.ToolOutput{Content: text, Captured: true}, nil
	}
	return text, nil
}
