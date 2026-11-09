package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SherkeyXD/ARTEX/db"
	"github.com/SherkeyXD/ARTEX/llm"
)

const activityFlushInterval = 80 * time.Millisecond
const activityFragmentRunes = 512

// A block has one identity across immutable activity rows. This works with
// both task SSE and conversation polling, including history replay after reload.
type activityStream struct {
	emit    func(db.Activity)
	prefix  string
	block   int
	kind    string
	id      string
	pending strings.Builder
	text    strings.Builder
	started time.Time
	first   bool
}

func newActivityStream(emit func(db.Activity)) *activityStream {
	return &activityStream{emit: emit, prefix: "stream:" + llm.NewSessionID()}
}

func (s *activityStream) add(kind, text string) {
	if text == "" {
		return
	}
	if s.kind != kind {
		s.boundary()
		s.block++
		s.kind = kind
		s.id = s.prefix + ":" + strconv.Itoa(s.block)
		s.started = time.Now()
		s.first = true
	}
	s.text.WriteString(text)
	runes := []rune(text)
	for len(runes) > 0 {
		count := activityFragmentRunes - utf8.RuneCountInString(s.pending.String())
		if len(runes) > count {
			s.pending.WriteString(string(runes[:count]))
			runes = runes[count:]
			s.flush()
		} else {
			s.pending.WriteString(string(runes))
			runes = nil
		}
	}
	if s.first {
		s.flush()
		s.first = false
	}
}

func (s *activityStream) flush() {
	if s.pending.Len() == 0 {
		return
	}
	fragment := s.pending.String()
	s.pending.Reset()
	metadata, _ := json.Marshal(map[string]any{"stream_id": s.id, "delta": true, "duration_ms": time.Since(s.started).Milliseconds(), "char_count": utf8.RuneCountInString(fragment)})
	s.emit(db.Activity{Kind: s.kind, ToolUseID: s.id, Summary: fragment, Detail: fragment, Metadata: metadata})
}

func (s *activityStream) boundary() { s.flush(); s.kind = ""; s.id = ""; s.text.Reset() }

func (s *activityStream) finalID(text string) string {
	if s.kind == "text" && text != "" && s.text.String() == text {
		return s.id
	}
	return ""
}
