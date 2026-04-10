package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/tool"
	"gopkg.in/yaml.v3"
)

// CronEntry defines a scheduled task.
type CronEntry struct {
	ID       string    `json:"id" yaml:"id"`
	Schedule string    `json:"schedule" yaml:"schedule"`
	AgentID  string    `json:"agent_id" yaml:"agent_id"`
	Prompt   string    `json:"prompt" yaml:"prompt"`
	Enabled  bool      `json:"enabled" yaml:"enabled"`
	LastRun  time.Time `json:"last_run,omitempty" yaml:"-"`
	NextRun  time.Time `json:"next_run,omitempty" yaml:"-"`
}

// CronConfig is the top-level cron.yaml structure.
type CronConfig struct {
	Entries []CronEntry `yaml:"entries"`
}

// HeartbeatResult records a heartbeat execution.
type HeartbeatResult struct {
	Timestamp time.Time `json:"timestamp"`
	Duration  string    `json:"duration"`
	Items     int       `json:"items"`
	Error     string    `json:"error,omitempty"`
}

// HeartbeatTask is a structured task parsed from HEARTBEAT.md's tasks: block.
type HeartbeatTask struct {
	Name     string `json:"name" yaml:"name"`
	Interval string `json:"interval" yaml:"interval"` // "1m", "5m", "30m", "1h"
	Agent    string `json:"agent" yaml:"agent"`       // default: "orchestrator"
	Prompt   string `json:"prompt" yaml:"prompt"`
	Once     bool   `json:"once,omitempty" yaml:"once,omitempty"` // auto-remove after first run
}

// HeartbeatTaskState tracks per-task execution state.
type HeartbeatTaskState struct {
	LastRun time.Time `json:"last_run"`
	Status  string    `json:"status"` // "completed", "error", "running"
	Error   string    `json:"error,omitempty"`
}

// HeartbeatTaskInfo is the user-facing view of a heartbeat task.
type HeartbeatTaskInfo struct {
	Name     string    `json:"name"`
	Interval string    `json:"interval"`
	Agent    string    `json:"agent"`
	Prompt   string    `json:"prompt"`
	LastRun  time.Time `json:"last_run,omitempty"`
	NextDue  time.Time `json:"next_due,omitempty"`
	Status   string    `json:"status,omitempty"`
}

// HeartbeatConfig is the parsed structure of HEARTBEAT.md.
type HeartbeatConfig struct {
	Content string          // freeform markdown (legacy)
	Tasks   []HeartbeatTask // structured tasks from tasks: YAML block
}

// Scheduler manages heartbeat and cron tasks.
type Scheduler struct {
	runner     *Runner
	workspaces *WorkspaceLoader
	registry   *Registry
	bus        *bus.Bus
	logger     *slog.Logger

	heartbeatInterval time.Duration
	cronEntries       []CronEntry
	cronPath          string
	heartbeatDir      string

	// Per-task heartbeat state
	heartbeatTasks map[string]*HeartbeatTaskState

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewScheduler creates a scheduler.
func NewScheduler(runner *Runner, workspaces *WorkspaceLoader, registry *Registry, msgBus *bus.Bus, logger *slog.Logger, heartbeatInterval string, cronPath, heartbeatDir string) *Scheduler {
	interval, err := time.ParseDuration(heartbeatInterval)
	if err != nil || interval <= 0 {
		interval = 30 * time.Minute
	}

	s := &Scheduler{
		runner:            runner,
		workspaces:        workspaces,
		registry:          registry,
		bus:               msgBus,
		logger:            logger,
		heartbeatInterval: interval,
		cronPath:          cronPath,
		heartbeatDir:      heartbeatDir,
		heartbeatTasks:    make(map[string]*HeartbeatTaskState),
	}

	s.loadCronEntries()
	s.loadHeartbeatState()
	return s
}

// Start begins the heartbeat and cron tickers.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)

	// Run BOOT.md if needed
	go s.runBootIfNeeded(ctx)

	// Heartbeat ticker
	go s.heartbeatLoop(ctx)

	// Cron ticker (check every minute)
	go s.cronLoop(ctx)

	s.logger.Info("scheduler started",
		"heartbeat_interval", s.heartbeatInterval,
		"cron_entries", len(s.cronEntries),
	)
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// HasPending returns true if there are active scheduled tasks.
func (s *Scheduler) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.cronEntries {
		if e.Enabled {
			return true
		}
	}
	return false
}

