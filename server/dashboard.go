package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/SherkeyXD/ARTEX/db"
)

type dashboardTask struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	EngineMode   string `json:"engine_mode"`
	GoalsTotal   int    `json:"goals_total"`
	GoalsMet     int    `json:"goals_met"`
	InFlight     int    `json:"in_flight"`
	LastActivity int64  `json:"last_activity_unix"`
	CreatedUnix  int64  `json:"created_unix"`
	sortKey      int64
}

type dashboardProfileTokens struct {
	ProfileID        *int64 `json:"profile_id,omitempty"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	TaskCount        int    `json:"task_count"`
}

type dashboardDailyTokens struct {
	ProfileID       *int64 `json:"profile_id,omitempty"`
	Date            string `json:"date"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	CacheReadTokens int    `json:"cache_read_tokens"`
}

type dashboardFinding struct {
	ID              string `json:"id"`
	VulnClass       string `json:"vulnclass"`
	Severity        string `json:"severity"`
	Summary         string `json:"summary"`
	TaskDescription string `json:"task_description,omitempty"`
	TS              string `json:"ts"`
}

type dashboardPending struct {
	ID        int64  `json:"id"`
	AgentName string `json:"agent_name"`
	ToolName  string `json:"tool_name"`
}

// dashboardSummary is the dashboard's single fast poll. It returns counts and the
// few rows the page renders, instead of every task and up to 500 findings.
func (s *Server) dashboardSummary(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"llm_configured": false,
		"tasks": map[string]any{
			"total": 0, "by_status": map[string]int{}, "recent": []dashboardTask{}, "latest_id": "",
		},
		"findings": map[string]any{
			"total":       0,
			"by_severity": map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
			"recent":      []dashboardFinding{},
		},
		"pending":       map[string]any{"total": 0, "items": []dashboardPending{}},
		"activity":      []ActivityDTO{},
		"legacy_tokens": map[string]any{"by_profile": []dashboardProfileTokens{}, "daily": []dashboardDailyTokens{}},
	}
	if s.m == nil || s.m.pg == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}

	configured := false
	if s.engine != nil {
		configured = s.engine.Ready()
		if active := s.m.ActiveTask(); active != nil {
			configured = s.engine.ReadyFor(active)
		}
	}
	out["llm_configured"] = configured

	list := s.m.List()
	metrics, _ := s.m.PG().TaskListMetricsAll()
	byStatus := map[string]int{}
	recent := make([]dashboardTask, 0, len(list))
	byProfile := map[string]*dashboardProfileTokens{}
	byDay := map[string]*dashboardDailyTokens{}
	latestID := ""
	if len(list) > 0 {
		latestID = list[0].ID
	}
	for _, t := range list {
		if t == nil {
			continue
		}
		status := s.resolvedTaskStatus(t)
		byStatus[status]++
		metric := metrics[t.ExpID]
		last := metric.LastActivity
		var live int64
		if s.engine != nil {
			live = s.engine.LastActivity(t.ID)
			if live > last {
				last = live
			}
		}
		recent = append(recent, dashboardTask{
			ID:           t.ID,
			Description:  t.Description,
			Status:       status,
			EngineMode:   s.taskEngineMode(t, metric.RunningIntents, live),
			GoalsTotal:   metric.Goals.Total,
			GoalsMet:     metric.Goals.Met,
			InFlight:     metric.RunningIntents,
			LastActivity: last,
			CreatedUnix:  t.CreatedAt,
			sortKey:      last,
		})
		s.foldDashboardTokens(byProfile, byDay, t, metric.Tokens)
	}
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].sortKey > recent[j].sortKey
	})
	if len(recent) > 5 {
		recent = recent[:5]
	}
	out["tasks"] = map[string]any{
		"total":     len(list),
		"by_status": byStatus,
		"recent":    recent,
		"latest_id": latestID,
	}
	out["legacy_tokens"] = map[string]any{
		"by_profile": dashboardProfileList(byProfile),
		"daily":      dashboardDailyList(byDay),
	}

	total, counts, rows, err := s.m.pg.DashboardFindings(8)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings := make([]dashboardFinding, 0, len(rows))
	for _, row := range rows {
		findings = append(findings, dashboardFinding{
			ID:              strconv.FormatInt(row.ID, 10),
			VulnClass:       row.VulnClass,
			Severity:        row.Severity,
			Summary:         row.Summary,
			TaskDescription: row.TaskDescription,
			TS:              rfc3339(row.CreatedAt),
		})
	}
	out["findings"] = map[string]any{
		"total": total,
		"by_severity": map[string]int{
			"critical": counts.Critical,
			"high":     counts.High,
			"medium":   counts.Medium,
			"low":      counts.Low,
		},
		"recent": findings,
	}

	pendingTotal, pendingRows, err := s.m.pg.DashboardPending(3)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending := make([]dashboardPending, 0, len(pendingRows))
	for _, row := range pendingRows {
		pending = append(pending, dashboardPending{ID: row.ID, AgentName: row.AgentName, ToolName: row.ToolName})
	}
	out["pending"] = map[string]any{"total": pendingTotal, "items": pending}

	activity, err := s.m.pg.RecentActivity(30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out["activity"] = activityDTOs(activity)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) taskEngineMode(t *Task, inFlight int, last int64) string {
	if s == nil || s.engine == nil || t == nil {
		return "idle"
	}
	paused := s.engine.IsPaused(t.ID)
	activeCalls := s.engine.ActiveLLMCalls(t.ID)
	running := s.engine.ReadyFor(t) && s.engine.Started(t.ID) && !paused
	stalled := running && activeCalls == 0 && inFlight > 0 && last > 0 && time.Now().Unix()-last > 120
	switch {
	case paused:
		return "paused"
	case stalled:
		return "stalled"
	case running && activeCalls > 0:
		return "exploring"
	default:
		return "idle"
	}
}

