package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ScheduleKind identifies the type of schedule for a job.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"    // one-shot at timestamp or relative duration
	ScheduleEvery ScheduleKind = "every" // fixed interval
	ScheduleCron  ScheduleKind = "cron"  // 5-field cron expression
)

// JobSchedule defines when a job should fire.
type JobSchedule struct {
	Kind     ScheduleKind `json:"kind"`
	At       string       `json:"at,omitempty"`       // ISO-8601 timestamp or Go duration ("2h", "30m")
	Every    string       `json:"every,omitempty"`     // Go duration string for interval
	Expr     string       `json:"expr,omitempty"`      // 5-field cron expression
	Timezone string       `json:"timezone,omitempty"`  // IANA timezone (for cron)
}

// JobState tracks runtime state for a job.
type JobState struct {
	NextRunAt        *time.Time `json:"next_run_at,omitempty"`
	LastRunAt        *time.Time `json:"last_run_at,omitempty"`
	RunningAt        *time.Time `json:"running_at,omitempty"`
	LastStatus       string     `json:"last_status,omitempty"` // "ok", "error", "skipped"
	LastError        string     `json:"last_error,omitempty"`
	ConsecutiveErrors int       `json:"consecutive_errors,omitempty"`
}

// Job is a scheduled unit of work. Matches OpenClaw's CronJob concept
// with three schedule kinds (at/every/cron) in a single unified type.
type Job struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Schedule        JobSchedule `json:"schedule"`
	AgentID         string      `json:"agent_id"`
	Prompt          string      `json:"prompt"`
	SessionTarget   string      `json:"session_target"` // "isolated" (default), "main"
	Enabled         bool        `json:"enabled"`
	DeleteAfterRun  bool        `json:"delete_after_run"`
	ContextMessages string      `json:"context_messages,omitempty"` // embedded context from creating session
	State           JobState    `json:"state"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// jobStoreFile is the on-disk format.
type jobStoreFile struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// JobStore persists jobs to a JSON file with mutex protection.
type JobStore struct {
	path string
	jobs map[string]*Job
	mu   sync.RWMutex
}

// NewJobStore creates or loads a job store from the given path.
func NewJobStore(path string) (*JobStore, error) {
	s := &JobStore{
		path: path,
		jobs: make(map[string]*Job),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Add adds a job to the store. If ID is empty, one is generated.
// For "at" schedule kind, DeleteAfterRun defaults to true unless explicitly set.
func (s *JobStore) Add(j Job) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if j.ID == "" {
		j.ID = uuid.NewString()[:8]
	}
	if _, exists := s.jobs[j.ID]; exists {
		return nil, fmt.Errorf("job %q already exists", j.ID)
	}

	now := time.Now()
	j.CreatedAt = now
	j.UpdatedAt = now
	if j.SessionTarget == "" {
		j.SessionTarget = "isolated"
	}
	if !j.Enabled {
		j.Enabled = true
	}

	// Compute initial NextRunAt
	next, err := ComputeNextRun(j.Schedule, now, nil)
	if err != nil {
		return nil, fmt.Errorf("compute next run: %w", err)
	}
	j.State.NextRunAt = next

	stored := j // copy
	s.jobs[j.ID] = &stored

	if err := s.save(); err != nil {
		delete(s.jobs, j.ID)
		return nil, err
	}
	return &stored, nil
}

// Remove deletes a job by ID. Returns the removed job, or error if not found.
func (s *JobStore) Remove(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	delete(s.jobs, id)
	if err := s.save(); err != nil {
		s.jobs[id] = j // restore
		return nil, err
	}
	return j, nil
}

// Get returns a job by ID.
func (s *JobStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// List returns all jobs sorted by creation time (newest first).
func (s *JobStore) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, k int) bool {
		return out[i].CreatedAt.After(out[k].CreatedAt)
	})
	return out
}

// Update applies a mutation function to a job and persists.
func (s *JobStore) Update(id string, fn func(j *Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	fn(j)
	j.UpdatedAt = time.Now()
	return s.save()
}

// NextDue returns all enabled jobs whose NextRunAt is at or before now,
// sorted by NextRunAt ascending (earliest first).
func (s *JobStore) NextDue(now time.Time) []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var due []Job
	for _, j := range s.jobs {
		if !j.Enabled {
			continue
		}
		if j.State.RunningAt != nil {
			continue // already running
		}
		if j.State.NextRunAt == nil {
			continue
		}
		if !j.State.NextRunAt.After(now) {
			due = append(due, *j)
		}
	}
	sort.Slice(due, func(i, k int) bool {
		return due[i].State.NextRunAt.Before(*due[k].State.NextRunAt)
	})
	return due
}

// NextWakeAt returns the earliest NextRunAt across all enabled jobs,
// or nil if no jobs are scheduled.
func (s *JobStore) NextWakeAt() *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var earliest *time.Time
	for _, j := range s.jobs {
		if !j.Enabled || j.State.NextRunAt == nil {
			continue
		}
		if earliest == nil || j.State.NextRunAt.Before(*earliest) {
			t := *j.State.NextRunAt
			earliest = &t
		}
	}
	return earliest
}

// ClearStaleRunning clears RunningAt markers left from a previous crash.
func (s *JobStore) ClearStaleRunning() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, j := range s.jobs {
		if j.State.RunningAt != nil {
			j.State.RunningAt = nil
			changed = true
		}
	}
	if changed {
		return s.save()
	}
	return nil
}

// MarkRunning sets the RunningAt timestamp for a job.
func (s *JobStore) MarkRunning(id string, now time.Time) error {
	return s.Update(id, func(j *Job) {
		j.State.RunningAt = &now
	})
}

// Count returns the number of jobs in the store.
func (s *JobStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

func (s *JobStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty store
		}
		return fmt.Errorf("read jobs: %w", err)
	}
	var f jobStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse jobs: %w", err)
	}
	for i := range f.Jobs {
		j := f.Jobs[i]
		s.jobs[j.ID] = &j
	}
	return nil
}

func (s *JobStore) save() error {
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	sort.Slice(jobs, func(i, k int) bool {
		return jobs[i].CreatedAt.Before(jobs[k].CreatedAt)
	})

	f := jobStoreFile{Version: 1, Jobs: jobs}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal jobs: %w", err)
	}

	// Atomic write: write to temp file then rename
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write jobs tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename jobs: %w", err)
	}
	return nil
}

// ComputeNextRun calculates the next run time for a schedule.
// For "at": parses the At field as ISO-8601 or Go duration relative to now.
// For "every": lastRun + interval, or now if never run.
// For "cron": walks forward minute-by-minute from now to find next match.
func ComputeNextRun(sched JobSchedule, now time.Time, lastRun *time.Time) (*time.Time, error) {
	switch sched.Kind {
	case ScheduleAt:
		return computeNextAt(sched.At, now)
	case ScheduleEvery:
		return computeNextEvery(sched.Every, now, lastRun)
	case ScheduleCron:
		return computeNextCron(sched.Expr, now)
	default:
		return nil, fmt.Errorf("unknown schedule kind %q", sched.Kind)
	}
}

func computeNextAt(at string, now time.Time) (*time.Time, error) {
	if at == "" {
		return nil, fmt.Errorf("at schedule requires 'at' field")
	}

	// Try ISO-8601 first
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		if t.After(now) {
			return &t, nil
		}
		return nil, nil // expired
	}

	// Try Go duration (relative to now)
	d, err := time.ParseDuration(at)
	if err != nil {
		return nil, fmt.Errorf("cannot parse at %q as timestamp or duration: %w", at, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("at duration must be positive, got %v", d)
	}
	t := now.Add(d)
	return &t, nil
}

func computeNextEvery(every string, now time.Time, lastRun *time.Time) (*time.Time, error) {
	if every == "" {
		return nil, fmt.Errorf("every schedule requires 'every' field")
	}
	d, err := time.ParseDuration(every)
	if err != nil {
		return nil, fmt.Errorf("parse every %q: %w", every, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("every duration must be positive, got %v", d)
	}
	if lastRun == nil {
		return &now, nil // due immediately
	}
	t := lastRun.Add(d)
	return &t, nil
}

func computeNextCron(expr string, now time.Time) (*time.Time, error) {
	if expr == "" {
		return nil, fmt.Errorf("cron schedule requires 'expr' field")
	}

	// Walk forward minute-by-minute, up to 366 days
	candidate := now.Truncate(time.Minute).Add(time.Minute)
	limit := now.Add(366 * 24 * time.Hour)

	for candidate.Before(limit) {
		if cronMatches(expr, candidate) {
			return &candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return nil, fmt.Errorf("no match for cron %q within 366 days", expr)
}
