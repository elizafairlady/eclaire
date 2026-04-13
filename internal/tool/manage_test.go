package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"gopkg.in/yaml.v3"
)

func newTestManageDeps(t *testing.T) (ManageDeps, string) {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	skillsDir := filepath.Join(dir, "skills")
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(agentsDir, 0o755)
	os.MkdirAll(skillsDir, 0o755)
	os.MkdirAll(flowsDir, 0o755)

	// In-memory job + flow run state for tests
	flowRuns := make(map[string]*FlowRunInfo)
	var jobs []JobInfo

	reloadCalled := false
	deps := ManageDeps{
		AgentsDir: agentsDir,
		SkillsDir: skillsDir,
		FlowsDir:  flowsDir,
		Reload: func() ReloadResult {
			reloadCalled = true
			_ = reloadCalled
			return ReloadResult{AgentsLoaded: 1, JobCount: len(jobs)}
		},
		CronList: func() []CronEntry {
			var out []CronEntry
			for _, j := range jobs {
				out = append(out, CronEntry{ID: j.ID, Schedule: j.Schedule, AgentID: j.AgentID, Prompt: j.Prompt, Enabled: j.Enabled})
			}
			return out
		},
		JobAdd: func(name, scheduleKind, scheduleValue, agentID, prompt, sessionTarget string, deleteAfterRun *bool, contextMessages string) (JobInfo, error) {
			info := JobInfo{
				ID:           name + "-id",
				Name:         name,
				ScheduleKind: scheduleKind,
				Schedule:     scheduleValue,
				AgentID:      agentID,
				Prompt:       prompt,
				Enabled:      true,
			}
			if deleteAfterRun != nil {
				info.DeleteAfterRun = *deleteAfterRun
			} else if scheduleKind == "at" {
				info.DeleteAfterRun = true
			}
			jobs = append(jobs, info)
			return info, nil
		},
		JobRemove: func(id string) error {
			for i, j := range jobs {
				if j.ID == id {
					jobs = append(jobs[:i], jobs[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("job %q not found", id)
		},
		JobList: func() []JobInfo { return jobs },
		JobRun:  func(ctx context.Context, id string) error { return nil },
		JobRuns: func(id string) []JobRunLogEntry { return nil },
		AgentList: func() []AgentInfo {
			return []AgentInfo{
				{ID: "orchestrator", Name: "Claire", Role: "simple", BuiltIn: true},
			}
		},
		FlowList: func() []FlowInfo {
			entries, _ := os.ReadDir(flowsDir)
			var out []FlowInfo
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				data, _ := os.ReadFile(filepath.Join(flowsDir, e.Name()))
				var f FlowInfo
				yaml.Unmarshal(data, &f)
				if f.ID != "" {
					out = append(out, f)
				}
			}
			return out
		},
		FlowRun: func(flowID, input string) (*FlowRunInfo, error) {
			info := &FlowRunInfo{
				ID:         "flow_" + flowID + "_test",
				FlowID:     flowID,
				Status:     "running",
				TotalSteps: 2,
			}
			flowRuns[info.ID] = info
			return info, nil
		},
		FlowGet: func(runID string) (*FlowRunInfo, bool) {
			info, ok := flowRuns[runID]
			return info, ok
		},
	}
	return deps, dir
}

func callManage(t *testing.T, deps ManageDeps, input string) fantasy.ToolResponse {
	t.Helper()
	tool := ManageTool(deps)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("ManageTool error: %v", err)
	}
	return resp
}

func TestManageAgentCreate(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"agent_create","agent_id":"qa-tester","agent_name":"QA Tester","agent_role":"complex","agent_tools":["shell","read"],"agent_soul":"You are a QA tester."}`)

	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Created agent") {
		t.Errorf("unexpected response: %s", resp.Content)
	}

	// Verify files on disk
	agentYaml := filepath.Join(deps.AgentsDir, "qa-tester", "agent.yaml")
	if _, err := os.Stat(agentYaml); err != nil {
		t.Fatalf("agent.yaml not created: %v", err)
	}
	soulMd := filepath.Join(deps.AgentsDir, "qa-tester", "workspace", "SOUL.md")
	if _, err := os.Stat(soulMd); err != nil {
		t.Fatalf("SOUL.md not created: %v", err)
	}
	data, _ := os.ReadFile(soulMd)
	if string(data) != "You are a QA tester." {
		t.Errorf("SOUL.md content = %q", string(data))
	}
}

func TestManageAgentCreateInvalidID(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"agent_create","agent_id":"Bad Agent!","agent_role":"simple"}`)
	if !resp.IsError {
		t.Error("expected error for invalid agent ID")
	}
}

