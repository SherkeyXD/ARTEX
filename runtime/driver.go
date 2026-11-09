package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TurnOptions configures a single turn execution.
type TurnOptions struct {
	ModelProfile    string
	ModelName       string
	SystemPrompt    string
	SystemPrompts   []string
	DynamicBoundary int
	NonStreaming    bool
	WorkingDir      string
	ToolOutputDir   string
	MaxDuration     time.Duration
	Settlement      *Settlement
	Compaction      *CompactionConfig
	Temperature     float64
	MaxTokens       int
	MaxIterations   int // Maximum model calls; 0 means unlimited.
	ToolOutputLimit int // Max chars of tool output before truncation (default: 4000)
	ReasoningEffort string
	ThinkingType    string
	Tools           []Tool
	Provider        LLMProvider
}

// Settlement gives a budget-exhausted run a separate, bounded write-back phase.
// The caller's cancellation still interrupts this phase.
type Settlement struct {
	Prompt         string
	PromptByReason map[TerminalReason]string
	DisabledTools  []string
	MaxTurns       int
	MaxDuration    time.Duration
}

// HarnessDriver defines the SPI for an underlying agent execution engine.
type HarnessDriver interface {
	ID() string
	Version() string
	ExecuteTurn(
		ctx context.Context,
		session *Session,
		opts TurnOptions,
		middlewares *MiddlewareChain,
		sink EventSink,
	) (*TurnResult, error)
}

// DriverRegistry holds available HarnessDriver implementations.
type DriverRegistry struct {
	mu      sync.RWMutex
	drivers map[string]HarnessDriver
}

var defaultDriverRegistry = &DriverRegistry{
	drivers: make(map[string]HarnessDriver),
}

// RegisterDriver registers a harness driver globally.
func RegisterDriver(driver HarnessDriver) {
	defaultDriverRegistry.Register(driver)
}

// GetDriver looks up a driver by ID.
func GetDriver(id string) (HarnessDriver, error) {
	return defaultDriverRegistry.Get(id)
}

// Register registers a driver in the registry.
func (r *DriverRegistry) Register(driver HarnessDriver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if driver != nil {
		r.drivers[driver.ID()] = driver
	}
}

// Get gets a driver by ID.
func (r *DriverRegistry) Get(id string) (HarnessDriver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[id]
	if !ok {
		return nil, fmt.Errorf("harness driver %q not found", id)
	}
	return d, nil
}

// List returns all registered driver IDs.
func (r *DriverRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.drivers))
	for id := range r.drivers {
		ids = append(ids, id)
	}
	return ids
}
