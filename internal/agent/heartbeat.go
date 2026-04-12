package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/tool"
	"gopkg.in/yaml.v3"
)

// HeartbeatTask is a structured task parsed from HEARTBEAT.md's tasks: block.
type HeartbeatTask struct {
	Name     string `json:"name" yaml:"name"`
	Interval string `json:"interval" yaml:"interval"` // "1m", "5m", "30m", "1h"
	Agent    string `json:"agent" yaml:"agent"`       // default: "orchestrator"
	Prompt   string `json:"prompt" yaml:"prompt"`
}

// HeartbeatConfig is the parsed structure of HEARTBEAT.md.
type HeartbeatConfig struct {
	Content string          // freeform markdown (legacy)
	Tasks   []HeartbeatTask // structured tasks from tasks: YAML block
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

const heartbeatJobPrefix = "heartbeat-"

// parseHeartbeatConfig extracts structured tasks from HEARTBEAT.md.
// If the file contains a YAML tasks: block (delimited by ```yaml ... ```),
// those are parsed as structured tasks. Everything else is freeform content.
func parseHeartbeatConfig(md string) HeartbeatConfig {
	config := HeartbeatConfig{Content: md}

	yamlStart := strings.Index(md, "```yaml")
	if yamlStart < 0 {
		yamlStart = strings.Index(md, "```yml")
	}
	if yamlStart < 0 {
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

	yamlContent := md[yamlStart:]
	if strings.HasPrefix(yamlContent, "```") {
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

// SyncHeartbeatJobs reads HEARTBEAT.md from the orchestrator workspace and
// ensures each structured task has a corresponding "every" job in the store.
// Jobs are keyed by "heartbeat-{taskName}". Stale heartbeat jobs (no longer
// in HEARTBEAT.md) are removed.
func (e *JobExecutor) SyncHeartbeatJobs(workspaces *WorkspaceLoader) error {
	ws, err := workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return nil
	}
	heartbeatMD := ws.Get(FileHeartbeat)
	if heartbeatMD == "" {
		return nil
	}

	config := parseHeartbeatConfig(heartbeatMD)
	if len(config.Tasks) == 0 {
		// No structured tasks — don't create heartbeat jobs.
		// The built-in HEARTBEAT.md is a template; it only runs when
		// the user defines structured tasks (## task: headers).
		return nil
	}

	// Build set of expected job IDs
	expected := make(map[string]HeartbeatTask, len(config.Tasks))
	for _, task := range config.Tasks {
		expected[heartbeatJobPrefix+task.Name] = task
	}

	// Sync: create/update jobs that should exist
	for id, task := range expected {
		agentID := task.Agent
		if agentID == "" {
			agentID = "orchestrator"
		}
		interval := task.Interval
		if interval == "" {
			interval = "30m"
		}
		if _, perr := time.ParseDuration(interval); perr != nil {
			e.logger.Warn("invalid heartbeat interval, skipping", "task", task.Name, "interval", interval)
			continue
		}

		existing, exists := e.store.Get(id)
		if exists {
			needsUpdate := existing.Prompt != task.Prompt ||
				existing.AgentID != agentID ||
				existing.Schedule.Every != interval
			if needsUpdate {
				e.store.Update(id, func(j *Job) {
					j.Prompt = task.Prompt
					j.AgentID = agentID
					j.Schedule.Every = interval
					j.Name = "heartbeat: " + task.Name
				})
			}
			continue
		}

		j := Job{
			ID:   id,
			Name: "heartbeat: " + task.Name,
			Schedule: JobSchedule{
				Kind:  ScheduleEvery,
				Every: interval,
			},
			AgentID:        agentID,
			Prompt:         task.Prompt,
			SessionTarget:  "isolated",
			Enabled:        true,
			DeleteAfterRun: false,
		}
		if _, err := e.store.Add(j); err != nil {
			e.logger.Warn("failed to create heartbeat job", "task", task.Name, "err", err)
		} else {
			e.logger.Info("created heartbeat job", "id", id, "interval", interval)
		}
	}

	// Remove heartbeat jobs that no longer have corresponding tasks
	for _, j := range e.store.List() {
		if !strings.HasPrefix(j.ID, heartbeatJobPrefix) {
			continue
		}
		if _, ok := expected[j.ID]; !ok {
			if _, err := e.store.Remove(j.ID); err != nil {
				e.logger.Warn("failed to remove stale heartbeat job", "id", j.ID, "err", err)
			} else {
				e.logger.Info("removed stale heartbeat job", "id", j.ID)
			}
		}
	}

	return nil
}

// RunBootIfNeeded checks BOOT.md and runs it once per day.
func (e *JobExecutor) RunBootIfNeeded(ctx context.Context, workspaces *WorkspaceLoader) {
	if workspaces.BootRanToday() {
		return
	}

	ws, err := workspaces.Load("orchestrator", nil)
	if err != nil || ws == nil {
		return
	}

	bootMD := ws.Get(FileBoot)
	if bootMD == "" {
		workspaces.MarkBootRan()
		return
	}

	e.logger.Info("running BOOT.md")

	orchestrator, ok := e.registry.Get("orchestrator")
	if !ok {
		return
	}

	_, err = e.runner.Run(ctx, RunConfig{
		AgentID:        "orchestrator",
		Agent:          orchestrator,
		Prompt:         "Execute this startup checklist:\n\n" + bootMD,
		PromptMode:     PromptModeFull,
		PermissionMode: tool.PermissionWriteOnly,
	}, func(ev StreamEvent) error { return nil })

	if err != nil {
		e.logger.Error("BOOT.md failed", "err", err)
	} else {
		e.logger.Info("BOOT.md completed")
	}

	workspaces.MarkBootRan()
}

// MigrateLegacyCron reads legacy cron.yaml entries and creates corresponding
// "cron" jobs in the store. Returns the number of entries migrated.
func (e *JobExecutor) MigrateLegacyCron(cronPath string) int {
	entries, err := readLegacyCronFile(cronPath)
	if err != nil || len(entries) == 0 {
		return 0
	}

	migrated := 0
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		id := "cron-" + entry.ID
		if _, exists := e.store.Get(id); exists {
			continue
		}

		j := Job{
			ID:   id,
			Name: "cron: " + entry.ID,
			Schedule: JobSchedule{
				Kind: ScheduleCron,
				Expr: entry.Schedule,
			},
			AgentID:        entry.AgentID,
			Prompt:         entry.Prompt,
			SessionTarget:  "isolated",
			Enabled:        true,
			DeleteAfterRun: false,
		}
		if _, err := e.store.Add(j); err != nil {
			e.logger.Warn("failed to migrate cron entry", "id", entry.ID, "err", err)
		} else {
			e.logger.Info("migrated cron entry to job", "id", id, "schedule", entry.Schedule)
			migrated++
		}
	}

	return migrated
}

// HeartbeatTaskList returns info for all heartbeat jobs.
func (e *JobExecutor) HeartbeatTaskList() []HeartbeatTaskInfo {
	var infos []HeartbeatTaskInfo
	for _, j := range e.store.List() {
		if !strings.HasPrefix(j.ID, heartbeatJobPrefix) {
			continue
		}
		taskName := strings.TrimPrefix(j.ID, heartbeatJobPrefix)
		info := HeartbeatTaskInfo{
			Name:     taskName,
			Interval: j.Schedule.Every,
			Agent:    j.AgentID,
			Prompt:   j.Prompt,
		}
		if j.State.LastRunAt != nil {
			info.LastRun = *j.State.LastRunAt
		}
		if j.State.NextRunAt != nil {
			info.NextDue = *j.State.NextRunAt
		}
		info.Status = j.State.LastStatus
		if info.Status == "" {
			info.Status = "never_run"
		}
		infos = append(infos, info)
	}
	return infos
}

// TriggerHeartbeatTask fires a specific heartbeat task immediately.
func (e *JobExecutor) TriggerHeartbeatTask(ctx context.Context, taskName string) error {
	id := heartbeatJobPrefix + taskName
	if _, ok := e.store.Get(id); !ok {
		return fmt.Errorf("heartbeat task %q not found (job %q)", taskName, id)
	}
	return e.RunImmediate(ctx, id)
}

// publishHeartbeatEvents publishes heartbeat bus events for heartbeat job completions.
func (e *JobExecutor) publishHeartbeatEvents(j Job, duration time.Duration, errStr string) {
	if !strings.HasPrefix(j.ID, heartbeatJobPrefix) {
		return
	}
	e.bus.Publish(bus.TopicHeartbeatCompleted, bus.HeartbeatEvent{
		Items:    1,
		Duration: duration.String(),
		Error:    errStr,
	})
}

// legacyCronEntry is used only for migrating cron.yaml.
type legacyCronEntry struct {
	ID       string `yaml:"id"`
	Schedule string `yaml:"schedule"`
	AgentID  string `yaml:"agent_id"`
	Prompt   string `yaml:"prompt"`
	Enabled  bool   `yaml:"enabled"`
}

type legacyCronConfig struct {
	Entries []legacyCronEntry `yaml:"entries"`
}

func readLegacyCronFile(path string) ([]legacyCronEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg legacyCronConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Entries, nil
}
