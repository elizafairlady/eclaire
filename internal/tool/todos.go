package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

// TodoItem is a single todo entry.
type TodoItem struct {
	Content    string `json:"content" jsonschema:"description=Task description in imperative form (e.g. Run tests)"`
	Status     string `json:"status" jsonschema:"description=Status: pending in_progress or completed"`
	ActiveForm string `json:"active_form" jsonschema:"description=Present continuous form shown during execution (e.g. Running tests)"`
}

// TodoWriteOutput is the structured response from the todos tool.
type TodoWriteOutput struct {
	IsNew         bool       `json:"is_new"`
	Todos         []TodoItem `json:"todos"`
	JustCompleted []string   `json:"just_completed,omitempty"`
	JustStarted   string     `json:"just_started,omitempty"`
	Completed     int        `json:"completed"`
	Total         int        `json:"total"`
}

type todosInput struct {
	Todos []TodoItem `json:"todos" jsonschema:"description=Complete todo list (replaces existing). Each item needs content, status, and active_form."`
}

// TodoStore manages per-session todo lists in memory.
type TodoStore struct {
	lists map[string][]TodoItem
	mu    sync.Mutex
}

// NewTodoStore creates a new store.
func NewTodoStore() *TodoStore {
	return &TodoStore{lists: make(map[string][]TodoItem)}
}

// Todos is the global todo store.
var Todos = NewTodoStore()

// Set replaces the todo list for a session.
func (s *TodoStore) Set(sessionID string, items []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists[sessionID] = items
}

// Get returns the todo list for a session.
func (s *TodoStore) Get(sessionID string) []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if items, ok := s.lists[sessionID]; ok {
		out := make([]TodoItem, len(items))
		copy(out, items)
		return out
	}
	return nil
}

// Diff computes what changed between old and new todos, updates the store, and returns structured output.
func (s *TodoStore) Diff(sessionID string, newTodos []TodoItem) TodoWriteOutput {
	s.mu.Lock()
	old := s.lists[sessionID]
	s.lists[sessionID] = newTodos
	s.mu.Unlock()

	isNew := len(old) == 0

	// Build set of previously completed items
	wasCompleted := make(map[string]bool)
	for _, item := range old {
		if item.Status == "completed" {
			wasCompleted[item.Content] = true
		}
	}

	var justCompleted []string
	var justStarted string
	completed := 0
	for _, item := range newTodos {
		if item.Status == "completed" {
			completed++
			if !wasCompleted[item.Content] {
				justCompleted = append(justCompleted, item.Content)
			}
		}
		if item.Status == "in_progress" && justStarted == "" {
			if item.ActiveForm != "" {
				justStarted = item.ActiveForm
			} else {
				justStarted = item.Content
			}
		}
	}

	return TodoWriteOutput{
		IsNew:         isNew,
		Todos:         newTodos,
		JustCompleted: justCompleted,
		JustStarted:   justStarted,
		Completed:     completed,
		Total:         len(newTodos),
	}
}

// TodosDeps holds optional dependencies for the todos tool.
type TodosDeps struct {
	Sessions      *persist.SessionStore          // optional, for persisting todo events
	SessionIDFunc func(ctx context.Context) string // extracts session ID from context; nil = "default"
}

// TodosTool creates the session-scoped todo management tool.
func TodosTool() Tool {
	return TodosToolWithDeps(TodosDeps{})
}

// TodosToolWithDeps creates the todos tool with optional session persistence.
func TodosToolWithDeps(deps TodosDeps) Tool {
	return NewTool("todos",
		"Update the structured task checklist for the current session. "+
			"Each todo needs content (imperative: 'Run tests'), active_form (present continuous: 'Running tests'), and status (pending/in_progress/completed). "+
			"Replaces the entire list each call. Use to track multi-step work progress.",
		TrustReadOnly, "agent",
		func(ctx context.Context, input todosInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if len(input.Todos) == 0 {
				return fantasy.NewTextErrorResponse("todos must not be empty"), nil
			}

			// Validate
			for i, item := range input.Todos {
				if strings.TrimSpace(item.Content) == "" {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("todo %d: content must not be empty", i)), nil
				}
				if strings.TrimSpace(item.ActiveForm) == "" {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("todo %d: active_form must not be empty", i)), nil
				}
				switch item.Status {
				case "pending", "in_progress", "completed":
				default:
					return fantasy.NewTextErrorResponse(fmt.Sprintf("todo %d: invalid status %q (must be pending, in_progress, or completed)", i, item.Status)), nil
				}
			}

			// Resolve session ID from context
			sessionID := "default"
			if deps.SessionIDFunc != nil {
				if sid := deps.SessionIDFunc(ctx); sid != "" {
					sessionID = sid
				}
			}

			// Compute diff and update store
			output := Todos.Diff(sessionID, input.Todos)

			// Persist to session events if store available
			if deps.Sessions != nil && sessionID != "default" {
				persistItems := make([]persist.TodoItem, len(input.Todos))
				for i, item := range input.Todos {
					persistItems[i] = persist.TodoItem{
						Content:    item.Content,
						Status:     item.Status,
						ActiveForm: item.ActiveForm,
					}
				}
				deps.Sessions.Append(sessionID, persist.EventTodoUpdate, persist.TodoData{Todos: persistItems})
			}

			return fantasy.ToolResponse{Content: formatTodoOutput(output)}, nil
		},
	)
}

func formatTodoOutput(o TodoWriteOutput) string {
	var sb strings.Builder

	if len(o.JustCompleted) > 0 {
		for _, c := range o.JustCompleted {
			sb.WriteString("✓ " + c + "\n")
		}
	}
	if o.JustStarted != "" {
		sb.WriteString("● " + o.JustStarted + "\n")
	}
	if len(o.JustCompleted) > 0 || o.JustStarted != "" {
		sb.WriteByte('\n')
	}

	for _, item := range o.Todos {
		switch item.Status {
		case "completed":
			sb.WriteString("  [✓] " + item.Content + "\n")
		case "in_progress":
			sb.WriteString("  [●] " + item.Content + "\n")
		default:
			sb.WriteString("  [ ] " + item.Content + "\n")
		}
	}

	sb.WriteString(fmt.Sprintf("\n%d/%d completed", o.Completed, o.Total))
	return sb.String()
}

// HasIncompleteTodos returns true if any todo is not completed.
func HasIncompleteTodos(todos []TodoItem) bool {
	for _, t := range todos {
		if t.Status != "completed" {
			return true
		}
	}
	return false
}

// CurrentTodoActiveForm returns the activeForm of the first in_progress todo, or empty.
func CurrentTodoActiveForm(todos []TodoItem) string {
	for _, t := range todos {
		if t.Status == "in_progress" {
			if t.ActiveForm != "" {
				return t.ActiveForm
			}
			return t.Content
		}
	}
	return ""
}
