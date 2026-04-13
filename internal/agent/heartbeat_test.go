package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHeartbeatConfig_StructuredTasks(t *testing.T) {
	md := `# Heartbeat Tasks

tasks:
  - name: check-reminders
    interval: 5m
    agent: orchestrator
    prompt: "Check for overdue reminders"
  - name: health-check
    interval: 1h
    agent: sysadmin
    prompt: "Run system health check"
`
	config := parseHeartbeatConfig(md)
	if len(config.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(config.Tasks))
	}
	if config.Tasks[0].Name != "check-reminders" {
		t.Errorf("task 0 name = %q", config.Tasks[0].Name)
	}
	if config.Tasks[0].Interval != "5m" {
		t.Errorf("task 0 interval = %q", config.Tasks[0].Interval)
	}
	if config.Tasks[1].Agent != "sysadmin" {
		t.Errorf("task 1 agent = %q", config.Tasks[1].Agent)
	}
}

func TestParseHeartbeatConfig_FencedYAML(t *testing.T) {
	md := "# Heartbeat\n\n```yaml\ntasks:\n  - name: test\n    interval: 10m\n    prompt: \"test prompt\"\n```\n"
	config := parseHeartbeatConfig(md)
	if len(config.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(config.Tasks))
	}
	if config.Tasks[0].Name != "test" {
		t.Errorf("task name = %q", config.Tasks[0].Name)
	}
}

func TestParseHeartbeatConfig_Legacy(t *testing.T) {
	md := `# Heartbeat
- Check reminders
- System health
- Daily summary
`
	config := parseHeartbeatConfig(md)
	if len(config.Tasks) != 0 {
		t.Fatalf("legacy format should have 0 structured tasks, got %d", len(config.Tasks))
	}
	if config.Content != md {
		t.Error("content should be the full markdown")
	}
}

func TestHeartbeatJobSync(t *testing.T) {
	dir := t.TempDir()

	// Create workspace with structured HEARTBEAT.md
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o755)
	os.WriteFile(filepath.Join(wsDir, "HEARTBEAT.md"), []byte(`tasks:
  - name: task-a
    interval: 10m
    agent: orchestrator
    prompt: "Do task A"
  - name: task-b
    interval: 1h
    agent: research
    prompt: "Do task B"
`), 0o644)

	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(agentsDir, 0o755)
	wsLoader := NewWorkspaceLoader(wsDir, agentsDir, "")

	jobStore, err := NewJobStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}

	exec := &JobExecutor{
		store:  jobStore,
		logger: testLogger(),
	}

	// Sync should create 2 heartbeat jobs
	if err := exec.SyncHeartbeatJobs(wsLoader); err != nil {
		t.Fatal(err)
	}

	jobs := jobStore.List()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	jobA, ok := jobStore.Get("heartbeat-task-a")
	if !ok {
		t.Fatal("heartbeat-task-a not found")
	}
	if jobA.Schedule.Kind != ScheduleEvery {
		t.Errorf("task-a kind = %q", jobA.Schedule.Kind)
	}
	if jobA.Schedule.Every != "10m" {
		t.Errorf("task-a every = %q", jobA.Schedule.Every)
	}
	if jobA.AgentID != "orchestrator" {
		t.Errorf("task-a agent = %q", jobA.AgentID)
	}

	jobB, ok := jobStore.Get("heartbeat-task-b")
	if !ok {
		t.Fatal("heartbeat-task-b not found")
	}
	if jobB.AgentID != "research" {
		t.Errorf("task-b agent = %q", jobB.AgentID)
	}

	// Update HEARTBEAT.md: remove task-b, change task-a interval
	os.WriteFile(filepath.Join(wsDir, "HEARTBEAT.md"), []byte(`tasks:
  - name: task-a
    interval: 5m
    agent: orchestrator
    prompt: "Do task A (updated)"
`), 0o644)

	// Re-sync should update task-a and remove task-b
	if err := exec.SyncHeartbeatJobs(wsLoader); err != nil {
		t.Fatal(err)
	}

	jobs = jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after re-sync, got %d", len(jobs))
	}

	jobA, ok = jobStore.Get("heartbeat-task-a")
	if !ok {
		t.Fatal("heartbeat-task-a should still exist")
	}
	if jobA.Schedule.Every != "5m" {
		t.Errorf("task-a interval should be updated to 5m, got %q", jobA.Schedule.Every)
	}
	if jobA.Prompt != "Do task A (updated)" {
		t.Errorf("task-a prompt should be updated, got %q", jobA.Prompt)
	}

	if _, ok := jobStore.Get("heartbeat-task-b"); ok {
		t.Error("heartbeat-task-b should have been removed")
	}
}

func TestHeartbeatTaskList(t *testing.T) {
	dir := t.TempDir()

	jobStore, err := NewJobStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Add a heartbeat job and a regular job
	jobStore.Add(Job{
		ID:       "heartbeat-test",
		Name:     "heartbeat: test",
		Schedule: JobSchedule{Kind: ScheduleEvery, Every: "10m"},
		AgentID:  "orchestrator",
		Prompt:   "test prompt",
		Enabled:  true,
	})
	jobStore.Add(Job{
		ID:       "regular-job",
		Name:     "some job",
		Schedule: JobSchedule{Kind: ScheduleCron, Expr: "0 9 * * *"},
		AgentID:  "research",
		Prompt:   "research task",
		Enabled:  true,
	})

	exec := &JobExecutor{
		store:  jobStore,
		logger: testLogger(),
	}

	infos := exec.HeartbeatTaskList()
	if len(infos) != 1 {
		t.Fatalf("expected 1 heartbeat task, got %d", len(infos))
	}
	if infos[0].Name != "test" {
		t.Errorf("name = %q", infos[0].Name)
	}
	if infos[0].Interval != "10m" {
		t.Errorf("interval = %q", infos[0].Interval)
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}
