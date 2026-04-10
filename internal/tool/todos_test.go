package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func callTodos(t *testing.T, input string) (fantasy.ToolResponse, TodoWriteOutput) {
	t.Helper()
	tool := TodosTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("TodosTool error: %v", err)
	}
	var output TodoWriteOutput
	if !resp.IsError {
		json.Unmarshal([]byte(resp.Content), &output)
	}
	return resp, output
}

func TestTodosTool(t *testing.T) {
	// Reset global store
	Todos = NewTodoStore()

	resp, output := callTodos(t, `{"todos":[
		{"content":"Write tests","status":"in_progress","active_form":"Writing tests"},
		{"content":"Deploy","status":"pending","active_form":"Deploying"}
	]}`)

	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if output.Total != 2 {
		t.Errorf("total = %d, want 2", output.Total)
	}
	if !output.IsNew {
		t.Error("should be is_new on first call")
	}
	if output.JustStarted != "Writing tests" {
		t.Errorf("just_started = %q, want 'Writing tests'", output.JustStarted)
	}
	if output.Completed != 0 {
		t.Errorf("completed = %d, want 0", output.Completed)
	}
}

func TestTodosToolDiff(t *testing.T) {
	Todos = NewTodoStore()

	// First call: create todos
	callTodos(t, `{"todos":[
		{"content":"Step 1","status":"in_progress","active_form":"Doing step 1"},
		{"content":"Step 2","status":"pending","active_form":"Doing step 2"}
	]}`)

	// Second call: complete step 1, start step 2
	_, output := callTodos(t, `{"todos":[
		{"content":"Step 1","status":"completed","active_form":"Doing step 1"},
		{"content":"Step 2","status":"in_progress","active_form":"Doing step 2"}
	]}`)

	if output.IsNew {
		t.Error("should not be is_new on second call")
	}
	if len(output.JustCompleted) != 1 || output.JustCompleted[0] != "Step 1" {
		t.Errorf("just_completed = %v, want [Step 1]", output.JustCompleted)
	}
	if output.JustStarted != "Doing step 2" {
		t.Errorf("just_started = %q", output.JustStarted)
	}
	if output.Completed != 1 {
		t.Errorf("completed = %d, want 1", output.Completed)
	}
}

func TestTodosToolAllCompleted(t *testing.T) {
	Todos = NewTodoStore()

	callTodos(t, `{"todos":[
		{"content":"Only task","status":"in_progress","active_form":"Doing it"}
	]}`)

	_, output := callTodos(t, `{"todos":[
		{"content":"Only task","status":"completed","active_form":"Doing it"}
	]}`)

	if output.Completed != 1 || output.Total != 1 {
		t.Errorf("completed=%d total=%d", output.Completed, output.Total)
	}
	if len(output.JustCompleted) != 1 {
		t.Errorf("just_completed = %v", output.JustCompleted)
	}
}

func TestTodosToolInvalidStatus(t *testing.T) {
	Todos = NewTodoStore()

	resp, _ := callTodos(t, `{"todos":[{"content":"Bad","status":"invalid","active_form":"Being bad"}]}`)
	if !resp.IsError {
		t.Error("should reject invalid status")
	}
	if !strings.Contains(resp.Content, "invalid status") {
		t.Errorf("error should mention invalid status: %s", resp.Content)
	}
}

func TestTodosToolEmptyContent(t *testing.T) {
	Todos = NewTodoStore()

	resp, _ := callTodos(t, `{"todos":[{"content":"","status":"pending","active_form":"Doing"}]}`)
	if !resp.IsError {
		t.Error("should reject empty content")
	}
}

func TestTodosToolEmptyActiveForm(t *testing.T) {
	Todos = NewTodoStore()

	resp, _ := callTodos(t, `{"todos":[{"content":"Task","status":"pending","active_form":""}]}`)
	if !resp.IsError {
		t.Error("should reject empty active_form")
	}
}

func TestTodosToolEmptyList(t *testing.T) {
	Todos = NewTodoStore()

	resp, _ := callTodos(t, `{"todos":[]}`)
	if !resp.IsError {
		t.Error("should reject empty todo list")
	}
}

func TestTodoStoreSessionIsolation(t *testing.T) {
	store := NewTodoStore()

	store.Set("sess-1", []TodoItem{{Content: "Task A", Status: "pending", ActiveForm: "Doing A"}})
	store.Set("sess-2", []TodoItem{{Content: "Task B", Status: "pending", ActiveForm: "Doing B"}})

	got1 := store.Get("sess-1")
	got2 := store.Get("sess-2")

	if len(got1) != 1 || got1[0].Content != "Task A" {
		t.Errorf("sess-1 = %v", got1)
	}
	if len(got2) != 1 || got2[0].Content != "Task B" {
		t.Errorf("sess-2 = %v", got2)
	}
}

func TestTodoStoreGetEmpty(t *testing.T) {
	store := NewTodoStore()
	got := store.Get("nonexistent")
	if got != nil {
		t.Error("should return nil for nonexistent session")
	}
}

func TestTodoStoreDiff(t *testing.T) {
	store := NewTodoStore()

	// First diff: is_new
	output := store.Diff("sess", []TodoItem{
		{Content: "A", Status: "pending", ActiveForm: "Doing A"},
		{Content: "B", Status: "in_progress", ActiveForm: "Doing B"},
	})
	if !output.IsNew {
		t.Error("first diff should be is_new")
	}
	if output.JustStarted != "Doing B" {
		t.Errorf("just_started = %q", output.JustStarted)
	}

	// Second diff: complete A
	output = store.Diff("sess", []TodoItem{
		{Content: "A", Status: "completed", ActiveForm: "Doing A"},
		{Content: "B", Status: "in_progress", ActiveForm: "Doing B"},
	})
	if output.IsNew {
		t.Error("second diff should not be is_new")
	}
	if len(output.JustCompleted) != 1 || output.JustCompleted[0] != "A" {
		t.Errorf("just_completed = %v", output.JustCompleted)
	}
}

func TestHasIncompleteTodos(t *testing.T) {
	if HasIncompleteTodos(nil) {
		t.Error("nil should be false")
	}
	if HasIncompleteTodos([]TodoItem{{Status: "completed"}}) {
		t.Error("all completed should be false")
	}
	if !HasIncompleteTodos([]TodoItem{{Status: "pending"}}) {
		t.Error("pending should be true")
	}
	if !HasIncompleteTodos([]TodoItem{{Status: "in_progress"}}) {
		t.Error("in_progress should be true")
	}
}

func TestCurrentTodoActiveForm(t *testing.T) {
	if CurrentTodoActiveForm(nil) != "" {
		t.Error("nil should return empty")
	}
	todos := []TodoItem{
		{Content: "Done", Status: "completed", ActiveForm: "Done stuff"},
		{Content: "Active", Status: "in_progress", ActiveForm: "Doing active stuff"},
		{Content: "Later", Status: "pending", ActiveForm: "Will do later"},
	}
	if got := CurrentTodoActiveForm(todos); got != "Doing active stuff" {
		t.Errorf("got %q, want 'Doing active stuff'", got)
	}
}
