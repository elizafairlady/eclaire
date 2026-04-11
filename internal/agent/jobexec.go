package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// Backoff schedule for transient errors (matching OpenClaw).
var transientBackoffs = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	5 * time.Minute,
}

const maxTransientRetries = 3

// ReminderFirer is the interface for checking and completing reminders.
// Implemented by tool.ReminderStore — defined here to avoid import cycle.
type ReminderFirer interface {
	// FireOverdue returns overdue reminders, marks each done (or advances recurrence), and persists.
	FireOverdue() ([]FiredReminder, error)
}

// FiredReminder is a reminder that just fired.
type FiredReminder struct {
	ID   string
	Text string
}

// JobExecutor runs scheduled jobs based on a unified timer loop.
// It replaces the separate heartbeatLoop and cronLoop from the old Scheduler.
type JobExecutor struct {
	store         *JobStore
	runLog        *RunLog
	notifications *NotificationStore
	reminders     ReminderFirer
	runner        *Runner
	registry      *Registry
	bus           *bus.Bus
	logger        *slog.Logger

	cancel     context.CancelFunc
	wg         sync.WaitGroup
	lastReapAt time.Time // throttles session reaping to every 5 minutes
}

// NewJobExecutor creates a job executor wired to all dependencies.
func NewJobExecutor(
	store *JobStore,
	runLog *RunLog,
	notifications *NotificationStore,
	runner *Runner,
	registry *Registry,
	msgBus *bus.Bus,
	logger *slog.Logger,
) *JobExecutor {
	return &JobExecutor{
		store:         store,
		runLog:        runLog,
		notifications: notifications,
		runner:        runner,
		registry:      registry,
		bus:           msgBus,
		logger:        logger,
	}
}

// SetReminders wires a ReminderFirer for periodic overdue checking.
func (e *JobExecutor) SetReminders(r ReminderFirer) {
	e.reminders = r
}

// Start begins the timer loop. Call Stop() to shut down.
func (e *JobExecutor) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	// Clear stale running markers from previous crash
	if err := e.store.ClearStaleRunning(); err != nil {
		e.logger.Warn("failed to clear stale running markers", "err", err)
	}

	// Recompute NextRunAt for all jobs
	e.recomputeAll()

	e.wg.Add(1)
	go e.timerLoop(ctx)

	e.logger.Info("job executor started", "jobs", e.store.Count())
}

// Stop shuts down the executor and waits for completion.
func (e *JobExecutor) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

// RunImmediate triggers a job immediately, regardless of schedule.
func (e *JobExecutor) RunImmediate(ctx context.Context, jobID string) error {
	j, ok := e.store.Get(jobID)
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	go e.executeAndApply(ctx, *j)
	return nil
}

func (e *JobExecutor) timerLoop(ctx context.Context) {
	defer e.wg.Done()

	for {
		// Compute wake delay
		delay := e.computeDelay()

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			e.tick(ctx)
		}
	}
}

func (e *JobExecutor) computeDelay() time.Duration {
	wake := e.store.NextWakeAt()
	if wake == nil {
		return 60 * time.Second // nothing scheduled, check again in 60s
	}
	delay := time.Until(*wake)
	if delay < 0 {
		return 0 // overdue, fire immediately
	}
	// Cap at 60 seconds (matching OpenClaw MAX_TIMER_DELAY_MS)
	if delay > 60*time.Second {
		return 60 * time.Second
	}
	return delay
}

func (e *JobExecutor) tick(ctx context.Context) {
	// Check overdue reminders every tick
	e.fireReminders()

	// Reap completed sessions (throttled to every 5 minutes)
	// Reference: OpenClaw src/cron/session-reaper.ts
	e.reapSessions()

	now := time.Now()
	due := e.store.NextDue(now)

	for _, j := range due {
		// Mark as running before spawning goroutine to prevent double-fire
		if err := e.store.MarkRunning(j.ID, now); err != nil {
			e.logger.Error("failed to mark job running", "id", j.ID, "err", err)
			continue
		}

		e.logger.Info("job firing", "id", j.ID, "name", j.Name, "kind", j.Schedule.Kind)
		e.bus.Publish(bus.TopicCronStarted, bus.CronEvent{
			EntryID: j.ID,
			AgentID: j.AgentID,
		})

		e.wg.Add(1)
		go func(job Job) {
			defer e.wg.Done()
			e.executeAndApply(ctx, job)
		}(j)
	}
}

