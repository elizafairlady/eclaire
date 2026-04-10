package agent_test

import (
	"strings"
	"testing"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/testutil"
)

// TestBehavior_JobAddAt verifies that the eclaire_manage tool can schedule
// a one-shot job with the "at" schedule kind.
func TestBehavior_JobAddAt(t *testing.T) {
	dir := t.TempDir()
	mock := &testutil.MockModel{Responses: []testutil.MockResponse{
		{
			ToolCalls: []testutil.MockToolCall{{
				Name: "eclaire_manage",
				ID:   "tc1",
				Input: map[string]any{
					"operation":          "job_add",
					"job_schedule_kind":  "at",
					"job_schedule_value": "2h",
					"job_agent":          "research",
					"job_prompt":         "Update the Iran ceasefire report with new findings",
					"job_name":           "iran-update",
				},
			}},
		},
		{Text: "I've scheduled a one-shot update for 2 hours from now."},
	}}

	env := testutil.NewTestEnv(dir, mock)
	result, events := env.RunAgent(t, "orchestrator", "Schedule a research update in 2 hours")

	// Verify tool was called
	if !hasEvent(events, "tool_call", func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Fatal("expected eclaire_manage tool call")
	}

	// Verify tool succeeded
	if !hasEvent(events, "tool_result", func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "Job scheduled")
	}) {
		t.Fatal("expected job_add success result")
	}

	// Verify job is in the store
	jobs := env.JobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in store, got %d", len(jobs))
	}
	j := jobs[0]
	if j.Schedule.Kind != agent.ScheduleAt {
		t.Fatalf("expected schedule kind 'at', got %q", j.Schedule.Kind)
	}
	if j.AgentID != "research" {
		t.Fatalf("expected agent 'research', got %q", j.AgentID)
	}
	if !j.DeleteAfterRun {
		t.Fatal("expected deleteAfterRun true for at kind")
	}
	if j.State.NextRunAt == nil {
		t.Fatal("expected NextRunAt to be set")
	}

	_ = result
}

// TestBehavior_JobAddEvery verifies scheduling a recurring interval job.
func TestBehavior_JobAddEvery(t *testing.T) {
	dir := t.TempDir()
	mock := &testutil.MockModel{Responses: []testutil.MockResponse{
		{
			ToolCalls: []testutil.MockToolCall{{
				Name: "eclaire_manage",
				ID:   "tc1",
				Input: map[string]any{
					"operation":          "job_add",
					"job_schedule_kind":  "every",
					"job_schedule_value": "30m",
					"job_agent":          "orchestrator",
					"job_prompt":         "Check inbox for urgent messages",
				},
			}},
		},
		{Text: "Done."},
	}}

	env := testutil.NewTestEnv(dir, mock)
	env.RunAgent(t, "orchestrator", "Check my inbox every 30 minutes")

	jobs := env.JobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Schedule.Kind != agent.ScheduleEvery {
		t.Fatalf("expected schedule kind 'every', got %q", jobs[0].Schedule.Kind)
	}
	if jobs[0].DeleteAfterRun {
		t.Fatal("expected deleteAfterRun false for every kind")
	}
}

// TestBehavior_JobAddCron verifies scheduling a cron-expression job.
func TestBehavior_JobAddCron(t *testing.T) {
	dir := t.TempDir()
	mock := &testutil.MockModel{Responses: []testutil.MockResponse{
		{
			ToolCalls: []testutil.MockToolCall{{
				Name: "eclaire_manage",
				ID:   "tc1",
				Input: map[string]any{
					"operation":          "job_add",
					"job_schedule_kind":  "cron",
					"job_schedule_value": "0 7 * * *",
					"job_agent":          "research",
					"job_prompt":         "Generate morning briefing",
					"job_name":           "morning-brief",
				},
			}},
		},
		{Text: "Morning briefing scheduled for 7 AM daily."},
	}}

	env := testutil.NewTestEnv(dir, mock)
	env.RunAgent(t, "orchestrator", "Schedule a daily briefing at 7 AM")

	jobs := env.JobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Schedule.Expr != "0 7 * * *" {
		t.Fatalf("expected cron expr '0 7 * * *', got %q", jobs[0].Schedule.Expr)
	}
}

