package agent

import (
	"context"
	"strconv"
	"strings"

	"github.com/SherkeyXD/ARTEX/llm"
	"github.com/SherkeyXD/ARTEX/sidequestion"
)

func attachSideCapture(ctx context.Context, prov llm.Provider) context.Context {
	ri := RunInfoFrom(ctx)
	p := sidequestion.Parent{TaskID: ri.TaskID, ExplorationID: ri.ExplorationID, IntentID: ri.IntentID}
	if strings.HasPrefix(ri.SessionID, "conv-") {
		p.ConversationID, _ = strconv.ParseInt(strings.TrimPrefix(ri.SessionID, "conv-"), 10, 64)
	}
	if p.ConversationID == 0 && (p.TaskID == 0 || p.ExplorationID == 0) {
		return ctx
	}
	ctx, _ = sidequestion.Attach(ctx, p, sidequestion.QueryDeps{}, prov)
	return ctx
}
