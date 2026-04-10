package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
)

func newTestBriefingDeps(t *testing.T) (BriefingDeps, string) {
	t.Helper()
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	briefingsDir := filepath.Join(wsDir, "briefings")
	os.MkdirAll(briefingsDir, 0o755)

	store := NewReminderStore(filepath.Join(dir, "reminders.json"))

	return BriefingDeps{
		Reminders:    store,
		WorkspaceDir: wsDir,
		BriefingsDir: briefingsDir,
		CronList: func() []CronEntry {
			return []CronEntry{
				{ID: "morning-check", Schedule: "0 9 * * *", AgentID: "orchestrator", Enabled: true},
			}
		},
	}, dir
}

func callBriefing(t *testing.T, deps BriefingDeps, input string) fantasy.ToolResponse {
	t.Helper()
	tool := BriefingTool(deps)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("BriefingTool error: %v", err)
	}
	return resp
}

func TestBriefingGenerate(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	resp := callBriefing(t, deps, `{"operation":"generate"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}

	// Should have header
	if !strings.Contains(resp.Content, "Daily Briefing") {
		t.Error("should contain 'Daily Briefing' header")
	}

	// Should have system section
	if !strings.Contains(resp.Content, "Good morning") {
		t.Error("should contain 'Good morning'")
	}

	// Should have cron section
	if !strings.Contains(resp.Content, "morning-check") {
		t.Error("should list cron entries")
	}
}

func TestBriefingWithReminders(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	// Add an overdue reminder
	deps.Reminders.Save([]Reminder{
		{ID: "r1", Text: "Overdue task", DueAt: time.Now().Add(-1 * time.Hour), CreatedAt: time.Now()},
		{ID: "r2", Text: "Today task", DueAt: time.Now().Add(2 * time.Hour), CreatedAt: time.Now()},
		{ID: "r3", Text: "Future task", DueAt: time.Now().Add(48 * time.Hour), CreatedAt: time.Now()},
	})

	resp := callBriefing(t, deps, `{"operation":"generate"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}

	if !strings.Contains(resp.Content, "overdue") {
		t.Error("should show overdue section")
	}
	if !strings.Contains(resp.Content, "Overdue task") {
		t.Error("should list overdue reminder")
	}
	if !strings.Contains(resp.Content, "Today task") {
		t.Error("should list today's reminder")
	}
	if !strings.Contains(resp.Content, "Future task") {
		t.Error("should list upcoming reminder")
	}
}

func TestBriefingWithYesterdayNotes(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	// Write yesterday's daily log
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	dailyDir := filepath.Join(deps.WorkspaceDir, "daily")
	os.MkdirAll(dailyDir, 0o755)
	os.WriteFile(filepath.Join(dailyDir, yesterday+".md"), []byte("Worked on API integration. Left off at auth middleware."), 0o644)

	resp := callBriefing(t, deps, `{"operation":"generate"}`)
	if !strings.Contains(resp.Content, "Yesterday's Notes") {
		t.Error("should have yesterday's notes section")
	}
	if !strings.Contains(resp.Content, "API integration") {
		t.Error("should contain yesterday's content")
	}
}

func TestBriefingSavesToFile(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	callBriefing(t, deps, `{"operation":"generate"}`)

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(deps.BriefingsDir, today+".md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("briefing file not saved: %v", err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Daily Briefing") {
		t.Error("saved file should contain briefing content")
	}
}

func TestBriefingEmptyOperation(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	// Empty operation should default to generate
	resp := callBriefing(t, deps, `{}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Daily Briefing") {
		t.Error("empty operation should default to generate")
	}
}

func TestBriefingNoReminders(t *testing.T) {
	deps, _ := newTestBriefingDeps(t)

	resp := callBriefing(t, deps, `{"operation":"generate"}`)
	if !strings.Contains(resp.Content, "No pending reminders") {
		t.Error("should say no pending reminders")
	}
}
