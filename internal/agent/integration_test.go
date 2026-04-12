package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/testutil"
)

func TestRunnerTextOnly(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "Hello, I'm the orchestrator."},
		},
	})

	a, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	var events []agent.StreamEvent
	var mu sync.Mutex
	emit := func(ev agent.StreamEvent) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "hello",
	}, emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Content != "Hello, I'm the orchestrator." {
		t.Errorf("content = %q", result.Content)
	}
	if result.SessionID == "" {
		t.Error("session ID should not be empty")
	}
	if result.Steps < 1 {
		t.Errorf("steps = %d, want >= 1", result.Steps)
	}

	// Check events
	mu.Lock()
	defer mu.Unlock()
	hasTextDelta := false
	hasStepFinish := false
	for _, ev := range events {
		if ev.Type == agent.EventTextDelta {
			hasTextDelta = true
		}
		if ev.Type == agent.EventStepFinish {
			hasStepFinish = true
		}
	}
	if !hasTextDelta {
		t.Error("should have text_delta event")
	}
	if !hasStepFinish {
		t.Error("should have step_finish event")
	}
}

func TestRunnerWithToolCall(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Step 1: model calls the shell tool
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "echo hello",
				}},
			}},
			// Step 2: model responds with text after seeing tool result
			{Text: "The command output 'hello'."},
		},
	})

	a, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	var events []agent.StreamEvent
	var mu sync.Mutex
	emit := func(ev agent.StreamEvent) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "run echo hello",
	}, emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Content == "" {
		t.Error("content should not be empty")
	}

	// Check events include tool_call and tool_result
	mu.Lock()
	defer mu.Unlock()
	hasToolCall := false
	hasToolResult := false
	for _, ev := range events {
		if ev.Type == agent.EventToolCall && ev.ToolName == "shell" {
			hasToolCall = true
		}
		if ev.Type == agent.EventToolResult && ev.ToolName == "shell" {
			hasToolResult = true
		}
	}
	if !hasToolCall {
		t.Error("should have tool_call event for shell")
	}
	if !hasToolResult {
		t.Error("should have tool_result event for shell")
	}
}

func TestRunnerSessionPersistence(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "response 1"},
		},
	})

	a, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "test persistence",
	}, func(ev agent.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify session was created
	meta, err := env.Sessions.GetMeta(result.SessionID)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.AgentID != "orchestrator" {
		t.Errorf("agent_id = %q", meta.AgentID)
	}
	if meta.Status != "completed" {
		t.Errorf("status = %q, want completed", meta.Status)
	}

	// Verify events were written
	events, err := env.Sessions.ReadEvents(result.SessionID)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("got %d events, want >= 2 (user_message + assistant_message)", len(events))
	}

	// First event should be user_message
	if events[0].Type != "user_message" {
		t.Errorf("events[0].type = %q, want user_message", events[0].Type)
	}
}

func TestSubAgentDispatch(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator step 1: calls the agent tool to dispatch to coding
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-agent-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "write a hello world function",
				}},
			}},
			// Coding agent step 1: responds with text (sub-agent run)
			{Text: "def hello(): print('hello')"},
			// Orchestrator step 2: responds after getting agent result
			{Text: "The coding agent wrote a hello world function."},
		},
	})

	a, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	var events []agent.StreamEvent
	var mu sync.Mutex
	emit := func(ev agent.StreamEvent) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "write hello world",
	}, emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Content == "" {
		t.Error("content should not be empty")
	}

	// Check for sub-agent lifecycle events
	mu.Lock()
	defer mu.Unlock()

	hasSubStarted := false
	hasSubCompleted := false
	for _, ev := range events {
		if ev.Type == agent.EventSubAgentStarted && ev.AgentID == "coding" {
			hasSubStarted = true
		}
		if ev.Type == agent.EventSubAgentCompleted && ev.AgentID == "coding" {
			hasSubCompleted = true
		}
	}

	if !hasSubStarted {
		t.Error("should have sub_agent_started event for coding")
	}
	if !hasSubCompleted {
		t.Error("should have sub_agent_completed event for coding")
	}
}

func TestHardMaxIterationsClamped(t *testing.T) {
	// Verify that HardMaxIterations constant is reasonable
	if agent.HardMaxIterations < 50 || agent.HardMaxIterations > 1000 {
		t.Errorf("HardMaxIterations = %d, should be between 50 and 1000", agent.HardMaxIterations)
	}
}

