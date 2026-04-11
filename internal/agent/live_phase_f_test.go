//go:build live

package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/testutil"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// =============================================================================
// Live LLM test: morning briefing
// =============================================================================

func TestLive_MorningBriefing(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	// Seed a reminder so the briefing has something to show
	store := tool.NewReminderStore(dir + "/reminders.json")
	store.Save([]tool.Reminder{
		{ID: "r1", Text: "Review deployment logs", DueAt: time.Now().Add(-30 * time.Minute), CreatedAt: time.Now()},
		{ID: "r2", Text: "Update PLAN.md", DueAt: time.Now().Add(2 * time.Hour), CreatedAt: time.Now()},
	})

	result, events := env.RunAgent(t, "orchestrator",
		"Good morning. Generate my daily briefing using the eclaire_briefing tool, then summarize what's important.")

	// Should have called the briefing tool
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_briefing"
	}) {
		t.Error("should call eclaire_briefing tool")
	}

	// Result should mention the overdue reminder
	lower := strings.ToLower(result.Content)
	if !strings.Contains(lower, "deployment") && !strings.Contains(lower, "overdue") && !strings.Contains(lower, "review") {
		t.Errorf("briefing should reference the overdue reminder, got: %s", result.Content)
	}
}

// =============================================================================
// Live LLM test: reminder management
// =============================================================================

func TestLive_ReminderManagement(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	// Ask Claire to create a reminder
	result, events := env.RunAgent(t, "orchestrator",
		"Create a reminder for me: 'Walk the dogs' due in 2 hours. Use the eclaire_reminder tool with operation 'add'.")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_reminder"
	}) {
		t.Error("should call eclaire_reminder tool")
	}

	// Verify the reminder was actually persisted
	store := tool.NewReminderStore(dir + "/reminders.json")
	pending, err := store.Pending()
	if err != nil {
		t.Fatalf("load reminders: %v", err)
	}
	if len(pending) == 0 {
		t.Error("should have at least 1 pending reminder after creation")
	}

	found := false
	for _, r := range pending {
		if strings.Contains(strings.ToLower(r.Text), "dog") || strings.Contains(strings.ToLower(r.Text), "walk") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("reminder about walking dogs not found, got: %v", pending)
	}

	_ = result
}

// =============================================================================
// Live LLM test: flow pipeline execution
// =============================================================================

func TestLive_FlowPipeline(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	// Create a 2-step flow
	flowsDir := filepath.Join(dir, "flows")
	os.MkdirAll(flowsDir, 0o755)
	os.WriteFile(filepath.Join(flowsDir, "analyze.yaml"), []byte(`
id: analyze
name: Analyze File
steps:
  - name: read
    agent: orchestrator
    prompt: "Read this file and describe its contents: {{.Input}}"
  - name: assess
    agent: orchestrator
    prompt: "Based on this file description, suggest one improvement:\n{{.PrevOutput}}"
`), 0o644)

	// Create the file to analyze
	target := filepath.Join(dir, "sample.go")
	os.WriteFile(target, []byte(`package main

func main() {
	println("hello")
}
`), 0o644)

	def, err := agent.LoadFlowFile(filepath.Join(flowsDir, "analyze.yaml"))
	if err != nil {
		t.Fatalf("load flow: %v", err)
	}

	taskRegistry := agent.NewTaskRegistry()
	executor := &agent.FlowExecutor{
		Runner:   env.Runner,
		Tasks:    taskRegistry,
		Registry: env.Registry,
		Bus:      env.Bus,
		Logger:   env.Logger,
	}

	var flowEvents []agent.StreamEvent
	run, err := executor.Run(t.Context(), *def, target, func(ev agent.StreamEvent) error {
		flowEvents = append(flowEvents, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("flow run: %v", err)
	}

	if run.Status != agent.FlowCompleted {
		t.Errorf("flow status = %q, want completed (error: %s)", run.Status, run.Error)
	}

	if len(run.StepOutputs) != 2 {
		t.Fatalf("expected 2 step outputs, got %d", len(run.StepOutputs))
	}

	// Step 1 should describe the file
	if !strings.Contains(strings.ToLower(run.StepOutputs[0]), "hello") &&
		!strings.Contains(strings.ToLower(run.StepOutputs[0]), "main") {
		t.Errorf("step 1 should describe the Go file, got: %s", run.StepOutputs[0][:min(200, len(run.StepOutputs[0]))])
	}

	// Step 2 should suggest an improvement
	if run.StepOutputs[1] == "" {
		t.Error("step 2 should suggest an improvement")
	}
}

// =============================================================================
// Live LLM test: heartbeat execution
// =============================================================================

func TestLive_Heartbeat(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	// Write HEARTBEAT.md
	wsDir := filepath.Join(dir, "workspace")
	os.WriteFile(filepath.Join(wsDir, "HEARTBEAT.md"), []byte(`# Heartbeat
- List any pending reminders using eclaire_reminder tool
- Report system status
`), 0o644)

	// Seed a reminder
	store := tool.NewReminderStore(dir + "/reminders.json")
	store.Save([]tool.Reminder{
		{ID: "r1", Text: "Check CI pipeline", DueAt: time.Now().Add(1 * time.Hour), CreatedAt: time.Now()},
	})

	// Wire workspace loader
	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(agentsDir, 0o755)
	wsLoader := agent.NewWorkspaceLoader(wsDir, agentsDir, "")
	skillLoader := agent.NewSkillLoader(filepath.Join(dir, "skills"), agentsDir, "")
	env.Runner.Workspaces = wsLoader
	env.Runner.ContextEngine = agent.NewContextEngine(nil, wsLoader, skillLoader)

	// Load HEARTBEAT.md and run as the scheduler would
	ws, err := wsLoader.Load("orchestrator", nil)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	heartbeatMD := ws.Get(agent.FileHeartbeat)
	if heartbeatMD == "" {
		t.Fatal("HEARTBEAT.md should have content")
	}

	orchestrator, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	result, events := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:    "orchestrator",
		Agent:      orchestrator,
		Prompt:     "Process this heartbeat checklist:\n\n" + heartbeatMD,
		PromptMode: agent.PromptModeFull,
	})

	// Should have called the reminder tool
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_reminder"
	}) {
		t.Error("heartbeat should call eclaire_reminder tool")
	}

	if result.Content == "" {
		t.Error("heartbeat should produce content")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
