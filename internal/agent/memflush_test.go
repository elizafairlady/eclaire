package agent

import (
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

func TestContextHash_Deterministic(t *testing.T) {
	msgs := []fantasy.Message{
		fantasy.NewUserMessage("hello"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "hi there"}}},
		fantasy.NewUserMessage("what's up"),
	}

	h1 := contextHash(msgs)
	h2 := contextHash(msgs)
	if h1 != h2 {
		t.Errorf("hash should be deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("hash length should be 16 hex chars, got %d", len(h1))
	}
}

func TestContextHash_ChangesWithContent(t *testing.T) {
	msgs1 := []fantasy.Message{
		fantasy.NewUserMessage("hello"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "response A"}}},
	}
	msgs2 := []fantasy.Message{
		fantasy.NewUserMessage("hello"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "response B"}}},
	}

	h1 := contextHash(msgs1)
	h2 := contextHash(msgs2)
	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
}

func TestContextHash_UsesLastThree(t *testing.T) {
	base := []fantasy.Message{
		fantasy.NewUserMessage("old message 1"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "old reply 1"}}},
		fantasy.NewUserMessage("old message 2"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "old reply 2"}}},
		fantasy.NewUserMessage("recent 1"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "recent 2"}}},
		fantasy.NewUserMessage("recent 3"),
	}

	// Adding more old messages shouldn't change the hash if the last 3 are the same
	extended := append([]fantasy.Message{fantasy.NewUserMessage("even older")}, base...)

	h1 := contextHash(base)
	h2 := contextHash(extended)
	// Different because message count changes (7 vs 8)
	if h1 == h2 {
		t.Error("different message counts should produce different hashes")
	}
}

func TestMemoryFlushState_ShouldFlush(t *testing.T) {
	state := newMemoryFlushState()

	msgs := []fantasy.Message{
		fantasy.NewUserMessage("test message"),
	}

	// First time: should flush
	if !state.shouldFlush("sess-1", msgs) {
		t.Error("should flush on first check")
	}

	// Record flush
	state.recordFlush("sess-1", msgs)

	// Same context: should NOT flush
	if state.shouldFlush("sess-1", msgs) {
		t.Error("should not flush same context")
	}

	// Different session: should flush
	if !state.shouldFlush("sess-2", msgs) {
		t.Error("should flush for different session")
	}

	// Add a message: should flush again
	msgs2 := append(msgs, fantasy.NewUserMessage("new message"))
	if !state.shouldFlush("sess-1", msgs2) {
		t.Error("should flush when context changes")
	}
}

func TestEventMemoryFlush_RebuildMessages(t *testing.T) {
	events := []persist.SessionEvent{
		{Type: persist.EventUserMessage, Data: mustJSON(persist.MessageData{Content: "hello"})},
		{Type: persist.EventMemoryFlush, Data: mustJSON(persist.MessageData{Content: "saved context"})},
		{Type: persist.EventAssistantMessage, Data: mustJSON(persist.MessageData{Content: "continued after flush"})},
	}

	messages := persist.RebuildMessages(events)
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	// The memory flush should be a system message
	if messages[1].Role != fantasy.MessageRoleSystem {
		t.Errorf("memory flush should be system role, got %s", messages[1].Role)
	}
	text := extractText(messages[1])
	if text != "[Memory saved before compaction]" {
		t.Errorf("memory flush text = %q, want %q", text, "[Memory saved before compaction]")
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