func (e *JobExecutor) fireReminders() {
	if e.reminders == nil || e.notifications == nil {
		return
	}
	fired, err := e.reminders.FireOverdue()
	if err != nil {
		e.logger.Warn("failed to check overdue reminders", "err", err)
		return
	}
	for _, r := range fired {
		e.bus.Publish(bus.TopicBackgroundResult, bus.BackgroundResult{
			Source:   "reminder",
			TaskName: r.Text,
			RefID:    r.ID,
			Status:   "completed",
			Content:  r.Text,
		})
		e.logger.Info("reminder fired", "id", r.ID, "text", r.Text)
	}
}

func (e *JobExecutor) executeAndApply(ctx context.Context, j Job) {
	start := time.Now()
	result, sessionID, err := e.executeJob(ctx, j)
	duration := time.Since(start)

	// Build run log entry
	entry := RunLogEntry{
		Timestamp: time.Now(),
		JobID:     j.ID,
		AgentID:   j.AgentID,
		SessionID: sessionID,
		Duration:  duration,
	}

	if err != nil {
		entry.Status = "error"
		entry.Error = err.Error()
		e.applyError(j, err, start)
	} else {
		entry.Status = "ok"
		entry.Summary = truncate(result, 500)
		e.applySuccess(j, result, start)
	}

	// Update session status for ephemeral sessions (Run() already does this,
	// but belt-and-suspenders for cases where the session was created but Run()
	// returned early). Skip persistent sessions (main/project).
	if sessionID != "" && e.runner.Sessions != nil {
		if meta, merr := e.runner.Sessions.GetMeta(sessionID); merr == nil && !isPersistentSession(meta) {
			sessStatus := "completed"
			if err != nil {
				sessStatus = "error"
			}
			e.runner.Sessions.UpdateStatus(sessionID, sessStatus)
		}
	}

	// Write run log
	if e.runLog != nil {
		if logErr := e.runLog.Append(entry); logErr != nil {
			e.logger.Warn("failed to write run log", "job", j.ID, "err", logErr)
		}
	}

	// Determine source tag for notifications and bus events
	source := "cron"
	if strings.HasPrefix(j.ID, heartbeatJobPrefix) {
		source = "heartbeat"
	}

	// Create notification
	if e.notifications != nil {
		sev := SeverityInfo
		title := fmt.Sprintf("Job %s completed", j.Name)
		content := truncate(result, 1000)
		if err != nil {
			sev = SeverityWarning
			title = fmt.Sprintf("Job %s failed", j.Name)
			content = err.Error()
		}
		e.notifications.Add(Notification{
			Severity: sev,
			Source:   source,
			Title:    title,
			Content:  content,
			AgentID:  j.AgentID,
			JobID:    j.ID,
		})
	}

	// Publish bus events
	status := "completed"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
	}

	// Publish heartbeat-specific events for heartbeat jobs
	e.publishHeartbeatEvents(j, duration, errStr)

	e.bus.Publish(bus.TopicCronCompleted, bus.CronEvent{
		EntryID:   j.ID,
		AgentID:   j.AgentID,
		SessionID: sessionID,
		Status:    status,
		Error:     errStr,
	})
	e.bus.Publish(bus.TopicBackgroundResult, bus.BackgroundResult{
		Source:   source,
		TaskName: j.Name,
		AgentID:  j.AgentID,
		Status:   status,
		Content:  truncate(result, 2000),
		Error:    errStr,
	})
}

func (e *JobExecutor) executeJob(ctx context.Context, j Job) (string, string, error) {
	a, ok := e.registry.Get(j.AgentID)
	if !ok {
		return "", "", fmt.Errorf("agent %q not found", j.AgentID)
	}

	prompt := j.Prompt
	if j.ContextMessages != "" {
		prompt = j.ContextMessages + "\n\n" + prompt
	}

	result, err := e.runner.Run(ctx, RunConfig{
		AgentID:        j.AgentID,
		Agent:          a,
		Prompt:         prompt,
		PromptMode:     PromptModeMinimal,
		PermissionMode: tool.PermissionWriteOnly,
	}, func(ev StreamEvent) error { return nil })

	if err != nil {
		return "", "", err
	}
	return result.Content, result.SessionID, nil
}

