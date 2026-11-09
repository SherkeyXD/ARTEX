package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProviderKeepsFinalUnterminatedSSEFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"final tail"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	provider, err := NewProvider(Config{Format: FormatOpenAI, BaseURL: server.URL, APIKey: "test", Model: "mock", MaxRetries: -1, EmptyResponseRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	acc := NewAccumulator()
	for event, err := range provider.Stream(context.Background(), CompletionRequest{Messages: []Message{UserText("hello")}}) {
		if err != nil {
			t.Fatal(err)
		}
		acc.Add(event)
	}
	if acc.Message().Text() != "final tail" || acc.StopReason != "end_turn" {
		t.Fatalf("final frame lost: message=%+v stop=%s", acc.Message(), acc.StopReason)
	}
}
