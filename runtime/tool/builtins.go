package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SherkeyXD/ARTEX/llm"
)

// DefaultTools returns the standard OS toolset available to all agents.
func DefaultTools() []CoreTool {
	return []CoreTool{
		BashTool(),
		ReadTool(),
		WriteTool(),
		EditTool(),
		LSTool(),
		GlobTool(),
		GrepTool(),
		SleepTool(),
	}
}

func resolvePath(tc *ToolContext, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	base := "."
	if tc != nil && tc.WorkingDir != "" {
		base = tc.WorkingDir
	}
	return filepath.Join(base, path)
}

// NewBash returns the command execution tool (alias for BashTool).
func NewBash() CoreTool { return BashTool() }

// NewRead returns the read tool (alias for ReadTool).
func NewRead() CoreTool { return ReadTool() }

// NewWrite returns the write tool (alias for WriteTool).
func NewWrite() CoreTool { return WriteTool() }

// NewEdit returns the edit tool (alias for EditTool).
func NewEdit() CoreTool { return EditTool() }

// NewLS returns the directory listing tool (alias for LSTool).
func NewLS() CoreTool { return LSTool() }

// NewGlob returns the file matching tool (alias for GlobTool).
func NewGlob() CoreTool { return GlobTool() }

// NewGrep returns the search tool (alias for GrepTool).
func NewGrep() CoreTool { return GrepTool() }

// NewSleep returns the sleep tool (alias for SleepTool).
func NewSleep() CoreTool { return SleepTool() }

func formatError(format string, args ...any) Result {
	if len(args) == 0 {
		return Errorf(format)
	}
	return Errorf(fmt.Sprintf(format, args...))
}

// BashTool returns the command execution tool.
func BashTool() CoreTool {
	return Build(Spec{
		Name:        "Bash",
		Description: "Run commands in a bash shell. For long running commands, consider running them in the background.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The command line string to execute",
				},
			},
			"required": []string{"command"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid bash arguments: %v", err), nil
			}
			cmdStr := strings.TrimSpace(args.Command)
			if cmdStr == "" {
				return formatError("command cannot be empty"), nil
			}

			cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
			if tc != nil && tc.WorkingDir != "" {
				cmd.Dir = tc.WorkingDir
			}
			env := os.Environ()
			if tc != nil && len(tc.Env) > 0 {
				env = append(env, tc.Env...)
			}
			cmd.Env = env

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()
			outStr := stdout.String()
			errStr := stderr.String()

			combined := outStr
			if errStr != "" {
				if combined != "" && !strings.HasSuffix(combined, "\n") {
					combined += "\n"
				}
				combined += errStr
			}
			combined = strings.TrimSpace(combined)

			if runErr != nil {
				if combined == "" {
					combined = runErr.Error()
				}
				return Result{
					Content: []llm.ContentBlock{llm.TextBlock(combined)},
					IsError: true,
				}, nil
			}

			if combined == "" {
				combined = "(command completed with no output)"
			}
			return Text(combined), nil
		},
	})
}

// ReadTool returns the file reading tool.
func ReadTool() CoreTool {
	return Build(Spec{
		Name:        "Read",
		Description: "Read the contents of a file.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path of the file to read",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Optional line offset to begin reading from",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of lines to read",
				},
			},
			"required": []string{"path"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Path     string `json:"path"`
				FilePath string `json:"file_path"`
				Offset   int    `json:"offset"`
				Limit    int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid read arguments: %v", err), nil
			}
			p := args.Path
			if p == "" {
				p = args.FilePath
			}
			target := resolvePath(tc, p)
			data, err := os.ReadFile(target)
			if err != nil {
				return formatError("failed to read file %q: %v", p, err), nil
			}

			lines := strings.Split(string(data), "\n")
			start := args.Offset
			if start < 0 {
				start = 0
			}
			if start >= len(lines) {
				return Text(""), nil
			}

			end := len(lines)
			if args.Limit > 0 && start+args.Limit < end {
				end = start + args.Limit
			}

			return Text(strings.Join(lines[start:end], "\n")), nil
		},
	})
}

// WriteTool returns the file writing tool.
func WriteTool() CoreTool {
	return Build(Spec{
		Name:        "Write",
		Description: "Write content to a file, replacing its contents if it exists.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path of the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write",
				},
			},
			"required": []string{"path", "content"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Path     string `json:"path"`
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid write arguments: %v", err), nil
			}
			p := args.Path
			if p == "" {
				p = args.FilePath
			}
			target := resolvePath(tc, p)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return formatError("failed to create directory for %q: %v", p, err), nil
			}
			if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
				return formatError("failed to write file %q: %v", p, err), nil
			}
			return Text(fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), p)), nil
		},
	})
}

