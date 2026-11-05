package db

import (
	"fmt"
	"time"
)

// CommandRecord is a paired tool_use + tool_result from the activity table
// (any tool, not just Bash). Command holds the raw tool input (JSON).
type CommandRecord struct {
	ID        int64     `json:"id"`
	ExpID     int64     `json:"exploration_id"`
	Worker    string    `json:"worker"`
	Tool      string    `json:"tool"`
	Command   string    `json:"command"`
	Output    string    `json:"output"`
	IsError   bool      `json:"is_error"`
	CreatedAt time.Time `json:"created_at"`
}

// commandFilter builds the WHERE clause shared by the tool-execution list and
// its per-tool tally, so the summary always describes exactly the rows the table
// pages through. Returns the clause, its args, and the next placeholder index.
func commandFilter(expID *int64, q string) (string, []any, int) {
	where := `WHERE u.kind = 'tool_use'`
	args := []any{}
	argN := 1

	if expID != nil {
		where += fmt.Sprintf(` AND u.exploration_id = $%d`, argN)
		args = append(args, *expID)
		argN++
	}
	if q != "" {
		where += fmt.Sprintf(` AND (u.tool ILIKE $%d OR u.detail ILIKE $%d)`, argN, argN)
		args = append(args, "%"+q+"%")
		argN++
	}
	return where, args, argN
}

// ToolStat is one tool's execution tally for the usage summary.
type ToolStat struct {
	Tool   string `json:"tool"`
	Total  int    `json:"total"`
	Errors int    `json:"errors"`
}

// ToolStats counts executions grouped by tool under the same filters
// ListCommands takes. Unpaginated on purpose: the tally describes the whole
// filtered set, not the page currently on screen.
func (d *DB) ToolStats(expID *int64, q string) ([]ToolStat, error) {
	where, args, _ := commandFilter(expID, q)

	rows, err := d.Query(`
SELECT COALESCE(NULLIF(u.tool,''),'-') AS tool, COUNT(*) AS total,
       COUNT(*) FILTER (WHERE COALESCE(r.is_error,false)) AS errors
FROM activity u
LEFT JOIN activity r ON r.tool_use_id = u.tool_use_id AND r.kind = 'tool_result'
`+where+`
GROUP BY 1
ORDER BY total DESC, tool ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ToolStat{}
	for rows.Next() {
		var s ToolStat
		if err := rows.Scan(&s.Tool, &s.Total, &s.Errors); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListCommands returns tool executions (tool_use + paired tool_result) across all
// explorations, with optional filtering and pagination. Covers every tool, not
// just Bash; q matches the tool name or its input.
func (d *DB) ListCommands(expID *int64, q string, page, size int) ([]CommandRecord, int, error) {
	if size <= 0 {
		size = 50
	}
	if page < 0 {
		page = 0
	}
	offset := page * size

	where, args, argN := commandFilter(expID, q)

	// count
	var total int
	countQ := `SELECT COUNT(*) FROM activity u ` + where
	if err := d.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// data query: join tool_use with its tool_result
	dataQ := `
SELECT u.id, u.exploration_id, COALESCE(u.worker,''), COALESCE(u.tool,''), COALESCE(u.detail,''),
       COALESCE(r.detail,''), COALESCE(r.is_error, false), u.created_at
FROM activity u
LEFT JOIN activity r ON r.tool_use_id = u.tool_use_id AND r.kind = 'tool_result'
` + where + `
ORDER BY u.id DESC
LIMIT $` + fmt.Sprintf("%d", argN) + ` OFFSET $` + fmt.Sprintf("%d", argN+1)

	args = append(args, size, offset)
	rows, err := d.Query(dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []CommandRecord{}
	for rows.Next() {
		var c CommandRecord
		if err := rows.Scan(&c.ID, &c.ExpID, &c.Worker, &c.Tool, &c.Command, &c.Output, &c.IsError, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ModelTokenStat is one model's aggregated token usage for a task, summed from the
// llm_usage metering ledger (see db/llm_usage.go). Calls is the number of LLM calls
// that hit this model.
type ModelTokenStat struct {
	Model            string `json:"model"`
	Calls            int    `json:"calls"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
}
