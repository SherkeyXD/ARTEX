package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Tool is the universal interface that all agent tools must satisfy.
// It is completely decoupled from any external framework.
type Tool interface {
	Name() string
	Description() string
	ParametersSchema() json.RawMessage
	Execute(ctx context.Context, rawArgs json.RawMessage) (any, error)
}

// UniversalTool is an alias for Tool.
type UniversalTool = Tool

// ToolFunc is a signature for functional tool execution.
type ToolFunc func(ctx context.Context, rawArgs json.RawMessage) (any, error)

// BaseTool is a convenience struct implementing Tool.
type BaseTool struct {
	ToolName        string          `json:"name"`
	ToolDescription string          `json:"description"`
	Schema          json.RawMessage `json:"parameters"`
	Handler         ToolFunc        `json:"-"`
}

func (b *BaseTool) Name() string                        { return b.ToolName }
func (b *BaseTool) Description() string                 { return b.ToolDescription }
func (b *BaseTool) ParametersSchema() json.RawMessage   { return b.Schema }
func (b *BaseTool) Execute(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	if b.Handler == nil {
		return nil, fmt.Errorf("tool %q has no execution handler", b.ToolName)
	}
	return b.Handler(ctx, rawArgs)
}

// NewFuncTool creates a new Tool from a function and schema.
func NewFuncTool(name, description string, schema json.RawMessage, fn ToolFunc) Tool {
	return &BaseTool{
		ToolName:        name,
		ToolDescription: description,
		Schema:          schema,
		Handler:         fn,
	}
}

// ToolRegistry manages a collection of tools available to an Agent.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register registers one or more tools.
func (r *ToolRegistry) Register(tools ...Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range tools {
		if t != nil && t.Name() != "" {
			r.tools[t.Name()] = t
		}
	}
}

// Get looks up a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns a slice of all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		res = append(res, t)
	}
	return res
}

// Names returns all registered tool names.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}