func TestManageAgentList(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"agent_list"}`)
	if !strings.Contains(resp.Content, "orchestrator") {
		t.Errorf("should list orchestrator: %s", resp.Content)
	}
}

func TestManageSkillCreate(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"skill_create","skill_name":"commit","skill_description":"Create git commits","skill_body":"Instructions for committing"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}

	skillPath := filepath.Join(deps.SkillsDir, "commit", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("SKILL.md not created: %v", err)
	}
	if !strings.Contains(string(data), "name: commit") {
		t.Errorf("SKILL.md missing frontmatter: %s", string(data))
	}
	if !strings.Contains(string(data), "Instructions for committing") {
		t.Errorf("SKILL.md missing body: %s", string(data))
	}
}

func TestManageCronAdd(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"cron_add","cron_id":"disk-check","cron_schedule":"0 * * * *","cron_agent":"sysadmin","cron_prompt":"Check disk usage"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Added") {
		t.Errorf("unexpected response: %s", resp.Content)
	}

	// Verify via CronList (reads from job store)
	entries := deps.CronList()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].AgentID != "sysadmin" {
		t.Errorf("agent = %q, want sysadmin", entries[0].AgentID)
	}
}

func TestManageCronRemove(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	// Add first
	resp := callManage(t, deps, `{"operation":"cron_add","cron_id":"to-remove","cron_schedule":"0 9 * * *","cron_agent":"orchestrator","cron_prompt":"Morning check"}`)
	if resp.IsError {
		t.Fatalf("cron_add error: %s", resp.Content)
	}

	// Extract the job ID from the response to remove it
	entries := deps.CronList()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after add, got %d", len(entries))
	}

	// Remove by the cron ID (handleCronRemove tries "cron-<id>" prefix then bare)
	resp = callManage(t, deps, fmt.Sprintf(`{"operation":"cron_remove","cron_id":"%s"}`, entries[0].ID))
	if !strings.Contains(resp.Content, "Removed") {
		t.Errorf("unexpected response: %s", resp.Content)
	}

	// Verify empty
	if remaining := deps.CronList(); len(remaining) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(remaining))
	}
}

func TestManageCronList(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"cron_list"}`)
	if !strings.Contains(resp.Content, "No cron") {
		t.Errorf("expected empty list: %s", resp.Content)
	}
}

func TestManageReload(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"reload"}`)
	if !strings.Contains(resp.Content, "Reload complete") {
		t.Errorf("unexpected response: %s", resp.Content)
	}
}

func TestManageUnknownOperation(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"unknown"}`)
	if !resp.IsError {
		t.Error("expected error for unknown operation")
	}
}