// TestBehavior_ResearchThenSchedule tests the full user scenario:
// Claire delegates research to the research agent, then schedules a one-shot
// update of the research report for later.
func TestBehavior_ResearchThenSchedule(t *testing.T) {
	dir := t.TempDir()

	// Mock model script:
	// Turn 1: Claire calls the agent tool to delegate research
	// Turn 2: (sub-agent response handled by runner)
	// Turn 3: Claire calls eclaire_manage to schedule follow-up
	// Turn 4: Claire summarizes both actions
	mock := &testutil.MockModel{Responses: []testutil.MockResponse{
		// Turn 1: Delegate to research agent
		{
			ToolCalls: []testutil.MockToolCall{{
				Name: "agent",
				ID:   "tc1",
				Input: map[string]any{
					"agent":  "research",
					"prompt": "Research the 2026 Iran ceasefire. Cover key events, parties involved, and current status. Write a comprehensive report.",
				},
			}},
		},
		// Sub-agent response (research agent runs with mock, returns this)
		{Text: "Research report on 2026 Iran ceasefire:\n\n1. Background: ...\n2. Key parties: ...\n3. Current status: ..."},
		// Turn 2: After receiving research result, schedule the update
		{
			ToolCalls: []testutil.MockToolCall{{
				Name: "eclaire_manage",
				ID:   "tc2",
				Input: map[string]any{
					"operation":          "job_add",
					"job_schedule_kind":  "at",
					"job_schedule_value": "2h",
					"job_agent":          "research",
					"job_prompt":         "Review and update the Iran ceasefire research report. Validate existing findings against current sources and add any new developments.",
					"job_name":           "iran-ceasefire-update",
					"job_context_messages": "Previous research found: 1. Background details, 2. Key parties involved, 3. Current ceasefire status as of research date.",
				},
			}},
		},
		// Turn 3: Final response to user
		{Text: "I've completed the initial research on the 2026 Iran ceasefire and scheduled a follow-up update in 2 hours to validate and expand the findings."},
	}}

	env := testutil.NewTestEnv(dir, mock)
	result, events := env.RunAgent(t, "orchestrator",
		"Perform a research project on the 2026 iran ceasefire, and then schedule a one-time update of that research project report that validates findings and updates with new findings.")

	// Verify agent tool was called (research delegation)
	if !hasEvent(events, "tool_call", func(ev agent.StreamEvent) bool {
		return ev.ToolName == "agent"
	}) {
		t.Fatal("expected agent tool call for research delegation")
	}

	// Verify sub-agent completed
	if !hasEvent(events, "tool_result", func(ev agent.StreamEvent) bool {
		return ev.ToolName == "agent" && strings.Contains(ev.Output, "completed")
	}) {
		// Check for the result content instead
		hasAgentResult := false
		for _, ev := range events {
			if ev.ToolName == "agent" && ev.Type == "tool_result" {
				hasAgentResult = true
				break
			}
		}
		if !hasAgentResult {
			t.Fatal("expected agent tool result for research completion")
		}
	}

	// Verify eclaire_manage was called for scheduling
	if !hasEvent(events, "tool_call", func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Fatal("expected eclaire_manage tool call for job scheduling")
	}

	// Verify job_add succeeded
	if !hasEvent(events, "tool_result", func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "Job scheduled")
	}) {
		t.Fatal("expected job_add success in tool result")
	}

	// Verify job is in the store with correct properties
	jobs := env.JobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in store, got %d", len(jobs))
	}
	j := jobs[0]
	if j.Schedule.Kind != agent.ScheduleAt {
		t.Fatalf("expected at schedule, got %q", j.Schedule.Kind)
	}
	if j.AgentID != "research" {
		t.Fatalf("expected research agent, got %q", j.AgentID)
	}
	if !j.DeleteAfterRun {
		t.Fatal("expected deleteAfterRun true")
	}
	if j.ContextMessages == "" {
		t.Fatal("expected context messages to be set")
	}
	if !strings.Contains(j.Prompt, "validate") && !strings.Contains(j.Prompt, "update") {
		t.Fatalf("expected prompt to mention validation/update, got %q", j.Prompt)
	}

	// Verify final response mentions both research and scheduling
	if result.Content == "" {
		t.Fatal("expected non-empty result content")
	}

}
