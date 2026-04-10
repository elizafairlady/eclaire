package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/fantasy"
	"gopkg.in/yaml.v3"
)

// ReloadResult contains the outcome of a reload operation.
type ReloadResult struct {
	AgentsLoaded   int      `json:"agents_loaded"`
	AgentsReplaced int      `json:"agents_replaced"`
	CronEntries    int      `json:"cron_entries"`
	Errors         []string `json:"errors,omitempty"`
}

// CronEntry mirrors agent.CronEntry to avoid import cycle.
type CronEntry struct {
	ID       string `json:"id" yaml:"id"`
	Schedule string `json:"schedule" yaml:"schedule"`
	AgentID  string `json:"agent_id" yaml:"agent_id"`
	Prompt   string `json:"prompt" yaml:"prompt"`
	Enabled  bool   `json:"enabled" yaml:"enabled"`
}

// CronConfig is the top-level cron.yaml structure.
type CronConfig struct {
	Entries []CronEntry `yaml:"entries"`
}

// AgentInfo is a snapshot of agent state for display.
type AgentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Role        string `json:"role"`
	BuiltIn     bool   `json:"built_in"`
}

// FlowInfo mirrors agent.FlowDef for display without import cycle.
type FlowInfo struct {
	ID          string     `json:"id" yaml:"id"`
	Name        string     `json:"name" yaml:"name"`
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`
	Steps       []FlowStep `json:"steps" yaml:"steps"`
}

// FlowStep mirrors agent.FlowStep.
type FlowStep struct {
	Name   string `json:"name" yaml:"name"`
	Agent  string `json:"agent" yaml:"agent"`
	Prompt string `json:"prompt" yaml:"prompt"`
}

// FlowRunInfo is the result of a flow run or status query.
type FlowRunInfo struct {
	ID          string   `json:"id"`
	FlowID      string   `json:"flow_id"`
	Status      string   `json:"status"`
	CurrentStep int      `json:"current_step"`
	TotalSteps  int      `json:"total_steps"`
	StepOutputs []string `json:"step_outputs,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// ManageDeps holds everything the eclaire_manage tool needs.
// HeartbeatTaskInfo mirrors agent.HeartbeatTaskInfo without import cycle.
type HeartbeatTaskInfo struct {
	Name     string `json:"name"`
	Interval string `json:"interval"`
	Agent    string `json:"agent"`
	Prompt   string `json:"prompt"`
	LastRun  string `json:"last_run,omitempty"`
	NextDue  string `json:"next_due,omitempty"`
	Status   string `json:"status,omitempty"`
}

// JobInfo is a snapshot of a job for display via the manage tool.
type JobInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ScheduleKind   string `json:"schedule_kind"`
	Schedule       string `json:"schedule"`        // human-readable schedule
	AgentID        string `json:"agent_id"`
	Prompt         string `json:"prompt"`
	Enabled        bool   `json:"enabled"`
	DeleteAfterRun bool   `json:"delete_after_run"`
	NextRunAt      string `json:"next_run_at,omitempty"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastStatus     string `json:"last_status,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

// JobRunLogEntry mirrors agent.RunLogEntry for the tool layer.
type JobRunLogEntry struct {
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

type ManageDeps struct {
	AgentsDir     string
	SkillsDir     string
	FlowsDir      string
	CronPath      string
	WorkspaceDir  string
	Reload        func() ReloadResult
	CronList      func() []CronEntry
	AgentList     func() []AgentInfo
	FlowList      func() []FlowInfo
	FlowRun       func(flowID, input string) (*FlowRunInfo, error)
	FlowGet       func(runID string) (*FlowRunInfo, bool)
	HeartbeatList func() []HeartbeatTaskInfo
	HeartbeatTrigger func(ctx context.Context, name string) error

	// Job operations (unified scheduling)
	JobAdd    func(name, scheduleKind, scheduleValue, agentID, prompt, sessionTarget string, deleteAfterRun *bool, contextMessages string) (JobInfo, error)
	JobRemove func(id string) error
	JobList   func() []JobInfo
	JobRun    func(ctx context.Context, id string) error
	JobRuns   func(id string) []JobRunLogEntry

	// Notification
	NotifyAdd func(title, content, severity string) error

	// Dreaming (memory consolidation)
	DreamingEnable  func() error
	DreamingDisable func() error
	DreamingStatus  func() DreamingStatusInfo
	DreamingTrigger func(ctx context.Context, phase string) error

	Logger    *slog.Logger
}

// DreamingStatusInfo is returned by the dreaming_status operation.
type DreamingStatusInfo struct {
	Enabled bool                    `json:"enabled"`
	Phases  []DreamingPhaseInfo     `json:"phases"`
}

// DreamingPhaseInfo reports the state of one dreaming phase.
type DreamingPhaseInfo struct {
	Phase   string `json:"phase"`
	Enabled bool   `json:"enabled"`
	LastRun string `json:"last_run,omitempty"`
	NextRun string `json:"next_run,omitempty"`
	Status  string `json:"status,omitempty"`
}

type manageInput struct {
	Operation string `json:"operation" jsonschema:"description=Operation: agent_create agent_list skill_create job_add job_remove job_list job_runs job_run notification_add cron_add cron_remove cron_list flow_create flow_list flow_run flow_status heartbeat_add heartbeat_remove heartbeat_list heartbeat_trigger dreaming_enable dreaming_disable dreaming_status dreaming_trigger reload"`

	// agent_create
	AgentID    string   `json:"agent_id,omitempty" jsonschema:"description=Agent ID (lowercase alphanumeric + hyphens)"`
	AgentName  string   `json:"agent_name,omitempty" jsonschema:"description=Human-readable agent name"`
	AgentDesc  string   `json:"agent_description,omitempty" jsonschema:"description=Short agent description"`
	AgentRole  string   `json:"agent_role,omitempty" jsonschema:"description=Agent role: simple or complex"`
	AgentTools []string `json:"agent_tools,omitempty" jsonschema:"description=List of tool names"`
	AgentSoul  string   `json:"agent_soul,omitempty" jsonschema:"description=SOUL.md content (system prompt)"`
	AgentModel string   `json:"agent_model,omitempty" jsonschema:"description=Model override"`

	// skill_create
	SkillName string `json:"skill_name,omitempty" jsonschema:"description=Skill name (lowercase alphanumeric + hyphens)"`
	SkillDesc string `json:"skill_description,omitempty" jsonschema:"description=Short skill description"`
	SkillBody string `json:"skill_body,omitempty" jsonschema:"description=Skill instructions (SKILL.md content after frontmatter)"`

	// cron_add / cron_remove
	CronID       string `json:"cron_id,omitempty" jsonschema:"description=Unique cron entry ID"`
	CronSchedule string `json:"cron_schedule,omitempty" jsonschema:"description=5-field cron expression"`
	CronAgent    string `json:"cron_agent,omitempty" jsonschema:"description=Agent ID to run"`
	CronPrompt   string `json:"cron_prompt,omitempty" jsonschema:"description=Prompt for the cron job"`

	// flow_create
	FlowID    string `json:"flow_id,omitempty" jsonschema:"description=Flow ID (lowercase alphanumeric + hyphens)"`
	FlowName  string `json:"flow_name,omitempty" jsonschema:"description=Human-readable flow name"`
	FlowDesc  string `json:"flow_description,omitempty" jsonschema:"description=Short flow description"`
	FlowSteps []struct {
		Name   string `json:"name"`
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
	} `json:"flow_steps,omitempty" jsonschema:"description=Ordered list of flow steps (name, agent, prompt template)"`

	// flow_run
	FlowInput string `json:"flow_input,omitempty" jsonschema:"description=Input text for the flow run"`

	// flow_status
	FlowRunID string `json:"flow_run_id,omitempty" jsonschema:"description=Flow run ID to check status"`

	// heartbeat_add / heartbeat_remove / heartbeat_trigger
	HeartbeatName     string `json:"heartbeat_name,omitempty" jsonschema:"description=Heartbeat task name"`
	HeartbeatInterval string `json:"heartbeat_interval,omitempty" jsonschema:"description=Interval (e.g. 1m, 5m, 30m, 1h)"`
	HeartbeatAgent    string `json:"heartbeat_agent,omitempty" jsonschema:"description=Agent to run (default: orchestrator)"`
	HeartbeatPrompt   string `json:"heartbeat_prompt,omitempty" jsonschema:"description=Task prompt"`

	// job_add / job_remove / job_run / job_runs (unified scheduling)
	JobID              string `json:"job_id,omitempty" jsonschema:"description=Job ID for remove/run/runs operations"`
	JobName            string `json:"job_name,omitempty" jsonschema:"description=Human-readable job name"`
	JobScheduleKind    string `json:"job_schedule_kind,omitempty" jsonschema:"description=Schedule kind: at (one-shot timestamp/duration), every (fixed interval), cron (5-field expression)"`
	JobScheduleValue   string `json:"job_schedule_value,omitempty" jsonschema:"description=Schedule value: ISO-8601 timestamp or Go duration for at (e.g. 2h, 30m, 2026-04-10T14:00:00Z), Go duration for every (e.g. 5m, 1h), 5-field cron expression for cron"`
	JobAgent           string `json:"job_agent,omitempty" jsonschema:"description=Agent ID to run the job"`
	JobPrompt          string `json:"job_prompt,omitempty" jsonschema:"description=Prompt for the scheduled job"`
	JobSessionTarget   string `json:"job_session_target,omitempty" jsonschema:"description=Session target: isolated (default) or main"`
	JobDeleteAfterRun  *bool  `json:"job_delete_after_run,omitempty" jsonschema:"description=Auto-delete job after successful run (default true for at kind)"`
	JobContextMessages string `json:"job_context_messages,omitempty" jsonschema:"description=Context from current conversation to include in scheduled job prompt"`

	// notification_add
	NotifyTitle    string `json:"notify_title,omitempty" jsonschema:"description=Notification title"`
	NotifyContent  string `json:"notify_content,omitempty" jsonschema:"description=Notification content/body"`
	NotifySeverity string `json:"notify_severity,omitempty" jsonschema:"description=Severity: info (default), warning, error, debug"`

	// dreaming_trigger
	DreamingPhase string `json:"dreaming_phase,omitempty" jsonschema:"description=Dreaming phase to trigger: light, deep, or rem"`
}

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ManageTool creates the eclaire_manage tool for runtime self-modification.
func ManageTool(deps ManageDeps) Tool {
	return NewTool("eclaire_manage",
		"Create and configure eclaire definitions (NOT for running agents — use the 'agent' tool to run/delegate). "+
			"Operations: agent_create, agent_list, skill_create, "+
			"job_add (schedule work: kind=at for one-shot, every for interval, cron for expression), job_remove, job_list, job_runs (execution history), job_run (trigger now), "+
			"notification_add (send a notification to the user — use when they ask to be notified), "+
			"cron_add, cron_remove, cron_list, flow_create, flow_list, flow_run, flow_status, "+
			"heartbeat_add, heartbeat_remove, heartbeat_list, heartbeat_trigger, "+
			"dreaming_enable, dreaming_disable, dreaming_status, dreaming_trigger (memory consolidation), "+
			"reload.",
		TrustModify, "manage",
		func(ctx context.Context, input manageInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch input.Operation {
			case "agent_create":
				return handleAgentCreate(deps, input)
			case "agent_list":
				return handleAgentList(deps)
			case "skill_create":
				return handleSkillCreate(deps, input)
			case "job_add":
				return handleJobAdd(deps, input)
			case "job_remove":
				return handleJobRemove(deps, input)
			case "job_list":
				return handleJobList(deps)
			case "job_runs":
				return handleJobRuns(deps, input)
			case "job_run":
				return handleJobRun(ctx, deps, input)
			case "cron_add":
				return handleCronAdd(deps, input)
			case "cron_remove":
				return handleCronRemove(deps, input)
			case "cron_list":
				return handleCronList(deps)
			case "flow_create":
				return handleFlowCreate(deps, input)
			case "flow_list":
				return handleFlowList(deps)
			case "flow_run":
				return handleFlowRun(deps, input)
			case "flow_status":
				return handleFlowStatus(deps, input)
			case "notification_add":
				return handleNotificationAdd(deps, input)
			case "heartbeat_add":
				return handleHeartbeatAdd(deps, input)
			case "heartbeat_remove":
				return handleHeartbeatRemove(deps, input)
			case "heartbeat_list":
				return handleHeartbeatList(deps)
			case "heartbeat_trigger":
				return handleHeartbeatTrigger(ctx, deps, input)
			case "dreaming_enable":
				return handleDreamingEnable(deps)
			case "dreaming_disable":
				return handleDreamingDisable(deps)
			case "dreaming_status":
				return handleDreamingStatus(deps)
			case "dreaming_trigger":
				return handleDreamingTrigger(ctx, deps, input)
			case "reload":
				return handleReload(deps)
			default:
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("unknown operation %q; valid: agent_create, agent_list, skill_create, job_add, job_remove, job_list, job_runs, job_run, notification_add, cron_add, cron_remove, cron_list, flow_create, flow_list, flow_run, flow_status, heartbeat_add, heartbeat_remove, heartbeat_list, heartbeat_trigger, dreaming_enable, dreaming_disable, dreaming_status, dreaming_trigger, reload", input.Operation),
				), nil
			}
		},
	)
}

func handleAgentCreate(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.AgentID == "" {
		return fantasy.NewTextErrorResponse("agent_id is required"), nil
	}
	if !validID.MatchString(input.AgentID) {
		return fantasy.NewTextErrorResponse("agent_id must be lowercase alphanumeric + hyphens"), nil
	}
	if input.AgentRole == "" {
		input.AgentRole = "simple"
	}

	agentDir := filepath.Join(deps.AgentsDir, input.AgentID)
	os.MkdirAll(agentDir, 0o755)

	// Write agent.yaml
	def := struct {
		ID          string   `yaml:"id"`
		Name        string   `yaml:"name"`
		Description string   `yaml:"description,omitempty"`
		Role        string   `yaml:"role"`
		Tools       []string `yaml:"tools,omitempty"`
		Model       string   `yaml:"model,omitempty"`
	}{
		ID:          input.AgentID,
		Name:        input.AgentName,
		Description: input.AgentDesc,
		Role:        input.AgentRole,
		Tools:       input.AgentTools,
		Model:       input.AgentModel,
	}
	if def.Name == "" {
		def.Name = input.AgentID
	}

	data, _ := yaml.Marshal(def)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), data, 0o644); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("write agent.yaml: %v", err)), nil
	}

	// Write SOUL.md if provided
	if input.AgentSoul != "" {
		wsDir := filepath.Join(agentDir, "workspace")
		os.MkdirAll(wsDir, 0o755)
		if err := os.WriteFile(filepath.Join(wsDir, "SOUL.md"), []byte(input.AgentSoul), 0o644); err != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("write SOUL.md: %v", err)), nil
		}
	}

	// Reload
	result := deps.Reload()
	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Created agent %q at %s. Reload: %d agents loaded, %d replaced.",
			input.AgentID, agentDir, result.AgentsLoaded, result.AgentsReplaced),
	}, nil
}

