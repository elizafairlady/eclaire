package agent_test

// Mock parity harness: deterministic scenario tests covering core agent loop behaviors.
// Modeled after Claw Code's mock_parity_scenarios.json.

import (
	"os"
	"strings"
	"testing"

	"github.com/elizafairlady/eclaire/internal/persist"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/testutil"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// =============================================================================
// Scenario 1: Basic streaming — model returns text, no tools
// =============================================================================

func TestMockParity_BasicStreaming(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{Text: "Hello! I'm Claire, your personal AI assistant."},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Hello")

	// Should have text delta events
	if !hasEvent(events, agent.EventTextDelta, nil) {
		t.Error("should emit text_delta events")
	}

	// Should have step_finish
	if !hasEvent(events, agent.EventStepFinish, nil) {
		t.Error("should emit step_finish event")
	}

	// Content should match
	if result.Content != "Hello! I'm Claire, your personal AI assistant." {
		t.Errorf("content = %q", result.Content)
	}

	// Should complete in 1 step
	if result.Steps != 1 {
		t.Errorf("steps = %d, want 1", result.Steps)
	}

	// Session should be created
	if result.SessionID == "" {
		t.Error("session should be created")
	}
}

// =============================================================================
// Scenario 2: Single tool roundtrip — model calls one tool, gets result, responds
// =============================================================================

func TestMockParity_SingleToolRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// Write a file for the read tool
	writeTestFile(t, dir+"/test.txt", "file contents here")

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "read", ID: "tc-1", Input: map[string]any{"path": dir + "/test.txt"}},
			}},
			{Text: "The file contains: file contents here"},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Read test.txt")

	// Should have tool_call event
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "read"
	}) {
		t.Error("should have read tool_call event")
	}

	// Should have tool_result event
	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "file contents here")
	}) {
		t.Error("tool_result should contain file contents")
	}

	// Should complete in 2 steps (tool call + final response)
	if result.Steps != 2 {
		t.Errorf("steps = %d, want 2", result.Steps)
	}
}

// =============================================================================
// Scenario 3: Multi-tool turn — model calls multiple tools in one turn
// =============================================================================

func TestMockParity_MultiToolTurn(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir+"/a.txt", "alpha")
	writeTestFile(t, dir+"/b.txt", "bravo")

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Model calls two tools in one turn
			{ToolCalls: []testutil.MockToolCall{
				{Name: "read", ID: "tc-1", Input: map[string]any{"path": dir + "/a.txt"}},
				{Name: "read", ID: "tc-2", Input: map[string]any{"path": dir + "/b.txt"}},
			}},
			{Text: "File a contains alpha, file b contains bravo."},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Read both files")

	// Should have 2 tool_call events
	toolCalls := countEvents(events, agent.EventToolCall)
	if toolCalls != 2 {
		t.Errorf("tool_call count = %d, want 2", toolCalls)
	}

	// Should have 2 tool_result events
	toolResults := countEvents(events, agent.EventToolResult)
	if toolResults != 2 {
		t.Errorf("tool_result count = %d, want 2", toolResults)
	}

	if !strings.Contains(result.Content, "alpha") || !strings.Contains(result.Content, "bravo") {
		t.Errorf("result should mention both files: %s", result.Content)
	}
}

// =============================================================================
// Scenario 4: Permission denial — ReadOnly mode blocks write tools
// =============================================================================

func TestMockParity_PermissionDenial(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Model tries to write
			{ToolCalls: []testutil.MockToolCall{
				{Name: "write", ID: "tc-1", Input: map[string]any{
					"path":    dir + "/output.txt",
					"content": "should not be written",
				}},
			}},
			{Text: "Permission denied, I cannot write in read-only mode."},
		},
	})

	a, _ := env.Registry.Get("orchestrator")
	_, events := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:        "orchestrator",
		Agent:          a,
		Prompt:         "Write a file",
		PermissionMode: tool.PermissionReadOnly,
	})

	// Tool result should indicate denial
	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "Permission denied") || strings.Contains(ev.Output, "denied")
	}) {
		t.Error("write should be denied in ReadOnly mode")
	}

	// File should NOT exist
	if _, err := readTestFile(dir + "/output.txt"); err == nil {
		t.Error("file should not have been written")
	}
}

// =============================================================================
// Scenario 5: Sub-agent delegation — orchestrator delegates to coding agent
// =============================================================================

func TestMockParity_SubAgentDelegation(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator delegates to coding agent
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "Write a hello world function",
				}},
			}},
			// Sub-agent (coding) response (consumed by agent tool)
			{Text: "Created hello world function."},
			// Orchestrator final response
			{Text: "The coding agent has created the hello world function."},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Create a hello world function")

	// Should have agent tool call
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "agent"
	}) {
		t.Error("should have agent tool call")
	}

	// Should have sub-agent events
	if !hasEvent(events, agent.EventSubAgentStarted, nil) {
		t.Error("should have sub_agent_started event")
	}

	if result.Content == "" {
		t.Error("orchestrator should produce final content")
	}
}

// =============================================================================
// Scenario 6: Conversation continuity — second message has history from first
// =============================================================================

func TestMockParity_ConversationContinuity(t *testing.T) {
	dir := t.TempDir()

	model := &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// First run: model says something
			{Text: "The secret word is 'pineapple'."},
			// Second run: model uses history to answer
			{Text: "The secret word was 'pineapple'."},
		},
	}

	env := testutil.NewTestEnv(dir, model)

	// First message
	result1, _ := env.RunAgent(t, "orchestrator", "Remember the word pineapple")

	if result1.SessionID == "" {
		t.Fatal("first run should create a session")
	}

	// Second message — continue the same session with history rebuilt from events
	events, _ := env.Sessions.ReadEvents(result1.SessionID)
	history := persist.RebuildMessages(events)

	a, _ := env.Registry.Get("orchestrator")
	result2, _ := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:   "orchestrator",
		Agent:     a,
		Prompt:    "What was the secret word?",
		SessionID: result1.SessionID,
		History:   history,
	})

	// Model should have received history in its second call
	if len(model.Calls) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(model.Calls))
	}

	// The second call should have messages (history) in the prompt
	secondCall := model.Calls[1]
	if len(secondCall.Prompt) < 2 {
		t.Errorf("second call should have history messages, got %d messages", len(secondCall.Prompt))
	}

	if result2.Content == "" {
		t.Error("second run should produce content")
	}

	// Both runs should use the same session
	if result2.SessionID != result1.SessionID {
		t.Errorf("session IDs should match: %s vs %s", result1.SessionID, result2.SessionID)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func readTestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}
