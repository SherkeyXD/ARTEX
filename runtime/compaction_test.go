package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"testing"
)

type summaryProvider struct {
	call func(CompletionRequest) (string, error)
}

func (p summaryProvider) Stream(_ context.Context, req CompletionRequest) iter.Seq2[LLMStreamEvent, error] {
	return func(yield func(LLMStreamEvent, error) bool) {
		text, err := p.call(req)
		yield(LLMStreamEvent{Type: LLMEventText, Text: text}, err)
	}
}

func compactionHistory() []Message {
	var messages []Message
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		messages = append(messages, NewUserMessage("objective "+id), NewAssistantMessage(ToolCallPart(id, "read", json.RawMessage(`{}`))), NewToolResultMessage(id, "read", strings.Repeat("facts ", 500), false, 0))
	}
	messages = append(messages, NewUserMessage("continue"))
	return messages
}

func TestCompactionPreservesCompleteToolExchanges(t *testing.T) {
	session := NewSession("session", "agent", "task")
	session.AppendMessage(compactionHistory()...)
	req := CompletionRequest{Messages: session.Messages(), NonStreaming: true}
	provider := summaryProvider{call: func(summary CompletionRequest) (string, error) {
		if contextTokens(summary)+summary.MaxTokens > 5000 {
			t.Fatal("summary request itself exceeds the context window")
		}
		if len(summary.Tools) != 0 || summary.ThinkingType != "disabled" || !summary.NonStreaming {
			t.Fatalf("unsafe summary request: %+v", summary)
		}
		return "objective and discovered facts preserved", nil
	}}
	changed, err := CompactSession(context.Background(), session, provider, req, &CompactionConfig{ContextWindow: 5000, KeepRecent: 3}, false)
	if err != nil || !changed {
		t.Fatalf("compact=%v err=%v", changed, err)
	}
	if session.Len() >= len(req.Messages) || !strings.Contains(session.Messages()[0].Text(), "discovered facts") {
		t.Fatal("summary not installed")
	}
	paired := map[string]bool{}
	for _, message := range session.Messages() {
		for _, call := range message.ToolCalls() {
			paired[call.ID] = true
		}
		for _, result := range message.ToolResults() {
			if !paired[result.ToolCallID] {
				t.Fatal("orphan tool result after compaction")
			}
			delete(paired, result.ToolCallID)
		}
	}
	if len(paired) != 0 {
		t.Fatal("dangling tool call after compaction")
	}
}

func TestCompactionFailureKeepsHistory(t *testing.T) {
	for _, failure := range []string{"network", "empty", "larger"} {
		t.Run(failure, func(t *testing.T) {
			session := NewSession("session", "agent", "task")
			session.AppendMessage(compactionHistory()...)
			before := session.Messages()
			provider := summaryProvider{call: func(CompletionRequest) (string, error) {
				switch failure {
				case "network":
					return "", errors.New("network failed")
				case "empty":
					return "", nil
				default:
					return strings.Repeat("larger", 10000), nil
				}
			}}
			changed, err := CompactSession(context.Background(), session, provider, CompletionRequest{Messages: before}, &CompactionConfig{ContextWindow: 5000, KeepRecent: 3}, true)
			if err == nil || changed || !reflect.DeepEqual(before, session.Messages()) {
				t.Fatalf("failed compaction mutated history: changed=%v err=%v", changed, err)
			}
		})
	}
}
