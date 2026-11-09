package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/skill"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

func TestDeferredDispatchHonorsUnlockApprovalAndResume(t *testing.T) {
	dir := t.TempDir()
	prior := ToolAugment
	defer func() { ToolAugment = prior }()
	executed := 0
	ToolAugment = func(context.Context, string) ([]actool.CoreTool, DeferredInfo, func()) {
		unlock := actool.NewUnlockSet()
		reg := skill.NewRegistry(skill.Skill{Name: "procedure", Description: "Operate target", Instructions: strings.Repeat("procedure instructions\n", 400), MCPs: []string{"restricted"}})
		unlockSkill := func(name string) {
			if name == "procedure" {
				unlock.Add("mcp__restricted__action")
			}
		}
		reg.OnInvoke = func(s skill.Skill) string { unlockSkill(s.Name); return "Target tool unlocked." }
		target := actool.Build(actool.Spec{Name: "mcp__restricted__action", Schema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}, Run: func(_ context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			executed++
			if !strings.Contains(string(input), "approved") {
				t.Errorf("target did not receive approved arguments: %s", input)
			}
			return actool.Text("target result"), nil
		}})
		return []actool.CoreTool{reg.Tool(), target}, DeferredInfo{Deferred: []string{target.Name()}, Unlock: unlock, UnlockSkill: unlockSkill}, func() {}
	}
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		for _, definition := range req.Tools {
			if definition.Name == "mcp__restricted__action" {
				t.Error("deferred schema eagerly advertised")
			}
		}
		switch calls {
		case 1:
			return behaviorMessage(behaviorCall("locked", "ExecuteExtraTool", `{"name":"mcp__restricted__action","input":{"value":"original"}}`)), "tool_use", llm.Usage{}, nil
		case 2:
			if !req.Messages[len(req.Messages)-1].Content[0].IsError || executed != 0 {
				t.Fatal("locked dispatch executed")
			}
			return behaviorMessage(behaviorCall("skill", "Skill", `{"name":"procedure"}`)), "tool_use", llm.Usage{}, nil
		case 3:
			if !strings.Contains(req.Messages[len(req.Messages)-1].Text(), "procedure instructions") {
				t.Fatal("full Skill instructions were truncated or discarded")
			}
			return behaviorMessage(behaviorCall("search", "SearchExtraTools", `{"query":"action"}`)), "tool_use", llm.Usage{}, nil
		case 4:
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content[0].Content[0].Text, "value") {
				t.Fatal("deferred discovery lost parameter schema")
			}
			return behaviorMessage(behaviorCall("dispatch", "ExecuteExtraTool", `{"name":"mcp__restricted__action","input":{"value":"original"}}`)), "tool_use", llm.Usage{}, nil
		case 5:
			return behaviorMessage(llm.TextBlock("finished first turn")), "stop", llm.Usage{}, nil
		case 6:
			return behaviorMessage(behaviorCall("resumed", "ExecuteExtraTool", `{"name":"mcp__restricted__action","input":{"value":"original"}}`)), "tool_use", llm.Usage{}, nil
		default:
			return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
		}
	}}
	guardCalls := 0
	hooks := &dispatchHooks{check: func(name string, input []byte) (bool, string, []byte) {
		if name == "ExecuteExtraTool" {
			t.Fatal("approval saw dispatch wrapper instead of actual tool")
		}
		if name == "mcp__restricted__action" {
			guardCalls++
			return false, "", []byte(`{"value":"approved"}`)
		}
		return false, "", nil
	}}
	for i := 0; i < 2; i++ {
		tools, def, cleanup := AugmentTools(context.Background(), "chat", nil)
		_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "chat", SessionID: "deferred", WorkingDir: dir, Provider: provider, Tools: tools, Deferred: def, Hooks: hooks, NonStreaming: true, Input: "continue", MaxTurns: 8})
		cleanup()
		if err != nil || reason != runtime.ReasonCompleted {
			t.Fatalf("reason=%s err=%v", reason, err)
		}
	}
	if executed != 2 || guardCalls != 2 {
		t.Fatalf("executed=%d guarded=%d; expected unlocked targets to be approved exactly once", executed, guardCalls)
	}
}

type dispatchHooks struct {
	check func(string, []byte) (bool, string, []byte)
}

func (h *dispatchHooks) PreToolUse(_ context.Context, name string, input []byte) (bool, string, []byte) {
	return h.check(name, input)
}
func (h *dispatchHooks) PostToolUse(context.Context, string, []byte, []byte, bool) {}

