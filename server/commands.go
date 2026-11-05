package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/SherkeyXD/ARTEX/db"
)

// pgListCommands returns tool executions (any tool) from the activity table.
func (s *Server) pgListCommands(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 0)
	size := atoiDefault(q.Get("size"), 50)
	keyword := q.Get("q")

	records, total, err := s.m.PG().ListCommands(commandTaskFilter(q.Get("task")), keyword, page, size)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"commands": records, "total": total})
}

// pgToolStats returns per-tool call counts for the tool-execution history, under
// the same task/keyword filters the list takes — the summary describes the whole
// filtered set, not the page on screen.
func (s *Server) pgToolStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pg := s.m.PG()
	if pg == nil {
		writeJSON(w, 200, map[string]any{"stats": []db.ToolStat{}})
		return
	}
	stats, err := pg.ToolStats(commandTaskFilter(q.Get("task")), q.Get("q"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"stats": stats})
}

// commandTaskFilter parses the ?task= exploration id. Absent or unparsable → nil
// (no filter), matching the list endpoint's long-standing lenient behaviour.
func commandTaskFilter(v string) *int64 {
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// pgTokenByModel returns a task's LLM token usage grouped by model, from the
// always-on llm_usage metering ledger. Accurate even with per-agent model bindings,
// pool rotation/failover, and interrupted runs (every call is metered on success or
// error). No recording toggle required.
func (s *Server) pgTokenByModel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("task"))
	if id == "" {
		writeErr(w, 400, "missing task")
		return
	}
	pg := s.m.PG()
	if pg == nil {
		writeJSON(w, 200, map[string]any{"models": []db.ModelTokenStat{}})
		return
	}
	models, err := pg.TokenByModel(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"models": models})
}

// pgUsageStats returns global llm_usage aggregates for the dashboard's "new" token
// view: per-profile totals (all-time) + per-(profile, day) buckets for the daily
// chart. Sourced from the always-on metering ledger, so it is accurate across
// per-agent bindings, pool rotation, and interrupted runs.
func (s *Server) pgUsageStats(w http.ResponseWriter, r *http.Request) {
	pg := s.m.PG()
	if pg == nil {
		writeJSON(w, 200, map[string]any{"by_profile": []db.ProfileUsage{}, "daily": []db.ProfileDayUsage{}})
		return
	}
	days := atoiDefault(r.URL.Query().Get("days"), 365)
	byProfile, err := pg.UsageByProfile()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	daily, err := pg.UsageDaily(days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"by_profile": byProfile, "daily": daily})
}
