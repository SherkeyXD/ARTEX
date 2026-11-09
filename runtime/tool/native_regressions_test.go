package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
)

func TestNativeRegressionNativeToolPreservesExtraMessages(t *testing.T) {
	base := Build(Spec{Name: "x", Run: func(context.Context, json.RawMessage, *ToolContext) (Result, error) {
		r := Text("ack")
		r.Extra = []llm.Message{llm.UserText("instructions")}
		return r, nil
	}})
	out, err := base.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := out.(runtime.ToolOutput)
	if !ok || len(value.Messages) != 1 {
		t.Fatalf("Extra messages discarded: %#v", out)
	}
}
func TestNativeRegressionNativeToolCapturesLargeOutput(t *testing.T) {
	base := Build(Spec{Name: "x", Run: func(context.Context, json.RawMessage, *ToolContext) (Result, error) {
		return Text(strings.Repeat("x", 100)), nil
	}})
	ctx := WithToolContext(context.Background(), &ToolContext{WorkingDir: t.TempDir(), OutputDir: t.TempDir(), MaxOutputChars: 10})
	out, err := base.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := out.(runtime.ToolOutput)
	if !ok || !value.Captured {
		t.Fatalf("large raw output was not captured: %#v", out)
	}
}