func (s *Scheduler) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runHeartbeat(ctx)
		}
	}
}

// RunHeartbeatNow triggers a heartbeat run immediately. Exported for testing.
func (s *Scheduler) RunHeartbeatNow(ctx context.Context) {
	s.runHeartbeat(ctx)
}

func (s *Scheduler) runHeartbeat(ctx context.Context) {
	// Load HEARTBEAT.md from orchestrator workspace
	// Check global workspace first, then embedded defaults
	ws, err := s.workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return
	}

	heartbeatMD := ws.Get(FileHeartbeat)
	if heartbeatMD == "" {
		return
	}

	// Parse structured tasks from HEARTBEAT.md
	config := parseHeartbeatConfig(heartbeatMD)

	if len(config.Tasks) > 0 {
		s.runHeartbeatTasks(ctx, config.Tasks)
	} else {
		// Legacy: run entire HEARTBEAT.md as a single prompt through orchestrator
		s.runHeartbeatLegacy(ctx, heartbeatMD)
	}
}

// runHeartbeatTasks executes individual heartbeat tasks that are due.
func (s *Scheduler) runHeartbeatTasks(ctx context.Context, tasks []HeartbeatTask) {
	now := time.Now()

	for _, task := range tasks {
		if !s.isTaskDue(task, now) {
			continue
		}

		agentID := task.Agent
		if agentID == "" {
			agentID = "orchestrator"
		}

		a, ok := s.registry.Get(agentID)
		if !ok {
			s.logger.Error("heartbeat task: agent not found", "task", task.Name, "agent", agentID)
			continue
		}

		s.logger.Info("heartbeat task starting", "task", task.Name, "agent", agentID)
		s.bus.Publish(bus.TopicHeartbeatStarted, bus.HeartbeatEvent{
			Items: 1,
		})

		// Mark running
		s.mu.Lock()
		s.heartbeatTasks[task.Name] = &HeartbeatTaskState{
			LastRun: now,
			Status:  "running",
		}
		s.mu.Unlock()

		start := time.Now()
		result, runErr := s.runner.Run(ctx, RunConfig{
			AgentID:        agentID,
			Agent:          a,
			Prompt:         task.Prompt,
			PromptMode:     PromptModeMinimal,
			PermissionMode: tool.PermissionWriteOnly,
		}, func(ev StreamEvent) error { return nil })

		duration := time.Since(start)

		// Update session status for ephemeral sessions (skip persistent main/project)
		if result != nil && result.SessionID != "" {
			if meta, merr := s.runner.Sessions.GetMeta(result.SessionID); merr == nil && !isPersistentSession(meta) {
				sessStatus := "completed"
				if runErr != nil {
					sessStatus = "error"
				}
				s.runner.Sessions.UpdateStatus(result.SessionID, sessStatus)
			}
		}

		// Update state
		s.mu.Lock()
		state := s.heartbeatTasks[task.Name]
		if runErr != nil {
			state.Status = "error"
			state.Error = runErr.Error()
			s.logger.Error("heartbeat task failed", "task", task.Name, "err", runErr)
		} else {
			state.Status = "completed"
			state.Error = ""
			s.logger.Info("heartbeat task completed", "task", task.Name, "duration", duration)
		}
		s.mu.Unlock()

		s.saveHeartbeatState()

		s.bus.Publish(bus.TopicHeartbeatCompleted, bus.HeartbeatEvent{
			Items:    1,
			Duration: duration.String(),
			Error:    state.Error,
		})

		_ = result
	}
}