func (e *JobExecutor) applySuccess(j Job, result string, startedAt time.Time) {
	if j.DeleteAfterRun {
		// One-shot: remove from store
		if _, err := e.store.Remove(j.ID); err != nil {
			e.logger.Warn("failed to remove one-shot job", "id", j.ID, "err", err)
		}
		return
	}

	e.store.Update(j.ID, func(job *Job) {
		now := time.Now()
		job.State.RunningAt = nil
		job.State.LastRunAt = &now
		job.State.LastStatus = "ok"
		job.State.LastError = ""
		job.State.ConsecutiveErrors = 0

		switch job.Schedule.Kind {
		case ScheduleAt:
			// One-shot that wasn't deleted: disable
			job.Enabled = false
			job.State.NextRunAt = nil
		case ScheduleEvery, ScheduleCron:
			next, err := ComputeNextRun(job.Schedule, now, job.State.LastRunAt)
			if err != nil {
				e.logger.Warn("failed to compute next run", "id", j.ID, "err", err)
				job.Enabled = false
			} else {
				job.State.NextRunAt = next
			}
		}
	})
}

func (e *JobExecutor) applyError(j Job, execErr error, startedAt time.Time) {
	e.store.Update(j.ID, func(job *Job) {
		now := time.Now()
		job.State.RunningAt = nil
		job.State.LastRunAt = &now
		job.State.LastStatus = "error"
		job.State.LastError = execErr.Error()
		job.State.ConsecutiveErrors++

		switch job.Schedule.Kind {
		case ScheduleAt:
			// Retry with backoff for one-shots
			if job.State.ConsecutiveErrors <= maxTransientRetries {
				idx := job.State.ConsecutiveErrors - 1
				if idx >= len(transientBackoffs) {
					idx = len(transientBackoffs) - 1
				}
				retryAt := now.Add(transientBackoffs[idx])
				job.State.NextRunAt = &retryAt
				e.logger.Info("one-shot retry scheduled",
					"id", j.ID, "attempt", job.State.ConsecutiveErrors, "retry_at", retryAt)
			} else {
				// Max retries exhausted: disable
				job.Enabled = false
				job.State.NextRunAt = nil
				e.logger.Warn("one-shot max retries exhausted, disabling",
					"id", j.ID, "errors", job.State.ConsecutiveErrors)
			}

		case ScheduleEvery, ScheduleCron:
			// For recurring jobs, apply backoff but keep enabled
			next, err := ComputeNextRun(job.Schedule, now, job.State.LastRunAt)
			if err != nil {
				job.Enabled = false
				return
			}
			// Apply error backoff: at least the backoff duration from now
			if job.State.ConsecutiveErrors <= len(transientBackoffs) {
				idx := job.State.ConsecutiveErrors - 1
				if idx < 0 {
					idx = 0
				}
				minNext := now.Add(transientBackoffs[idx])
				if next != nil && next.Before(minNext) {
					next = &minNext
				}
			}
			job.State.NextRunAt = next
		}
	})
}

func (e *JobExecutor) recomputeAll() {
	now := time.Now()
	for _, j := range e.store.List() {
		if !j.Enabled {
			continue
		}
		// For "at" jobs, NextRunAt is the actual fire time set at creation.
		// Don't recompute — it would recalculate relative durations from now.
		if j.Schedule.Kind == ScheduleAt && j.State.NextRunAt != nil {
			continue
		}
		e.store.Update(j.ID, func(job *Job) {
			next, err := ComputeNextRun(job.Schedule, now, timeOrNil(job.State.LastRunAt))
			if err != nil {
				e.logger.Warn("failed to recompute next run", "id", j.ID, "err", err)
				return
			}
			job.State.NextRunAt = next
		})
	}
}

func timeOrNil(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

const (
	reapInterval  = 5 * time.Minute
	reapRetention = 24 * time.Hour
)

// reapSessions archives completed/error sessions older than retention.
// Throttled to run at most every 5 minutes.
// Reference: OpenClaw src/cron/session-reaper.ts
func (e *JobExecutor) reapSessions() {
	if e.runner == nil || e.runner.Sessions == nil {
		return
	}
	now := time.Now()
	if now.Sub(e.lastReapAt) < reapInterval {
		return
	}
	e.lastReapAt = now

	count, err := e.runner.Sessions.ReapCompleted(reapRetention)
	if err != nil {
		e.logger.Warn("session reaping failed", "err", err)
		return
	}
	if count > 0 {
		e.logger.Info("reaped sessions", "count", count)
	}
}
