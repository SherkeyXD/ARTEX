package tool

import (
	"strings"
	"sync"
)

// UnlockSet is a session-scoped, concurrency-safe set of deferred-tool names.
type UnlockSet struct {
	mu  sync.RWMutex
	set map[string]bool
}

// NewUnlockSet constructs an UnlockSet initialized with initial tool names.
func NewUnlockSet(initial ...string) *UnlockSet {
	s := &UnlockSet{set: make(map[string]bool)}
	for _, n := range initial {
		s.set[n] = true
	}
	return s
}

// Add adds tool names to the unlock set.
func (s *UnlockSet) Add(names ...string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		s.set[n] = true
	}
}

// Has checks whether a tool name is currently unlocked.
func (s *UnlockSet) Has(name string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set[name]
}

// List returns a slice of all currently unlocked tool names.
func (s *UnlockSet) List() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.set))
	for n := range s.set {
		out = append(out, n)
	}
	return out
}

// RenderDeferredToolsBlock formats an informational block for available deferred tools.
// The harness/agent injects this into system instructions so the model is aware of
// conditionally unlocked or dynamically loaded tools without bloating the static Schema.
func RenderDeferredToolsBlock(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<available-deferred-tools>\n")
	sb.WriteString("以下工具已经解锁，但完整 Schema 延迟加载：先用 SearchExtraTools 查找工具与参数，再用 ExecuteExtraTool 执行。\n")
	for _, n := range names {
		sb.WriteString("- ")
		sb.WriteString(n)
		sb.WriteString("\n")
	}
	sb.WriteString("优先使用基础核心工具；仅在需要专有功能时加载延迟工具的 Schema。\n")
	sb.WriteString("</available-deferred-tools>")
	return sb.String()
}