func handleAgentList(deps ManageDeps) (fantasy.ToolResponse, error) {
	agents := deps.AgentList()
	var sb strings.Builder
	for _, a := range agents {
		builtin := ""
		if a.BuiltIn {
			builtin = " [builtin]"
		}
		sb.WriteString(fmt.Sprintf("- %s (%s, %s)%s: %s\n", a.ID, a.Name, a.Role, builtin, a.Description))
	}
	if sb.Len() == 0 {
		return fantasy.ToolResponse{Content: "No agents registered."}, nil
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleSkillCreate(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.SkillName == "" {
		return fantasy.NewTextErrorResponse("skill_name is required"), nil
	}
	if !validID.MatchString(input.SkillName) {
		return fantasy.NewTextErrorResponse("skill_name must be lowercase alphanumeric + hyphens"), nil
	}
	if input.SkillDesc == "" {
		return fantasy.NewTextErrorResponse("skill_description is required"), nil
	}

	skillDir := filepath.Join(deps.SkillsDir, input.SkillName)
	os.MkdirAll(skillDir, 0o755)

	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", input.SkillName, input.SkillDesc, input.SkillBody)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("write SKILL.md: %v", err)), nil
	}

	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Created skill %q at %s/SKILL.md", input.SkillName, skillDir),
	}, nil
}

