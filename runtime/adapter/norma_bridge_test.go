package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Autumn-27/artex/runtime/adapter"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func TestWrapNormaTool(t *testing.T) {
	normaCoreTool := actool.Build(actool.Spec{
		Name:        "test_tool",
		Description: "A tool for testing the adapter",
		Schema:      map[string]any{"type": "object"},
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision { return acperm.Allowed() },
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			return actool.Text("result-from-norma-tool"), nil
		},
	})

	universalTool := adapter.WrapNormaTool(normaCoreTool)
	if universalTool == nil {
		t.Fatalf("expected non-nil wrapped tool")
	}

	if universalTool.Name() != "test_tool" {
		t.Fatalf("unexpected name: %s", universalTool.Name())
	}
	if universalTool.Description() != "A tool for testing the adapter" {
		t.Fatalf("unexpected desc: %s", universalTool.Description())
	}

	res, err := universalTool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	resStr, ok := res.(string)
	if !ok || resStr != "result-from-norma-tool" {
		t.Fatalf("unexpected result: %v", res)
	}
}
