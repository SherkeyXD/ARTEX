package runtime

import (
	"context"
	"fmt"
	"time"
)

// AgentConfig defines the configuration of an agent.
type AgentConfig struct {
	Name            string
	SystemPrompt    string
	SystemPrompts   []string
	DynamicBoundary int
	NonStreaming    bool
	WorkingDir      string
	ToolOutputDir   string
	MaxDuration     time.Duration
	Settlement      *Settlement
	Compaction      *CompactionConfig
	Tools           []Tool
	DriverID        string // e.g. "pi" (defaults to "pi")
	ModelName       string
	ModelProfile    string
	Temperature     float64
	MaxTokens       int
	MaxIterations   int
	ToolOutputLimit int
	ReasoningEffort string
	ThinkingType    string
	Provider        LLMProvider
}

// AgentRunner coordinates turn execution using the universal runtime.
type AgentRunner struct {
	config      AgentConfig
	driver      HarnessDriver
	middlewares *MiddlewareChain
	sink        EventSink
}

// NewAgentRunner constructs an AgentRunner from configuration.
func NewAgentRunner(cfg AgentConfig, driver HarnessDriver, middlewares *MiddlewareChain, sink EventSink) (*AgentRunner, error) {
	if driver == nil {
		driverID := cfg.DriverID
		if driverID == "" {
			driverID = "pi"
		}
		var err error
		driver, err = GetDriver(driverID)
		if err != nil {
			return nil, fmt.Errorf("resolve driver %q: %w", driverID, err)
		}
	}
	if middlewares == nil {
		middlewares = NewMiddlewareChain()
	}
	return &AgentRunner{
		config:      cfg,
		driver:      driver,
		middlewares: middlewares,
		sink:        sink,
	}, nil
}

// RunTurn executes one agent interaction turn.
func (r *AgentRunner) RunTurn(ctx context.Context, session *Session, userPrompt string) (*TurnResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session cannot be nil")
	}

	turnIndex := session.Len() / 2
	tc := &TurnContext{
		SessionID:   session.ID,
		AgentID:     r.config.Name,
		TaskID:      session.TaskID,
		TurnIndex:   turnIndex,
		InputPrompt: userPrompt,
		Metadata:    make(map[string]any),
	}

	// 1. Append user message if provided
	if userPrompt != "" {
		session.AppendMessage(NewUserMessage(userPrompt))
	}
	if err := session.Checkpoint(); err != nil {
		return &TurnResult{Reason: ReasonModelError, Error: err}, err
	}

	// 2. Lifecycle: OnBeforeTurn
	if err := r.middlewares.OnBeforeTurn(ctx, tc); err != nil {
		return nil, fmt.Errorf("middleware before turn: %w", err)
	}

	// 3. Emit turn start event
	if r.sink != nil {
		r.sink.Emit(ctx, Event{
			Type:      EventTurnStart,
			Timestamp: time.Now(),
			SessionID: session.ID,
			AgentID:   r.config.Name,
			TurnIndex: turnIndex,
			Text:      userPrompt,
		})
	}

	// 4. Prepare TurnOptions
	opts := TurnOptions{
		ModelProfile:    r.config.ModelProfile,
		ModelName:       r.config.ModelName,
		SystemPrompt:    r.config.SystemPrompt,
		SystemPrompts:   r.config.SystemPrompts,
		DynamicBoundary: r.config.DynamicBoundary,
		NonStreaming:    r.config.NonStreaming,
		WorkingDir:      r.config.WorkingDir,
		ToolOutputDir:   r.config.ToolOutputDir,
		MaxDuration:     r.config.MaxDuration,
		Settlement:      r.config.Settlement,
		Compaction:      r.config.Compaction,
		Temperature:     r.config.Temperature,
		MaxTokens:       r.config.MaxTokens,
		MaxIterations:   r.config.MaxIterations,
		ToolOutputLimit: r.config.ToolOutputLimit,
		ReasoningEffort: r.config.ReasoningEffort,
		ThinkingType:    r.config.ThinkingType,
		Tools:           r.config.Tools,
		Provider:        r.config.Provider,
	}

	// 5. Delegate to HarnessDriver
	turnResult, err := r.driver.ExecuteTurn(ctx, session, opts, r.middlewares, r.sink)
	if err != nil {
		if r.sink != nil {
			r.sink.Emit(ctx, Event{
				Type:      EventError,
				Timestamp: time.Now(),
				SessionID: session.ID,
				AgentID:   r.config.Name,
				TurnIndex: turnIndex,
				Error:     err.Error(),
			})
		}
	}
	if turnResult == nil {
		return nil, err
	}

	// 6. Accumulate tokens to session
	session.RecordUsage(turnResult.TokenUsage)
	if saveErr := session.Checkpoint(); saveErr != nil {
		if err == nil {
			err = saveErr
		}
		turnResult.Error = err
	}

	// 7. Lifecycle: OnAfterTurn
	r.middlewares.OnAfterTurn(ctx, tc, turnResult)

	// 8. Emit turn end event
	if r.sink != nil {
		r.sink.Emit(ctx, Event{
			Type:       EventTurnEnd,
			Timestamp:  time.Now(),
			SessionID:  session.ID,
			AgentID:    r.config.Name,
			TurnIndex:  turnIndex,
			Text:       turnResult.FinalText,
			TokenUsage: &turnResult.TokenUsage,
			DurationMs: turnResult.Duration.Milliseconds(),
			TTFTMs:     turnResult.TTFT.Milliseconds(),
		})
	}

	return turnResult, err
}
