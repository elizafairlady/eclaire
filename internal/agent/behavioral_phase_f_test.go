package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/testutil"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// =============================================================================
// Behavioral test: morning briefing generation
// =============================================================================

func TestBehavior_MorningBriefing(t *testing.T) {
	dir := t.TempDir()

	// Set up reminder store with test data
	reminderStore := tool.NewReminderStore(filepath.Join(dir, "reminders.json"))
	reminderStore.Save([]tool.Reminder{
		{ID: "r1", Text: "Review PRs", DueAt: time.Now().Add(-1 * time.Hour), CreatedAt: time.Now()},
		{ID: "r2", Text: "Team standup", DueAt: time.Now().Add(2 * time.Hour), CreatedAt: time.Now()},
	})

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Model calls the briefing tool
			{ToolCalls: []testutil.MockToolCall{
				{Name: "eclaire_briefing", ID: "tc-1", Input: map[string]any{
					"operation": "generate",
				}},
			}},
			// Model calls reminder list
			{ToolCalls: []testutil.MockToolCall{
				{Name: "eclaire_reminder", ID: "tc-2", Input: map[string]any{
					"operation": "list",
				}},
			}},
			// Model responds with briefing summary
			{Text: "Good morning! You have 1 overdue reminder (Review PRs) and 1 upcoming today (Team standup)."},
		},
	})

	// Register reminder and briefing tools
	env.Tools.Register(tool.ReminderTool(reminderStore))
	env.Tools.Register(tool.BriefingTool(tool.BriefingDeps{
		Reminders:    reminderStore,
		WorkspaceDir: dir + "/workspace",
		BriefingsDir: dir + "/workspace/briefings",
	}))

	result, events := env.RunAgent(t, "orchestrator", "Good morning. Give me my briefing.")

	// Should have called the briefing tool
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_briefing"
	}) {
		t.Error("should have called eclaire_briefing tool")
	}

	// Should have called the reminder tool
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_reminder"
	}) {
		t.Error("should have called eclaire_reminder tool")
	}

	// Tool results should have been produced
	toolResultCount := countEvents(events, agent.EventToolResult)
	if toolResultCount < 1 {
		t.Error("should have at least 1 tool result")
	}

	if result.Content == "" {
		t.Error("result should have content")
	}
}

// =============================================================================
// Behavioral test: scope switching
// =============================================================================

// Scope switching is implemented at the TUI layer (/scope command injects a system
// message). At the agent level, we verify that different prompts with scope context
// produce distinct runs and the model receives both.
func TestBehavior_ScopeSwitch(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "Work mode: reviewing your PRs and tickets."},
			{Text: "Personal mode: dog training schedule and errands."},
		},
	})

	// Run 1: work scope context in prompt
	result1, _ := env.RunAgent(t, "orchestrator",
		"[Scope: work] What should I focus on today?")

	if result1.Content == "" {
		t.Error("work scope run should produce content")
	}
	if !strings.Contains(result1.Content, "Work mode") {
		t.Errorf("unexpected work result: %s", result1.Content)
	}

	// Run 2: personal scope context in prompt
	result2, _ := env.RunAgent(t, "orchestrator",
		"[Scope: personal] What should I focus on today?")

	if result2.Content == "" {
		t.Error("personal scope run should produce content")
	}
	if !strings.Contains(result2.Content, "Personal mode") {
		t.Errorf("unexpected personal result: %s", result2.Content)
	}

	// Model should have received 2 calls
	if len(env.Model.Calls) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(env.Model.Calls))
	}
}

// =============================================================================
// Behavioral test: pipeline composition (flow execution)
// =============================================================================