// runHeartbeatLegacy runs the entire HEARTBEAT.md as a single prompt (original behavior).
func (s *Scheduler) runHeartbeatLegacy(ctx context.Context, heartbeatMD string) {
	items := 0
	for _, line := range strings.Split(heartbeatMD, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			items++
		}
	}

	s.logger.Info("heartbeat starting", "items", items)
	s.bus.Publish(bus.TopicHeartbeatStarted, bus.HeartbeatEvent{Items: items})

	start := time.Now()

	orchestrator, ok := s.registry.Get("orchestrator")
	if !ok {
		s.logger.Error("heartbeat: orchestrator not found")
		return
	}

	prompt := "Process this heartbeat checklist:\n\n" + heartbeatMD
	_, err := s.runner.Run(ctx, RunConfig{
		AgentID:    "orchestrator",
		Agent:      orchestrator,
		Prompt:     prompt,
		PromptMode: PromptModeFull,
	}, func(ev StreamEvent) error { return nil })

	duration := time.Since(start)
	result := HeartbeatResult{
		Timestamp: time.Now(),
		Duration:  duration.String(),
		Items:     items,
	}
	if err != nil {
		result.Error = err.Error()
		s.logger.Error("heartbeat failed", "err", err)
	} else {
		s.logger.Info("heartbeat completed", "duration", duration, "items", items)
	}

	s.saveHeartbeatResult(result)
	s.bus.Publish(bus.TopicHeartbeatCompleted, bus.HeartbeatEvent{
		Items:    items,
		Duration: duration.String(),
		Error:    result.Error,
	})
}

// isTaskDue returns true if enough time has passed since the task's last run.
func (s *Scheduler) isTaskDue(task HeartbeatTask, now time.Time) bool {
	interval, err := time.ParseDuration(task.Interval)
	if err != nil || interval <= 0 {
		interval = s.heartbeatInterval // fallback to global interval
	}

	s.mu.Lock()
	state, exists := s.heartbeatTasks[task.Name]
	s.mu.Unlock()

	if !exists || state.LastRun.IsZero() {
		return true // never run
	}
	return now.Sub(state.LastRun) >= interval
}

// HeartbeatTaskList returns info for all configured heartbeat tasks.
func (s *Scheduler) HeartbeatTaskList() []HeartbeatTaskInfo {
	ws, err := s.workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return nil
	}
	heartbeatMD := ws.Get(FileHeartbeat)
	if heartbeatMD == "" {
		return nil
	}

	config := parseHeartbeatConfig(heartbeatMD)
	now := time.Now()
	var infos []HeartbeatTaskInfo

	for _, task := range config.Tasks {
		info := HeartbeatTaskInfo{
			Name:     task.Name,
			Interval: task.Interval,
			Agent:    task.Agent,
			Prompt:   task.Prompt,
		}
		if info.Agent == "" {
			info.Agent = "orchestrator"
		}

		s.mu.Lock()
		if state, ok := s.heartbeatTasks[task.Name]; ok {
			info.LastRun = state.LastRun
			info.Status = state.Status

			interval, _ := time.ParseDuration(task.Interval)
			if interval > 0 {
				info.NextDue = state.LastRun.Add(interval)
			}
		} else {
			info.Status = "never_run"
			info.NextDue = now // due immediately
		}
		s.mu.Unlock()

		infos = append(infos, info)
	}
	return infos
}

// TriggerHeartbeatTask manually fires a specific heartbeat task.
func (s *Scheduler) TriggerHeartbeatTask(ctx context.Context, taskName string) error {
	ws, err := s.workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return fmt.Errorf("failed to load workspace")
	}
	heartbeatMD := ws.Get(FileHeartbeat)
	config := parseHeartbeatConfig(heartbeatMD)

	for _, task := range config.Tasks {
		if task.Name == taskName {
			s.runHeartbeatTasks(ctx, []HeartbeatTask{task})
			return nil
		}
	}
	return fmt.Errorf("heartbeat task %q not found", taskName)
}

