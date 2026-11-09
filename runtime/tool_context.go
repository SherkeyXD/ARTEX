package runtime

import "context"

// ToolExecutionContext binds execution state to a call without process-wide chdir.
type ToolExecutionContext struct {
	WorkingDir     string
	AgentID        string
	ToolUseID      string
	OutputDir      string
	MaxOutputChars int
}

type toolExecutionKey struct{}

func WithToolExecution(ctx context.Context, execution ToolExecutionContext) context.Context {
	return context.WithValue(ctx, toolExecutionKey{}, execution)
}

func ToolExecutionFrom(ctx context.Context) ToolExecutionContext {
	execution, _ := ctx.Value(toolExecutionKey{}).(ToolExecutionContext)
	return execution
}

// ToolOutput preserves extra messages such as a skill's instruction injection.
type ToolOutput struct {
	Content  string
	Messages []Message
	Captured bool
}
