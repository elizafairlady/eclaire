package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLog_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "runs"))

	err := rl.Append(RunLogEntry{
		Timestamp: time.Now(),
		JobID:     "job1",
		Status:    "ok",
		Summary:   "completed successfully",
		Duration:  2 * time.Second,
		AgentID:   "research",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = rl.Append(RunLogEntry{
		Timestamp: time.Now(),
		JobID:     "job1",
		Status:    "error",
		Error:     "network timeout",
		Duration:  30 * time.Second,
		AgentID:   "research",
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := rl.Read("job1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first
	if entries[0].Status != "error" {
		t.Fatalf("expected newest first (error), got %q", entries[0].Status)
	}
	if entries[1].Status != "ok" {
		t.Fatalf("expected second entry (ok), got %q", entries[1].Status)
	}
}

func TestRunLog_ReadLimit(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "runs"))

	for i := 0; i < 10; i++ {
		rl.Append(RunLogEntry{
			Timestamp: time.Now(),
			JobID:     "job2",
			Status:    "ok",
			Summary:   "run",
		})
	}

	entries, _ := rl.Read("job2", 3)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries with limit, got %d", len(entries))
	}
}

func TestRunLog_ReadNonexistent(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "runs"))

	entries, err := rl.Read("nonexistent", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for nonexistent job, got %d", len(entries))
	}
}

func TestRunLog_SeparateJobFiles(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "runs"))

	rl.Append(RunLogEntry{JobID: "a", Status: "ok", Timestamp: time.Now()})
	rl.Append(RunLogEntry{JobID: "b", Status: "ok", Timestamp: time.Now()})
	rl.Append(RunLogEntry{JobID: "b", Status: "ok", Timestamp: time.Now()})

	aEntries, _ := rl.Read("a", 0)
	bEntries, _ := rl.Read("b", 0)
	if len(aEntries) != 1 {
		t.Fatalf("expected 1 entry for job a, got %d", len(aEntries))
	}
	if len(bEntries) != 2 {
		t.Fatalf("expected 2 entries for job b, got %d", len(bEntries))
	}
}

func TestRunLog_Prune(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	rl := NewRunLog(runsDir)

	// Write a large file that exceeds 2MB
	os.MkdirAll(runsDir, 0o700)
	path := filepath.Join(runsDir, "big.jsonl")
	f, _ := os.Create(path)
	line := `{"timestamp":"2026-04-09T00:00:00Z","job_id":"big","status":"ok","summary":"` + strings.Repeat("x", 500) + `"}` + "\n"
	for i := 0; i < 5000; i++ {
		f.WriteString(line)
	}
	f.Close()

	// Append one more to trigger prune
	rl.Append(RunLogEntry{
		Timestamp: time.Now(),
		JobID:     "big",
		Status:    "ok",
		Summary:   "trigger prune",
	})

	// Verify file was pruned
	entries, _ := rl.Read("big", 0)
	if len(entries) > 2001 {
		t.Fatalf("expected pruned to ~2000, got %d", len(entries))
	}
}
