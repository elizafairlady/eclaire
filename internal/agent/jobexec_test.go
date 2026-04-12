package agent_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/testutil"
)

func newJobExecTestEnv(t *testing.T) (*agent.JobStore, *agent.RunLog, *agent.NotificationStore, *agent.JobExecutor, *testutil.TestEnv) {
	t.Helper()
	dir := t.TempDir()

	mock := &testutil.MockModel{Responses: []testutil.MockResponse{
		{Text: "Job executed successfully"},
		{Text: "Job executed successfully"},
		{Text: "Job executed successfully"},
		{Text: "Job executed successfully"},
		{Text: "Job executed successfully"},
	}}
	env := testutil.NewTestEnv(dir, mock)

	store, err := agent.NewJobStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	runLog := agent.NewRunLog(filepath.Join(dir, "runs"))
	notifs, err := agent.NewNotificationStore(filepath.Join(dir, "notif.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	exec := agent.NewJobExecutor(store, runLog, notifs, env.Runner, env.Registry, env.Bus, env.Logger)
	return store, runLog, notifs, exec, env
}

func TestJobExecutor_AtFiresWhenDue(t *testing.T) {
	store, runLog, _, exec, _ := newJobExecTestEnv(t)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:             "at-due",
		Name:           "past-due",
		AgentID:        "orchestrator",
		Prompt:         "test prompt",
		DeleteAfterRun: true,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("at-due", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	deadline := time.After(3 * time.Second)
	for {
		entries, _ := runLog.Read("at-due", 1)
		if len(entries) > 0 {
			if entries[0].Status != "ok" {
				t.Fatalf("expected status ok, got %q (err: %s)", entries[0].Status, entries[0].Error)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job to execute")
		case <-time.After(50 * time.Millisecond):
		}
	}

	_, ok := store.Get("at-due")
	if ok {
		t.Fatal("expected job removed after deleteAfterRun")
	}
}

func TestJobExecutor_EveryReschedules(t *testing.T) {
	store, runLog, _, exec, _ := newJobExecTestEnv(t)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:       "every-job",
		Name:     "recurring",
		AgentID:  "orchestrator",
		Prompt:   "check something",
		Schedule: agent.JobSchedule{Kind: agent.ScheduleEvery, Every: "5m"},
	})
	store.Update("every-job", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	deadline := time.After(3 * time.Second)
	for {
		entries, _ := runLog.Read("every-job", 1)
		if len(entries) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for every job to execute")
		case <-time.After(50 * time.Millisecond):
		}
	}

	j, ok := store.Get("every-job")
	if !ok {
		t.Fatal("expected every job to still exist")
	}
	if !j.Enabled {
		t.Fatal("expected every job to still be enabled")
	}
	if j.State.NextRunAt == nil {
		t.Fatal("expected NextRunAt to be set for rescheduled job")
	}
	if j.State.LastStatus != "ok" {
		t.Fatalf("expected last status ok, got %q", j.State.LastStatus)
	}
}

func TestJobExecutor_NoNotificationOnSuccess(t *testing.T) {
	store, _, notifs, exec, _ := newJobExecTestEnv(t)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:             "notif-job",
		Name:           "notify-me",
		AgentID:        "orchestrator",
		Prompt:         "do work",
		DeleteAfterRun: true,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("notif-job", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	// Wait for job to run (it will be deleted after success since DeleteAfterRun=true)
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := store.Get("notif-job"); !ok {
			break // job was deleted = it ran successfully
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job to complete")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Successful completion should NOT create a notification
	total, _ := notifs.Count()
	if total != 0 {
		all := notifs.List(agent.NotificationFilter{})
		t.Errorf("expected 0 notifications for successful job, got %d: %v", total, all[0].Title)
	}
}

func TestJobExecutor_NotificationOnFailure(t *testing.T) {
	store, _, notifs, exec, _ := newJobExecTestEnv(t)

	past := time.Now().Add(-time.Minute)
	// Use a non-existent agent to force a failure
	store.Add(agent.Job{
		ID:             "fail-job",
		Name:           "will-fail",
		AgentID:        "nonexistent-agent",
		Prompt:         "do work",
		DeleteAfterRun: false,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("fail-job", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	deadline := time.After(3 * time.Second)
	for {
		total, _ := notifs.Count()
		if total > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for failure notification")
		case <-time.After(50 * time.Millisecond):
		}
	}

	all := notifs.List(agent.NotificationFilter{})
	if len(all) == 0 {
		t.Fatal("expected notification for failed job")
	}
	if all[0].Source != "cron" {
		t.Fatalf("expected source 'cron', got %q", all[0].Source)
	}
}

func TestJobExecutor_RunImmediate(t *testing.T) {
	store, runLog, _, exec, _ := newJobExecTestEnv(t)

	future := time.Now().Add(time.Hour)
	store.Add(agent.Job{
		ID:       "future-job",
		Name:     "future",
		AgentID:  "orchestrator",
		Prompt:   "run later",
		Schedule: agent.JobSchedule{Kind: agent.ScheduleAt, At: "1h"},
	})
	store.Update("future-job", func(j *agent.Job) {
		j.State.NextRunAt = &future
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := exec.RunImmediate(ctx, "future-job")
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		entries, _ := runLog.Read("future-job", 1)
		if len(entries) > 0 {
			if entries[0].Status != "ok" {
				t.Fatalf("expected ok, got %q", entries[0].Status)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for immediate execution")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestJobExecutor_ClearsStaleRunning(t *testing.T) {
	dir := t.TempDir()
	store, _ := agent.NewJobStore(filepath.Join(dir, "jobs.json"))

	now := time.Now()
	store.Add(agent.Job{
		ID:       "stale",
		Name:     "stale",
		AgentID:  "orchestrator",
		Prompt:   "test",
		Schedule: agent.JobSchedule{Kind: agent.ScheduleEvery, Every: "5m"},
	})
	store.Update("stale", func(j *agent.Job) {
		j.State.RunningAt = &now
	})

	j, _ := store.Get("stale")
	if j.State.RunningAt == nil {
		t.Fatal("expected RunningAt set before start")
	}

	mock := &testutil.MockModel{}
	env := testutil.NewTestEnv(dir, mock)
	exec := agent.NewJobExecutor(store, nil, nil, env.Runner, env.Registry, env.Bus, env.Logger)

	ctx, cancel := context.WithCancel(context.Background())
	exec.Start(ctx)
	cancel()
	exec.Stop()

	j, _ = store.Get("stale")
	if j.State.RunningAt != nil {
		t.Fatal("expected RunningAt cleared on start")
	}
}

func TestJobExecutor_BusEvents(t *testing.T) {
	store, _, _, exec, env := newJobExecTestEnv(t)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:             "bus-job",
		Name:           "bus-test",
		AgentID:        "orchestrator",
		Prompt:         "test",
		DeleteAfterRun: true,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("bus-job", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startedCh := env.Bus.Subscribe(ctx, bus.TopicCronStarted)
	completedCh := env.Bus.Subscribe(ctx, bus.TopicCronCompleted)

	exec.Start(ctx)
	defer exec.Stop()

	select {
	case ev := <-startedCh:
		ce := ev.Payload.(bus.CronEvent)
		if ce.EntryID != "bus-job" {
			t.Fatalf("expected entry ID 'bus-job', got %q", ce.EntryID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for started event")
	}

	select {
	case ev := <-completedCh:
		ce := ev.Payload.(bus.CronEvent)
		if ce.Status != "completed" {
			t.Fatalf("expected status 'completed', got %q", ce.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for completed event")
	}
}

func TestJobExecutor_SessionTargetMain(t *testing.T) {
	store, _, _, exec, _ := newJobExecTestEnv(t)

	// Wire up main session routing
	sysEvents := agent.NewSystemEventQueue()
	exec.SetMainSession("main-session-id", sysEvents)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:             "main-target",
		Name:           "main-test",
		AgentID:        "orchestrator",
		Prompt:         "run in main",
		SessionTarget:  "main",
		DeleteAfterRun: true,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("main-target", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	// Wait for job execution
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job execution")
		default:
		}
		events := sysEvents.Peek("main-session-id")
		if len(events) > 0 {
			found := false
			for _, ev := range events {
				if ev.Source == "cron" {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// System event should have been enqueued to main session
	events := sysEvents.Peek("main-session-id")
	if len(events) == 0 {
		t.Fatal("expected system event enqueued to main session")
	}
}

func TestJobExecutor_SessionTargetIsolatedNoSysEvent(t *testing.T) {
	store, _, _, exec, _ := newJobExecTestEnv(t)

	sysEvents := agent.NewSystemEventQueue()
	exec.SetMainSession("main-session-id", sysEvents)

	past := time.Now().Add(-time.Minute)
	store.Add(agent.Job{
		ID:             "isolated-target",
		Name:           "isolated-test",
		AgentID:        "orchestrator",
		Prompt:         "run isolated",
		SessionTarget:  "isolated",
		DeleteAfterRun: true,
		Schedule:       agent.JobSchedule{Kind: agent.ScheduleAt, At: "1m"},
	})
	store.Update("isolated-target", func(j *agent.Job) {
		j.State.NextRunAt = &past
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.Start(ctx)
	defer exec.Stop()

	// Wait for job execution (check it was deleted = executed)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job execution")
		default:
		}
		if _, ok := store.Get("isolated-target"); !ok {
			break // deleted after run = executed
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Isolated target should NOT directly enqueue system events via JobExecutor
	// (the bus subscriber in gateway handles that separately)
	events := sysEvents.Peek("main-session-id")
	if len(events) != 0 {
		t.Errorf("expected no system events from isolated job, got %d", len(events))
	}
}
