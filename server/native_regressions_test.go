package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SherkeyXD/ARTEX/agent"
	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/runtime/skill"
	tool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

type reviewRecorder struct{ calls int }

func (r *reviewRecorder) InsertToolUsage(*db.ToolUsage) error { r.calls++; return nil }
func TestNativeRegressionExecutePreservesToolMetering(t *testing.T) {
	r := &reviewRecorder{}
	base := tool.Build(tool.Spec{Name: "x", Run: func(context.Context, json.RawMessage, *tool.ToolContext) (tool.Result, error) {
		return tool.Text("ok"), nil
	}})
	wrapped := meterTool(base, r, "x", "worker", agent.RunInfo{})
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 1 {
		t.Fatalf("usage records=%d, expected 1", r.calls)
	}
}

type nativeSkillRecorder struct{ rows []*db.SkillUsage }

func (r *nativeSkillRecorder) InsertSkillUsage(row *db.SkillUsage) error {
	r.rows = append(r.rows, row)
	return nil
}

func TestNativeRegressionExecutePreservesSkillMetering(t *testing.T) {
	r := &nativeSkillRecorder{}
	reg := skill.NewRegistry(skill.Skill{Name: "display-name", Dir: "/skills/procedure", Instructions: "follow these steps"})
	wrapped := &meteredSkill{CoreTool: reg.Tool(), pg: r, reg: reg, agentKey: "chat", ri: agent.RunInfo{SessionID: "conv-1"}}
	for _, name := range []string{"display-name", "missing"} {
		input, _ := json.Marshal(map[string]string{"name": name})
		_, _ = wrapped.Execute(context.Background(), input)
	}
	if len(r.rows) != 2 || !r.rows[0].Found || r.rows[0].Skill != "procedure" || r.rows[1].Found || r.rows[0].SessionID != "conv-1" {
		t.Fatalf("Skill usage ledger lost: %+v", r.rows)
	}
}