func TestManageFlowCreate(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{
		"operation": "flow_create",
		"flow_id": "research-pipeline",
		"flow_name": "Research Pipeline",
		"flow_description": "Multi-step research flow",
		"flow_steps": [
			{"name": "search", "agent": "research", "prompt": "Search for {{.Input}}"},
			{"name": "summarize", "agent": "orchestrator", "prompt": "Summarize: {{.PrevOutput}}"}
		]
	}`)

	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Created flow") {
		t.Errorf("unexpected response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "2 steps") {
		t.Errorf("expected 2 steps in response: %s", resp.Content)
	}

	// Verify file on disk
	flowPath := filepath.Join(deps.FlowsDir, "research-pipeline.yaml")
	data, err := os.ReadFile(flowPath)
	if err != nil {
		t.Fatalf("flow YAML not created: %v", err)
	}

	var flow FlowInfo
	if err := yaml.Unmarshal(data, &flow); err != nil {
		t.Fatalf("parse flow YAML: %v", err)
	}
	if flow.ID != "research-pipeline" {
		t.Errorf("flow ID = %q", flow.ID)
	}
	if flow.Name != "Research Pipeline" {
		t.Errorf("flow Name = %q", flow.Name)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(flow.Steps))
	}
	if flow.Steps[0].Agent != "research" {
		t.Errorf("step 0 agent = %q", flow.Steps[0].Agent)
	}
}

func TestManageFlowCreateInvalidID(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"flow_create","flow_id":"Bad Flow!","flow_steps":[{"name":"x","agent":"y","prompt":"z"}]}`)
	if !resp.IsError {
		t.Error("expected error for invalid flow ID")
	}
}

func TestManageFlowCreateNoSteps(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"flow_create","flow_id":"empty","flow_steps":[]}`)
	if !resp.IsError {
		t.Error("expected error for empty steps")
	}
}

func TestManageFlowList(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	// Empty list first
	resp := callManage(t, deps, `{"operation":"flow_list"}`)
	if strings.Contains(resp.Content, "research") {
		t.Error("should be empty initially")
	}

	// Create a flow
	callManage(t, deps, `{
		"operation": "flow_create",
		"flow_id": "test-flow",
		"flow_name": "Test Flow",
		"flow_steps": [{"name": "s1", "agent": "orchestrator", "prompt": "do it"}]
	}`)

	// Now list should show it
	resp = callManage(t, deps, `{"operation":"flow_list"}`)
	if !strings.Contains(resp.Content, "test-flow") {
		t.Errorf("should contain test-flow: %s", resp.Content)
	}
}

func TestManageFlowRun(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"flow_run","flow_id":"my-pipeline","flow_input":"hello world"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Flow started") {
		t.Errorf("unexpected response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "my-pipeline") {
		t.Errorf("should reference flow ID: %s", resp.Content)
	}
}

func TestManageFlowRunMissingID(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"flow_run"}`)
	if !resp.IsError {
		t.Error("expected error for missing flow_id")
	}
}

func TestManageFlowStatus(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	// Run a flow first to populate the run state
	callManage(t, deps, `{"operation":"flow_run","flow_id":"check-flow"}`)

	// Check status
	resp := callManage(t, deps, `{"operation":"flow_status","flow_run_id":"flow_check-flow_test"}`)
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "running") {
		t.Errorf("should show running status: %s", resp.Content)
	}
}

func TestManageFlowStatusNotFound(t *testing.T) {
	deps, _ := newTestManageDeps(t)

	resp := callManage(t, deps, `{"operation":"flow_status","flow_run_id":"nonexistent"}`)
	if !resp.IsError {
		t.Error("expected error for nonexistent run")
	}
}

// --- Job operation tests ---

