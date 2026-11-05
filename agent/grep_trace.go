package agent

import (
	"fmt"
	"strings"
)

// GrepMatch represents a matching line and its surrounding context snippet.
type GrepMatch struct {
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// ExtractGrepMatches searches text for query (case-insensitive) and returns
// up to maxMatches snippets, each showing contextLines before and after.
// Overlapping windows are automatically merged.
// Excessively long lines (> 300 runes) are centered/truncated around the match
// to protect the model's context window.
func ExtractGrepMatches(text, query string, contextLines, maxMatches int) []GrepMatch {
	if text == "" || query == "" {
		return nil
	}
	if contextLines < 0 {
		contextLines = 2
	}
	if maxMatches <= 0 {
		maxMatches = 5
	}

	lines := strings.Split(text, "\n")
	qLower := strings.ToLower(query)

	var matchIndices []int
	for i, l := range lines {
		if strings.Contains(strings.ToLower(l), qLower) {
			matchIndices = append(matchIndices, i)
		}
	}
	if len(matchIndices) == 0 {
		return nil
	}

	// Group overlapping ranges [start, end] (0-based inclusive)
	type matchRange struct {
		start, end int
		hitLines   []int
	}
	var ranges []matchRange
	for _, idx := range matchIndices {
		s := idx - contextLines
		if s < 0 {
			s = 0
		}
		e := idx + contextLines
		if e >= len(lines) {
			e = len(lines) - 1
		}

		if len(ranges) > 0 && s <= ranges[len(ranges)-1].end+1 {
			// Merge with previous overlapping/adjacent range
			prev := &ranges[len(ranges)-1]
			if e > prev.end {
				prev.end = e
			}
			prev.hitLines = append(prev.hitLines, idx)
		} else {
			ranges = append(ranges, matchRange{
				start:    s,
				end:      e,
				hitLines: []int{idx},
			})
		}
	}

	out := make([]GrepMatch, 0, len(ranges))
	for _, r := range ranges {
		if len(out) >= maxMatches {
			break
		}
		var sb strings.Builder
		for i := r.start; i <= r.end; i++ {
			lineNum := i + 1
			isHit := false
			for _, h := range r.hitLines {
				if h == i {
					isHit = true
					break
				}
			}

			lineContent := truncateLongLine(lines[i], qLower, isHit)
			prefix := "  "
			if isHit {
				prefix = "> "
			}
			fmt.Fprintf(&sb, "%s%4d | %s", prefix, lineNum, lineContent)
			if i < r.end {
				sb.WriteString("\n")
			}
		}

		primaryLine := r.hitLines[0] + 1
		out = append(out, GrepMatch{
			Line:    primaryLine,
			Snippet: sb.String(),
		})
	}

	return out
}

func truncateLongLine(line, qLower string, isHit bool) string {
	runes := []rune(line)
	const maxLen = 300
	if len(runes) <= maxLen {
		return line
	}

	if !isHit {
		return string(runes[:maxLen]) + "..."
	}

	// Center truncation around query match
	lineLower := strings.ToLower(line)
	bytePos := strings.Index(lineLower, qLower)
	if bytePos < 0 {
		return string(runes[:maxLen]) + "..."
	}

	runePos := len([]rune(line[:bytePos]))
	start := runePos - 100
	if start < 0 {
		start = 0
	}
	end := runePos + len([]rune(qLower)) + 100
	if end > len(runes) {
		end = len(runes)
	}

	var res strings.Builder
	if start > 0 {
		res.WriteString("...")
	}
	res.WriteString(string(runes[start:end]))
	if end < len(runes) {
		res.WriteString("...")
	}
	return res.String()
}
