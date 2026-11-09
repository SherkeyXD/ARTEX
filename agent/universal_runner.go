package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/guard"
	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/adapter"
	_ "github.com/SherkeyXD/ARTEX/runtime/harness/pi"
	"github.com/SherkeyXD/ARTEX/runtime/middleware"
	actool "github.com/SherkeyXD/ARTEX/runtime/tool"
)

// HarnessKind identifies the underlying agent execution harness.
type HarnessKind string

const (
	HarnessPi HarnessKind = "pi"
)

// ActiveHarness returns the configured harness (defaults to pi).
func ActiveHarness() HarnessKind {
	return HarnessPi
}

// UniversalAgentOptions contains all execution parameters needed to run an agent turn
// under the Universal Agent Runtime.
type UniversalAgentOptions struct {
	AgentKey          string
	SessionID         string
	TaskID            int64
	Model             string
	Provider          llm.Provider
	UniversalProvider runtime.LLMProvider
	SystemPrompt      string
	SystemPrompts     []string
	DynamicBoundary   int
	NonStreaming      bool
	MaxDuration       time.Duration
	Settlement        *runtime.Settlement
	Compaction        *runtime.CompactionConfig
	SessionDir        string
	Ephemeral         bool
	ToolOutputDir     string
	Hooks             guard.HookRunner
	Input             string
	Tools             []actool.CoreTool
	UniversalTools    []runtime.Tool
	MaxTurns          int
	MaxTokens         int
	ToolOutputLimit   int
	WorkingDir        string
	Guard             *guard.Guard
	GuardFunc         middleware.GuardFunc
	Emit              func(db.Activity)
	Deferred          DeferredInfo
	Todos             *actool.TodoStore
}

// RunUniversalAgent executes one agent turn using the Universal Agent Runtime (Pi Driver).
// It adapts legacy tool and LLM definitions into canonical runtime.Tool and LLMProvider interfaces,
// configures middleware pipelines (Guard, Metrics), and captures turn activity streams.
func RunUniversalAgent(ctx context.Context, opts UniversalAgentOptions) (string, runtime.TerminalReason, error) {
	if opts.WorkingDir != "" {
		var err error
		opts.WorkingDir, err = filepath.Abs(opts.WorkingDir)
		if err != nil {
			return "", runtime.ReasonModelError, err
		}
	}
	ctx = llm.WithSessionID(ctx, opts.SessionID)
	// 1. Resolve tools to universal runtime.Tools
	var tools []runtime.Tool
	if len(opts.UniversalTools) > 0 {
		tools = opts.UniversalTools
	} else if len(opts.Tools) > 0 {
		tools = adapter.ToUniversalTools(opts.Tools)
	}

	// 2. Resolve LLM provider
	llmProv := opts.UniversalProvider
	if llmProv == nil && opts.Provider != nil {
		llmProv = adapter.ToUniversalProvider(opts.Provider)
	}

	// 3. Create or resume Session
	sessionDir := opts.SessionDir
	if sessionDir == "" && opts.WorkingDir != "" {
		sessionDir = filepath.Join(opts.WorkingDir, ".artex", "runtime-sessions")
	}
	if opts.Ephemeral || opts.SessionID == "" {
		sessionDir = ""
	}
	session, release, err := runtime.OpenSession(ctx, sessionDir, opts.SessionID, opts.AgentKey, fmt.Sprintf("%d", opts.TaskID))
	if err != nil {
		reason := runtime.ReasonModelError
		if ctx.Err() != nil {
			reason = runtime.ReasonAbortedStreaming
		}
		return "", reason, err
	}
	defer release()
	shells := actool.NewShellManager(ctx, opts.WorkingDir, nil)
	defer shells.Close()
	ctx = actool.WithShellManager(ctx, shells)
	if opts.Todos == nil {
		opts.Todos = actool.NewTodoStore()
	}
	if value, ok := session.GetMetadata("todos"); ok {
		data, marshalErr := json.Marshal(value)
		var todos []actool.Todo
		if marshalErr != nil {
			return "", runtime.ReasonModelError, marshalErr
		}
		if err := json.Unmarshal(data, &todos); err != nil {
			return "", runtime.ReasonModelError, fmt.Errorf("restore todos: %w", err)
		}
		opts.Todos.Set(todos)
	}
	defer func() { session.SetMetadata("todos", opts.Todos.List()); _ = session.Checkpoint() }()
	tools = appendMissingTools(tools, opts.Todos.TodoWrite(), opts.Todos.TodoRead())
	seedUnlockFromHistory(adapter.ToProviderMessages(session.Messages()), opts.Deferred.UnlockSkill)
	if names, ok := session.GetMetadata("loaded_skills"); ok && opts.Deferred.UnlockSkill != nil {
		data, _ := json.Marshal(names)
		var restored []string
		_ = json.Unmarshal(data, &restored)
		for _, name := range restored {
			opts.Deferred.UnlockSkill(name)
		}
	}

	// 4. Setup Middleware chain: Guard + Metrics
	var guardFunc middleware.GuardFunc
	if opts.GuardFunc != nil {
		guardFunc = opts.GuardFunc
	}
	guardMw := middleware.NewGuardMiddleware(guardFunc)
	metrics := middleware.NewMetricsRecorder(nil)
	chain := runtime.NewMiddlewareChain(guardMw, metrics)
	chain.Add(&sessionStateMiddleware{session: session, todos: opts.Todos, calls: make(map[string]*runtime.ToolCallData)})
	if len(opts.Deferred.Deferred) > 0 {
		tools, chain = configureDeferredTools(tools, opts.Deferred, chain)
	}
	hooks := opts.Hooks
	if hooks == nil && opts.Guard != nil {
		hooks = opts.Guard.Hooks()
	}
	if hooks != nil {
		chain.Add(&runtimeHooks{hooks: hooks, inputs: make(map[string][]byte)})
	}

	// 5. Build AgentConfig
	outputLimit := opts.ToolOutputLimit
	if outputLimit <= 0 {
		outputLimit = 4000
	}

	sysPrompt := opts.SystemPrompt
	if len(opts.SystemPrompts) > 0 {
		sysPrompt = strings.Join(opts.SystemPrompts, "\n\n")
	}

	cfg := runtime.AgentConfig{
		Name:            opts.AgentKey,
		SystemPrompt:    sysPrompt,
		SystemPrompts:   opts.SystemPrompts,
		DynamicBoundary: opts.DynamicBoundary,
		NonStreaming:    opts.NonStreaming,
		MaxDuration:     opts.MaxDuration,
		Settlement:      opts.Settlement,
		Compaction:      opts.Compaction,
		WorkingDir:      opts.WorkingDir,
		ToolOutputDir:   opts.ToolOutputDir,
		Tools:           tools,
		DriverID:        "pi",
		ModelName:       opts.Model,
		MaxTokens:       opts.MaxTokens,
		MaxIterations:   opts.MaxTurns,
		ToolOutputLimit: outputLimit,
		Provider:        llmProv,
	}
	if cfg.ToolOutputDir == "" && opts.WorkingDir != "" {
		cfg.ToolOutputDir = filepath.Join(opts.WorkingDir, "cmd-output")
	}

	// 6. Execute turn with unified activity capture
	return captureRuntimeTurn(ctx, cfg, session, chain, opts.Input, opts.Emit)
}

