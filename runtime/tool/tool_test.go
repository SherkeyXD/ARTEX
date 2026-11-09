package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolBuildAndExecute(t *testing.T) {
	tl := Build(Spec{
		Name:        "Echo",
		Description: "Echoes input text",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"msg": map[string]any{"type": "string"},
			},
			"required": []string{"msg"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return Errorf(fmt.Sprintf("parse error: %v", err)), nil
			}
			return Text("Echo: " + args.Msg), nil
		},
	})

	if tl.Name() != "Echo" {
		t.Fatalf("name = %q, want Echo", tl.Name())
	}
	if tl.Description() != "Echoes input text" {
		t.Fatalf("description = %q", tl.Description())
	}

	// 1. Call via CoreTool
	res, err := tl.Call(context.Background(), json.RawMessage(`{"msg":"hello"}`), nil)
	if err != nil || res.IsError || res.Flatten() != "Echo: hello" {
		t.Fatalf("unexpected call result: res=%+v, err=%v", res, err)
	}

	// 2. Call via runtime.Tool Execute
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"msg":"world"}`))
	if err != nil || out != "Echo: world" {
		t.Fatalf("unexpected execute result: out=%v, err=%v", out, err)
	}
}

func TestToolPanicRecovery(t *testing.T) {
	tl := Build(Spec{
		Name: "PanicTool",
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			panic("something went terribly wrong")
		},
	})

	res, err := tl.Call(context.Background(), json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if !res.IsError || !strings.Contains(res.Flatten(), "something went terribly wrong") {
		t.Fatalf("expected error result with panic message, got %+v", res)
	}
}

func TestDefaultToolsFilesAndBash(t *testing.T) {
	dir := t.TempDir()
	tc := &ToolContext{WorkingDir: dir}
	ctx := WithToolContext(context.Background(), tc)

	// 1. Write file
	writeTool := WriteTool()
	res, err := writeTool.Call(ctx, json.RawMessage(`{"path":"hello.txt","content":"line1\nline2\nline3"}`), tc)
	if err != nil || res.IsError {
		t.Fatalf("write failed: %+v, %v", res, err)
	}

	// 2. Read file
	readTool := ReadTool()
	res, err = readTool.Call(ctx, json.RawMessage(`{"path":"hello.txt","offset":1,"limit":1}`), tc)
	if err != nil || res.IsError || res.Flatten() != "line2" {
		t.Fatalf("read failed: %+v, %v", res, err)
	}

	// 3. Edit file
	editTool := EditTool()
	res, err = editTool.Call(ctx, json.RawMessage(`{"path":"hello.txt","old_string":"line2","new_string":"LINE_TWO"}`), tc)
	if err != nil || res.IsError {
		t.Fatalf("edit failed: %+v, %v", res, err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if !strings.Contains(string(content), "LINE_TWO") {
		t.Fatalf("edit did not update file content: %s", content)
	}

	// 4. LS
	lsTool := LSTool()
	res, err = lsTool.Call(ctx, json.RawMessage(`{}`), tc)
	if err != nil || res.IsError || !strings.Contains(res.Flatten(), "hello.txt") {
		t.Fatalf("ls failed: %+v, %v", res, err)
	}

	// 5. Grep
	grepTool := GrepTool()
	res, err = grepTool.Call(ctx, json.RawMessage(`{"pattern":"LINE_TWO","path":"."}`), tc)
	if err != nil || res.IsError || !strings.Contains(res.Flatten(), "LINE_TWO") {
		t.Fatalf("grep failed: %+v, %v", res, err)
	}

	// 6. Bash
	bashTool := BashTool()
	res, err = bashTool.Call(ctx, json.RawMessage(`{"command":"cat hello.txt | grep LINE_TWO"}`), tc)
	if err != nil || res.IsError || !strings.Contains(res.Flatten(), "LINE_TWO") {
		t.Fatalf("bash failed: %+v, %v", res, err)
	}

	// 7. Sleep
	sleepTool := SleepTool()
	start := time.Now()
	res, err = sleepTool.Call(ctx, json.RawMessage(`{"seconds":0.05}`), tc)
	if err != nil || res.IsError || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("sleep failed: %+v, %v", res, err)
	}
}

func TestTodoStore(t *testing.T) {
	store := NewTodoStore()
	if len(store.List()) != 0 {
		t.Fatal("new store must be empty")
	}

	wTool := store.TodoWrite()
	input := `{"todos":[{"content":"recon","status":"in_progress"},{"content":"exploit","status":"pending"}]}`
	res, err := wTool.Call(context.Background(), json.RawMessage(input), nil)
	if err != nil || res.IsError {
		t.Fatalf("TodoWrite failed: %+v, %v", res, err)
	}

	list := store.List()
	if len(list) != 2 || list[0].Status != TodoInProgress {
		t.Fatalf("unexpected todos: %+v", list)
	}

	rTool := store.TodoRead()
	res, err = rTool.Call(context.Background(), json.RawMessage(`{}`), nil)
	if err != nil || res.IsError || !strings.Contains(res.Flatten(), "recon") {
		t.Fatalf("TodoRead failed: %+v, %v", res, err)
	}
}

func TestRenderDeferredToolsBlock(t *testing.T) {
	if RenderDeferredToolsBlock(nil) != "" {
		t.Fatal("empty slice should produce empty string")
	}
	out := RenderDeferredToolsBlock([]string{"tool_a", "tool_b"})
	if !strings.Contains(out, "<available-deferred-tools>") || !strings.Contains(out, "tool_a") {
		t.Fatalf("unexpected deferred block output: %s", out)
	}
}