func jobTestDeps(t *testing.T) ManageDeps {
	t.Helper()
	deps, _ := newTestManageDeps(t)

	var jobs []JobInfo
	deps.JobAdd = func(name, scheduleKind, scheduleValue, agentID, prompt, sessionTarget string, deleteAfterRun *bool, contextMessages string) (JobInfo, error) {
		info := JobInfo{
			ID:           name + "-id",
			Name:         name,
			ScheduleKind: scheduleKind,
			Schedule:     scheduleValue,
			AgentID:      agentID,
			Prompt:       prompt,
			Enabled:      true,
		}
		if deleteAfterRun != nil {
			info.DeleteAfterRun = *deleteAfterRun
		} else if scheduleKind == "at" {
			info.DeleteAfterRun = true
		}
		jobs = append(jobs, info)
		return info, nil
	}
	deps.JobRemove = func(id string) error {
		for i, j := range jobs {
			if j.ID == id {
				jobs = append(jobs[:i], jobs[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("job %q not found", id)
	}
	deps.JobList = func() []JobInfo { return jobs }
	deps.JobRun = func(ctx context.Context, id string) error { return nil }
	deps.JobRuns = func(id string) []JobRunLogEntry { return nil }

	return deps
}

func TestManageJobAdd_At(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "at",
		"job_schedule_value": "2h",
		"job_agent": "research",
		"job_prompt": "update the report"
	}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "research") {
		t.Fatalf("expected response to mention agent, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "at") {
		t.Fatalf("expected response to mention schedule kind, got: %s", resp.Content)
	}
}

func TestManageJobAdd_Every(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "every",
		"job_schedule_value": "30m",
		"job_agent": "orchestrator",
		"job_prompt": "check inbox"
	}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
}

func TestManageJobAdd_Cron(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "cron",
		"job_schedule_value": "0 7 * * *",
		"job_agent": "research",
		"job_prompt": "morning briefing"
	}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
}

func TestManageJobAdd_InvalidKind(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "bogus",
		"job_schedule_value": "1h",
		"job_agent": "research",
		"job_prompt": "test"
	}`)
	if !resp.IsError {
		t.Fatal("expected error for invalid kind")
	}
}

func TestManageJobAdd_MissingFields(t *testing.T) {
	deps := jobTestDeps(t)

	// Missing kind
	resp := callManage(t, deps, `{"operation": "job_add", "job_agent": "research", "job_prompt": "test"}`)
	if !resp.IsError {
		t.Fatal("expected error for missing kind")
	}

	// Missing agent
	resp = callManage(t, deps, `{"operation": "job_add", "job_schedule_kind": "at", "job_schedule_value": "1h", "job_prompt": "test"}`)
	if !resp.IsError {
		t.Fatal("expected error for missing agent")
	}

	// Missing prompt
	resp = callManage(t, deps, `{"operation": "job_add", "job_schedule_kind": "at", "job_schedule_value": "1h", "job_agent": "research"}`)
	if !resp.IsError {
		t.Fatal("expected error for missing prompt")
	}
}

func TestManageJobRemove(t *testing.T) {
	deps := jobTestDeps(t)

	// Add a job first
	callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "at",
		"job_schedule_value": "1h",
		"job_agent": "research",
		"job_prompt": "test"
	}`)

	resp := callManage(t, deps, `{"operation": "job_remove", "job_id": "research-at-id"}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}

	// Verify list is empty
	listResp := callManage(t, deps, `{"operation": "job_list"}`)
	if strings.Contains(listResp.Content, "research") {
		t.Fatal("expected empty list after remove")
	}
}

func TestManageJobList(t *testing.T) {
	deps := jobTestDeps(t)

	callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "every",
		"job_schedule_value": "5m",
		"job_agent": "sysadmin",
		"job_prompt": "health check"
	}`)

	resp := callManage(t, deps, `{"operation": "job_list"}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "sysadmin") {
		t.Fatalf("expected list to contain agent, got: %s", resp.Content)
	}
}

func TestManageJobRun(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{"operation": "job_run", "job_id": "test-id"}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
}

func TestManageJobRuns_Empty(t *testing.T) {
	deps := jobTestDeps(t)

	resp := callManage(t, deps, `{"operation": "job_runs", "job_id": "test-id"}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "No execution history") {
		t.Fatalf("expected empty history message, got: %s", resp.Content)
	}
}

func TestManageJobAdd_NotAvailable(t *testing.T) {
	deps, _ := newTestManageDeps(t)
	deps.JobAdd = nil // explicitly nil to test unavailable path

	resp := callManage(t, deps, `{
		"operation": "job_add",
		"job_schedule_kind": "at",
		"job_schedule_value": "1h",
		"job_agent": "research",
		"job_prompt": "test"
	}`)
	if !resp.IsError {
		t.Fatal("expected error when job scheduling not available")
	}
}