func TestBehavior_PipelineComposition(t *testing.T) {
	dir := t.TempDir()

	// Create a 2-step flow
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0o755)
	os.WriteFile(filepath.Join(flowsDir, "research-and-summarize.yaml"), []byte(`
id: research-and-summarize
name: Research and Summarize
steps:
  - name: research
    agent: orchestrator
    prompt: "Research this topic: {{.Input}}"
  - name: summarize
    agent: orchestrator
    prompt: "Summarize the following research:\n{{.PrevOutput}}"
`), 0o644)

	// Mock model: step1 returns research, step2 returns summary
	model := &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "Research findings: Go 1.26 has range-over-func and improved generics."},
			{Text: "Summary: Go 1.26 brings range-over-func and better generics."},
		},
	}

	env := testutil.NewTestEnv(dir, model)
	msgBus := env.Bus

	// Load flow definition
	def, err := agent.LoadFlowFile(filepath.Join(flowsDir, "research-and-summarize.yaml"))
	if err != nil {
		t.Fatalf("load flow: %v", err)
	}

	taskRegistry := agent.NewTaskRegistry()
	executor := &agent.FlowExecutor{
		Runner:   env.Runner,
		Tasks:    taskRegistry,
		Registry: env.Registry,
		Bus:      msgBus,
		Logger:   env.Logger,
	}

	var flowEvents []agent.StreamEvent
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run, err := executor.Run(ctx, *def, "Go 1.26 features", func(ev agent.StreamEvent) error {
		flowEvents = append(flowEvents, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("flow run: %v", err)
	}

	// Verify flow completed
	if run.Status != agent.FlowCompleted {
		t.Errorf("flow status = %q, want completed", run.Status)
	}

	// Verify both steps ran
	if len(run.StepOutputs) != 2 {
		t.Fatalf("expected 2 step outputs, got %d", len(run.StepOutputs))
	}

	// Step 1 output should be in step 2 output
	if !strings.Contains(run.StepOutputs[0], "Research findings") {
		t.Errorf("step 1 output = %q", run.StepOutputs[0])
	}
	if !strings.Contains(run.StepOutputs[1], "Summary") {
		t.Errorf("step 2 output = %q", run.StepOutputs[1])
	}

	// Verify flow events were emitted
	hasFlowStarted := false
	hasFlowCompleted := false
	for _, ev := range flowEvents {
		if ev.Type == "flow_started" {
			hasFlowStarted = true
		}
		if ev.Type == "flow_completed" {
			hasFlowCompleted = true
		}
	}
	if !hasFlowStarted {
		t.Error("missing flow_started event")
	}
	if !hasFlowCompleted {
		t.Error("missing flow_completed event")
	}

	// Verify task registry has 2 tasks
	tasks := taskRegistry.ListByFlow(run.ID)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks in registry, got %d", len(tasks))
	}
}

// =============================================================================
// Behavioral test: heartbeat via unified job executor
// =============================================================================