// parseHeartbeatConfig extracts structured tasks from HEARTBEAT.md.
// If the file contains a YAML tasks: block (delimited by ```yaml ... ```),
// those are parsed as structured tasks. Everything else is freeform content.
func parseHeartbeatConfig(md string) HeartbeatConfig {
	config := HeartbeatConfig{Content: md}

	// Look for YAML block with tasks
	yamlStart := strings.Index(md, "```yaml")
	if yamlStart < 0 {
		yamlStart = strings.Index(md, "```yml")
	}
	if yamlStart < 0 {
		// Also try bare tasks: at the start of a line
		for _, line := range strings.Split(md, "\n") {
			if strings.TrimSpace(line) == "tasks:" {
				yamlStart = strings.Index(md, "tasks:")
				break
			}
		}
	}

	if yamlStart < 0 {
		return config
	}

	// Extract YAML content
	yamlContent := md[yamlStart:]
	if strings.HasPrefix(yamlContent, "```") {
		// Fenced block
		endIdx := strings.Index(yamlContent[3:], "```")
		if endIdx >= 0 {
			yamlContent = yamlContent[strings.Index(yamlContent, "\n")+1 : endIdx+3]
		}
	}

	var parsed struct {
		Tasks []HeartbeatTask `yaml:"tasks"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err == nil && len(parsed.Tasks) > 0 {
		config.Tasks = parsed.Tasks
	}

	return config
}

// loadHeartbeatState reads persisted heartbeat task state from disk.
func (s *Scheduler) loadHeartbeatState() {
	path := s.heartbeatDir + "/.heartbeat_state.json"
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state map[string]*HeartbeatTaskState
	if json.Unmarshal(data, &state) == nil {
		s.heartbeatTasks = state
	}
}

// saveHeartbeatState persists heartbeat task state to disk.
func (s *Scheduler) saveHeartbeatState() {
	s.mu.Lock()
	data, _ := json.MarshalIndent(s.heartbeatTasks, "", "  ")
	s.mu.Unlock()

	os.MkdirAll(s.heartbeatDir, 0o700)
	os.WriteFile(s.heartbeatDir+"/.heartbeat_state.json", data, 0o644)
}

func (s *Scheduler) cronLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkCron(ctx)
		}
	}
}

func (s *Scheduler) checkCron(ctx context.Context) {
	s.mu.Lock()
	entries := make([]CronEntry, len(s.cronEntries))
	copy(entries, s.cronEntries)
	s.mu.Unlock()

	now := time.Now()
	for i, entry := range entries {
		if !entry.Enabled {
			continue
		}
		if !cronMatches(entry.Schedule, now) {
			continue
		}
		// Don't re-run within the same minute
		if !entry.LastRun.IsZero() && now.Sub(entry.LastRun) < time.Minute {
			continue
		}

		s.logger.Info("cron firing", "id", entry.ID, "agent", entry.AgentID)
		s.bus.Publish(bus.TopicCronStarted, bus.CronEvent{
			EntryID: entry.ID,
			AgentID: entry.AgentID,
		})

		// Run in isolated session
		go s.runCronEntry(ctx, entry)

		// Update last run
		s.mu.Lock()
		s.cronEntries[i].LastRun = now
		s.mu.Unlock()
	}
}

func (s *Scheduler) runCronEntry(ctx context.Context, entry CronEntry) {
	a, ok := s.registry.Get(entry.AgentID)
	if !ok {
		s.logger.Error("cron: agent not found", "id", entry.AgentID)
		return
	}

	result, err := s.runner.Run(ctx, RunConfig{
		AgentID:        entry.AgentID,
		Agent:          a,
		Prompt:         entry.Prompt,
		PromptMode:     PromptModeMinimal,
		PermissionMode: tool.PermissionWriteOnly,
	}, func(ev StreamEvent) error { return nil })

	status := "completed"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
		s.logger.Error("cron failed", "id", entry.ID, "err", err)
	} else {
		s.logger.Info("cron completed", "id", entry.ID, "session", result.SessionID)
	}

	// Update session status for ephemeral sessions (skip persistent main/project)
	if result != nil && result.SessionID != "" {
		if meta, merr := s.runner.Sessions.GetMeta(result.SessionID); merr == nil && !isPersistentSession(meta) {
			s.runner.Sessions.UpdateStatus(result.SessionID, status)
		}
	}

	s.bus.Publish(bus.TopicCronCompleted, bus.CronEvent{
		EntryID: entry.ID,
		AgentID: entry.AgentID,
		Status:  status,
		Error:   errStr,
	})
}

func (s *Scheduler) runBootIfNeeded(ctx context.Context) {
	if s.workspaces.BootRanToday() {
		return
	}

	ws, err := s.workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return
	}

	bootMD := ws.Get(FileBoot)
	if bootMD == "" {
		s.workspaces.MarkBootRan()
		return
	}

	s.logger.Info("running BOOT.md")

	orchestrator, ok := s.registry.Get("orchestrator")
	if !ok {
		return
	}

	_, err = s.runner.Run(ctx, RunConfig{
		AgentID:    "orchestrator",
		Agent:      orchestrator,
		Prompt:     "Execute this startup checklist:\n\n" + bootMD,
		PromptMode: PromptModeFull,
	}, func(ev StreamEvent) error { return nil })

	if err != nil {
		s.logger.Error("BOOT.md failed", "err", err)
	} else {
		s.logger.Info("BOOT.md completed")
	}

	s.workspaces.MarkBootRan()
}

// ReloadCron re-reads cron.yaml from disk and replaces the in-memory entries.
func (s *Scheduler) ReloadCron() (int, error) {
	data, err := os.ReadFile(s.cronPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.mu.Lock()
			s.cronEntries = nil
			s.mu.Unlock()
			return 0, nil
		}
		return 0, fmt.Errorf("read cron: %w", err)
	}
	var cfg CronConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Errorf("parse cron: %w", err)
	}
	s.mu.Lock()
	s.cronEntries = cfg.Entries
	s.mu.Unlock()
	s.logger.Info("cron reloaded", "entries", len(cfg.Entries))
	return len(cfg.Entries), nil
}

// Entries returns a snapshot of current cron entries.
func (s *Scheduler) Entries() []CronEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CronEntry, len(s.cronEntries))
	copy(out, s.cronEntries)
	return out
}

func (s *Scheduler) loadCronEntries() {
	data, err := os.ReadFile(s.cronPath)
	if err != nil {
		return
	}
	var cfg CronConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		s.logger.Warn("failed to parse cron.yaml", "err", err)
		return
	}
	s.cronEntries = cfg.Entries
}

func (s *Scheduler) saveHeartbeatResult(result HeartbeatResult) {
	os.MkdirAll(s.heartbeatDir, 0o700)
	path := s.heartbeatDir + "/last_run.json"
	data, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile(path, data, 0o644)
}

// cronMatches checks if a 5-field cron expression matches the given time.
// Format: minute hour day-of-month month day-of-week
func cronMatches(schedule string, t time.Time) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
	}{
		{fields[0], t.Minute()},
		{fields[1], t.Hour()},
		{fields[2], t.Day()},
		{fields[3], int(t.Month())},
		{fields[4], int(t.Weekday())},
	}

	for _, c := range checks {
		if !cronFieldMatches(c.field, c.value) {
			return false
		}
	}
	return true
}

func cronFieldMatches(field string, value int) bool {
	if field == "*" {
		return true
	}

	// Handle ranges like "1-5"
	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		lo, _ := strconv.Atoi(parts[0])
		hi, _ := strconv.Atoi(parts[1])
		return value >= lo && value <= hi
	}

	// Handle intervals like "*/5"
	if strings.HasPrefix(field, "*/") {
		interval, _ := strconv.Atoi(strings.TrimPrefix(field, "*/"))
		if interval <= 0 {
			return false
		}
		return value%interval == 0
	}

	// Handle comma-separated values like "0,15,30,45"
	for _, part := range strings.Split(field, ",") {
		v, _ := strconv.Atoi(strings.TrimSpace(part))
		if v == value {
			return true
		}
	}

	return false
}
