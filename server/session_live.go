package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/SherkeyXD/ARTEX/db"
)

type sessionPendingAgent struct {
	AgentName string `json:"agent_name"`
}

// taskSessionLive is the session tab's combined poll: chat state, token totals,
// role LLM resolution, and pending-approval agent names.
func (s *Server) taskSessionLive(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	s.chatMu.Lock()
	running := s.chatBusy[t.ID]
	s.chatMu.Unlock()

	sessions, err := t.Store.TokenStatsBySession()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := t.Store.TokenTotal()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	roles, err := s.taskRoleResolutions(t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	pending := []sessionPendingAgent{}
	if s.m.pg != nil {
		names, err := s.m.pg.PendingInterceptAgents(t.ID, 100)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, name := range names {
			pending = append(pending, sessionPendingAgent{AgentName: name})
		}
	}
	if sessions == nil {
		sessions = []db.SessionTokenUsage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chat_running":       running,
		"tokens":             map[string]any{"total": tokenTotalDTO(total), "sessions": sessions},
		"llm":                roles,
		"pending_intercepts": pending,
	})
}

func (s *Server) taskRoleResolutions(t *Task) (map[string]taskLLMResolution, error) {
	roles := map[string]taskLLMResolution{}
	for responseKey, agentKey := range map[string]string{
		"mainagent": "mainagent",
		"planner":   "planner",
		"worker":    "worker",
	} {
		resolved, err := s.resolveTaskRoleLLM(t, agentKey)
		if err != nil {
			return nil, err
		}
		roles[responseKey] = resolved
	}
	return roles, nil
}

// intentHistoryRevision fingerprints this task's intents and those inherited
// from direct sources. The session list poll sends it back and skips the page
// query when the fingerprint is unchanged.
func intentHistoryRevision(t *Task) (string, error) {
	parts := make([]string, 0, 4)
	add := func(store *db.ExplorationStore, label string) error {
		rev, err := store.IntentRevision()
		if err != nil {
			return err
		}
		parts = append(parts, label+"="+rev)
		return nil
	}
	if err := add(t.Store, t.ID); err != nil {
		return "", err
	}
	sources, err := t.Store.DirectSourceStores()
	if err != nil {
		return "", err
	}
	for _, source := range sources {
		if err := add(source.Store, strconv.FormatInt(source.Task.TaskID, 10)); err != nil {
			return "", err
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|"), nil
}
