package persist

import (
	"encoding/json"
	"testing"

	"charm.land/fantasy"
)

func makeEvent(evType string, data any) SessionEvent {
	raw, _ := json.Marshal(data)
	return SessionEvent{Type: evType, Data: raw}
}

func TestRebuildMessages(t *testing.T) {
	events := []SessionEvent{
		makeEvent(EventUserMessage, MessageData{Content: "Hello"}),
		makeEvent(EventAssistantMessage, MessageData{Content: "Hi there"}),
		makeEvent(EventToolCall, ToolCallData{Name: "shell", Input: "ls"}),
		makeEvent(EventToolResult, ToolCallData{Name: "shell", ToolCallID: "tc1", Output: "files"}),
		makeEvent(EventStepFinish, StepData{TokensIn: 100, TokensOut: 50}), // should be skipped
		makeEvent(EventSystemMessage, MessageData{Content: "info"}),        // should be skipped
	}

	msgs := RebuildMessages(events)

	// user, assistant, tool_call (appended to assistant), tool_result = 3 messages
	// (tool_call merges into assistant message)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3 (user, assistant+tool_call, tool_result)", len(msgs))
	}
	if msgs[0].Role != fantasy.MessageRoleUser {
		t.Errorf("msg[0].Role = %q", msgs[0].Role)
	}
	if msgs[1].Role != fantasy.MessageRoleAssistant {
		t.Errorf("msg[1].Role = %q", msgs[1].Role)
	}
	// Assistant message should have 2 parts: text + tool call
	if len(msgs[1].Content) != 2 {
		t.Errorf("msg[1] parts = %d, want 2 (text + tool_call)", len(msgs[1].Content))
	}
	if msgs[2].Role != fantasy.MessageRoleTool {
		t.Errorf("msg[2].Role = %q", msgs[2].Role)
	}
}

func TestRebuildMessagesEmpty(t *testing.T) {
	msgs := RebuildMessages(nil)
	if len(msgs) != 0 {
		t.Errorf("got %d, want 0", len(msgs))
	}
}

func TestRebuildMessagesCompaction(t *testing.T) {
	events := []SessionEvent{
		makeEvent(EventUserMessage, MessageData{Content: "old message 1"}),
		makeEvent(EventAssistantMessage, MessageData{Content: "old response"}),
		makeEvent(EventCompaction, MessageData{Content: "Summary of conversation so far"}),
		makeEvent(EventUserMessage, MessageData{Content: "new message"}),
	}

	msgs := RebuildMessages(events)

	// Compaction should replace everything before it
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (summary + new message)", len(msgs))
	}
}
