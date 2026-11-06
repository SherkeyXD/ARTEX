package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SherkeyXD/ARTEX/db"
)

func TestFormatAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-48 * time.Hour), "2d ago"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatAge(tc.t)
			if got != tc.want {
				t.Errorf("formatAge(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

func TestCompactNodeIncludesAge(t *testing.T) {
	node := &db.Node{
		ID:        101,
		Kind:      db.KindFact,
		State:     db.StateFactConfirmed,
		Payload:   json.RawMessage(`{"summary":"nginx 1.25 detected"}`),
		CreatedAt: time.Now().Add(-10 * time.Minute),
	}
	m := compactNode(node)
	if m["id"] != int64(101) || m["state"] != db.StateFactConfirmed {
		t.Fatalf("unexpected compactNode content: %+v", m)
	}
	if age, ok := m["age"].(string); !ok || age != "10m ago" {
		t.Fatalf("want age '10m ago', got %v", m["age"])
	}
}