func TestBehavior_Heartbeat(t *testing.T) {
	dir := t.TempDir()

	// Set up workspace with structured HEARTBEAT.md
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o755)
	os.WriteFile(filepath.Join(wsDir, "HEARTBEAT.md"), []byte(`# Heartbeat Checks

tasks:
  - name: reminder-check
    interval: 30m
    agent: orchestrator
    prompt: "Check for overdue reminders and system health"
`), 0o644)

	model := &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "Heartbeat complete. No overdue reminders. System healthy."},
		},
	}

	env := testutil.NewTestEnv(dir, model)

	reminderStore := tool.NewReminderStore(filepath.Join(dir, "reminders.json"))
	env.Tools.Register(tool.ReminderTool(reminderStore))

	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(agentsDir, 0o755)
	wsLoader := agent.NewWorkspaceLoader(wsDir, agentsDir, "")
	skillLoader := agent.NewSkillLoader(filepath.Join(dir, "skills"), agentsDir, "")
	env.Runner.Workspaces = wsLoader
	env.Runner.ContextEngine = agent.NewContextEngine(nil, wsLoader, skillLoader)

	// Create job store and executor
	jobStore, err := agent.NewJobStore(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	runLog := agent.NewRunLog(filepath.Join(dir, "runs"))
	notifStore, _ := agent.NewNotificationStore(filepath.Join(dir, "notifications.jsonl"))
	msgBus := env.Bus

	jobExec := agent.NewJobExecutor(jobStore, runLog, notifStore, env.Runner, env.Registry, msgBus, env.Logger)

	// Sync heartbeat tasks to jobs
	if err := jobExec.SyncHeartbeatJobs(wsLoader); err != nil {
		t.Fatalf("sync heartbeat: %v", err)
	}

	// Verify job was created
	jobs := jobStore.List()
	found := false
	for _, j := range jobs {
		if j.ID == "heartbeat-reminder-check" {
			found = true
			if j.Schedule.Kind != agent.ScheduleEvery {
				t.Errorf("expected schedule kind 'every', got %q", j.Schedule.Kind)
			}
			if j.Schedule.Every != "30m" {
				t.Errorf("expected interval '30m', got %q", j.Schedule.Every)
			}
		}
	}
	if !found {
		t.Fatal("heartbeat job not created")
	}

	// Subscribe to heartbeat completed events
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	heartbeatCh := msgBus.Subscribe(ctx, bus.TopicHeartbeatCompleted)

	// Trigger the heartbeat job
	if err := jobExec.TriggerHeartbeatTask(ctx, "reminder-check"); err != nil {
		t.Fatalf("trigger heartbeat: %v", err)
	}

	// Wait for heartbeat completed event
	select {
	case ev := <-heartbeatCh:
		hbEvent, ok := ev.Payload.(bus.HeartbeatEvent)
		if !ok {
			t.Fatal("unexpected event type")
		}
		if hbEvent.Items != 1 {
			t.Errorf("heartbeat items = %d, want 1", hbEvent.Items)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for heartbeat event")
	}
}

// =============================================================================
// Behavioral test: BOOT.md startup
// =============================================================================

func TestBehavior_Boot(t *testing.T) {
	dir := t.TempDir()

	// Set up workspace with BOOT.md
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o755)
	os.WriteFile(filepath.Join(wsDir, "BOOT.md"), []byte(`# Morning Startup
- Review yesterday's daily log
- Check for failed cron jobs
- Prepare morning briefing
`), 0o644)

	model := &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Model responds to boot prompt
			{Text: "Boot complete. No failed jobs. Briefing prepared."},
		},
	}

	env := testutil.NewTestEnv(dir, model)

	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(agentsDir, 0o755)
	wsLoader := agent.NewWorkspaceLoader(wsDir, agentsDir, "")
	skillLoader := agent.NewSkillLoader(filepath.Join(dir, "skills"), agentsDir, "")
	env.Runner.Workspaces = wsLoader
	env.Runner.ContextEngine = agent.NewContextEngine(nil, wsLoader, skillLoader)

	// Verify boot has not run today
	if wsLoader.BootRanToday() {
		t.Fatal("boot should not have run yet")
	}

	// Simulate boot by running the prompt directly (same as scheduler does)
	ws, err := wsLoader.Load("orchestrator", nil)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	bootMD := ws.Get(agent.FileBoot)
	if bootMD == "" {
		t.Fatal("BOOT.md should have content")
	}

	orchestrator, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	result, events := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:    "orchestrator",
		Agent:      orchestrator,
		Prompt:     "Execute this startup checklist:\n\n" + bootMD,
		PromptMode: agent.PromptModeFull,
	})

	if result.Content == "" {
		t.Error("boot should produce content")
	}
	if !strings.Contains(result.Content, "Boot complete") {
		t.Errorf("unexpected boot result: %s", result.Content)
	}

	// Mark boot as ran
	wsLoader.MarkBootRan()
	if !wsLoader.BootRanToday() {
		t.Error("boot should be marked as ran")
	}

	// Verify events were emitted
	if !hasEvent(events, agent.EventTextDelta, nil) {
		t.Error("boot should emit text events")
	}
	_ = events
}