func handleCronAdd(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.CronID == "" || input.CronSchedule == "" || input.CronAgent == "" || input.CronPrompt == "" {
		return fantasy.NewTextErrorResponse("cron_id, cron_schedule, cron_agent, and cron_prompt are all required"), nil
	}
	if len(strings.Fields(input.CronSchedule)) != 5 {
		return fantasy.NewTextErrorResponse("cron_schedule must be a 5-field cron expression (minute hour day month weekday)"), nil
	}

	entries := readCronFile(deps.CronPath)

	// Upsert by ID
	found := false
	for i, e := range entries {
		if e.ID == input.CronID {
			entries[i] = CronEntry{
				ID:      input.CronID,
				Schedule: input.CronSchedule,
				AgentID: input.CronAgent,
				Prompt:  input.CronPrompt,
				Enabled: true,
			}
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, CronEntry{
			ID:       input.CronID,
			Schedule: input.CronSchedule,
			AgentID:  input.CronAgent,
			Prompt:   input.CronPrompt,
			Enabled:  true,
		})
	}

	if err := writeCronFile(deps.CronPath, entries); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("write cron.yaml: %v", err)), nil
	}

	result := deps.Reload()
	action := "Added"
	if found {
		action = "Updated"
	}
	return fantasy.ToolResponse{
		Content: fmt.Sprintf("%s cron entry %q. Schedule: %s. %d total entries.", action, input.CronID, input.CronSchedule, result.CronEntries),
	}, nil
}

