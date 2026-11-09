package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SherkeyXD/ARTEX/runtime/adapter"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

func TestToUniversalTool(t *testing.T) {
	legacyCoreTool := actool.Build(actool.Spec{
		Name:        "test_tool",
		Description: "A test tool",
		Schema: map[string]any{
			"type": "object",
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return actool.Text("result-from-tool"), nil
		},
	})

	universalTool := adapter.ToUniversalTool(legacyCoreTool)
	if universalTool == nil {
		t.Fatal("expected non-nil universal tool")
	}

	if universalTool.Name() != "test_tool" {
		t.Fatalf("expected tool name test_tool, got %q", universalTool.Name())
	}

	if universalTool.Description() != "A test tool" {
		t.Fatalf("expected description 'A test tool', got %q", universalTool.Description())
	}

	res, err := universalTool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	resStr, ok := res.(string)
	if !ok || resStr != "result-from-tool" {
		t.Fatalf("unexpected execute result: %v", res)
	}
}
