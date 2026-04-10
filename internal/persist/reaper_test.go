package persist

import (
	"testing"
	"time"
)

func TestReapCompleted(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	// Create sessions with different statuses
	active, _ := store.Create("a", "Active session", "")
	completed, _ := store.Create("a", "Completed session", "")
	errored, _ := store.Create("a", "Error session", "")

	store.UpdateStatus(completed.ID, "completed")
	store.UpdateStatus(errored.ID, "error")

	// With zero retention, all completed/error sessions should be reaped
	count, err := store.ReapCompleted(0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("reaped %d sessions, want 2", count)
	}

	// Active session should still exist
	if _, err := store.GetMeta(active.ID); err != nil {
		t.Errorf("active session should still exist: %v", err)
	}

	// Completed and errored sessions should be archived (GetMeta fails)
	if _, err := store.GetMeta(completed.ID); err == nil {
		t.Error("completed session should have been archived")
	}
	if _, err := store.GetMeta(errored.ID); err == nil {
		t.Error("errored session should have been archived")
	}
}

func TestReapCompleted_RespectsRetention(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	meta, _ := store.Create("a", "Recent completed", "")
	store.UpdateStatus(meta.ID, "completed")

	// With 1 hour retention, recently completed sessions should NOT be reaped
	count, _ := store.ReapCompleted(1 * time.Hour)
	if count != 0 {
		t.Errorf("reaped %d sessions, want 0 (session is too recent)", count)
	}
}

func TestReapCompleted_SkipsMainSession(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore(dir)

	main, _ := store.GetOrCreateMain("orchestrator")
	store.UpdateStatus(main.ID, "completed")

	count, _ := store.ReapCompleted(0)
	if count != 0 {
		t.Errorf("reaped %d sessions, want 0 (main session should never be reaped)", count)
	}
}