func handleCronRemove(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.CronID == "" {
		return fantasy.NewTextErrorResponse("cron_id is required"), nil
	}

	entries := readCronFile(deps.CronPath)

	var kept []CronEntry
	found := false
	for _, e := range entries {
		if e.ID == input.CronID {
			found = true
			continue
		}
		kept = append(kept, e)
	}

	if !found {
		return fantasy.ToolResponse{Content: fmt.Sprintf("Cron entry %q not found.", input.CronID)}, nil
	}

	if err := writeCronFile(deps.CronPath, kept); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("write cron.yaml: %v", err)), nil
	}

	result := deps.Reload()
	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Removed cron entry %q. %d entries remaining.", input.CronID, result.CronEntries),
	}, nil
}

func handleCronList(deps ManageDeps) (fantasy.ToolResponse, error) {
	entries := deps.CronList()
	if len(entries) == 0 {
		return fantasy.ToolResponse{Content: "No cron entries."}, nil
	}
	var sb strings.Builder
	for _, e := range entries {
		enabled := "enabled"
		if !e.Enabled {
			enabled = "disabled"
		}
		sb.WriteString(fmt.Sprintf("- %s [%s] (%s) agent=%s: %s\n", e.ID, e.Schedule, enabled, e.AgentID, e.Prompt))
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleNotificationAdd(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.NotifyTitle == "" {
		return fantasy.NewTextErrorResponse("notify_title is required"), nil
	}
	if deps.NotifyAdd == nil {
		return fantasy.NewTextErrorResponse("notification system not available"), nil
	}
	sev := input.NotifySeverity
	if sev == "" {
		sev = "info"
	}
	if err := deps.NotifyAdd(input.NotifyTitle, input.NotifyContent, sev); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to create notification: %v", err)), nil
	}
	return fantasy.ToolResponse{Content: fmt.Sprintf("Notification created: %s", input.NotifyTitle)}, nil
}

func handleReload(deps ManageDeps) (fantasy.ToolResponse, error) {
	result := deps.Reload()
	data, _ := json.MarshalIndent(result, "", "  ")
	return fantasy.ToolResponse{Content: "Reload complete:\n" + string(data)}, nil
}

func handleFlowCreate(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.FlowID == "" {
		return fantasy.NewTextErrorResponse("flow_id is required"), nil
	}
	if !validID.MatchString(input.FlowID) {
		return fantasy.NewTextErrorResponse("flow_id must be lowercase alphanumeric + hyphens"), nil
	}
	if len(input.FlowSteps) == 0 {
		return fantasy.NewTextErrorResponse("flow_steps is required (at least one step)"), nil
	}

	for i, step := range input.FlowSteps {
		if step.Name == "" || step.Agent == "" || step.Prompt == "" {
			return fantasy.NewTextErrorResponse(
				fmt.Sprintf("flow_steps[%d]: name, agent, and prompt are all required", i),
			), nil
		}
	}

	// Build the YAML structure
	flow := FlowInfo{
		ID:          input.FlowID,
		Name:        input.FlowName,
		Description: input.FlowDesc,
	}
	if flow.Name == "" {
		flow.Name = input.FlowID
	}
	for _, step := range input.FlowSteps {
		flow.Steps = append(flow.Steps, FlowStep{
			Name:   step.Name,
			Agent:  step.Agent,
			Prompt: step.Prompt,
		})
	}

	os.MkdirAll(deps.FlowsDir, 0o755)
	data, _ := yaml.Marshal(flow)
	path := filepath.Join(deps.FlowsDir, input.FlowID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("write flow: %v", err)), nil
	}

	return fantasy.ToolResponse{
		Content: fmt.Sprintf("Created flow %q at %s (%d steps).", input.FlowID, path, len(flow.Steps)),
	}, nil
}

