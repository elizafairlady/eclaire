package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
)

func newTestReminderStore(t *testing.T) *ReminderStore {
	t.Helper()
	return NewReminderStore(filepath.Join(t.TempDir(), "reminders.json"))
}

func callReminder(t *testing.T, store *ReminderStore, input string) fantasy.ToolResponse {
	t.Helper()
	tool := ReminderTool(store)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("ReminderTool error: %v", err)
	}
	return resp
}

func TestReminderAdd(t *testing.T) {
	store := newTestReminderStore(t)

	resp := callReminder(t, store, `{"operation":"add","text":"Walk the dogs","due":"2h"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Walk the dogs") {
		t.Errorf("should contain reminder text: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Reminder added") {
		t.Errorf("should confirm addition: %s", resp.Content)
	}

	// Verify persisted
	reminders, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reminders) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(reminders))
	}
	if reminders[0].Text != "Walk the dogs" {
		t.Errorf("text = %q", reminders[0].Text)
	}
	if reminders[0].DueAt.Before(time.Now()) {
		t.Error("due should be in the future")
	}
}

func TestReminderAddAbsoluteTime(t *testing.T) {
	store := newTestReminderStore(t)

	resp := callReminder(t, store, `{"operation":"add","text":"Meeting","due":"2026-12-25 09:00"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}

	reminders, _ := store.Load()
	if len(reminders) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(reminders))
	}
	if reminders[0].DueAt.Year() != 2026 || reminders[0].DueAt.Month() != 12 {
		t.Errorf("due = %v", reminders[0].DueAt)
	}
}

func TestReminderAddMissingText(t *testing.T) {
	store := newTestReminderStore(t)
	resp := callReminder(t, store, `{"operation":"add","due":"2h"}`)
	if !resp.IsError {
		t.Error("expected error for missing text")
	}
}

func TestReminderAddMissingDue(t *testing.T) {
	store := newTestReminderStore(t)
	resp := callReminder(t, store, `{"operation":"add","text":"something"}`)
	if !resp.IsError {
		t.Error("expected error for missing due")
	}
}

func TestReminderList(t *testing.T) {
	store := newTestReminderStore(t)

	// Empty list
	resp := callReminder(t, store, `{"operation":"list"}`)
	if !strings.Contains(resp.Content, "No pending") {
		t.Errorf("expected empty list: %s", resp.Content)
	}

	// Add two reminders
	callReminder(t, store, `{"operation":"add","text":"First","due":"1h"}`)
	callReminder(t, store, `{"operation":"add","text":"Second","due":"2h"}`)

	resp = callReminder(t, store, `{"operation":"list"}`)
	if !strings.Contains(resp.Content, "First") {
		t.Errorf("should list First: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Second") {
		t.Errorf("should list Second: %s", resp.Content)
	}
}

func TestReminderDone(t *testing.T) {
	store := newTestReminderStore(t)

	callReminder(t, store, `{"operation":"add","text":"Finish PR","due":"1h"}`)

	reminders, _ := store.Load()
	id := reminders[0].ID

	resp := callReminder(t, store, `{"operation":"done","id":"`+id+`"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "marked done") {
		t.Errorf("should confirm done: %s", resp.Content)
	}

	// Should not appear in pending list
	pending, _ := store.Pending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestReminderDoneNotFound(t *testing.T) {
	store := newTestReminderStore(t)
	resp := callReminder(t, store, `{"operation":"done","id":"nonexistent"}`)
	if !resp.IsError {
		t.Error("expected error for nonexistent ID")
	}
}

func TestReminderSnooze(t *testing.T) {
	store := newTestReminderStore(t)

	callReminder(t, store, `{"operation":"add","text":"Review","due":"1m"}`)

	reminders, _ := store.Load()
	id := reminders[0].ID
	originalDue := reminders[0].DueAt

	resp := callReminder(t, store, `{"operation":"snooze","id":"`+id+`","duration":"2h"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "snoozed") {
		t.Errorf("should confirm snooze: %s", resp.Content)
	}

	reminders, _ = store.Load()
	if !reminders[0].DueAt.After(originalDue) {
		t.Error("snoozed due should be later than original")
	}
}

func TestReminderRecurrence(t *testing.T) {
	store := newTestReminderStore(t)

	callReminder(t, store, `{"operation":"add","text":"Standup","due":"1h","recurrence":"daily"}`)

	reminders, _ := store.Load()
	id := reminders[0].ID
	originalDue := reminders[0].DueAt

	// Mark done should advance, not complete
	resp := callReminder(t, store, `{"operation":"done","id":"`+id+`"}`)
	if !strings.Contains(resp.Content, "advanced") {
		t.Errorf("recurring should advance, not complete: %s", resp.Content)
	}

	reminders, _ = store.Load()
	if reminders[0].Completed {
		t.Error("recurring reminder should not be completed")
	}
	if !reminders[0].DueAt.After(originalDue) {
		t.Error("due should be advanced")
	}
}

func TestReminderOverdue(t *testing.T) {
	store := newTestReminderStore(t)

	// Manually write an overdue reminder
	store.Save([]Reminder{{
		ID:        "old1",
		Text:      "Past due",
		DueAt:     time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}})

	overdue, err := store.Overdue()
	if err != nil {
		t.Fatalf("overdue: %v", err)
	}
	if len(overdue) != 1 {
		t.Errorf("expected 1 overdue, got %d", len(overdue))
	}

	// List should show OVERDUE status
	resp := callReminder(t, store, `{"operation":"list"}`)
	if !strings.Contains(resp.Content, "OVERDUE") {
		t.Errorf("should show OVERDUE: %s", resp.Content)
	}
}

func TestParseDue(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		// Durations
		{"2h", true},
		{"30m", true},
		{"1d", true},
		{"2h30m", true},
		// Datetimes
		{"2026-04-09 09:00", true},
		{"2026-04-09T14:00:00", true},
		{"2026-04-09T14:00", true},
		{"2026-04-09", true},
		{"14:00", true},
		// Natural language
		{"today", true},
		{"tonight", true},
		{"tomorrow", true},
		{"tomorrow morning", true},
		{"tomorrow evening", true},
		{"in 2 hours", true},
		{"in 30 minutes", true},
		{"in 3 days", true},
		{"in 2h", true},
		{"in 30m", true},
		// Invalid
		{"garbage", false},
		{"", false},
	}
	for _, tt := range tests {
		_, err := parseDue(tt.input)
		if (err == nil) != tt.ok {
			t.Errorf("parseDue(%q) error=%v, want ok=%v", tt.input, err, tt.ok)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"2h", 2 * time.Hour},
		{"30m", 30 * time.Minute},
		{"1d", 24 * time.Hour},
		{"3d", 72 * time.Hour},
	}
	for _, tt := range tests {
		got, err := parseDuration(tt.input)
		if err != nil {
			t.Errorf("parseDuration(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestUnknownReminderOperation(t *testing.T) {
	store := newTestReminderStore(t)
	resp := callReminder(t, store, `{"operation":"unknown"}`)
	if !resp.IsError {
		t.Error("expected error for unknown operation")
	}
}
