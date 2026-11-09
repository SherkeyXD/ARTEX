package agent

import (
	"context"
	"strconv"
	"strings"

	"github.com/SherkeyXD/ARTEX/sidequestion"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

func attachSideCapture(ctx context.Context, opts *agentcore.Options) context.Context {
	ri := RunInfoFrom(ctx)
	p := sidequestion.Parent{TaskID: ri.TaskID, ExplorationID: ri.ExplorationID, IntentID: ri.IntentID}
	if strings.HasPrefix(ri.SessionID, "conv-") {
		p.ConversationID, _ = strconv.ParseInt(strings.TrimPrefix(ri.SessionID, "conv-"), 10, 64)
	}
	if p.ConversationID == 0 && (p.TaskID == 0 || p.ExplorationID == 0) {
		return ctx
	}
	var prov llm.Provider
	var deps harness.QueryDeps
	if opts != nil {
		prov = opts.Provider
		deps = opts.Deps
	}
	ctx, newDeps := sidequestion.Attach(ctx, p, deps, prov)
	if opts != nil {
		opts.Deps = newDeps
	}
	return ctx
}