func handleFlowList(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.FlowList == nil {
		return fantasy.ToolResponse{Content: "No flows available."}, nil
	}
	flows := deps.FlowList()
	if len(flows) == 0 {
		return fantasy.ToolResponse{Content: "No flows defined."}, nil
	}
	var sb strings.Builder
	for _, f := range flows {
		sb.WriteString(fmt.Sprintf("- %s (%s, %d steps): %s\n", f.ID, f.Name, len(f.Steps), f.Description))
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleFlowRun(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.FlowID == "" {
		return fantasy.NewTextErrorResponse("flow_id is required"), nil
	}
	if deps.FlowRun == nil {
		return fantasy.NewTextErrorResponse("flow execution not available"), nil
	}

	info, err := deps.FlowRun(input.FlowID, input.FlowInput)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("flow run failed: %v", err)), nil
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return fantasy.ToolResponse{Content: "Flow started:\n" + string(data)}, nil
}

func handleFlowStatus(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.FlowRunID == "" {
		return fantasy.NewTextErrorResponse("flow_run_id is required"), nil
	}
	if deps.FlowGet == nil {
		return fantasy.NewTextErrorResponse("flow status not available"), nil
	}

	info, ok := deps.FlowGet(input.FlowRunID)
	if !ok {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("flow run %q not found", input.FlowRunID)), nil
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return fantasy.ToolResponse{Content: string(data)}, nil
}

func readCronFile(path string) []CronEntry {
	return ReadCronFilePublic(path)
}

func writeCronFile(path string, entries []CronEntry) error {
	return WriteCronFilePublic(path, entries)
}

// ReadCronFilePublic reads and parses the cron.yaml file.
func ReadCronFilePublic(path string) []CronEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg CronConfig
	yaml.Unmarshal(data, &cfg)
	return cfg.Entries
}

