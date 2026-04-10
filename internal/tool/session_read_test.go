package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

func TestSessionReadTool(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	// Create a session and add some events
	meta, err := store.Create("research", "Test research session", "")
	if err != nil {
		t.Fatal(err)
	}

	store.Append(meta.ID, persist.EventUserMessage, persist.MessageData{Content: "Research something"})
	store.Append(meta.ID, persist.EventToolCall, persist.ToolCallData{Name: "web_search", Input: `{"query":"test"}`})
	store.Append(meta.ID, persist.EventToolResult, persist.ToolCallData{Name: "web_search", Output: "search results here"})
	store.Append(meta.ID, persist.EventAssistantMessage, persist.MessageData{Content: "Here is my research report with findings."})

	tool := SessionReadTool(store)

	info := tool.Info()
	if info.Name != "session_read" {
		t.Errorf("name = %q, want session_read", info.Name)
	}

	// Read the session
	input := sessionReadInput{SessionID: meta.ID, Last: 5}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: string(inputJSON)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}

	// Should contain the assistant message
	if got := resp.Content; got == "" {
		t.Fatal("expected non-empty response")
	}
	if !contains(resp.Content, "Here is my research report") {
		t.Errorf("response should contain assistant message, got: %s", resp.Content)
	}
	if !contains(resp.Content, "search results here") {
		t.Errorf("response should contain tool result, got: %s", resp.Content)
	}
}

func TestSessionReadToolEmpty(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	meta, _ := store.Create("test", "Empty session", "")

	tool := SessionReadTool(store)
	input := sessionReadInput{SessionID: meta.ID}
	inputJSON, _ := json.Marshal(input)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{Input: string(inputJSON)})

	if !contains(resp.Content, "No messages found") {
		t.Errorf("expected 'No messages found', got: %s", resp.Content)
	}
}

func TestSessionReadToolNotFound(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	tool := SessionReadTool(store)
	input := sessionReadInput{SessionID: "nonexistent"}
	inputJSON, _ := json.Marshal(input)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{Input: string(inputJSON)})

	if !resp.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestSessionReadToolLastN(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)
	meta, _ := store.Create("test", "Many messages", "")

	// Add 10 assistant messages
	for i := 0; i < 10; i++ {
		store.Append(meta.ID, persist.EventAssistantMessage, persist.MessageData{
			Content: fmt.Sprintf("Message %d", i),
		})
	}

	tool := SessionReadTool(store)
	input := sessionReadInput{SessionID: meta.ID, Last: 3}
	inputJSON, _ := json.Marshal(input)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{Input: string(inputJSON)})

	// Should contain messages 7, 8, 9 (last 3)
	if !contains(resp.Content, "Message 7") {
		t.Error("should contain Message 7")
	}
	if !contains(resp.Content, "Message 9") {
		t.Error("should contain Message 9")
	}
	if contains(resp.Content, "Message 6") {
		t.Error("should NOT contain Message 6")
	}
}

func TestSessionReadToolMissingID(t *testing.T) {
	dir := t.TempDir()
	store := persist.NewSessionStore(dir)

	tool := SessionReadTool(store)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{Input: `{}`})

	if !resp.IsError {
		t.Error("expected error for missing session_id")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