func (s *Server) foldDashboardTokens(byProfile map[string]*dashboardProfileTokens, byDay map[string]*dashboardDailyTokens, t *Task, usage db.TokenUsage) {
	var profileID *int64
	key := "none"
	if t != nil {
		if id := t.llmStateSnapshot().ProfileID; id != nil {
			copied := *id
			profileID = &copied
			key = strconv.FormatInt(copied, 10)
		}
	}
	bucket := byProfile[key]
	if bucket == nil {
		bucket = &dashboardProfileTokens{ProfileID: profileID}
		byProfile[key] = bucket
	}
	bucket.InputTokens += usage.InputTokens
	bucket.OutputTokens += usage.OutputTokens
	bucket.CacheReadTokens += usage.CacheReadTokens
	bucket.CacheWriteTokens += usage.CacheWriteTokens
	bucket.TaskCount++

	date := ""
	if t != nil && t.CreatedAt > 0 {
		created := rfc3339(time.Unix(t.CreatedAt, 0))
		if len(created) >= 10 {
			date = created[:10]
		}
	}
	dayKey := key + "|" + date
	day := byDay[dayKey]
	if day == nil {
		day = &dashboardDailyTokens{ProfileID: profileID, Date: date}
		byDay[dayKey] = day
	}
	day.InputTokens += usage.InputTokens
	day.OutputTokens += usage.OutputTokens
	day.CacheReadTokens += usage.CacheReadTokens
}

func dashboardProfileList(in map[string]*dashboardProfileTokens) []dashboardProfileTokens {
	out := make([]dashboardProfileTokens, 0, len(in))
	for _, bucket := range in {
		out = append(out, *bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		left := profileSortKey(out[i].ProfileID)
		right := profileSortKey(out[j].ProfileID)
		return left < right
	})
	return out
}

func dashboardDailyList(in map[string]*dashboardDailyTokens) []dashboardDailyTokens {
	out := make([]dashboardDailyTokens, 0, len(in))
	for _, bucket := range in {
		out = append(out, *bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return profileSortKey(out[i].ProfileID) < profileSortKey(out[j].ProfileID)
	})
	return out
}

func profileSortKey(id *int64) int64 {
	if id == nil {
		return -1
	}
	return *id
}
