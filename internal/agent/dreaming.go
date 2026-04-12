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

// Dreaming phase prompts.
// Reference: OpenClaw src/memory-host-sdk/dreaming.ts

func lightDreamingPrompt() string {
	return `You are performing a LIGHT dreaming phase — a periodic shallow consolidation of today's activity.

Instructions:
1. Read today's daily notes using memory_read (today's date).
2. Extract key facts, decisions, and patterns from the notes.
3. Write a concise summary of findings to today's daily log using memory_write with type=daily.

Focus on:
- Decisions made and their context
- Facts learned about systems, people, or processes
- File paths and code locations referenced
- Errors encountered and how they were resolved
- Tool usage patterns worth noting

Skip trivial or routine observations. Be concise — one line per finding.`
}

func deepDreamingPrompt() string {
	return `You are performing a DEEP dreaming phase — a daily consolidation that promotes durable knowledge to long-term memory.

Instructions:
1. Read daily logs from the past 7 days using memory_read for each day.
2. Read the curated MEMORY.md using memory_read.
3. Identify facts, patterns, or insights that appeared in 3+ different contexts across days.
4. Promote durable, high-confidence insights to MEMORY.md using memory_write with type=curated.
5. Note any entries in MEMORY.md that are contradicted by recent evidence — mark them for review.

Rules:
- Only promote facts that are well-established across multiple observations.
- Keep entries concise — one line per fact.
- Do not duplicate entries already in MEMORY.md.
- Prefix new entries with today's date in brackets: [2006-01-02].
- If MEMORY.md has stale entries, note them with [REVIEW] prefix.`
}

func remDreamingPrompt() string {
	return `You are performing a REM dreaming phase — a weekly reflection that extracts meta-patterns and themes.

Instructions:
1. Read MEMORY.md using memory_read.
2. Read recent daily logs from the past week using memory_read.
3. Extract recurring themes and meta-patterns across all memory sources.
4. Write a dream diary entry to today's daily log using memory_write with type=daily.

Format the diary entry with a "## Dream Diary" header. Capture:
- Recurring themes in the user's work (what projects, what concerns)
- Patterns in tool usage or problem-solving approaches
- Cross-cutting concerns that span multiple projects or conversations
- Shifts in priorities or interests over time
- Observations about what works well vs. what causes friction

This is reflective, not prescriptive. Observe patterns, don't make recommendations.`
}
