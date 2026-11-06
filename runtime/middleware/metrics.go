package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/Autumn-27/artex/runtime"
)

// StepMetric records latency and volume metrics for a single execution step.
type StepMetric struct {
	StepID      string        `json:"step_id"`
	Kind        string        `json:"kind"` // "ttft" | "thinking" | "text" | "tool"
	ToolName    string        `json:"tool_name,omitempty"`
	Duration    time.Duration `json:"duration"`
	Offset      time.Duration `json:"offset"`
	CharCount   int           `json:"char_count,omitempty"`
	InputTokens int           `json:"input_tokens,omitempty"`
}

// MetricsRecorder is a Universal Runtime Middleware that tracks TTFT, thinking duration,
// and tool execution times for the Context & Latency Monitoring Dashboard.
type MetricsRecorder struct {
	runtime.BaseMiddleware

	mu         sync.Mutex
	turnStart  time.Time
	ttft       time.Duration
	thinkingMs int64
	toolStats  []StepMetric
	onStepEnd  func(metric StepMetric)
}

// NewMetricsRecorder constructs a MetricsRecorder.
func NewMetricsRecorder(onStepEnd func(metric StepMetric)) *MetricsRecorder {
	return &MetricsRecorder{
		onStepEnd: onStepEnd,
		toolStats: make([]StepMetric, 0),
	}
}

func (m *MetricsRecorder) OnBeforeTurn(ctx context.Context, tc *runtime.TurnContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turnStart = time.Now()
	m.ttft = 0
	m.thinkingMs = 0
	m.toolStats = make([]StepMetric, 0)
	return nil
}

func (m *MetricsRecorder) OnFirstToken(ctx context.Context, tc *runtime.TurnContext, ttft time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttft = ttft
	if m.onStepEnd != nil {
		m.onStepEnd(StepMetric{
			StepID:   "ttft",
			Kind:     "ttft",
			Duration: ttft,
			Offset:   0,
		})
	}
}

func (m *MetricsRecorder) OnAfterToolExec(ctx context.Context, tc *runtime.TurnContext, result *runtime.ToolResultData, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	offset := time.Since(m.turnStart)
	metric := StepMetric{
		StepID:    result.ToolCallID,
		Kind:      "tool",
		ToolName:  result.ToolName,
		Duration:  duration,
		Offset:    offset,
		CharCount: result.CharCount,
	}
	m.toolStats = append(m.toolStats, metric)
	if m.onStepEnd != nil {
		m.onStepEnd(metric)
	}
}

// Stats returns recorded tool stats for this turn.
func (m *MetricsRecorder) Stats() []StepMetric {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]StepMetric, len(m.toolStats))
	copy(res, m.toolStats)
	return res
}

// TTFT returns captured TTFT.
func (m *MetricsRecorder) TTFT() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ttft
}
