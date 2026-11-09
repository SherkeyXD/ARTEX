package agent

import (
	"context"
	"strconv"
	"strings"

	"github.com/SherkeyXD/ARTEX/sidequestion"
	"github.com/Autumn-27/norma/agentcore"
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
	if opts == nil {
		ctx, _ = sidequestion.Attach(ctx, p, sidequestion.QueryDeps{}, nil)
		return ctx
	}
	nativeProv := toNativeProvider(opts.Provider)
	nativeDeps := toNativeQueryDeps(opts.Deps)
	ctx, newDeps := sidequestion.Attach(ctx, p, nativeDeps, nativeProv)
	opts.Deps = toNormaQueryDeps(newDeps)
	return ctx
}
