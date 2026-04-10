package agent

import (
	"strings"
	"testing"
)

func TestSystemEventQueue_EnqueueAndDrain(t *testing.T) {
	q := NewSystemEventQueue()

	q.Enqueue("main", "job completed", "cron", "cron:test")
	q.Enqueue("main", "heartbeat done", "heartbeat", "hb:1")

	events := q.Drain("main")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Text != "job completed" {
		t.Errorf("events[0].Text = %q, want %q", events[0].Text, "job completed")
	}
	if events[1].Text != "heartbeat done" {
		t.Errorf("events[1].Text = %q, want %q", events[1].Text, "heartbeat done")
	}

	// After drain, queue should be empty
	if q.HasEvents("main") {
		t.Error("queue should be empty after drain")
	}
	events2 := q.Drain("main")
	if len(events2) != 0 {
		t.Errorf("second drain got %d events, want 0", len(events2))
	}
}

func TestSystemEventQueue_ConsecutiveDedup(t *testing.T) {
	q := NewSystemEventQueue()

	ok1 := q.Enqueue("main", "same text", "cron", "k1")
	ok2 := q.Enqueue("main", "same text", "cron", "k2")
	ok3 := q.Enqueue("main", "different", "cron", "k3")
	ok4 := q.Enqueue("main", "same text", "cron", "k4") // not consecutive anymore

	if !ok1 {
		t.Error("first enqueue should succeed")
	}
	if ok2 {
		t.Error("consecutive duplicate should be deduped")
	}
	if !ok3 {
		t.Error("different text should succeed")
	}
	if !ok4 {
		t.Error("non-consecutive duplicate should succeed")
	}

	events := q.Drain("main")
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
}

func TestSystemEventQueue_MaxCap(t *testing.T) {
	q := NewSystemEventQueue()

	for i := 0; i < 30; i++ {
		q.Enqueue("main", strings.Repeat("x", i+1), "cron", "")
	}

	events := q.Drain("main")
	if len(events) != MaxEventsPerSession {
		t.Fatalf("got %d events, want %d (cap)", len(events), MaxEventsPerSession)
	}

	// Should have the last 20 events (events 10-29)
	if len(events[0].Text) != 11 {
		t.Errorf("first event text len = %d, want 11 (10th event)", len(events[0].Text))
	}
}

func TestSystemEventQueue_Peek(t *testing.T) {
	q := NewSystemEventQueue()
	q.Enqueue("main", "test", "system", "")

	peeked := q.Peek("main")
	if len(peeked) != 1 {
		t.Fatalf("peek got %d events, want 1", len(peeked))
	}

	// Peek should not clear
	if !q.HasEvents("main") {
		t.Error("queue should still have events after peek")
	}
}

func TestSystemEventQueue_SessionIsolation(t *testing.T) {
	q := NewSystemEventQueue()
	q.Enqueue("session-a", "event for A", "cron", "")
	q.Enqueue("session-b", "event for B", "cron", "")

	eventsA := q.Drain("session-a")
	if len(eventsA) != 1 || eventsA[0].Text != "event for A" {
		t.Errorf("session-a events wrong: %v", eventsA)
	}

	eventsB := q.Drain("session-b")
	if len(eventsB) != 1 || eventsB[0].Text != "event for B" {
		t.Errorf("session-b events wrong: %v", eventsB)
	}
}

func TestSystemEventQueue_EmptyText(t *testing.T) {
	q := NewSystemEventQueue()
	ok := q.Enqueue("main", "", "cron", "")
	if ok {
		t.Error("empty text should not be enqueued")
	}
	ok = q.Enqueue("main", "  \n  ", "cron", "")
	if ok {
		t.Error("whitespace-only text should not be enqueued")
	}
}

func TestFormatDrained(t *testing.T) {
	events := []SystemEvent{
		{Text: "cron 'monitor' completed", Source: "cron"},
		{Text: "sub-agent 'research' finished", Source: "subagent"},
	}

	result := FormatDrained(events)
	if !strings.HasPrefix(result, "# System Events\n") {
		t.Errorf("should start with header, got: %s", result)
	}
	if !strings.Contains(result, "cron 'monitor' completed") {
		t.Error("should contain first event text")
	}
	if !strings.Contains(result, "sub-agent 'research' finished") {
		t.Error("should contain second event text")
	}
}

func TestFormatDrained_Empty(t *testing.T) {
	if result := FormatDrained(nil); result != "" {
		t.Errorf("nil events should return empty string, got: %q", result)
	}
	if result := FormatDrained([]SystemEvent{}); result != "" {
		t.Errorf("empty events should return empty string, got: %q", result)
	}
}
