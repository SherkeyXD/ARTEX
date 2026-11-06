package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/runtime"
	"github.com/SherkeyXD/ARTEX/runtime/adapter"
	_ "github.com/SherkeyXD/ARTEX/runtime/harness/pi"
	"github.com/SherkeyXD/ARTEX/runtime/middleware"
	"github.com/Autumn-27/norma/harness"
	actool "github.com/Autumn-27/norma/tool"
)

// HarnessKind identifies the underlying agent execution harness.
type HarnessKind string

const (
	HarnessNorma HarnessKind = "norma"
	HarnessPi    HarnessKind = "pi"
)

// ActiveHarness returns the configured harness (from ARTEX_HARNESS env, defaulting to norma).
func ActiveHarness() HarnessKind {
	h := os.Getenv("ARTEX_HARNESS")
	if h == "pi" {
		return HarnessPi
	}
	return HarnessNorma
}

// runWorkerWithPi executes the worker intent using the Pi Harness.
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
	// 1. Adapt existing tools to runtime.Tool
	universalTools := adapter.WrapNormaTools(tools)

	// 2. Adapt LLM provider
	llmProv := adapter.WrapNormaProvider(w.prov)

	// 3. Create Session
	sessionID := WorkerSessionID(explorationID(ts), intent.ID)
	session := runtime.NewSession(sessionID, name, fmt.Sprintf("%d", taskID))

	// 4. Set up Middleware chain: Approval Guard + Metrics
	guard := middleware.NewGuardMiddleware(nil) // approval handled via standard capture/intercept
	metrics := middleware.NewMetricsRecorder(nil)
	chain := runtime.NewMiddlewareChain(guard, metrics)

	// 5. Build AgentRunner
	cfg := runtime.AgentConfig{
		Name:            name,
		SystemPrompt:    sysBody,
		Tools:           universalTools,
		DriverID:        "pi",
		ModelName:       w.model,
		MaxTokens:       w.maxTokens(),
		MaxIterations:   w.maxTurns,
		ToolOutputLimit: 4000,
		Provider:        llmProv,
	}

	runner, err := runtime.NewAgentRunner(cfg, nil, chain, nil)
	if err != nil {
		return harness.ReasonModelError, fmt.Errorf("create pi runner: %w", err)
	}

	// 6. Execute turn with unified activity capture
	_, reason, err := captureRuntimeTurn(ctx, runner, session, input, emitWrap)
	return reason, err
}