// runWorkerWithUniversalRuntime executes the worker intent using Universal Agent Runtime.
func (w *Worker) runWorkerWithUniversalRuntime(
	ctx context.Context,
	name string,
	taskID int64,
	ts *db.ExplorationStore,
	intent *db.Node,
	emitWrap func(db.Activity),
	input string,
	tools []actool.CoreTool,
	sysBody string,
	hooks guard.HookRunner,
	settings ...UniversalAgentOptions,
) (runtime.TerminalReason, error) {
	sessionID := WorkerSessionID(explorationID(ts), intent.ID)
	clock := taskClockFrom(ctx)
	maxDuration, clamped := clampMaxDuration(clock.DeadlineUnix, w.runTimeout)
	options := UniversalAgentOptions{
		AgentKey:        name,
		SessionID:       sessionID,
		TaskID:          taskID,
		Model:           w.model,
		Provider:        w.prov,
		SystemPrompt:    sysBody,
		Input:           input,
		Tools:           tools,
		MaxTurns:        w.maxTurns,
		MaxTokens:       w.maxTokens(),
		NonStreaming:    w.nonStreaming(),
		MaxDuration:     maxDuration,
		Settlement:      wrapupSettlementForTask("worker", []string{"Bash", "shell_open", "shell_send"}, clamped),
		Compaction:      compactionConfig(w.compactionWindow()),
		ToolOutputLimit: 4000,
		WorkingDir:      ensureRunDir(w.workDir, taskID, intent.ID),
		Hooks:           hooks,
		Emit:            emitWrap,
	}
	if len(settings) > 0 {
		options.SystemPrompts = settings[0].SystemPrompts
		options.DynamicBoundary = settings[0].DynamicBoundary
		options.Deferred = settings[0].Deferred
	}
	_, reason, err := RunUniversalAgent(ctx, options)
	return reason, err
}

type runtimeHooks struct {
	runtime.BaseMiddleware
	hooks  guard.HookRunner
	inputs map[string][]byte
}

func (h *runtimeHooks) OnBeforeToolExec(ctx context.Context, _ *runtime.TurnContext, call *runtime.ToolCallData) (bool, string, error) {
	blocked, reason, updated := h.hooks.PreToolUse(ctx, call.Name, call.Arguments)
	if len(updated) > 0 {
		if !json.Valid(updated) {
			return false, "", fmt.Errorf("hook returned invalid tool arguments")
		}
		call.Arguments = append(json.RawMessage(nil), updated...)
	}
	h.inputs[call.ID] = append([]byte(nil), call.Arguments...)
	return !blocked, reason, nil
}

func (h *runtimeHooks) OnAfterToolExec(ctx context.Context, _ *runtime.TurnContext, result *runtime.ToolResultData, _ time.Duration) {
	if input, ok := h.inputs[result.ToolCallID]; ok {
		h.hooks.PostToolUse(ctx, result.ToolName, input, []byte(result.Content), result.IsError)
		delete(h.inputs, result.ToolCallID)
	}
}

// runWorkerWithPi is a backward-compatible alias for runWorkerWithUniversalRuntime.
func (w *Worker) runWorkerWithPi(
	ctx context.Context,
	name string,
	taskID int64,
	ts *db.ExplorationStore,
	intent *db.Node,
	emitWrap func(db.Activity),
	input string,
	tools []actool.CoreTool,
	sysBody string,
) (runtime.TerminalReason, error) {
	return w.runWorkerWithUniversalRuntime(ctx, name, taskID, ts, intent, emitWrap, input, tools, sysBody, nil)
}
