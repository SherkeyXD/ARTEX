package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/adapter"
	_ "github.com/SherkeyXD/ARTEX/runtime/harness/pi"
	"github.com/SherkeyXD/ARTEX/runtime/middleware"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

// HarnessKind identifies the underlying agent execution harness.
type HarnessKind string

const (
	HarnessPi     HarnessKind = "pi"
	HarnessNorma  HarnessKind = "norma"  // legacy fallback
	HarnessLegacy HarnessKind = "legacy" // alias
)

// ActiveHarness returns the configured harness (from ARTEX_HARNESS env, defaulting to pi).
func ActiveHarness() HarnessKind {
	h := os.Getenv("ARTEX_HARNESS")
	if h == "norma" || h == "legacy" {
		return HarnessNorma
	}
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
	Input             string
	Tools             []actool.CoreTool
	UTools            []runtime.UTool
	UniversalTools    []runtime.Tool
	MaxTurns          int
	MaxTokens         int
	ToolOutputLimit   int
	WorkingDir        string
	Emit              func(db.Activity)
}

// RunUniversalAgent executes one agent turn using the Universal Agent Runtime (Pi Driver).
// It adapts legacy tool and LLM definitions into canonical UTool and LLMProvider interfaces,
// configures middleware pipelines (Guard, Metrics), and captures turn activity streams.
func RunUniversalAgent(ctx context.Context, opts UniversalAgentOptions) (string, harness.TerminalReason, error) {
	// 1. Resolve tools to universal UTools
	var uTools []runtime.Tool
	if len(opts.UTools) > 0 {
		uTools = opts.UTools
	} else if len(opts.UniversalTools) > 0 {
		uTools = opts.UniversalTools
	} else if len(opts.Tools) > 0 {
		uTools = adapter.ToUniversalTools(opts.Tools)
	}

	// 2. Resolve LLM provider
	llmProv := opts.UniversalProvider
	if llmProv == nil && opts.Provider != nil {
		llmProv = adapter.ToUniversalProvider(opts.Provider)
	}

	// 3. Create or resume Session
	session := runtime.NewSession(opts.SessionID, opts.AgentKey, fmt.Sprintf("%d", opts.TaskID))

	// 4. Setup Middleware chain: Guard + Metrics
	guard := middleware.NewGuardMiddleware(nil)
	metrics := middleware.NewMetricsRecorder(nil)
	chain := runtime.NewMiddlewareChain(guard, metrics)

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
		Tools:           uTools,
		DriverID:        "pi",
		ModelName:       opts.Model,
		MaxTokens:       opts.MaxTokens,
		MaxIterations:   opts.MaxTurns,
		ToolOutputLimit: outputLimit,
		Provider:        llmProv,
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
) (harness.TerminalReason, error) {
	sessionID := WorkerSessionID(explorationID(ts), intent.ID)
	_, reason, err := RunUniversalAgent(ctx, UniversalAgentOptions{
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
		ToolOutputLimit: 4000,
		WorkingDir:      ensureRunDir(w.workDir, taskID, intent.ID),
		Emit:            emitWrap,
	})
	return reason, err
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
) (harness.TerminalReason, error) {
	return w.runWorkerWithUniversalRuntime(ctx, name, taskID, ts, intent, emitWrap, input, tools, sysBody)
}
