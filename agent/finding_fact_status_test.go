package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusToolsValidation(t *testing.T) {
	ts := NewToolSet(nil, "worker-1")

	// 1. update_finding_status nil store refusal
	ufTool := ts.updateFindingStatus()
	res, err := ufTool.Call(context.Background(), json.RawMessage(`{"finding_id": 1, "status": "fixed", "reason": "patched"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "任务上下文") {
		t.Fatalf("want task context refusal, got: %s", res.Flatten())
	}

	// 2. update_finding_status invalid status
	res, err = ufTool.Call(context.Background(), json.RawMessage(`{"finding_id": 1, "status": "invalid_xyz", "reason": "test"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "无效的漏洞状态") {
		t.Fatalf("want invalid status error, got: %s", res.Flatten())
	}

	// 3. update_fact_status nil store refusal
	ufactTool := ts.updateFactStatus()
	res, err = ufactTool.Call(context.Background(), json.RawMessage(`{"fact_id": 2, "state": "invalidated", "reason": "port closed"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "任务上下文") {
		t.Fatalf("want task context refusal, got: %s", res.Flatten())
	}
}

func TestToolsInclusionInWorkerAndPlanner(t *testing.T) {
	ts := NewToolSet(nil, "")
	hasTool := func(tools []string, target string) bool {
		for _, name := range tools {
			if name == target {
				return true
			}
		}
		return false
	}

	getNames := func(tools []actoolTool) []string {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Name())
		}
		return names
	}

	workerNames := getNames(wrapTools(ts.WorkerTools()))
	plannerNames := getNames(wrapTools(ts.PlannerTools()))

	if !hasTool(workerNames, "update_finding_status") {
		t.Errorf("WorkerTools missing update_finding_status")
	}
	if !hasTool(workerNames, "update_fact_status") {
		t.Errorf("WorkerTools missing update_fact_status")
	}

	if !hasTool(plannerNames, "update_finding_status") {
		t.Errorf("PlannerTools missing update_finding_status")
	}
	if !hasTool(plannerNames, "update_fact_status") {
		t.Errorf("PlannerTools missing update_fact_status")
	}
}

type actoolTool interface {
	Name() string
}

func wrapTools[T actoolTool](tools []T) []actoolTool {
	out := make([]actoolTool, len(tools))
	for i, t := range tools {
		out[i] = t
	}
	return out
}
