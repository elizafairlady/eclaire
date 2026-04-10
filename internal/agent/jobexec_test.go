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

func TestJobExecutor_NotificationOnComplete(t *testing.T) {
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

	deadline := time.After(3 * time.Second)
	for {
		total, _ := notifs.Count()
		if total > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for notification")
		case <-time.After(50 * time.Millisecond):
		}
	}

	all := notifs.List(agent.NotificationFilter{})
	if len(all) == 0 {
		t.Fatal("expected at least one notification")
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
