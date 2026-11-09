package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func InteractiveShellDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_CORE_DISABLE_INTERACTIVE_SHELL"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func ShellSessionTools() []CoreTool {
	return []CoreTool{NewShellOpen(), NewShellSend(), NewShellRead(), NewShellClose(), NewShellList()}
}

func shellTool(name, description string, schema map[string]any, run func(context.Context, *ShellManager, json.RawMessage) (any, error)) CoreTool {
	return Build(Spec{Name: name, Description: description, Schema: schema, Run: func(ctx context.Context, input json.RawMessage, _ *ToolContext) (Result, error) {
		if InteractiveShellDisabled() {
			return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
		}
		manager := ShellManagerFrom(ctx)
		if manager == nil {
			return Errorf("interactive shell manager is not attached to this run"), nil
		}
		value, err := run(ctx, manager, input)
		if err != nil {
			return Result{}, err
		}
		data, err := json.Marshal(value)
		if err != nil {
			return Result{}, err
		}
		return Text(string(data)), nil
	}})
}

func NewShellOpen() CoreTool {
	return shellTool("shell_open", "Open a persistent PTY in this agent run. Use shell_send/read for interaction and shell_close when finished.", map[string]any{"type": "object", "properties": map[string]any{
		"command": map[string]any{"type": "string"}, "mode": map[string]any{"type": "string", "enum": []string{"interactive", "hands-free", "dispatch", "monitor"}}, "watch": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "prompt_regex": map[string]any{"type": "string"}, "rows": map[string]any{"type": "integer"}, "cols": map[string]any{"type": "integer"}, "quiet_ms": map[string]any{"type": "integer"}, "startup_grace_ms": map[string]any{"type": "integer"}}, "required": []string{"command"}}, func(ctx context.Context, manager *ShellManager, input json.RawMessage) (any, error) {
		var args ShellOpenOptions
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		return manager.Open(ctx, args)
	})
}

func NewShellSend() CoreTool {
	return shellTool("shell_send", "Send text or a terminal key to an existing shell, then read its new output.", map[string]any{"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}, "input": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"}}, "required": []string{"session_id"}}, func(ctx context.Context, manager *ShellManager, input json.RawMessage) (any, error) {
		var args struct {
			SessionID string `json:"session_id"`
			Input     string `json:"input"`
			Key       string `json:"key"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		return manager.Send(ctx, args.SessionID, args.Input, args.Key)
	})
}

func NewShellRead() CoreTool {
	return shellTool("shell_read", "Read new shell output or the rendered terminal screen.", map[string]any{"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}, "screen": map[string]any{"type": "boolean"}}, "required": []string{"session_id"}}, func(ctx context.Context, manager *ShellManager, input json.RawMessage) (any, error) {
		var args struct {
			SessionID string `json:"session_id"`
			Screen    bool   `json:"screen"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		return manager.Read(ctx, args.SessionID, args.Screen)
	})
}

func NewShellClose() CoreTool {
	return shellTool("shell_close", "Terminate a shell and its child processes, releasing its PTY.", map[string]any{"type": "object", "properties": map[string]any{"session_id": map[string]any{"type": "string"}}, "required": []string{"session_id"}}, func(_ context.Context, manager *ShellManager, input json.RawMessage) (any, error) {
		var args struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		if err := manager.CloseSession(args.SessionID); err != nil {
			return nil, err
		}
		return map[string]any{"session_id": args.SessionID, "closed": true}, nil
	})
}

func NewShellList() CoreTool {
	return shellTool("shell_list", "List interactive shells belonging to this agent run.", map[string]any{"type": "object", "properties": map[string]any{}}, func(_ context.Context, manager *ShellManager, _ json.RawMessage) (any, error) {
		return manager.List(), nil
	})
}

type shellManagerKey struct{}

func WithShellManager(ctx context.Context, manager *ShellManager) context.Context {
	return context.WithValue(ctx, shellManagerKey{}, manager)
}
func ShellManagerFrom(ctx context.Context) *ShellManager {
	manager, _ := ctx.Value(shellManagerKey{}).(*ShellManager)
	return manager
}

func shellKey(key string) (string, error) {
	switch strings.ToLower(key) {
	case "":
		return "", nil
	case "enter", "return":
		return "\r", nil
	case "ctrl-c":
		return "\x03", nil
	case "ctrl-d":
		return "\x04", nil
	case "ctrl-z":
		return "\x1a", nil
	case "tab":
		return "\t", nil
	case "escape", "esc":
		return "\x1b", nil
	case "up":
		return "\x1b[A", nil
	case "down":
		return "\x1b[B", nil
	case "right":
		return "\x1b[C", nil
	case "left":
		return "\x1b[D", nil
	case "backspace":
		return "\x7f", nil
	default:
		return "", fmt.Errorf("unknown terminal key %q", key)
	}
}
