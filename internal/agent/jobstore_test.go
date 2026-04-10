package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJobStore_AddRemoveGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	s, err := NewJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count() != 0 {
		t.Fatalf("expected 0 jobs, got %d", s.Count())
	}

	j, err := s.Add(Job{
		Name:    "test-job",
		AgentID: "research",
		Prompt:  "do something",
		Schedule: JobSchedule{
			Kind:  ScheduleAt,
			At:    "1h",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == "" {
		t.Fatal("expected generated ID")
	}
	if j.SessionTarget != "isolated" {
		t.Fatalf("expected default session target 'isolated', got %q", j.SessionTarget)
	}
	if !j.Enabled {
		t.Fatal("expected enabled by default")
	}
	if s.Count() != 1 {
		t.Fatalf("expected 1 job, got %d", s.Count())
	}

	got, ok := s.Get(j.ID)
	if !ok {
		t.Fatal("job not found")
	}
	if got.Name != "test-job" {
		t.Fatalf("expected name 'test-job', got %q", got.Name)
	}

	removed, err := s.Remove(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != "test-job" {
		t.Fatalf("expected removed job name 'test-job', got %q", removed.Name)
	}
	if s.Count() != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", s.Count())
	}

	_, ok = s.Get(j.ID)
	if ok {
		t.Fatal("expected job not found after remove")
	}
}

func TestJobStore_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	_, err := s.Add(Job{ID: "dup", Name: "first", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Add(Job{ID: "dup", Name: "second", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestJobStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	s1, _ := NewJobStore(path)
	s1.Add(Job{
		ID:       "persist-1",
		Name:     "persisted",
		AgentID:  "coding",
		Prompt:   "hello",
		Schedule: JobSchedule{Kind: ScheduleEvery, Every: "10m"},
	})

	// Reload from disk
	s2, err := NewJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Count() != 1 {
		t.Fatalf("expected 1 job after reload, got %d", s2.Count())
	}
	j, ok := s2.Get("persist-1")
	if !ok {
		t.Fatal("persisted job not found")
	}
	if j.Name != "persisted" {
		t.Fatalf("expected name 'persisted', got %q", j.Name)
	}
	if j.AgentID != "coding" {
		t.Fatalf("expected agent 'coding', got %q", j.AgentID)
	}
}

func TestJobStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	s, err := NewJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count() != 0 {
		t.Fatalf("expected 0 jobs for nonexistent file, got %d", s.Count())
	}
}

func TestJobStore_Update(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))
	s.Add(Job{ID: "upd", Name: "before", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})

	err := s.Update("upd", func(j *Job) {
		j.Name = "after"
		j.Enabled = false
	})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := s.Get("upd")
	if j.Name != "after" {
		t.Fatalf("expected name 'after', got %q", j.Name)
	}
	if j.Enabled {
		t.Fatal("expected disabled after update")
	}
}

func TestJobStore_List(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	s.Add(Job{ID: "a", Name: "first", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Add(Job{ID: "b", Name: "second", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Add(Job{ID: "c", Name: "third", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(list))
	}
	// Newest first
	if list[0].ID != "c" {
		t.Fatalf("expected newest first, got %q", list[0].ID)
	}
}

func TestJobStore_NextDue(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	// Add job with past NextRunAt (due)
	s.Add(Job{ID: "due", Name: "due", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("due", func(j *Job) { j.State.NextRunAt = &past })

	// Add job with future NextRunAt (not due)
	s.Add(Job{ID: "notdue", Name: "notdue", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("notdue", func(j *Job) { j.State.NextRunAt = &future })

	// Add disabled job with past NextRunAt (should not appear)
	s.Add(Job{ID: "disabled", Name: "disabled", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("disabled", func(j *Job) {
		j.State.NextRunAt = &past
		j.Enabled = false
	})

	due := s.NextDue(now)
	if len(due) != 1 {
		t.Fatalf("expected 1 due job, got %d", len(due))
	}
	if due[0].ID != "due" {
		t.Fatalf("expected due job 'due', got %q", due[0].ID)
	}
}

func TestJobStore_NextDueSkipsRunning(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	now := time.Now()
	past := now.Add(-time.Hour)

	s.Add(Job{ID: "running", Name: "running", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("running", func(j *Job) {
		j.State.NextRunAt = &past
		j.State.RunningAt = &now
	})

	due := s.NextDue(now)
	if len(due) != 0 {
		t.Fatalf("expected 0 due jobs (running excluded), got %d", len(due))
	}
}

func TestJobStore_ClearStaleRunning(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	now := time.Now()
	s.Add(Job{ID: "stale", Name: "stale", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("stale", func(j *Job) { j.State.RunningAt = &now })

	j, _ := s.Get("stale")
	if j.State.RunningAt == nil {
		t.Fatal("expected RunningAt set")
	}

	s.ClearStaleRunning()

	j, _ = s.Get("stale")
	if j.State.RunningAt != nil {
		t.Fatal("expected RunningAt cleared")
	}
}

func TestJobStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	s, _ := NewJobStore(path)

	s.Add(Job{ID: "atom", Name: "atomic", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})

	// Verify no .tmp file left behind
	_, err := os.Stat(path + ".tmp")
	if err == nil {
		t.Fatal("temp file should not exist after successful write")
	}

	// Verify the actual file exists and is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f jobStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("corrupt JSON: %v", err)
	}
	if len(f.Jobs) != 1 {
		t.Fatalf("expected 1 job in file, got %d", len(f.Jobs))
	}
}

func TestComputeNextRun_At_Duration(t *testing.T) {
	now := time.Now()
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleAt, At: "2h"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected non-nil next run")
	}
	diff := next.Sub(now)
	if diff < 119*time.Minute || diff > 121*time.Minute {
		t.Fatalf("expected ~2h from now, got %v", diff)
	}
}

func TestComputeNextRun_At_Timestamp(t *testing.T) {
	now := time.Now()
	future := now.Add(3 * time.Hour).Format(time.RFC3339)
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleAt, At: future}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected non-nil next run")
	}
}

func TestComputeNextRun_At_Expired(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour).Format(time.RFC3339)
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleAt, At: past}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("expected nil for expired timestamp, got %v", next)
	}
}

func TestComputeNextRun_Every(t *testing.T) {
	now := time.Now()

	// Never run → due now
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleEvery, Every: "5m"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || !next.Equal(now) {
		t.Fatalf("expected due now for never-run, got %v", next)
	}

	// Last run 3 minutes ago with 5m interval → due in 2 minutes
	lastRun := now.Add(-3 * time.Minute)
	next, err = ComputeNextRun(JobSchedule{Kind: ScheduleEvery, Every: "5m"}, now, &lastRun)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected non-nil")
	}
	expected := lastRun.Add(5 * time.Minute)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_Cron(t *testing.T) {
	// Every hour at minute 0
	now := time.Date(2026, 4, 9, 14, 30, 0, 0, time.UTC)
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleCron, Expr: "0 * * * *"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected non-nil")
	}
	expected := time.Date(2026, 4, 9, 15, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_Cron_Daily(t *testing.T) {
	// Every day at 7:00
	now := time.Date(2026, 4, 9, 8, 0, 0, 0, time.UTC) // already past 7:00 today
	next, err := ComputeNextRun(JobSchedule{Kind: ScheduleCron, Expr: "0 7 * * *"}, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected non-nil")
	}
	expected := time.Date(2026, 4, 10, 7, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_InvalidKind(t *testing.T) {
	_, err := ComputeNextRun(JobSchedule{Kind: "bogus"}, time.Now(), nil)
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestComputeNextRun_At_Empty(t *testing.T) {
	_, err := ComputeNextRun(JobSchedule{Kind: ScheduleAt, At: ""}, time.Now(), nil)
	if err == nil {
		t.Fatal("expected error for empty at")
	}
}

func TestComputeNextRun_Every_Empty(t *testing.T) {
	_, err := ComputeNextRun(JobSchedule{Kind: ScheduleEvery, Every: ""}, time.Now(), nil)
	if err == nil {
		t.Fatal("expected error for empty every")
	}
}

func TestComputeNextRun_Cron_Empty(t *testing.T) {
	_, err := ComputeNextRun(JobSchedule{Kind: ScheduleCron, Expr: ""}, time.Now(), nil)
	if err == nil {
		t.Fatal("expected error for empty expr")
	}
}

func TestJobStore_NextWakeAt(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	// Empty store → nil
	if w := s.NextWakeAt(); w != nil {
		t.Fatalf("expected nil for empty store, got %v", w)
	}

	now := time.Now()
	t1 := now.Add(10 * time.Minute)
	t2 := now.Add(5 * time.Minute)

	s.Add(Job{ID: "later", Name: "later", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "10m"}})
	s.Update("later", func(j *Job) { j.State.NextRunAt = &t1 })

	s.Add(Job{ID: "sooner", Name: "sooner", Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"}})
	s.Update("sooner", func(j *Job) { j.State.NextRunAt = &t2 })

	w := s.NextWakeAt()
	if w == nil {
		t.Fatal("expected non-nil wake time")
	}
	if !w.Equal(t2) {
		t.Fatalf("expected earliest %v, got %v", t2, w)
	}
}

func TestJobStore_DeleteAfterRunDefault(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))

	// "at" kind with DeleteAfterRun explicitly set to true
	j, _ := s.Add(Job{
		Name:           "oneshot",
		Schedule:       JobSchedule{Kind: ScheduleAt, At: "1h"},
		DeleteAfterRun: true,
	})
	if !j.DeleteAfterRun {
		t.Fatal("expected DeleteAfterRun true for at kind")
	}

	// "every" kind — should keep whatever was passed
	j2, _ := s.Add(Job{
		Name:     "recurring",
		Schedule: JobSchedule{Kind: ScheduleEvery, Every: "5m"},
	})
	if j2.DeleteAfterRun {
		t.Fatal("expected DeleteAfterRun false for every kind (not explicitly set)")
	}
}

func TestJobStore_RemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))
	_, err := s.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

func TestJobStore_UpdateNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewJobStore(filepath.Join(dir, "jobs.json"))
	err := s.Update("nonexistent", func(j *Job) {})
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}
