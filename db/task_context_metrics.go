package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type ContextSummary struct {
	TotalTokens        int64   `json:"total_tokens"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	TotalDurationMs    int64   `json:"total_duration_ms"`
	ThinkingDurationMs int64   `json:"thinking_duration_ms"`
	ToolDurationMs     int64   `json:"tool_duration_ms"`
	LLMDurationMs      int64   `json:"llm_duration_ms"`
	StepCount          int     `json:"step_count"`
	LLMCalls           int     `json:"llm_calls"`
	ToolCalls          int     `json:"tool_calls"`
	AvgTokensPerSec    float64 `json:"avg_tokens_per_sec"`
}

type ContextCategory struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"` // tool_output, history, thinking, system, user, tool_use
	Tokens     int64   `json:"tokens"`
	Chars      int64   `json:"chars"`
	Percentage float64 `json:"percentage"`
	Color      string  `json:"color"`
}

type TopToolOutput struct {
	Tool            string `json:"tool"`
	Summary         string `json:"summary"`
	CharCount       int64  `json:"char_count"`
	EstimatedTokens int64  `json:"estimated_tokens"`
	DurationMs      int64  `json:"duration_ms"`
	NodeID          *int64 `json:"node_id,omitempty"`
}

type ContextComposition struct {
	Categories     []ContextCategory `json:"categories"`
	TopToolOutputs []TopToolOutput   `json:"top_tool_outputs"`
}

type WaterfallStep struct {
	ID         int64     `json:"id"`
	NodeID     *int64    `json:"node_id,omitempty"`
	Worker     string    `json:"worker"`
	Kind       string    `json:"kind"` // tool_result, thinking, text, result
	Tool       string    `json:"tool,omitempty"`
	Summary    string    `json:"summary"`
	DurationMs int64     `json:"duration_ms"`
	CharCount  int64     `json:"char_count,omitempty"`
	Tokens     int64     `json:"tokens,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	OffsetMs   int64     `json:"offset_ms"`
}

type EvolutionTurn struct {
	TurnIndex        int       `json:"turn_index"`
	Worker           string    `json:"worker"`
	NodeID           *int64    `json:"node_id,omitempty"`
	CumulativeInput  int64     `json:"cumulative_input"`
	CumulativeOutput int64     `json:"cumulative_output"`
	CumulativeCache  int64     `json:"cumulative_cache"`
	TurnDurationMs   int64     `json:"turn_duration_ms"`
	TTFTMs           int64     `json:"ttft_ms"`
	ThinkingMs       int64     `json:"thinking_ms"`
	ToolMs           int64     `json:"tool_ms"`
	Timestamp        time.Time `json:"timestamp"`
}

type FleetAgent struct {
	Worker           string `json:"worker"`
	Role             string `json:"role"`
	IntentID         *int64 `json:"intent_id,omitempty"`
	Model            string `json:"model"`
	Calls            int    `json:"calls"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	DurationMs       int64  `json:"duration_ms"`
	StepCount        int    `json:"step_count"`
	ToolCalls        int    `json:"tool_calls"`
	Status           string `json:"status"` // running, completed, idle
}

type TaskContextMetricsReport struct {
	TaskID      int64              `json:"task_id"`
	Summary     ContextSummary     `json:"summary"`
	Composition ContextComposition `json:"composition"`
	Waterfall   []WaterfallStep    `json:"waterfall"`
	Turns       []EvolutionTurn    `json:"turns"`
	Fleet       []FleetAgent       `json:"fleet"`
}

type activityMetaPayload struct {
	DurationMs int64 `json:"duration_ms"`
	TotalMs    int64 `json:"total_ms"`
	TTFTMs     int64 `json:"ttft_ms"`
	CharCount  int64 `json:"char_count"`
}

