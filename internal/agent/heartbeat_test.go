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

func TestMigrateLegacyCron(t *testing.T) {
	dir := t.TempDir()

	// Create legacy cron.yaml
	cronPath := filepath.Join(dir, "cron.yaml")
	os.WriteFile(cronPath, []byte(`entries:
  - id: daily-news
    schedule: "0 9 * * *"
    agent_id: research
    prompt: "Fetch daily news summary"
    enabled: true
  - id: weekly-cleanup
    schedule: "0 0 * * 0"
    agent_id: sysadmin
    prompt: "Run weekly cleanup"
    enabled: true
  - id: disabled-job
    schedule: "0 12 * * *"
    agent_id: orchestrator
    prompt: "This is disabled"
    enabled: false
`), 0o644)

	jobStore, err := NewJobStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}

	exec := &JobExecutor{
		store:  jobStore,
		logger: testLogger(),
	}

	migrated := exec.MigrateLegacyCron(cronPath)
	if migrated != 2 {
		t.Fatalf("expected 2 migrated entries, got %d", migrated)
	}

	// Verify migrated jobs
	daily, ok := jobStore.Get("cron-daily-news")
	if !ok {
		t.Fatal("cron-daily-news not found")
	}
	if daily.Schedule.Kind != ScheduleCron {
		t.Errorf("kind = %q, want cron", daily.Schedule.Kind)
	}
	if daily.Schedule.Expr != "0 9 * * *" {
		t.Errorf("expr = %q", daily.Schedule.Expr)
	}
	if daily.AgentID != "research" {
		t.Errorf("agent = %q", daily.AgentID)
	}

	weekly, ok := jobStore.Get("cron-weekly-cleanup")
	if !ok {
		t.Fatal("cron-weekly-cleanup not found")
	}
	if weekly.AgentID != "sysadmin" {
		t.Errorf("agent = %q", weekly.AgentID)
	}

	// Disabled entry should not be migrated
	if _, ok := jobStore.Get("cron-disabled-job"); ok {
		t.Error("disabled entry should not be migrated")
	}

	// Calling again should not create duplicates
	migrated2 := exec.MigrateLegacyCron(cronPath)
	if migrated2 != 0 {
		t.Errorf("second migration should migrate 0, got %d", migrated2)
	}
	if jobStore.Count() != 2 {
		t.Errorf("should still have 2 jobs, got %d", jobStore.Count())
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
