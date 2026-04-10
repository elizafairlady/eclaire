package agent

import (
	"context"
	"testing"
)

func TestEmitContext(t *testing.T) {
	called := false
	emit := func(ev StreamEvent) error {
		called = true
		return nil
	}

	ctx := ContextWithEmit(context.Background(), emit)

	fn, ok := EmitFromContext(ctx)
	if !ok {
		t.Fatal("EmitFromContext should return true")
	}
	fn(StreamEvent{Type: EventTextDelta})
	if !called {
		t.Error("emit function should have been called")
	}
}

func TestEmitContextMissing(t *testing.T) {
	_, ok := EmitFromContext(context.Background())
	if ok {
		t.Error("EmitFromContext should return false for bare context")
	}
}

func TestEmitContextPreservesFunction(t *testing.T) {
	var captured StreamEvent
	emit := func(ev StreamEvent) error {
		captured = ev
		return nil
	}

	ctx := ContextWithEmit(context.Background(), emit)
	fn, _ := EmitFromContext(ctx)

	fn(StreamEvent{Type: EventToolCall, ToolName: "shell", AgentID: "coding"})

	if captured.Type != EventToolCall {
		t.Errorf("type = %q, want tool_call", captured.Type)
	}
	if captured.ToolName != "shell" {
		t.Errorf("tool_name = %q, want shell", captured.ToolName)
	}
	if captured.AgentID != "coding" {
		t.Errorf("agent_id = %q, want coding", captured.AgentID)
	}
}

func TestStreamEventConstants(t *testing.T) {
	// Verify sub-agent event type constants exist and are distinct
	types := []string{
		EventSubAgentStarted,
		EventSubAgentToolCall,
		EventSubAgentToolResult,
		EventSubAgentCompleted,
	}
	seen := map[string]bool{}
	for _, typ := range types {
		if typ == "" {
			t.Error("event type constant should not be empty")
		}
		if seen[typ] {
			t.Errorf("duplicate event type: %s", typ)
		}
		seen[typ] = true
	}
}

func TestStreamEventNestedFields(t *testing.T) {
	ev := StreamEvent{
		Type:    EventSubAgentToolCall,
		Nested:  true,
		AgentID: "coding",
		TaskID:  "task_coding_123",
	}

	if !ev.Nested {
		t.Error("Nested should be true")
	}
	if ev.AgentID != "coding" {
		t.Errorf("AgentID = %q, want coding", ev.AgentID)
	}
	if ev.TaskID != "task_coding_123" {
		t.Errorf("TaskID = %q, want task_coding_123", ev.TaskID)
	}
}
