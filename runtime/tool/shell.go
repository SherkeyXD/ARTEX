package tool

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// InteractiveShellDisabled reports whether interactive shell sessions are turned off
// via AGENT_CORE_DISABLE_INTERACTIVE_SHELL.
func InteractiveShellDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_CORE_DISABLE_INTERACTIVE_SHELL"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ShellSessionTools returns the five interactive-shell tools.
func ShellSessionTools() []CoreTool {
	return []CoreTool{
		NewShellOpen(),
		NewShellSend(),
		NewShellRead(),
		NewShellClose(),
		NewShellList(),
	}
}

// NewShellOpen opens an interactive shell session.
func NewShellOpen() CoreTool {
	return Build(Spec{
		Name:        "shell_open",
		Description: "Open a persistent PTY interactive session for interactive/TTY tools. Single-shot non-interactive commands should use Bash.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":          map[string]any{"type": "string", "description": "The command to run"},
				"mode":             map[string]any{"type": "string", "description": "interactive | hands-free | dispatch | monitor"},
				"watch":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Keywords to watch in monitor mode"},
				"prompt_regex":     map[string]any{"type": "string", "description": "Prompt regex pattern"},
				"rows":             map[string]any{"type": "integer", "description": "Rows"},
				"cols":             map[string]any{"type": "integer", "description": "Cols"},
				"quiet_ms":         map[string]any{"type": "integer", "description": "Quiet threshold in ms"},
				"startup_grace_ms": map[string]any{"type": "integer", "description": "Startup grace in ms"},
			},
			"required": []string{"command"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			if InteractiveShellDisabled() {
				return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
			}
			return Errorf("interactive shell is not supported in this runtime environment"), nil
		},
	})
}

// NewShellSend sends input to an open shell session.
func NewShellSend() CoreTool {
	return Build(Spec{
		Name:        "shell_send",
		Description: "Send text or keys to an existing shell session.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{"type": "string", "description": "The target shell session ID"},
				"input":      map[string]any{"type": "string", "description": "Text input to send"},
				"key":        map[string]any{"type": "string", "description": "Special key name"},
			},
			"required": []string{"session_id"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			if InteractiveShellDisabled() {
				return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
			}
			return Errorf("shell session not found"), nil
		},
	})
}

// NewShellRead reads output from an open shell session.
func NewShellRead() CoreTool {
	return Build(Spec{
		Name:        "shell_read",
		Description: "Read output from an open shell session.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{"type": "string", "description": "The target shell session ID"},
				"screen":     map[string]any{"type": "boolean", "description": "Whether to return rendered screen snapshot"},
			},
			"required": []string{"session_id"},
		},
		ReadOnly: func(json.RawMessage) bool { return true },
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			if InteractiveShellDisabled() {
				return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
			}
			return Errorf("shell session not found"), nil
		},
	})
}

// NewShellClose closes an open shell session.
func NewShellClose() CoreTool {
	return Build(Spec{
		Name:        "shell_close",
		Description: "Close an open shell session.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id": map[string]any{"type": "string", "description": "The target shell session ID"},
			},
			"required": []string{"session_id"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			if InteractiveShellDisabled() {
				return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
			}
			return Text("closed"), nil
		},
	})
}

// NewShellList lists open shell sessions.
func NewShellList() CoreTool {
	return Build(Spec{
		Name:        "shell_list",
		Description: "List active interactive shell sessions.",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		ReadOnly: func(json.RawMessage) bool { return true },
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			if InteractiveShellDisabled() {
				return Errorf("交互式 shell 已被全局停用(AGENT_CORE_DISABLE_INTERACTIVE_SHELL)"), nil
			}
			return Text("[]"), nil
		},
	})
}
