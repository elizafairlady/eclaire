package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DreamPhase identifies one of the three dreaming phases.
// Reference: OpenClaw src/memory-host-sdk/dreaming.ts
type DreamPhase string

const (
	PhaseLight DreamPhase = "light"
	PhaseDeep  DreamPhase = "deep"
	PhaseREM   DreamPhase = "rem"
)

// DreamingJobPrefix is the ID prefix for dreaming jobs in the JobStore.
const DreamingJobPrefix = "dreaming-"

// DreamPhaseStatus reports the state of a single dreaming phase.
type DreamPhaseStatus struct {
	Phase   DreamPhase `json:"phase"`
	Enabled bool       `json:"enabled"`
	LastRun *time.Time `json:"last_run,omitempty"`
	NextRun *time.Time `json:"next_run,omitempty"`
	Status  string     `json:"status,omitempty"`
}

// DreamingStatus reports the state of all dreaming phases.
type DreamingStatus struct {
	Enabled bool               `json:"enabled"`
	Phases  []DreamPhaseStatus `json:"phases"`
}

// DreamingService manages the three dreaming phases as scheduled jobs.
// Dreaming jobs are stored in the unified JobStore and executed by the JobExecutor.
// Disabled by default — user or Claire enables via eclaire_manage.
//
// Reference: OpenClaw src/memory-host-sdk/dreaming.ts
type DreamingService struct {
	jobStore  *JobStore
	jobExec   *JobExecutor
	logger    *slog.Logger
}

// NewDreamingService creates a dreaming service.
func NewDreamingService(store *JobStore, exec *JobExecutor, logger *slog.Logger) *DreamingService {
	return &DreamingService{
		jobStore:  store,
		jobExec:   exec,
		logger:    logger,
	}
}

// phaseDefinitions returns the three dreaming phase job configurations.
func phaseDefinitions() []struct {
	id       string
	name     string
	phase    DreamPhase
	schedule JobSchedule
	prompt   string
} {
	return []struct {
		id       string
		name     string
		phase    DreamPhase
		schedule JobSchedule
		prompt   string
	}{
		{
			id:       DreamingJobPrefix + "light",
			name:     "dreaming-light",
			phase:    PhaseLight,
			schedule: JobSchedule{Kind: ScheduleEvery, Every: "6h"},
			prompt:   lightDreamingPrompt(),
		},
		{
			id:       DreamingJobPrefix + "deep",
			name:     "dreaming-deep",
			phase:    PhaseDeep,
			schedule: JobSchedule{Kind: ScheduleCron, Expr: "0 3 * * *"},
			prompt:   deepDreamingPrompt(),
		},
		{
			id:       DreamingJobPrefix + "rem",
			name:     "dreaming-rem",
			phase:    PhaseREM,
			schedule: JobSchedule{Kind: ScheduleCron, Expr: "0 5 * * 0"},
			prompt:   remDreamingPrompt(),
		},
	}
}

// EnsureJobs idempotently creates the three dreaming jobs in the JobStore.
// Jobs are created disabled by default.
func (d *DreamingService) EnsureJobs() error {
	for _, def := range phaseDefinitions() {
		if _, ok := d.jobStore.Get(def.id); ok {
			continue // already exists
		}

		now := time.Now()
		next, _ := ComputeNextRun(def.schedule, now, nil)

		job := Job{
			ID:             def.id,
			Name:           def.name,
			Schedule:       def.schedule,
			AgentID:        "orchestrator",
			Prompt:         def.prompt,
			SessionTarget:  "isolated",
			Enabled:        false, // disabled by default, matching OpenClaw
			DeleteAfterRun: false,
			State: JobState{
				NextRunAt: next,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := d.jobStore.Add(job); err != nil {
			d.logger.Warn("failed to create dreaming job", "phase", def.name, "err", err)
		} else {
			// JobStore.Add() forces Enabled=true — disable immediately.
			// Dreaming is off by default (matching OpenClaw).
			d.jobStore.Update(def.id, func(j *Job) {
				j.Enabled = false
			})
			d.logger.Info("created dreaming job", "phase", def.name, "enabled", false)
		}
	}
	return nil
}

// Enable enables all three dreaming phases.
func (d *DreamingService) Enable() error {
	for _, def := range phaseDefinitions() {
		d.jobStore.Update(def.id, func(j *Job) {
			j.Enabled = true
			now := time.Now()
			next, _ := ComputeNextRun(j.Schedule, now, nil)
			j.State.NextRunAt = next
		})
	}
	d.logger.Info("dreaming enabled")
	return nil
}

// Disable disables all three dreaming phases.
func (d *DreamingService) Disable() error {
	for _, def := range phaseDefinitions() {
		d.jobStore.Update(def.id, func(j *Job) {
			j.Enabled = false
		})
	}
	d.logger.Info("dreaming disabled")
	return nil
}

// Status reports the current state of all dreaming phases.
func (d *DreamingService) Status() DreamingStatus {
	anyEnabled := false
	var phases []DreamPhaseStatus

	for _, def := range phaseDefinitions() {
		ps := DreamPhaseStatus{Phase: def.phase}
		if job, ok := d.jobStore.Get(def.id); ok {
			ps.Enabled = job.Enabled
			ps.LastRun = job.State.LastRunAt
			ps.NextRun = job.State.NextRunAt
			ps.Status = job.State.LastStatus
			if job.Enabled {
				anyEnabled = true
			}
		}
		phases = append(phases, ps)
	}

	return DreamingStatus{
		Enabled: anyEnabled,
		Phases:  phases,
	}
}

// TriggerPhase immediately executes a specific dreaming phase.
func (d *DreamingService) TriggerPhase(ctx context.Context, phase DreamPhase) error {
	jobID := DreamingJobPrefix + string(phase)
	if _, ok := d.jobStore.Get(jobID); !ok {
		return fmt.Errorf("dreaming phase %q not found (run EnsureJobs first)", phase)
	}
	return d.jobExec.RunImmediate(ctx, jobID)
}
