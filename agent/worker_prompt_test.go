package agent

import (
	"strings"
	"testing"

	"github.com/SherkeyXD/ARTEX/db"
)

func TestWorkerSystemPromptIsStaticAcrossIntents(t *testing.T) {
	// workerSystem should produce identical byte-for-byte system prompts
	// regardless of what intent or task is being executed, enabling prefix cache hits.
	sys1 := workerSystem("/data")
	sys2 := workerSystem("/data")

	if sys1 != sys2 {
		t.Fatalf("workerSystem output is not deterministic:\nsys1:\n%s\n\nsys2:\n%s", sys1, sys2)
	}

	// Dynamic intent paths (e.g. /tasks/1/i10) must NOT appear in the system prompt.
	if strings.Contains(sys1, "/tasks/") || strings.Contains(sys1, "i10") {
		t.Fatalf("workerSystem contains dynamic run dir path: %q", sys1)
	}

	// Dynamic intent ID / payload must NOT appear in the system prompt.
	if strings.Contains(sys1, "【你领到的意图") || strings.Contains(sys1, "意图 id:") {
		t.Fatalf("workerSystem contains dynamic intent payload/id: %q", sys1)
	}

	// Relative path instruction must be present.
	if !strings.Contains(sys1, "当前工作目录") || !strings.Contains(sys1, "相对路径") {
		t.Fatalf("workerSystem missing relative path instruction: %q", sys1)
	}
}

func TestRenderWorkerLaunchInputPackagesIntentAndOverview(t *testing.T) {
	intent := &db.Node{
		ID:      42,
		Payload: []byte(`{"goal":"探测 8080 端口 HTTP 服务"}`),
	}
	targetAssets := "本意图 asset_ids 对应的目标资产：\n[{\"id\":101,\"domain\":\"example.com\"}]"
	overview := "\n\n【全局探索态势（只读）】：\n{\"facts\":[]}"

	input := renderWorkerLaunchInput(intent, targetAssets, overview)

	// User input must contain the intent ID and payload.
	if !strings.Contains(input, "意图 id: 42") {
		t.Fatalf("input missing intent id: %s", input)
	}
	if !strings.Contains(input, "探测 8080 端口 HTTP 服务") {
		t.Fatalf("input missing intent payload: %s", input)
	}

	// User input must contain the target assets.
	if !strings.Contains(input, "example.com") {
		t.Fatalf("input missing target assets: %s", input)
	}

	// User input must contain the kick-off instruction.
	if !strings.Contains(input, "开始执行上面这条意图：只做它、只产生事实、assets、finding、做完即停。") {
		t.Fatalf("input missing kick-off directive: %s", input)
	}

	// User input must contain the overview.
	if !strings.Contains(input, "全局探索态势") {
		t.Fatalf("input missing overview: %s", input)
	}
}
