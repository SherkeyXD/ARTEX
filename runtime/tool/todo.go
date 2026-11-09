package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoStatus represents the progress state of a planned step.
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

// Todo is a single task or milestone item tracked across turns.
type Todo struct {
	ID      string     `json:"id,omitempty"`
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

// TodoStore manages a concurrent-safe list of Todo items for an agent session.
type TodoStore struct {
	mu    sync.RWMutex
	items []Todo
}

// NewTodoStore creates an empty TodoStore.
func NewTodoStore() *TodoStore {
	return &TodoStore{}
}

// List returns a copy of all current todo items.
func (s *TodoStore) List() []Todo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Todo, len(s.items))
	copy(out, s.items)
	return out
}

// Set replaces the entire list of todo items.
func (s *TodoStore) Set(items []Todo) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make([]Todo, len(items))
	copy(s.items, items)
}

// Add appends one or more items to the store.
func (s *TodoStore) Add(items ...Todo) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, items...)
}

// TodoWrite returns a CoreTool that allows an LLM to update its planning list.
func (s *TodoStore) TodoWrite() CoreTool {
	return Build(Spec{
		Name:        "TodoWrite",
		Description: "Update the agent's persistent planning todo list. Replaces the current list with new items.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
							"status": map[string]any{
								"type": "string",
								"enum": []string{"pending", "in_progress", "completed"},
							},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			var args struct {
				Todos []Todo `json:"todos"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return Errorf(fmt.Sprintf("invalid todos arguments: %v", err)), nil
			}
			s.Set(args.Todos)
			return Text(fmt.Sprintf("Updated todo list (%d items)", len(args.Todos))), nil
		},
	})
}

// TodoRead returns a CoreTool that outputs the current planning list.
func (s *TodoStore) TodoRead() CoreTool {
	return Build(Spec{
		Name:        "TodoRead",
		Description: "Read the agent's current planning todo list.",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Run: func(ctx context.Context, input json.RawMessage, tc *ToolContext) (Result, error) {
			items := s.List()
			if len(items) == 0 {
				return Text("(todo list is empty)"), nil
			}
			var sb strings.Builder
			for i, it := range items {
				mark := "☐"
				if it.Status == TodoInProgress {
					mark = "▶"
				} else if it.Status == TodoCompleted {
					mark = "✔"
				}
				sb.WriteString(fmt.Sprintf("%d. %s [%s] %s\n", i+1, mark, it.Status, it.Content))
			}
			return Text(strings.TrimRight(sb.String(), "\n")), nil
		},
	})
}
