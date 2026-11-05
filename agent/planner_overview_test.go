package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDistillOverviewForPromptPrunesBloatedFields(t *testing.T) {
	// Build a heavily bloated overview data structure mimicking late-stage task graphs.
	bloatedDigests := make([]map[string]any, 15)
	for i := 0; i < 15; i++ {
		bloatedDigests[i] = map[string]any{
			"id":           int64(i + 1),
			"body":         strings.Repeat(fmt.Sprintf("这是冷区总结段落 %d，包含了大量历史事实的综合归纳。", i), 10),
			"member_count": 20,
		}
	}

	bloatedRelated := make([]map[string]any, 3)
	for i := 0; i < 3; i++ {
		bloatedRelated[i] = map[string]any{
			"task_id":        int64(100 + i),
			"title":          fmt.Sprintf("关联任务 %d", i),
			"goal":           "获取核心数据库权限",
			"findings_count": 5,
			"facts_count":    30,
			"huge_payload":   strings.Repeat("这是继承黑板中的巨型文本块，包含了海量跨任务节点记录。", 200),
		}
	}

	openIntents := make([]map[string]any, 25)
	for i := 0; i < 25; i++ {
		openIntents[i] = map[string]any{"id": int64(200 + i), "summary": fmt.Sprintf("开放意图 %d", i)}
	}

	recentFacts := make([]map[string]any, 20)
	for i := 0; i < 20; i++ {
		recentFacts[i] = map[string]any{"id": int64(300 + i), "summary": fmt.Sprintf("事实 %d", i)}
	}

	recentDone := make([]map[string]any, 12)
	for i := 0; i < 12; i++ {
		recentDone[i] = map[string]any{"id": int64(400 + i), "summary": fmt.Sprintf("完成意图 %d", i)}
	}

	findings := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		findings[i] = map[string]any{"id": int64(500 + i), "summary": fmt.Sprintf("漏洞 %d", i)}
	}

	original := map[string]any{
		"task": map[string]any{
			"description": "测试任务",
			"goal":        "拿下目标核心",
		},
		"goals": []map[string]any{
			{"id": int64(1), "state": "open", "text": "获取目标系统最高控制权"},
		},
		"coverage": map[string]any{
			"pct":         45,
			"tested":      9,
			"denominator": 20,
			"host_count":  3,
		},
		"frontier_open":       25,
		"done_intents_total":  12,
		"facts":               20,
		"findings_total":      10,
		"cold_digests":        bloatedDigests,
		"related_tasks":       bloatedRelated,
		"open_intents":        openIntents,
		"recent_done_intents": recentDone,
		"recent_facts":        recentFacts,
		"finding_list":        findings,
	}

	origJSON, _ := json.Marshal(original)
	distilled := distillOverviewForPrompt(original)
	distilledJSON, _ := json.Marshal(distilled)

	t.Logf("Original size: %d bytes, Distilled size: %d bytes", len(origJSON), len(distilledJSON))

	// Reduction should be massive (>70%).
	if len(distilledJSON) >= len(origJSON)*3/10 {
		t.Fatalf("expected at least 70%% reduction, got orig=%d, distilled=%d", len(origJSON), len(distilledJSON))
	}

	// Essential fields must be preserved.
	if distilled["task"] == nil || distilled["goals"] == nil || distilled["coverage"] == nil {
		t.Fatalf("critical fields missing from distilled output")
	}

	// Limits enforced on lists:
	if oi := distilled["open_intents"].([]map[string]any); len(oi) > 10 {
		t.Fatalf("open_intents cap not respected: got %d", len(oi))
	}
	if rf := distilled["recent_facts"].([]map[string]any); len(rf) > 6 {
		t.Fatalf("recent_facts cap not respected: got %d", len(rf))
	}
	if rdi := distilled["recent_done_intents"].([]map[string]any); len(rdi) > 5 {
		t.Fatalf("recent_done_intents cap not respected: got %d", len(rdi))
	}
	if fl := distilled["finding_list"].([]map[string]any); len(fl) > 5 {
		t.Fatalf("finding_list cap not respected: got %d", len(fl))
	}

	// Check cold_digests does not have the massive body.
	if cds := distilled["cold_digests"].([]map[string]any); len(cds) > 0 {
		if _, hasBody := cds[0]["body"]; hasBody {
			t.Fatalf("cold_digests body should not be retained uncompacted")
		}
		if sm, ok := cds[0]["summary"].(string); !ok || len([]rune(sm)) > 120 {
			t.Fatalf("cold_digests summary too long or missing: %v", cds[0]["summary"])
		}
	}

	// Check related_tasks does not have huge_payload.
	if rts := distilled["related_tasks"].([]map[string]any); len(rts) > 0 {
		if _, hasHuge := rts[0]["huge_payload"]; hasHuge {
			t.Fatalf("related_tasks huge_payload should have been stripped")
		}
	}
}

func TestRenderGraphOverviewFormatsDistilledOutput(t *testing.T) {
	data := map[string]any{
		"goals": []map[string]any{
			{"id": int64(1), "state": "open", "text": "目标1"},
		},
		"facts": 3,
	}

	rendered := renderGraphOverview(data)
	if !strings.Contains(rendered, "本轮精简态势") {
		t.Fatalf("rendered string missing lean overview header: %s", rendered)
	}
	if !strings.Contains(rendered, "目标1") {
		t.Fatalf("rendered string missing goal content: %s", rendered)
	}
}

func TestRenderTriggersFindingStatusChange(t *testing.T) {
	evs := []TriggerEvent{
		{
			Kind:     "finding",
			IntentID: 15,
			Detail:   "漏洞 #42 状态变更为 fixed: 验证补丁已生效",
		},
		{
			Kind:     "finding",
			IntentID: 16,
			Detail:   "SQL注入漏洞在 /api/login 处被确认",
		},
	}

	out := renderTriggers(nil, evs)
	if !strings.Contains(out, "更新了状态：漏洞 #42 状态变更为 fixed") {
		t.Fatalf("status change event not rendered properly: %s", out)
	}
	if !strings.Contains(out, "报告了一个 finding：SQL注入漏洞") {
		t.Fatalf("normal finding report event not rendered properly: %s", out)
	}
}
