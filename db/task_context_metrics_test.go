package db

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestTaskContextMetrics(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	task, err := d.CreateTask("context test", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM tasks WHERE id=$1`, task.ID)
	expID := task.ExplorationID

	store := d.Exploration(expID)

	// Append activities
	metaTool, _ := json.Marshal(map[string]any{"duration_ms": 1500, "char_count": 2048})
	_, err = store.AppendActivity(Activity{
		Worker:   "worker",
		Kind:     "tool_result",
		Tool:     "Bash",
		Summary:  "nmap -sV target.com",
		Detail:   "Starting Nmap scan on target.com...",
		Metadata: metaTool,
	})
	if err != nil {
		t.Fatal(err)
	}

	metaThink, _ := json.Marshal(map[string]any{"duration_ms": 2500, "char_count": 512})
	_, err = store.AppendActivity(Activity{
		Worker:   "planner",
		Kind:     "thinking",
		Summary:  "Thinking about attack vectors",
		Detail:   "I should check for open web ports first.",
		Metadata: metaThink,
	})
	if err != nil {
		t.Fatal(err)
	}

	metaRes, _ := json.Marshal(map[string]any{"total_ms": 4000, "ttft_ms": 350})
	inTok, outTok, cRead := 1000, 200, 400
	_, err = store.AppendActivity(Activity{
		Worker:          "planner",
		Kind:            "result",
		Summary:         "Done planning",
		Detail:          "Created 2 intents.",
		Metadata:        metaRes,
		InputTokens:     &inTok,
		OutputTokens:    &outTok,
		CacheReadTokens: &cRead,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert LLM usage
	taskIDStr := fmt.Sprint(task.ID)
	_ = d.InsertLLMUsage(&LLMUsage{
		TaskID:        taskIDStr,
		ExplorationID: expID,
		Worker:        "planner",
		Model:         "deepseek-reasoner",
		ProfileName:   "default",
		LatencyMs:     3800,
		InputTokens:   1000,
		OutputTokens:  200,
		CacheRead:     400,
		Status:        "ok",
	})

	report, err := d.TaskContextMetrics(task.ID, expID)
	if err != nil {
		t.Fatalf("TaskContextMetrics failed: %v", err)
	}

	if report.Summary.TotalTokens != 1200 {
		t.Errorf("expected total tokens 1200, got %d", report.Summary.TotalTokens)
	}
	if report.Summary.ToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", report.Summary.ToolCalls)
	}
	if report.Summary.ThinkingDurationMs != 2500 {
		t.Errorf("expected thinking duration 2500, got %d", report.Summary.ThinkingDurationMs)
	}
	if report.Summary.ToolDurationMs != 1500 {
		t.Errorf("expected tool duration 1500, got %d", report.Summary.ToolDurationMs)
	}
	if len(report.Composition.TopToolOutputs) != 1 {
		t.Fatalf("expected 1 top tool output, got %d", len(report.Composition.TopToolOutputs))
	}
	if report.Composition.TopToolOutputs[0].Tool != "Bash" {
		t.Errorf("expected tool Bash, got %s", report.Composition.TopToolOutputs[0].Tool)
	}
	if len(report.Turns) != 1 {
		t.Errorf("expected 1 evolution turn, got %d", len(report.Turns))
	}

	globalReport, err := d.GlobalContextMetrics(30)
	if err != nil {
		t.Fatalf("GlobalContextMetrics failed: %v", err)
	}
	if globalReport.Summary.TotalTokens < 1200 {
		t.Errorf("expected global total tokens >= 1200, got %d", globalReport.Summary.TotalTokens)
	}
	if len(globalReport.Composition.TopToolOutputs) < 1 {
		t.Errorf("expected at least 1 top tool output in global report, got %d", len(globalReport.Composition.TopToolOutputs))
	}
}