// EditTool returns the exact-match file editing tool.
func EditTool() CoreTool {
	return Build(Spec{
		Name:        "Edit",
		Description: "Replace text in a file. old_string must match exactly and appear only once in the file.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path of the file to edit",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "Exact text to find and replace",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "Replacement text",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Path      string `json:"path"`
				FilePath  string `json:"file_path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid edit arguments: %v", err), nil
			}
			p := args.Path
			if p == "" {
				p = args.FilePath
			}
			target := resolvePath(tc, p)
			data, err := os.ReadFile(target)
			if err != nil {
				return formatError("failed to read file for edit: %v", err), nil
			}

			content := string(data)
			count := strings.Count(content, args.OldString)
			if count == 0 {
				return formatError("old_string was not found in file %q", p), nil
			}
			if count > 1 {
				return formatError("old_string matched %d times in file %q; must be uniquely identifiable", count, p), nil
			}

			updated := strings.Replace(content, args.OldString, args.NewString, 1)
			if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
				return formatError("failed to save edited file: %v", err), nil
			}
			return Text(fmt.Sprintf("Successfully edited %s", p)), nil
		},
	})
}

// LSTool returns directory listing tool.
func LSTool() CoreTool {
	return Build(Spec{
		Name:        "LS",
		Description: "List files and directories in a given path.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to list (defaults to working directory)",
				},
			},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(input, &args)
			target := resolvePath(tc, args.Path)
			if target == "" {
				target = "."
			}

			entries, err := os.ReadDir(target)
			if err != nil {
				return formatError("failed to list directory %q: %v", target, err), nil
			}

			var sb strings.Builder
			for _, e := range entries {
				info, err := e.Info()
				if err != nil {
					continue
				}
				suffix := ""
				if e.IsDir() {
					suffix = "/"
				}
				sb.WriteString(fmt.Sprintf("%10d %s %s%s\n", info.Size(), info.ModTime().Format("2006-01-02 15:04"), e.Name(), suffix))
			}
			return Text(strings.TrimRight(sb.String(), "\n")), nil
		},
	})
}

// GlobTool returns the filename pattern matching tool.
func GlobTool() CoreTool {
	return Build(Spec{
		Name:        "Glob",
		Description: "Find files matching a glob pattern.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern (e.g. *.go, src/**/*.js)",
				},
			},
			"required": []string{"pattern"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Pattern string `json:"pattern"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid glob arguments: %v", err), nil
			}
			base := "."
			if tc != nil && tc.WorkingDir != "" {
				base = tc.WorkingDir
			}

			var matches []string
			err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				rel, relErr := filepath.Rel(base, path)
				if relErr != nil {
					rel = path
				}
				matched, matchErr := filepath.Match(args.Pattern, d.Name())
				if matchErr == nil && matched {
					matches = append(matches, rel)
				}
				return nil
			})
			if err != nil {
				return formatError("glob failed: %v", err), nil
			}
			return Text(strings.Join(matches, "\n")), nil
		},
	})
}

// GrepTool returns regex file searching tool.
func GrepTool() CoreTool {
	return Build(Spec{
		Name:        "Grep",
		Description: "Search files for a regular expression pattern.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regular expression pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Path to file or directory to search in",
				},
			},
			"required": []string{"pattern"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Pattern  string `json:"pattern"`
				Path     string `json:"path"`
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid grep arguments: %v", err), nil
			}
			p := args.Path
			if p == "" {
				p = args.FilePath
			}
			re, err := regexp.Compile(args.Pattern)
			if err != nil {
				return formatError("invalid regex pattern %q: %v", args.Pattern, err), nil
			}

			target := resolvePath(tc, p)
			var sb strings.Builder
			count := 0
			_ = filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				data, readErr := os.ReadFile(p)
				if readErr != nil {
					return nil
				}
				lines := strings.Split(string(data), "\n")
				for lineNum, line := range lines {
					if re.MatchString(line) {
						sb.WriteString(fmt.Sprintf("%s:%d:%s\n", p, lineNum+1, line))
						count++
						if count > 200 {
							return fs.SkipAll
						}
					}
				}
				return nil
			})
			return Text(strings.TrimRight(sb.String(), "\n")), nil
		},
	})
}

// SleepTool returns a context-cancellable pause tool.
func SleepTool() CoreTool {
	return Build(Spec{
		Name:        "Sleep",
		Description: "Pause execution for a given number of seconds.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"seconds": map[string]any{
					"type":        "number",
					"description": "Number of seconds to sleep",
				},
			},
			"required": []string{"seconds"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Seconds float64 `json:"seconds"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return formatError("invalid sleep arguments: %v", err), nil
			}
			dur := time.Duration(args.Seconds * float64(time.Second))
			select {
			case <-time.After(dur):
				return Text(fmt.Sprintf("Slept for %.1f seconds", args.Seconds)), nil
			case <-ctx.Done():
				return formatError("sleep cancelled: %v", ctx.Err()), nil
			}
		},
	})
}
