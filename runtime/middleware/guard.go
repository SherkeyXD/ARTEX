package middleware

import (
	"context"
	"encoding/json"

	"github.com/Autumn-27/artex/runtime"
)

// GuardFunc evaluates whether a tool call should be allowed to proceed.
type GuardFunc func(ctx context.Context, agentID, toolName string, args json.RawMessage) (proceed bool, reason string, err error)

// GuardMiddleware enforces approval and interception policies before any tool executes.
type GuardMiddleware struct {
	runtime.BaseMiddleware
	check GuardFunc
}

// NewGuardMiddleware creates a guard middleware from a policy function.
func NewGuardMiddleware(check GuardFunc) *GuardMiddleware {
	return &GuardMiddleware{check: check}
}

func (g *GuardMiddleware) OnBeforeToolExec(ctx context.Context, tc *runtime.TurnContext, call *runtime.ToolCallData) (bool, string, error) {
	if g.check == nil {
		return true, "", nil
	}
	agentID := ""
	if tc != nil {
		agentID = tc.AgentID
	}
	return g.check(ctx, agentID, call.Name, call.Arguments)
}
