package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

func TestTaskStatusTool(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	meta, err := store.Create("coding", "test task", "")
	if err != nil {
		t.Fatal(err)
	}

	tool := TaskStatusTool(store)
	if tool.Info().Name != "task_status" {
		t.Errorf("name = %q, want task_status", tool.Info().Name)
	}
	if tool.TrustTier() != TrustReadOnly {
		t.Errorf("tier = %d, want ReadOnly", tool.TrustTier())
	}

	// Call the tool
	input := taskStatusInput{SessionID: meta.ID}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: string(inputJSON),
	})
	if err != nil {
		t.Fatal(err)
	}

	text := resp.Content
	if !strings.Contains(text, meta.ID) {
		t.Errorf("response should contain session ID %q", meta.ID)
	}
	if !strings.Contains(text, "coding") {
		t.Error("response should contain agent_id 'coding'")
	}
	if !strings.Contains(text, "active") {
		t.Error("response should contain status 'active'")
	}
}

func TestTaskStatusToolMissing(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	tool := TaskStatusTool(store)
	input := taskStatusInput{SessionID: "nonexistent"}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: string(inputJSON),
	})
	if err != nil {
		t.Fatal(err)
	}

	text := resp.Content
	if !strings.Contains(text, "not found") {
		t.Errorf("response should contain 'not found', got: %s", text)
	}
}

func TestTaskStatusToolEmpty(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	tool := TaskStatusTool(store)
	input := taskStatusInput{SessionID: ""}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: string(inputJSON),
	})
	if err != nil {
		t.Fatal(err)
	}

	text := resp.Content
	if !strings.Contains(text, "session_id is required") {
		t.Errorf("response should contain validation error, got: %s", text)
	}
}

func TestTaskStatusToolChildSession(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	parent, err := store.Create("orchestrator", "parent task", "")
	if err != nil {
		t.Fatal(err)
	}

	child, err := store.SpawnChild(parent.ID, "coding", "child task")
	if err != nil {
		t.Fatal(err)
	}

	tool := TaskStatusTool(store)
	input := taskStatusInput{SessionID: child.ID}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: string(inputJSON),
	})
	if err != nil {
		t.Fatal(err)
	}

	text := resp.Content
	if !strings.Contains(text, parent.ID) {
		t.Errorf("child session response should contain parent_id %q", parent.ID)
	}
}
