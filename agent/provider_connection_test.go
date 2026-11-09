package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SherkeyXD/ARTEX/llm"
)

func TestConnectionUsesConfiguredWireMode(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if got, _ := body["stream"].(bool); got != streaming {
					t.Errorf("wire stream=%v, want %v", got, streaming)
				}
				if !strings.HasPrefix(r.Header.Get("x-session-id"), "conntest-") {
					t.Error("missing session header")
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			_, text, err := TestConnection(context.Background(), Config{Format: llm.FormatOpenAI, BaseURL: server.URL, APIKey: "test", Model: "mock", Stream: streaming, SessionHeaderKey: "x-session-id"})
			if err != nil || text != "OK" {
				t.Fatalf("text=%q err=%v", text, err)
			}
		})
	}
}