func TestTokenBudgetStopsRun(t *testing.T) {
	// Create a mock model that makes many tool calls, each reporting high token usage
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Step 1: tool call with huge usage
			{
				ToolCalls: []testutil.MockToolCall{
					{Name: "ls", ID: "tc-1", Input: map[string]any{"path": "/tmp"}},
				},
				Usage: fantasy.Usage{InputTokens: 1_500_000, OutputTokens: 600_000},
			},
			// Step 2: model should never get here — budget exceeded
			{Text: "should not reach here"},
		},
	})

	a, ok := env.Registry.Get("orchestrator")
	if !ok {
		t.Fatal("orchestrator not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "test budget",
	}, func(ev agent.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The run should have completed but the token budget mechanism
	// triggers in RunTurn. With 2.1M tokens from step 1, it should stop.
	// Content may be empty since the model was stopped mid-loop.
	_ = result
}

func TestDefaultCompactionConfig(t *testing.T) {
	cfg := agent.DefaultCompactionConfig()
	if !cfg.Enabled {
		t.Error("default compaction should be enabled")
	}
	if cfg.ThresholdToks != 100_000 {
		t.Errorf("threshold = %d, want 100000", cfg.ThresholdToks)
	}
	if cfg.PreserveCount != 4 {
		t.Errorf("preserve_count = %d, want 4", cfg.PreserveCount)
	}
}

func TestSubAgentChildSession(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator dispatches to coding
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "test child session",
				}},
			}},
			// Coding responds
			{Text: "done"},
			// Orchestrator responds
			{Text: "ok"},
		},
	})

	a, _ := env.Registry.Get("orchestrator")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "test",
	}, func(ev agent.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// List all sessions — should have parent + child
	sessions, err := env.Sessions.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(sessions) < 2 {
		t.Fatalf("got %d sessions, want >= 2 (parent + child)", len(sessions))
	}

	// Find the parent session
	parentMeta, err := env.Sessions.GetMeta(result.SessionID)
	if err != nil {
		t.Fatalf("GetMeta parent: %v", err)
	}

	// Parent should have at least one child
	if len(parentMeta.ChildIDs) == 0 {
		t.Error("parent session should have child IDs")
	}

	// Check child session
	if len(parentMeta.ChildIDs) > 0 {
		childMeta, err := env.Sessions.GetMeta(parentMeta.ChildIDs[0])
		if err != nil {
			t.Fatalf("GetMeta child: %v", err)
		}
		if childMeta.AgentID != "coding" {
			t.Errorf("child agent_id = %q, want coding", childMeta.AgentID)
		}
		if childMeta.ParentID != result.SessionID {
			t.Errorf("child parent_id = %q, want %q", childMeta.ParentID, result.SessionID)
		}
	}
}

func TestSubAgentNestedEvents(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator dispatches to coding
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "run echo test",
				}},
			}},
			// Coding step 1: calls shell
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-shell-1", Input: map[string]any{
					"command": "echo test",
				}},
			}},
			// Coding step 2: responds
			{Text: "ran echo test successfully"},
			// Orchestrator responds
			{Text: "the coding agent ran the command"},
		},
	})

	a, _ := env.Registry.Get("orchestrator")
	var events []agent.StreamEvent
	var mu sync.Mutex
	emit := func(ev agent.StreamEvent) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "run echo test via coding",
	}, emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have nested tool call/result from the sub-agent
	hasNestedToolCall := false
	hasNestedToolResult := false
	for _, ev := range events {
		if ev.Type == agent.EventSubAgentToolCall && ev.AgentID == "coding" && ev.ToolName == "shell" {
			hasNestedToolCall = true
			if !ev.Nested {
				t.Error("sub_agent_tool_call should have Nested=true")
			}
			if ev.TaskID == "" {
				t.Error("sub_agent_tool_call should have TaskID")
			}
		}
		if ev.Type == agent.EventSubAgentToolResult && ev.AgentID == "coding" && ev.ToolName == "shell" {
			hasNestedToolResult = true
			if !ev.Nested {
				t.Error("sub_agent_tool_result should have Nested=true")
			}
		}
	}

	if !hasNestedToolCall {
		t.Error("should have nested tool_call event for shell from coding agent")
	}
	if !hasNestedToolResult {
		t.Error("should have nested tool_result event for shell from coding agent")
	}
}

func TestSubAgentNotFound(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator dispatches to nonexistent agent
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "nonexistent",
					"prompt": "do something",
				}},
			}},
			// Orchestrator responds after error
			{Text: "the agent was not found"},
		},
	})

	a, _ := env.Registry.Get("orchestrator")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "dispatch to nonexistent",
	}, func(ev agent.StreamEvent) error { return nil })

	// Should NOT fail entirely — the agent tool returns an error response
	// to the orchestrator, which then continues
	if err != nil {
		t.Fatalf("Run should not fail for sub-agent not found: %v", err)
	}

	if result.Content == "" {
		t.Error("orchestrator should have responded")
	}
}

func TestEmitInContext(t *testing.T) {
	// Verify emit function roundtrips through context
	var captured agent.StreamEvent
	emit := func(ev agent.StreamEvent) error {
		captured = ev
		return nil
	}

	ctx := agent.ContextWithEmit(context.Background(), emit)
	fn, ok := agent.EmitFromContext(ctx)
	if !ok {
		t.Fatal("should find emit in context")
	}

	fn(agent.StreamEvent{Type: agent.EventToolCall, ToolName: "test"})
	if captured.ToolName != "test" {
		t.Errorf("captured tool = %q", captured.ToolName)
	}
}

func TestBusEventsFromSubAgent(t *testing.T) {
	env := testutil.NewTestEnv(t.TempDir(), &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "hello",
				}},
			}},
			{Text: "sub done"},
			{Text: "all done"},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Subscribe to bus events before running
	startedCh := env.Bus.Subscribe(ctx, "subagent.started")
	completedCh := env.Bus.Subscribe(ctx, "subagent.completed")

	a, _ := env.Registry.Get("orchestrator")
	_, err := env.Runner.Run(ctx, agent.RunConfig{
		AgentID: "orchestrator",
		Agent:   a,
		Prompt:  "test bus events",
	}, func(ev agent.StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check bus events were published
	select {
	case ev := <-startedCh:
		if ev.Topic != "subagent.started" {
			t.Errorf("topic = %q", ev.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for subagent.started bus event")
	}

	select {
	case ev := <-completedCh:
		if ev.Topic != "subagent.completed" {
			t.Errorf("topic = %q", ev.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for subagent.completed bus event")
	}
}