func TestDeferredDispatchCannotBypassSettlementToolRestriction(t *testing.T) {
	count, executed := 0, 0
	target := actool.Build(actool.Spec{Name: "Bash", Run: func(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
		executed++
		return actool.Text("ran"), nil
	}})
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		count++
		if count <= 2 {
			return behaviorMessage(behaviorCall("call", "ExecuteExtraTool", `{"name":"Bash","input":{}}`)), "tool_use", llm.Usage{}, nil
		}
		if !req.Messages[len(req.Messages)-1].Content[0].IsError {
			t.Fatal("disabled target executed through dispatcher")
		}
		return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
	}}
	_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "worker", SessionID: "settlement-dispatch", WorkingDir: t.TempDir(), Provider: provider, Input: "work", Tools: []actool.CoreTool{target}, Deferred: DeferredInfo{Deferred: []string{"Bash"}, Unlock: actool.NewUnlockSet("Bash")}, NonStreaming: true, MaxTurns: 1, Settlement: &runtime.Settlement{Prompt: "settle", DisabledTools: []string{"Bash"}, MaxTurns: 2}})
	if err != nil || reason != runtime.ReasonMaxTurns || executed != 1 {
		t.Fatalf("reason=%s err=%v executed=%d", reason, err, executed)
	}
}

func TestSessionTodosPersistAcrossColdResume(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		switch calls {
		case 1:
			return behaviorMessage(behaviorCall("write", "TodoWrite", `{"todos":[{"content":"carry this task","status":"pending"}]}`)), "tool_use", llm.Usage{}, nil
		case 2:
			return behaviorMessage(llm.TextBlock("recorded")), "stop", llm.Usage{}, nil
		case 3:
			return behaviorMessage(behaviorCall("read", "TodoRead", `{}`)), "tool_use", llm.Usage{}, nil
		default:
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content[0].Content[0].Text, "carry this task") {
				t.Fatal("persisted todos lost")
			}
			return behaviorMessage(llm.TextBlock("done")), "stop", llm.Usage{}, nil
		}
	}}
	for i := 0; i < 2; i++ {
		chat := NewChatAgent(provider, "mock", dir, 64000)
		chat.SetNonStreaming(func() bool { return true })
		if _, err := chat.Chat(context.Background(), "chat", "todos", "continue", 3, 0, false, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNetworkToolsRespectSearchToggle(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		tools, err := addNetworkTools(nil, WebSearchOpts{Enabled: enabled})
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, tool := range tools {
			found[tool.Name()] = true
		}
		if !found["WebFetch"] || found["web_search"] != enabled {
			t.Fatalf("enabled=%v tools=%v", enabled, found)
		}
	}
}

func TestFailedSkillCallDoesNotRestoreUnlock(t *testing.T) {
	messages := []llm.Message{behaviorMessage(behaviorCall("call", "Skill", `{"name":"missing"}`)), {Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("call", "error", true)}}}
	seedUnlockFromHistory(messages, func(name string) { t.Fatalf("failed skill %q unlocked on resume", name) })
}

func TestMalformedDeferredDispatchIsToolError(t *testing.T) {
	executed, calls := 0, 0
	target := actool.Build(actool.Spec{Name: "target", Run: func(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
		executed++
		return actool.Text("ran"), nil
	}})
	provider := &behaviorProvider{complete: func(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		calls++
		if calls == 1 {
			return behaviorMessage(behaviorCall("malformed", "ExecuteExtraTool", `{"name":"target","input":"invalid"}`)), "tool_use", llm.Usage{}, nil
		}
		result := req.Messages[len(req.Messages)-1].Content[0]
		if !result.IsError || !strings.Contains(result.Content[0].Text, "must be an object") {
			t.Fatalf("dispatch failure lost: %+v", result)
		}
		return behaviorMessage(llm.TextBlock("recovered")), "stop", llm.Usage{}, nil
	}}
	_, reason, err := RunUniversalAgent(context.Background(), UniversalAgentOptions{AgentKey: "chat", SessionID: "invalid-dispatch", Provider: provider, Input: "work", Tools: []actool.CoreTool{target}, Deferred: DeferredInfo{Deferred: []string{"target"}, Unlock: actool.NewUnlockSet("target")}, NonStreaming: true, MaxTurns: 3})
	if err != nil || reason != runtime.ReasonCompleted || executed != 0 {
		t.Fatalf("reason=%s err=%v executed=%d", reason, err, executed)
	}
}
