package agent

import (
	"strings"
	"testing"
)

func TestExtractGrepMatches_Basic(t *testing.T) {
	text := `line 1: init
line 2: setup config
line 3: start server
line 4: found secret_token = "admin_12345"
line 5: connected to db
line 6: ready for requests`

	matches := ExtractGrepMatches(text, "secret_token", 1, 5)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if m.Line != 4 {
		t.Errorf("expected primary match line 4, got %d", m.Line)
	}
	// Context 1 line before and after -> lines 3, 4, 5
	if !strings.Contains(m.Snippet, "3 | line 3: start server") {
		t.Errorf("missing line 3 context: %s", m.Snippet)
	}
	if !strings.Contains(m.Snippet, ">    4 | line 4: found secret_token") {
		t.Errorf("missing line 4 hit: %s", m.Snippet)
	}
	if !strings.Contains(m.Snippet, "5 | line 5: connected to db") {
		t.Errorf("missing line 5 context: %s", m.Snippet)
	}
	if strings.Contains(m.Snippet, "line 1") || strings.Contains(m.Snippet, "line 6") {
		t.Errorf("snippet should not contain lines 1 or 6: %s", m.Snippet)
	}
}

func TestExtractGrepMatches_OverlappingRangesMerged(t *testing.T) {
	text := `line 1
line 2: hit_one
line 3: normal
line 4: hit_two
line 5: normal
line 6`

	// contextLines = 1:
	// hit_one (line 2) -> lines 1-3
	// hit_two (line 4) -> lines 3-5
	// They overlap at line 3 -> merged into range 1-5!
	matches := ExtractGrepMatches(text, "hit_", 1, 5)
	if len(matches) != 1 {
		t.Fatalf("expected merged range (1 match), got %d", len(matches))
	}
	m := matches[0]
	if m.Line != 2 {
		t.Errorf("expected primary match line 2, got %d", m.Line)
	}
	if !strings.Contains(m.Snippet, ">    2 | line 2: hit_one") {
		t.Errorf("expected hit 1 marked: %s", m.Snippet)
	}
	if !strings.Contains(m.Snippet, ">    4 | line 4: hit_two") {
		t.Errorf("expected hit 2 marked: %s", m.Snippet)
	}
	if !strings.Contains(m.Snippet, "  3 | line 3: normal") {
		t.Errorf("expected middle line 3 in merged snippet: %s", m.Snippet)
	}
}

func TestExtractGrepMatches_TruncateLongLine(t *testing.T) {
	prefix := strings.Repeat("A", 300)
	suffix := strings.Repeat("B", 300)
	longLine := prefix + " FLAG{grep_secret} " + suffix

	matches := ExtractGrepMatches(longLine, "FLAG{grep_secret}", 1, 5)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	snippet := matches[0].Snippet
	if !strings.Contains(snippet, "FLAG{grep_secret}") {
		t.Fatalf("snippet missing target: %s", snippet)
	}
	// Verify line was truncated and didn't dump all 600+ chars
	if len(snippet) > 400 {
		t.Errorf("snippet too long, line was not truncated properly: len=%d", len(snippet))
	}
	if !strings.Contains(snippet, "...") {
		t.Errorf("expected ellipsis in truncated line: %s", snippet)
	}
}

func TestExtractGrepMatches_CaseInsensitiveAndEmpty(t *testing.T) {
	if ExtractGrepMatches("", "foo", 2, 5) != nil {
		t.Error("expected nil on empty text")
	}
	if ExtractGrepMatches("hello world", "", 2, 5) != nil {
		t.Error("expected nil on empty query")
	}
	matches := ExtractGrepMatches("Hello WoRLD", "world", 1, 5)
	if len(matches) != 1 {
		t.Fatalf("expected case-insensitive match, got %d", len(matches))
	}
}

func TestSearchAndGetWorkerTraceTools_Definition(t *testing.T) {
	ts := NewToolSet(nil, "")
	searchTool := ts.searchAllWorkerTraces()
	if searchTool.Name() != "search_all_worker_traces" {
		t.Fatalf("unexpected name: %s", searchTool.Name())
	}
	if !strings.Contains(searchTool.Description(), "grep") {
		t.Errorf("searchAllWorkerTraces description should mention grep, got: %s", searchTool.Description())
	}

	getTraceTool := ts.getWorkerTrace()
	if getTraceTool.Name() != "get_worker_trace" {
		t.Fatalf("unexpected name: %s", getTraceTool.Name())
	}
	if !strings.Contains(getTraceTool.Description(), "grep") {
		t.Errorf("getWorkerTrace description should mention grep, got: %s", getTraceTool.Description())
	}
}