// WriteCronFilePublic writes cron entries to the cron.yaml file.
func WriteCronFilePublic(path string, entries []CronEntry) error {
	cfg := CronConfig{Entries: entries}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// --- Heartbeat handlers ---

func handleHeartbeatAdd(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.HeartbeatName == "" {
		return fantasy.NewTextErrorResponse("heartbeat_name is required"), nil
	}
	if input.HeartbeatInterval == "" {
		return fantasy.NewTextErrorResponse("heartbeat_interval is required (e.g. 1m, 5m, 30m)"), nil
	}
	if input.HeartbeatPrompt == "" {
		return fantasy.NewTextErrorResponse("heartbeat_prompt is required"), nil
	}

	agent := input.HeartbeatAgent
	if agent == "" {
		agent = "orchestrator"
	}

	// Read or create HEARTBEAT.md
	heartbeatPath := filepath.Join(deps.WorkspaceDir, "HEARTBEAT.md")
	existing, _ := os.ReadFile(heartbeatPath)

	// Parse existing tasks
	var tasks []struct {
		Name     string `yaml:"name"`
		Interval string `yaml:"interval"`
		Agent    string `yaml:"agent"`
		Prompt   string `yaml:"prompt"`
	}

	content := string(existing)
	if strings.Contains(content, "tasks:") {
		// Extract existing YAML
		yamlStart := strings.Index(content, "tasks:")
		yamlContent := content[yamlStart:]
		var parsed struct {
			Tasks []struct {
				Name     string `yaml:"name"`
				Interval string `yaml:"interval"`
				Agent    string `yaml:"agent"`
				Prompt   string `yaml:"prompt"`
			} `yaml:"tasks"`
		}
		yaml.Unmarshal([]byte(yamlContent), &parsed)
		tasks = parsed.Tasks
	}

	// Check for duplicate
	for _, t := range tasks {
		if t.Name == input.HeartbeatName {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("heartbeat task %q already exists", input.HeartbeatName)), nil
		}
	}

	// Add new task
	tasks = append(tasks, struct {
		Name     string `yaml:"name"`
		Interval string `yaml:"interval"`
		Agent    string `yaml:"agent"`
		Prompt   string `yaml:"prompt"`
	}{
		Name:     input.HeartbeatName,
		Interval: input.HeartbeatInterval,
		Agent:    agent,
		Prompt:   input.HeartbeatPrompt,
	})

	// Write HEARTBEAT.md
	var sb strings.Builder
	sb.WriteString("# Heartbeat Tasks\n\n")
	sb.WriteString("tasks:\n")
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("  - name: %s\n", t.Name))
		sb.WriteString(fmt.Sprintf("    interval: %s\n", t.Interval))
		sb.WriteString(fmt.Sprintf("    agent: %s\n", t.Agent))
		sb.WriteString(fmt.Sprintf("    prompt: %q\n", t.Prompt))
	}

	os.MkdirAll(filepath.Dir(heartbeatPath), 0o755)
	if err := os.WriteFile(heartbeatPath, []byte(sb.String()), 0o644); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to write HEARTBEAT.md: %v", err)), nil
	}

	return fantasy.NewTextResponse(fmt.Sprintf("Added heartbeat task %q (every %s, agent: %s)", input.HeartbeatName, input.HeartbeatInterval, agent)), nil
}