// TaskContextMetrics computes fine-grained context composition, step latencies,
// token accounting, and agent fleet performance for a task.
func (d *DB) TaskContextMetrics(taskID int64, expID int64) (*TaskContextMetricsReport, error) {
	taskIDStr := strconv.FormatInt(taskID, 10)
	report := &TaskContextMetricsReport{
		TaskID: taskID,
		Composition: ContextComposition{
			Categories:     []ContextCategory{},
			TopToolOutputs: []TopToolOutput{},
		},
		Waterfall: []WaterfallStep{},
		Turns:     []EvolutionTurn{},
		Fleet:     []FleetAgent{},
	}

	// 1. Query llm_usage ledger for this task
	fleetMap := make(map[string]*FleetAgent)
	var totalLLMLatency int64
	var totalInputTokens, totalOutputTokens, totalCacheRead, totalCacheWrite int64
	var totalCalls int

	usageRows, err := d.Query(`
SELECT COALESCE(NULLIF(worker,''),'(unknown)') AS worker,
       COALESCE(NULLIF(model,''),'(unknown)') AS model,
       COUNT(*),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(cache_read), 0),
       COALESCE(SUM(cache_write), 0),
       COALESCE(SUM(latency_ms), 0)
FROM llm_usage
WHERE task_id = $1
GROUP BY worker, model
ORDER BY SUM(input_tokens) + SUM(output_tokens) DESC`, taskIDStr)
	if err == nil {
		defer usageRows.Close()
		for usageRows.Next() {
			var w, m string
			var calls int
			var inTok, outTok, cRead, cWrite, lat int64
			if scanErr := usageRows.Scan(&w, &m, &calls, &inTok, &outTok, &cRead, &cWrite, &lat); scanErr == nil {
				totalCalls += calls
				totalInputTokens += inTok
				totalOutputTokens += outTok
				totalCacheRead += cRead
				totalCacheWrite += cWrite
				totalLLMLatency += lat

				fleetKey := w + "|" + m
				role := w
				switch w {
				case "planner":
					role = "任务规划器 (Planner)"
				case "worker":
					role = "意图执行者 (Worker)"
				case "mainagent":
					role = "主对话 Agent (Main)"
				case "goals":
					role = "目标引导 (Goals)"
				}
				fleetMap[fleetKey] = &FleetAgent{
					Worker:           w,
					Role:             role,
					Model:            m,
					Calls:            calls,
					InputTokens:      inTok,
					OutputTokens:     outTok,
					CacheReadTokens:  cRead,
					CacheWriteTokens: cWrite,
					DurationMs:       lat,
					Status:           "completed",
				}
			}
		}
	}

	report.Summary.LLMCalls = totalCalls
	report.Summary.InputTokens = totalInputTokens
	report.Summary.OutputTokens = totalOutputTokens
	report.Summary.CacheReadTokens = totalCacheRead
	report.Summary.CacheWriteTokens = totalCacheWrite
	report.Summary.TotalTokens = totalInputTokens + totalOutputTokens
	report.Summary.LLMDurationMs = totalLLMLatency
	if totalInputTokens+totalCacheRead > 0 {
		report.Summary.CacheHitRate = float64(totalCacheRead) / float64(totalInputTokens+totalCacheRead)
	}

	// 2. Query activity table for step latencies and context breakdown
	actRows, err := d.Query(`
SELECT id, node_id, COALESCE(worker,''), COALESCE(kind,''), COALESCE(tool,''), COALESCE(summary,''),
       metadata, created_at, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
       LENGTH(COALESCE(detail,'')) AS detail_len
FROM activity
WHERE exploration_id = $1
ORDER BY id ASC`, expID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query activities: %w", err)
	}
	defer actRows.Close()

	var firstActivityTime time.Time
	var prevActivityTime time.Time
	var totalToolDuration int64
	var totalThinkingDuration int64
	var toolCallsCount int

	var charToolOutputs int64
	var charThinking int64
	var charReplies int64
	var charToolUse int64
	var charDirectives int64

	var topOutputs []TopToolOutput
	var turnIndex int
	var currentTurnThinkingMs int64
	var currentTurnToolMs int64
	var currentTurnTTFTMs int64

	stepCount := 0
	for actRows.Next() {
		var id int64
		var nodeID sql.NullInt64
		var worker, kind, tool, summary string
		var metaRaw json.RawMessage
		var createdAt time.Time
		var inTok, outTok, cRead, cWrite sql.NullInt64
		var detailLen int64

		if err := actRows.Scan(&id, &nodeID, &worker, &kind, &tool, &summary, &metaRaw, &createdAt,
			&inTok, &outTok, &cRead, &cWrite, &detailLen); err != nil {
			continue
		}
		stepCount++

		if firstActivityTime.IsZero() {
			firstActivityTime = createdAt
		}

		var meta activityMetaPayload
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &meta)
		}

		// Calculate step duration
		duration := meta.DurationMs
		if duration <= 0 && !prevActivityTime.IsZero() {
			delta := createdAt.Sub(prevActivityTime).Milliseconds()
			if delta > 0 && delta < 300_000 { // clamp to reasonable step window (< 5m)
				duration = delta
			}
		}
		prevActivityTime = createdAt

		var nidPtr *int64
		if nodeID.Valid {
			v := nodeID.Int64
			nidPtr = &v
		}

		// Update agent fleet stats
		if worker != "" {
			for _, f := range fleetMap {
				if f.Worker == worker {
					f.StepCount++
					if kind == "tool_use" {
						f.ToolCalls++
					}
					if nidPtr != nil && f.IntentID == nil {
						f.IntentID = nidPtr
					}
				}
			}
		}

		switch kind {
		case "tool_result":
			toolCallsCount++
			totalToolDuration += duration
			currentTurnToolMs += duration
			charToolOutputs += detailLen

			// Collect top tool output
			topOutputs = append(topOutputs, TopToolOutput{
				Tool:            tool,
				Summary:         summary,
				CharCount:       detailLen,
				EstimatedTokens: (detailLen + 3) / 4,
				DurationMs:      duration,
				NodeID:          nidPtr,
			})

			report.Waterfall = append(report.Waterfall, WaterfallStep{
				ID:         id,
				NodeID:     nidPtr,
				Worker:     worker,
				Kind:       "tool_result",
				Tool:       tool,
				Summary:    summary,
				DurationMs: duration,
				CharCount:  detailLen,
				Timestamp:  createdAt,
				OffsetMs:   createdAt.Sub(firstActivityTime).Milliseconds(),
			})

		case "thinking":
			totalThinkingDuration += duration
			currentTurnThinkingMs += duration
			charThinking += detailLen

			report.Waterfall = append(report.Waterfall, WaterfallStep{
				ID:         id,
				NodeID:     nidPtr,
				Worker:     worker,
				Kind:       "thinking",
				Summary:    summary,
				DurationMs: duration,
				CharCount:  detailLen,
				Timestamp:  createdAt,
				OffsetMs:   createdAt.Sub(firstActivityTime).Milliseconds(),
			})

		case "text":
			charReplies += detailLen
			report.Waterfall = append(report.Waterfall, WaterfallStep{
				ID:         id,
				NodeID:     nidPtr,
				Worker:     worker,
				Kind:       "text",
				Summary:    summary,
				DurationMs: duration,
				CharCount:  detailLen,
				Timestamp:  createdAt,
				OffsetMs:   createdAt.Sub(firstActivityTime).Milliseconds(),
			})

		case "tool_use":
			charToolUse += detailLen

		case "user":
			charDirectives += detailLen

		case "result":
			turnIndex++
			if meta.TTFTMs > 0 {
				currentTurnTTFTMs = meta.TTFTMs
			}
			turnDur := meta.TotalMs
			if turnDur <= 0 {
				turnDur = currentTurnThinkingMs + currentTurnToolMs
			}

			var curIn, curOut, curCache int64
			if inTok.Valid {
				curIn = inTok.Int64
			}
			if outTok.Valid {
				curOut = outTok.Int64
			}
			if cRead.Valid {
				curCache = cRead.Int64
			}

			report.Turns = append(report.Turns, EvolutionTurn{
				TurnIndex:        turnIndex,
				Worker:           worker,
				NodeID:           nidPtr,
				CumulativeInput:  curIn,
				CumulativeOutput: curOut,
				CumulativeCache:  curCache,
				TurnDurationMs:   turnDur,
				TTFTMs:           currentTurnTTFTMs,
				ThinkingMs:       currentTurnThinkingMs,
				ToolMs:           currentTurnToolMs,
				Timestamp:        createdAt,
			})

			// Reset per-turn accumulators
			currentTurnThinkingMs = 0
			currentTurnToolMs = 0
			currentTurnTTFTMs = 0
		}
	}

	report.Summary.StepCount = stepCount
	report.Summary.ToolCalls = toolCallsCount
	report.Summary.ThinkingDurationMs = totalThinkingDuration
	report.Summary.ToolDurationMs = totalToolDuration
	report.Summary.TotalDurationMs = totalLLMLatency + totalToolDuration
	if report.Summary.OutputTokens > 0 && (totalLLMLatency-totalThinkingDuration) > 0 {
		genSec := float64(totalLLMLatency-totalThinkingDuration) / 1000.0
		report.Summary.AvgTokensPerSec = float64(report.Summary.OutputTokens) / genSec
	}

	// Sort Top Tool Outputs descending by character size, keep top 10
	sort.Slice(topOutputs, func(i, j int) bool {
		return topOutputs[i].CharCount > topOutputs[j].CharCount
	})
	if len(topOutputs) > 10 {
		topOutputs = topOutputs[:10]
	}
	report.Composition.TopToolOutputs = topOutputs

	// Compute context composition breakdown
	// Estimate baseline system prompt tokens/chars if not explicitly tracked
	const estSystemPromptChars = int64(8000)
	totalChars := charToolOutputs + charThinking + charReplies + charToolUse + charDirectives + estSystemPromptChars
	if totalChars <= 0 {
		totalChars = 1
	}

	addCat := func(name, cat string, chars int64, color string) {
		pct := float64(chars) / float64(totalChars) * 100.0
		report.Composition.Categories = append(report.Composition.Categories, ContextCategory{
			Name:       name,
			Category:   cat,
			Chars:      chars,
			Tokens:     (chars + 3) / 4,
			Percentage: pct,
			Color:      color,
		})
	}

	addCat("工具返回 (Tool Outputs)", "tool_output", charToolOutputs, "#f59e0b")
	addCat("历史对话 (History)", "history", charReplies, "#3b82f6")
	addCat("思考过程 (Thinking)", "thinking", charThinking, "#8b5cf6")
	addCat("工具指令 (Tool Calls)", "tool_use", charToolUse, "#06b6d4")
	addCat("系统提示 (System Prompt)", "system", estSystemPromptChars, "#64748b")
	if charDirectives > 0 {
		addCat("任务指令 (Directives)", "user", charDirectives, "#10b981")
	}

	// Sort categories descending by tokens
	sort.Slice(report.Composition.Categories, func(i, j int) bool {
		return report.Composition.Categories[i].Chars > report.Composition.Categories[j].Chars
	})

	// Flatten fleet agents into slice
	for _, f := range fleetMap {
		report.Fleet = append(report.Fleet, *f)
	}
	sort.Slice(report.Fleet, func(i, j int) bool {
		return report.Fleet[i].InputTokens+report.Fleet[i].OutputTokens > report.Fleet[j].InputTokens+report.Fleet[j].OutputTokens
	})

	return report, nil
}
