package runtime

import (
	"context"
	"time"
)

// TurnContext provides metadata and execution context for a single turn.
type TurnContext struct {
	SessionID   string
	AgentID     string
	TaskID      string
	TurnIndex   int
	InputPrompt string
	Metadata    map[string]any
}

// Middleware defines the lifecycle interceptor for universal agent execution.
// Middlewares are used for Approval Guards, Latency & Token Ledger, Context Compaction, etc.
type Middleware interface {
	OnBeforeTurn(ctx context.Context, tc *TurnContext) error
	OnModelCallStart(ctx context.Context, tc *TurnContext)
	OnFirstToken(ctx context.Context, tc *TurnContext, ttft time.Duration)
	OnThinkingChunk(ctx context.Context, tc *TurnContext, chunk string)
	OnBeforeToolExec(ctx context.Context, tc *TurnContext, call *ToolCallData) (proceed bool, reason string, err error)
	OnAfterToolExec(ctx context.Context, tc *TurnContext, result *ToolResultData, duration time.Duration)
	OnAfterTurn(ctx context.Context, tc *TurnContext, res *TurnResult)
}

// BaseMiddleware provides empty default implementations of all Middleware methods.
type BaseMiddleware struct{}

func (BaseMiddleware) OnBeforeTurn(ctx context.Context, tc *TurnContext) error { return nil }
func (BaseMiddleware) OnModelCallStart(ctx context.Context, tc *TurnContext)    {}
func (BaseMiddleware) OnFirstToken(ctx context.Context, tc *TurnContext, ttft time.Duration) {}
func (BaseMiddleware) OnThinkingChunk(ctx context.Context, tc *TurnContext, chunk string) {}
func (BaseMiddleware) OnBeforeToolExec(ctx context.Context, tc *TurnContext, call *ToolCallData) (bool, string, error) {
	return true, "", nil
}
func (BaseMiddleware) OnAfterToolExec(ctx context.Context, tc *TurnContext, result *ToolResultData, duration time.Duration) {
}
func (BaseMiddleware) OnAfterTurn(ctx context.Context, tc *TurnContext, res *TurnResult) {}

// MiddlewareChain composes multiple middlewares into a pipeline.
type MiddlewareChain struct {
	middlewares []Middleware
}

// NewMiddlewareChain creates a new chain from middlewares.
func NewMiddlewareChain(m ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{middlewares: m}
}

// Add appends middlewares to the chain.
func (c *MiddlewareChain) Add(m ...Middleware) {
	c.middlewares = append(c.middlewares, m...)
}

func (c *MiddlewareChain) OnBeforeTurn(ctx context.Context, tc *TurnContext) error {
	for _, m := range c.middlewares {
		if err := m.OnBeforeTurn(ctx, tc); err != nil {
			return err
		}
	}
	return nil
}

func (c *MiddlewareChain) OnModelCallStart(ctx context.Context, tc *TurnContext) {
	for _, m := range c.middlewares {
		m.OnModelCallStart(ctx, tc)
	}
}

func (c *MiddlewareChain) OnFirstToken(ctx context.Context, tc *TurnContext, ttft time.Duration) {
	for _, m := range c.middlewares {
		m.OnFirstToken(ctx, tc, ttft)
	}
}

func (c *MiddlewareChain) OnThinkingChunk(ctx context.Context, tc *TurnContext, chunk string) {
	for _, m := range c.middlewares {
		m.OnThinkingChunk(ctx, tc, chunk)
	}
}

func (c *MiddlewareChain) OnBeforeToolExec(ctx context.Context, tc *TurnContext, call *ToolCallData) (bool, string, error) {
	for _, m := range c.middlewares {
		proceed, reason, err := m.OnBeforeToolExec(ctx, tc, call)
		if err != nil {
			return false, reason, err
		}
		if !proceed {
			return false, reason, nil
		}
	}
	return true, "", nil
}

func (c *MiddlewareChain) OnAfterToolExec(ctx context.Context, tc *TurnContext, result *ToolResultData, duration time.Duration) {
	for _, m := range c.middlewares {
		m.OnAfterToolExec(ctx, tc, result, duration)
	}
}

func (c *MiddlewareChain) OnAfterTurn(ctx context.Context, tc *TurnContext, res *TurnResult) {
	for _, m := range c.middlewares {
		m.OnAfterTurn(ctx, tc, res)
	}
}