func handleHeartbeatRemove(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if input.HeartbeatName == "" {
		return fantasy.NewTextErrorResponse("heartbeat_name is required"), nil
	}

	heartbeatPath := filepath.Join(deps.WorkspaceDir, "HEARTBEAT.md")
	existing, err := os.ReadFile(heartbeatPath)
	if err != nil {
		return fantasy.NewTextErrorResponse("no HEARTBEAT.md found"), nil
	}

	content := string(existing)
	if !strings.Contains(content, "tasks:") {
		return fantasy.NewTextErrorResponse("no structured tasks found in HEARTBEAT.md"), nil
	}

	yamlContent := content[strings.Index(content, "tasks:"):]
	var parsed struct {
		Tasks []struct {
			Name     string `yaml:"name"`
			Interval string `yaml:"interval"`
			Agent    string `yaml:"agent"`
			Prompt   string `yaml:"prompt"`
		} `yaml:"tasks"`
	}
	yaml.Unmarshal([]byte(yamlContent), &parsed)

	found := false
	var remaining []struct {
		Name     string `yaml:"name"`
		Interval string `yaml:"interval"`
		Agent    string `yaml:"agent"`
		Prompt   string `yaml:"prompt"`
	}
	for _, t := range parsed.Tasks {
		if t.Name == input.HeartbeatName {
			found = true
			continue
		}
		remaining = append(remaining, t)
	}

	if !found {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("heartbeat task %q not found", input.HeartbeatName)), nil
	}

	var sb strings.Builder
	sb.WriteString("# Heartbeat Tasks\n\n")
	if len(remaining) > 0 {
		sb.WriteString("tasks:\n")
		for _, t := range remaining {
			sb.WriteString(fmt.Sprintf("  - name: %s\n", t.Name))
			sb.WriteString(fmt.Sprintf("    interval: %s\n", t.Interval))
			sb.WriteString(fmt.Sprintf("    agent: %s\n", t.Agent))
			sb.WriteString(fmt.Sprintf("    prompt: %q\n", t.Prompt))
		}
	}

	os.WriteFile(heartbeatPath, []byte(sb.String()), 0o644)
	return fantasy.NewTextResponse(fmt.Sprintf("Removed heartbeat task %q (%d remaining)", input.HeartbeatName, len(remaining))), nil
}

func handleHeartbeatList(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.HeartbeatList == nil {
		return fantasy.NewTextErrorResponse("heartbeat listing not available"), nil
	}
	tasks := deps.HeartbeatList()
	if len(tasks) == 0 {
		return fantasy.NewTextResponse("No heartbeat tasks configured."), nil
	}
	data, _ := json.MarshalIndent(tasks, "", "  ")
	return fantasy.NewTextResponse(string(data)), nil
}

func handleHeartbeatTrigger(ctx context.Context, deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.HeartbeatTrigger == nil {
		return fantasy.NewTextErrorResponse("heartbeat trigger not available"), nil
	}
	if input.HeartbeatName == "" {
		return fantasy.NewTextErrorResponse("heartbeat_name is required"), nil
	}
	if err := deps.HeartbeatTrigger(ctx, input.HeartbeatName); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("trigger failed: %v", err)), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Triggered heartbeat task %q", input.HeartbeatName)), nil
}

// --- Job handlers (unified scheduling) ---

