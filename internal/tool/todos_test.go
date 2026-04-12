package tool

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func callTodos(t *testing.T, input string) fantasy.ToolResponse {
	t.Helper()
	tool := TodosTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("TodosTool error: %v", err)
	}
	return resp
}

func TestTodosTool(t *testing.T) {
	Todos = NewTodoStore()

	resp := callTodos(t, `{"todos":[
		{"content":"Write tests","status":"in_progress","active_form":"Writing tests"},
		{"content":"Deploy","status":"pending","active_form":"Deploying"}
	]}`)

	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "● Writing tests") {
		t.Errorf("should show just_started: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "[●] Write tests") {
		t.Errorf("should show in_progress item: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "[ ] Deploy") {
		t.Errorf("should show pending item: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "0/2 completed") {
		t.Errorf("should show 0/2: %s", resp.Content)
	}
}

func TestTodosToolDiff(t *testing.T) {
	Todos = NewTodoStore()

	callTodos(t, `{"todos":[
		{"content":"Step 1","status":"in_progress","active_form":"Doing step 1"},
		{"content":"Step 2","status":"pending","active_form":"Doing step 2"}
	]}`)

	resp := callTodos(t, `{"todos":[
		{"content":"Step 1","status":"completed","active_form":"Doing step 1"},
		{"content":"Step 2","status":"in_progress","active_form":"Doing step 2"}
	]}`)

	if !strings.Contains(resp.Content, "✓ Step 1") {
		t.Errorf("should show just_completed: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "● Doing step 2") {
		t.Errorf("should show just_started: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "1/2 completed") {
		t.Errorf("should show 1/2: %s", resp.Content)
	}
}

func TestTodosToolAllCompleted(t *testing.T) {
	Todos = NewTodoStore()

	callTodos(t, `{"todos":[
		{"content":"Only task","status":"in_progress","active_form":"Doing it"}
	]}`)

	resp := callTodos(t, `{"todos":[
		{"content":"Only task","status":"completed","active_form":"Doing it"}
	]}`)

	if !strings.Contains(resp.Content, "✓ Only task") {
		t.Errorf("should show just_completed: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "1/1 completed") {
		t.Errorf("should show 1/1: %s", resp.Content)
	}
}

func TestTodosToolInvalidStatus(t *testing.T) {
	Todos = NewTodoStore()

	resp := callTodos(t, `{"todos":[{"content":"Bad","status":"invalid","active_form":"Being bad"}]}`)
	if !resp.IsError {
		t.Error("should reject invalid status")
	}
	if !strings.Contains(resp.Content, "invalid status") {
		t.Errorf("error should mention invalid status: %s", resp.Content)
	}
}

func TestTodosToolEmptyContent(t *testing.T) {
	Todos = NewTodoStore()

	resp := callTodos(t, `{"todos":[{"content":"","status":"pending","active_form":"Doing"}]}`)
	if !resp.IsError {
		t.Error("should reject empty content")
	}
}

func TestTodosToolEmptyActiveForm(t *testing.T) {
	Todos = NewTodoStore()

	resp := callTodos(t, `{"todos":[{"content":"Task","status":"pending","active_form":""}]}`)
	if !resp.IsError {
		t.Error("should reject empty active_form")
	}
}

func TestTodosToolEmptyList(t *testing.T) {
	Todos = NewTodoStore()

	resp := callTodos(t, `{"todos":[]}`)
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
	if output.Completed != 1 {
		t.Errorf("completed = %d", output.Completed)
	}
}
