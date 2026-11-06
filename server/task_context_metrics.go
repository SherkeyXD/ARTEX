package server

import (
	"net/http"
	"strconv"
)

// taskContextMetrics returns the context composition, step latencies,
// token breakdown, and agent fleet performance for a task.
func (s *Server) taskContextMetrics(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	taskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid task id")
		return
	}

	var expID int64
	if t, ok := s.m.Task(idStr); ok && t != nil && t.Store != nil {
		expID = t.Store.ID()
	} else if s.m != nil && s.m.pg != nil {
		dbTask, err := s.m.pg.GetTask(taskID)
		if err != nil {
			writeErr(w, 404, "task not found")
			return
		}
		expID = dbTask.ExplorationID
	} else {
		writeErr(w, 404, "task not found")
		return
	}

	report, err := s.m.pg.TaskContextMetrics(taskID, expID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// globalContextMetrics returns system-wide context composition, step latencies,
// token breakdown, and agent fleet performance.
func (s *Server) globalContextMetrics(w http.ResponseWriter, r *http.Request) {
	if s.m == nil || s.m.pg == nil {
		writeErr(w, 503, "database unavailable")
		return
	}
	days := 30
	if q := r.URL.Query().Get("days"); q != "" {
		if d, err := strconv.Atoi(q); err == nil && d > 0 {
			days = d
		}
	}
	report, err := s.m.pg.GlobalContextMetrics(days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}