func handleJobAdd(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.JobAdd == nil {
		return fantasy.NewTextErrorResponse("job scheduling not available"), nil
	}
	if input.JobScheduleKind == "" {
		return fantasy.NewTextErrorResponse("job_schedule_kind is required (at, every, or cron)"), nil
	}
	switch input.JobScheduleKind {
	case "at", "every", "cron":
	default:
		return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid job_schedule_kind %q; must be at, every, or cron", input.JobScheduleKind)), nil
	}
	if input.JobScheduleValue == "" {
		return fantasy.NewTextErrorResponse("job_schedule_value is required"), nil
	}
	if input.JobAgent == "" {
		return fantasy.NewTextErrorResponse("job_agent is required"), nil
	}
	if input.JobPrompt == "" {
		return fantasy.NewTextErrorResponse("job_prompt is required"), nil
	}

	name := input.JobName
	if name == "" {
		name = input.JobAgent + "-" + input.JobScheduleKind
	}

	info, err := deps.JobAdd(name, input.JobScheduleKind, input.JobScheduleValue,
		input.JobAgent, input.JobPrompt, input.JobSessionTarget,
		input.JobDeleteAfterRun, input.JobContextMessages)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("job_add failed: %v", err)), nil
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return fantasy.ToolResponse{Content: "Job scheduled:\n" + string(data)}, nil
}

func handleJobRemove(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.JobRemove == nil {
		return fantasy.NewTextErrorResponse("job scheduling not available"), nil
	}
	if input.JobID == "" {
		return fantasy.NewTextErrorResponse("job_id is required"), nil
	}
	if err := deps.JobRemove(input.JobID); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("job_remove failed: %v", err)), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Removed job %q", input.JobID)), nil
}

func handleJobList(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.JobList == nil {
		return fantasy.NewTextErrorResponse("job scheduling not available"), nil
	}
	jobs := deps.JobList()
	if len(jobs) == 0 {
		return fantasy.NewTextResponse("No scheduled jobs."), nil
	}
	var sb strings.Builder
	for _, j := range jobs {
		enabled := "enabled"
		if !j.Enabled {
			enabled = "disabled"
		}
		sb.WriteString(fmt.Sprintf("- %s [%s %s] (%s) agent=%s next=%s last=%s\n",
			j.ID, j.ScheduleKind, j.Schedule, enabled, j.AgentID, j.NextRunAt, j.LastStatus))
	}
	return fantasy.ToolResponse{Content: sb.String()}, nil
}

func handleJobRuns(deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.JobRuns == nil {
		return fantasy.NewTextErrorResponse("job scheduling not available"), nil
	}
	if input.JobID == "" {
		return fantasy.NewTextErrorResponse("job_id is required"), nil
	}
	entries := deps.JobRuns(input.JobID)
	if len(entries) == 0 {
		return fantasy.NewTextResponse(fmt.Sprintf("No execution history for job %q.", input.JobID)), nil
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	return fantasy.ToolResponse{Content: string(data)}, nil
}

func handleJobRun(ctx context.Context, deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.JobRun == nil {
		return fantasy.NewTextErrorResponse("job scheduling not available"), nil
	}
	if input.JobID == "" {
		return fantasy.NewTextErrorResponse("job_id is required"), nil
	}
	if err := deps.JobRun(ctx, input.JobID); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("job_run failed: %v", err)), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Triggered job %q", input.JobID)), nil
}

// Dreaming operations

func handleDreamingEnable(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.DreamingEnable == nil {
		return fantasy.NewTextErrorResponse("dreaming not available"), nil
	}
	if err := deps.DreamingEnable(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("dreaming_enable failed: %v", err)), nil
	}
	return fantasy.NewTextResponse("Dreaming enabled (light/deep/REM phases)"), nil
}

func handleDreamingDisable(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.DreamingDisable == nil {
		return fantasy.NewTextErrorResponse("dreaming not available"), nil
	}
	if err := deps.DreamingDisable(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("dreaming_disable failed: %v", err)), nil
	}
	return fantasy.NewTextResponse("Dreaming disabled"), nil
}

func handleDreamingStatus(deps ManageDeps) (fantasy.ToolResponse, error) {
	if deps.DreamingStatus == nil {
		return fantasy.NewTextErrorResponse("dreaming not available"), nil
	}
	status := deps.DreamingStatus()
	data, _ := json.MarshalIndent(status, "", "  ")
	return fantasy.NewTextResponse(string(data)), nil
}

func handleDreamingTrigger(ctx context.Context, deps ManageDeps, input manageInput) (fantasy.ToolResponse, error) {
	if deps.DreamingTrigger == nil {
		return fantasy.NewTextErrorResponse("dreaming not available"), nil
	}
	phase := input.DreamingPhase
	if phase == "" {
		return fantasy.NewTextErrorResponse("dreaming_phase is required (light, deep, or rem)"), nil
	}
	if err := deps.DreamingTrigger(ctx, phase); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("dreaming_trigger failed: %v", err)), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Triggered dreaming phase %q", phase)), nil
}
