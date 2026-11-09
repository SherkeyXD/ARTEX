package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SherkeyXD/ARTEX/runtime"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

func addNetworkTools(tools []actool.CoreTool, options WebSearchOpts) ([]actool.CoreTool, error) {
	tools = append(tools, actool.NewWebFetch())
	if options.Enabled {
		search, err := actool.NewWebSearch(actool.WebSearchConfig{Backend: options.Backend, BraveAPIKey: options.BraveKey, TavilyAPIKey: options.TavilyKey, DeepSeekBaseURL: options.DeepSeekBaseURL, DeepSeekAPIKey: options.DeepSeekAPIKey, DeepSeekModel: options.DeepSeekModel, Proxy: options.Proxy})
		if err != nil {
			return nil, err
		}
		tools = append(tools, search)
	}
	return tools, nil
}

func appendMissingTools(tools []runtime.Tool, extra ...actool.CoreTool) []runtime.Tool {
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	for _, tool := range extra {
		if !names[tool.Name()] {
			tools = append(tools, tool)
			names[tool.Name()] = true
		}
	}
	return tools
}

type sessionStateMiddleware struct {
	runtime.BaseMiddleware
	session *runtime.Session
	todos   *actool.TodoStore
	calls   map[string]*runtime.ToolCallData
}

func (m *sessionStateMiddleware) OnBeforeToolExec(_ context.Context, _ *runtime.TurnContext, call *runtime.ToolCallData) (bool, string, error) {
	m.calls[call.ID] = call
	return true, "", nil
}

func (m *sessionStateMiddleware) OnAfterToolExec(_ context.Context, _ *runtime.TurnContext, result *runtime.ToolResultData, _ time.Duration) {
	m.session.SetMetadata("todos", m.todos.List())
	call := m.calls[result.ToolCallID]
	delete(m.calls, result.ToolCallID)
	if call == nil || call.Name != "Skill" || result.IsError {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(call.Arguments, &input) != nil || input.Name == "" {
		return
	}
	names := []string{}
	if current, ok := m.session.GetMetadata("loaded_skills"); ok {
		data, _ := json.Marshal(current)
		_ = json.Unmarshal(data, &names)
	}
	for _, name := range names {
		if name == input.Name {
			return
		}
	}
	m.session.SetMetadata("loaded_skills", append(names, input.Name))
}

type deferredTool struct {
	runtime.Tool
	unlock *actool.UnlockSet
}

func (t *deferredTool) SchemaVisible() bool { return false }
func (t *deferredTool) Execute(ctx context.Context, input json.RawMessage) (any, error) {
	if !t.unlock.Has(t.Name()) {
		return nil, fmt.Errorf("tool %q is locked; load its assigned skill first", t.Name())
	}
	return t.Tool.Execute(ctx, input)
}

type deferredGate struct {
	runtime.BaseMiddleware
	names  map[string]bool
	unlock *actool.UnlockSet
}

func (g *deferredGate) OnBeforeToolExec(_ context.Context, _ *runtime.TurnContext, call *runtime.ToolCallData) (bool, string, error) {
	if g.names[call.Name] && !g.unlock.Has(call.Name) {
		return false, "工具尚未解锁，请先加载绑定的 Skill", nil
	}
	return true, "", nil
}

// ExecuteExtraTool is expanded to the real target before guard hooks run, so
// deferred execution cannot bypass approval, audit or settlement restrictions.
type deferredExecutor struct {
	runtime.Tool
	names map[string]bool
}

func (t *deferredExecutor) ExpandCall(call *runtime.ToolCallData) error {
	var input struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return err
	}
	if !t.names[input.Name] {
		return fmt.Errorf("unknown deferred tool %q", input.Name)
	}
	if len(input.Input) == 0 {
		input.Input = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(input.Input, &object) != nil || object == nil {
		return fmt.Errorf("deferred tool input must be an object")
	}
	call.Name, call.Arguments = input.Name, input.Input
	return nil
}

func configureDeferredTools(tools []runtime.Tool, def DeferredInfo, chain *runtime.MiddlewareChain) ([]runtime.Tool, *runtime.MiddlewareChain) {
	names := make(map[string]bool)
	for _, name := range def.Deferred {
		names[name] = true
	}
	byName := make(map[string]runtime.Tool)
	out := make([]runtime.Tool, 0, len(tools)+2)
	for _, tool := range tools {
		byName[tool.Name()] = tool
		if names[tool.Name()] {
			tool = &deferredTool{Tool: tool, unlock: def.Unlock}
		}
		out = append(out, tool)
	}
	chain.Add(&deferredGate{names: names, unlock: def.Unlock})
	search := runtime.NewFuncTool("SearchExtraTools", "Find unlocked deferred tools by name or purpose and return their parameter schemas.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`), func(_ context.Context, input json.RawMessage) (any, error) {
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		var results []runtime.ToolDefinition
		keys := make([]string, 0, len(names))
		for name := range names {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			tool := byName[name]
			if tool == nil || !def.Unlock.Has(name) {
				continue
			}
			if args.Query != "" && !strings.Contains(strings.ToLower(name+" "+tool.Description()), strings.ToLower(args.Query)) {
				continue
			}
			results = append(results, runtime.ToolDefinition{Name: name, Description: tool.Description(), Parameters: tool.ParametersSchema()})
			if len(results) >= 20 {
				break
			}
		}
		return results, nil
	})
	executor := &deferredExecutor{Tool: runtime.NewFuncTool("ExecuteExtraTool", "Call an unlocked tool discovered with SearchExtraTools. Approval applies to the target tool.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"input":{"type":"object"}},"required":["name","input"]}`), func(context.Context, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("invalid deferred tool call")
	}), names: names}
	out = append(out, search, executor)
	return out, chain
}
