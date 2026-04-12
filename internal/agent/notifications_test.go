package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
)

func TestNotificationStore_AddAndList(t *testing.T) {
	dir := t.TempDir()
	s, err := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	s.Add(Notification{Severity: SeverityInfo, Source: "cron", Title: "Job done", Content: "ok"})
	s.Add(Notification{Severity: SeverityError, Source: "agent", Title: "Blocked", Content: "needs approval"})

	total, unread := s.Count()
	if total != 2 {
		t.Fatalf("expected 2 total, got %d", total)
	}
	if unread != 2 {
		t.Fatalf("expected 2 unread, got %d", unread)
	}

	all := s.List(NotificationFilter{})
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	// Newest first
	if all[0].Title != "Blocked" {
		t.Fatalf("expected newest first, got %q", all[0].Title)
	}
}

func TestNotificationStore_Pending(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityInfo, Title: "a"})
	s.Add(Notification{Severity: SeverityInfo, Title: "b", Read: true})
	s.Add(Notification{Severity: SeverityInfo, Title: "c"})

	pending := s.Pending()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
}

func TestNotificationStore_Drain(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityInfo, Title: "a"})
	s.Add(Notification{Severity: SeverityWarning, Title: "b"})

	drained := s.Drain()
	if len(drained) != 2 {
		t.Fatalf("expected 2 drained, got %d", len(drained))
	}

	// After drain, all should be read
	_, unread := s.Count()
	if unread != 0 {
		t.Fatalf("expected 0 unread after drain, got %d", unread)
	}

	// Second drain should return empty
	drained2 := s.Drain()
	if len(drained2) != 0 {
		t.Fatalf("expected 0 on second drain, got %d", len(drained2))
	}
}

func TestNotificationStore_SeverityFilter(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityDebug, Title: "debug"})
	s.Add(Notification{Severity: SeverityInfo, Title: "info"})
	s.Add(Notification{Severity: SeverityWarning, Title: "warning"})
	s.Add(Notification{Severity: SeverityError, Title: "error"})

	warnings := s.List(NotificationFilter{Severity: SeverityWarning})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Title != "warning" {
		t.Fatalf("expected warning, got %q", warnings[0].Title)
	}
}

func TestNotificationStore_SourceFilter(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityInfo, Source: "cron", Title: "cron1"})
	s.Add(Notification{Severity: SeverityInfo, Source: "heartbeat", Title: "hb1"})
	s.Add(Notification{Severity: SeverityInfo, Source: "cron", Title: "cron2"})

	cronOnly := s.List(NotificationFilter{Source: "cron"})
	if len(cronOnly) != 2 {
		t.Fatalf("expected 2 cron, got %d", len(cronOnly))
	}
}

func TestNotificationStore_MarkRead(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityInfo, Title: "a"})
	s.Add(Notification{Severity: SeverityInfo, Title: "b"})

	all := s.List(NotificationFilter{})
	s.MarkRead(all[0].ID)

	_, unread := s.Count()
	if unread != 1 {
		t.Fatalf("expected 1 unread after MarkRead, got %d", unread)
	}
}

func TestNotificationStore_MarkAllRead(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	s.Add(Notification{Severity: SeverityInfo, Title: "a"})
	s.Add(Notification{Severity: SeverityInfo, Title: "b"})
	s.Add(Notification{Severity: SeverityInfo, Title: "c"})

	count := s.MarkAllRead()
	if count != 3 {
		t.Fatalf("expected 3 marked, got %d", count)
	}

	_, unread := s.Count()
	if unread != 0 {
		t.Fatalf("expected 0 unread, got %d", unread)
	}
}

func TestNotificationStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notif.jsonl")

	s1, _ := NewNotificationStore(path)
	s1.Add(Notification{Severity: SeverityInfo, Source: "test", Title: "persisted", Content: "data"})
	s1.Add(Notification{Severity: SeverityError, Source: "test", Title: "error", Content: "bad"})

	// Mark first as read
	all := s1.List(NotificationFilter{})
	s1.MarkRead(all[1].ID) // "persisted" is index 1 (newest first is "error")

	// Reload
	s2, err := NewNotificationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	total, unread := s2.Count()
	if total != 2 {
		t.Fatalf("expected 2 total after reload, got %d", total)
	}
	if unread != 1 {
		t.Fatalf("expected 1 unread after reload, got %d", unread)
	}
}

func TestNotificationStore_Limit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))

	for i := 0; i < 10; i++ {
		s.Add(Notification{Severity: SeverityInfo, Title: "item"})
	}

	limited := s.List(NotificationFilter{Limit: 3})
	if len(limited) != 3 {
		t.Fatalf("expected 3 with limit, got %d", len(limited))
	}
}

func TestNotificationStore_BusIntegration(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewNotificationStore(filepath.Join(dir, "notif.jsonl"))
	b := bus.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.SubscribeToBus(ctx, b)

	// Publish a failed BackgroundResult — only errors create notifications
	b.Publish(bus.TopicBackgroundResult, bus.BackgroundResult{
		Source:   "cron",
		TaskName: "daily-report",
		AgentID:  "research",
		Status:   "error",
		Content:  "Connection timeout",
	})

	// Give the goroutine a moment to process
	time.Sleep(50 * time.Millisecond)

	total, _ := s.Count()
	if total != 1 {
		t.Fatalf("expected 1 notification from bus, got %d", total)
	}

	all := s.List(NotificationFilter{})
	if all[0].Source != "cron" {
		t.Fatalf("expected source 'cron', got %q", all[0].Source)
	}
}
