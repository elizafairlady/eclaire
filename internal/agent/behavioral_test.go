package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/testutil"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// hasEvent checks if any event matches the given type and optional field.
func hasEvent(events []agent.StreamEvent, evType string, check func(agent.StreamEvent) bool) bool {
	for _, ev := range events {
		if ev.Type == evType && (check == nil || check(ev)) {
			return true
		}
	}
	return false
}

// countEvents counts events matching a type.
func countEvents(events []agent.StreamEvent, evType string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == evType {
			n++
		}
	}
	return n
}

// =============================================================================
// Category 1: Direct tool use — orchestrator handles simple tasks itself
// =============================================================================

// The orchestrator should read a file directly, not delegate.
func TestBehavior_DirectRead(t *testing.T) {
	dir := t.TempDir()

	// Set up a file to read
	target := filepath.Join(dir, "hello.txt")
	os.WriteFile(target, []byte("Hello from eclaire!"), 0o644)

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Model calls read tool
			{ToolCalls: []testutil.MockToolCall{
				{Name: "read", ID: "tc-1", Input: map[string]any{
					"path": target,
				}},
			}},
			// Model responds with the content
			{Text: "The file contains: Hello from eclaire!"},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Read the file "+target)

	// Verify: orchestrator used read tool directly (no agent delegation)
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "read"
	}) {
		t.Error("orchestrator should have called read tool directly")
	}

	if hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "agent"
	}) {
		t.Error("orchestrator should NOT have delegated a simple read")
	}

	if !strings.Contains(result.Content, "Hello from eclaire") {
		t.Errorf("response should mention file content, got: %s", result.Content)
	}
}

// The orchestrator should run a shell command directly.
func TestBehavior_DirectShell(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "echo 'eclaire works'",
				}},
			}},
			{Text: "The command output: eclaire works"},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Run echo 'eclaire works'")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "shell"
	}) {
		t.Error("orchestrator should have called shell directly")
	}

	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "shell" && strings.Contains(ev.Output, "eclaire works")
	}) {
		t.Error("shell result should contain 'eclaire works'")
	}
}

// The orchestrator should create a file directly.
func TestBehavior_DirectWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output.txt")

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "write", ID: "tc-1", Input: map[string]any{
					"path":    target,
					"content": "created by eclaire",
				}},
			}},
			{Text: "File created."},
		},
	})

	env.RunAgent(t, "orchestrator", "Create a file at "+target)

	// Verify file actually exists
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if string(data) != "created by eclaire" {
		t.Errorf("file content = %q", string(data))
	}
}

// The orchestrator should use glob to find files.
func TestBehavior_DirectGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("text"), 0o644)

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "glob", ID: "tc-1", Input: map[string]any{
					"pattern": dir + "/*.go",
				}},
			}},
			{Text: "Found 2 Go files: a.go and b.go"},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Find all Go files in "+dir)

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "glob"
	}) {
		t.Error("should have used glob")
	}
}

// =============================================================================
// Category 2: Delegation — orchestrator dispatches to specialists
// =============================================================================

// When asked to write code, the orchestrator delegates to coding agent.
func TestBehavior_DelegateToCoding(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator delegates to coding
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "Write a Go function that returns the fibonacci sequence up to n",
				}},
			}},
			// Coding agent writes the file
			{ToolCalls: []testutil.MockToolCall{
				{Name: "write", ID: "tc-2", Input: map[string]any{
					"path":    filepath.Join(dir, "fib.go"),
					"content": "package main\n\nfunc fib(n int) []int {\n\tresult := []int{0, 1}\n\tfor i := 2; i < n; i++ {\n\t\tresult = append(result, result[i-1]+result[i-2])\n\t}\n\treturn result\n}\n",
				}},
			}},
			// Coding agent confirms
			{Text: "Created fib.go with fibonacci function."},
			// Orchestrator reports result
			{Text: "The coding agent created fib.go with a fibonacci function."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Write a fibonacci function in Go")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "agent"
	}) {
		t.Error("orchestrator should delegate complex code to coding agent")
	}

	if !hasEvent(events, agent.EventSubAgentStarted, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "coding"
	}) {
		t.Error("should emit sub_agent_started for coding")
	}

	if !hasEvent(events, agent.EventSubAgentCompleted, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "coding"
	}) {
		t.Error("should emit sub_agent_completed for coding")
	}

	if _, err := os.Stat(filepath.Join(dir, "fib.go")); os.IsNotExist(err) {
		t.Error("fib.go should have been created by coding agent")
	}
}

// =============================================================================
// Category 3: Multi-step workflows — read → edit → verify
// =============================================================================

// Read a file, edit it, verify the edit with a command.
func TestBehavior_ReadEditVerify(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	os.WriteFile(target, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0o644)

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Step 1: read the file
			{ToolCalls: []testutil.MockToolCall{
				{Name: "read", ID: "tc-1", Input: map[string]any{
					"path": target,
				}},
			}},
			// Step 2: edit the file
			{ToolCalls: []testutil.MockToolCall{
				{Name: "edit", ID: "tc-2", Input: map[string]any{
					"path":       target,
					"old_string": "println(\"hello\")",
					"new_string": "println(\"goodbye\")",
				}},
			}},
			// Step 3: verify with shell
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-3", Input: map[string]any{
					"command": "cat " + target,
				}},
			}},
			// Done
			{Text: "Changed hello to goodbye and verified."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Change 'hello' to 'goodbye' in "+target)

	// Verify the tool sequence: read → edit → shell
	toolSequence := []string{}
	for _, ev := range events {
		if ev.Type == agent.EventToolCall {
			toolSequence = append(toolSequence, ev.ToolName)
		}
	}
	if len(toolSequence) < 3 {
		t.Fatalf("expected >= 3 tool calls, got %d: %v", len(toolSequence), toolSequence)
	}
	if toolSequence[0] != "read" {
		t.Errorf("first tool should be read, got %s", toolSequence[0])
	}
	if toolSequence[1] != "edit" {
		t.Errorf("second tool should be edit, got %s", toolSequence[1])
	}

	// Verify file was actually edited
	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "goodbye") {
		t.Errorf("file should contain 'goodbye', got: %s", string(data))
	}
}

// =============================================================================
// Category 4: Error recovery — tool fails, agent retries
// =============================================================================

// When shell fails, agent should see the error and can respond.
func TestBehavior_ToolFailure(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Run a command that will fail
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "cat /nonexistent/file/path",
				}},
			}},
			// Agent sees error, responds
			{Text: "The file does not exist."},
		},
	})

	result, events := env.RunAgent(t, "orchestrator", "Read /nonexistent/file/path with cat")

	// Tool result should contain the error
	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "shell" && (strings.Contains(ev.Output, "No such file") || strings.Contains(ev.Output, "error"))
	}) {
		t.Error("shell result should show error output")
	}

	// Agent should still respond (not crash)
	if result.Content == "" {
		t.Error("agent should respond after tool failure")
	}
}

// =============================================================================
// Category 5: Sub-agent nested visibility
// =============================================================================

// Sub-agent's tool calls should be visible as nested events.
func TestBehavior_NestedToolVisibility(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator delegates to sysadmin
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "sysadmin",
					"prompt": "Check disk usage",
				}},
			}},
			// Sysadmin uses shell
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-2", Input: map[string]any{
					"command": "df -h /",
				}},
			}},
			// Sysadmin responds
			{Text: "Disk usage checked."},
			// Orchestrator reports
			{Text: "The sysadmin checked disk usage."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Check disk usage")

	if !hasEvent(events, agent.EventSubAgentToolCall, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "sysadmin" && ev.ToolName == "shell" && ev.Nested
	}) {
		t.Error("should see nested shell tool call from sysadmin")
	}

	if !hasEvent(events, agent.EventSubAgentToolResult, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "sysadmin" && ev.ToolName == "shell" && ev.Nested
	}) {
		t.Error("should see nested shell tool result from sysadmin")
	}

	for _, ev := range events {
		if ev.Nested && ev.TaskID == "" {
			t.Errorf("nested event %s should have TaskID", ev.Type)
		}
	}
}

// =============================================================================
// Category 6: Session persistence
// =============================================================================

// Session events should persist to JSONL and be readable.
func TestBehavior_SessionPersistence(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "echo persistence test",
				}},
			}},
			{Text: "Done."},
		},
	})

	result, _ := env.RunAgent(t, "orchestrator", "run echo persistence test")

	// Read back session events from JSONL
	events, err := env.Sessions.ReadEvents(result.SessionID)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}

	// Should have: user_message, tool_call, tool_result, step_finish, assistant_message
	types := map[string]bool{}
	for _, ev := range events {
		types[ev.Type] = true
	}

	required := []string{"user_message", "tool_call", "tool_result", "step_finish", "assistant_message"}
	for _, r := range required {
		if !types[r] {
			t.Errorf("session should contain event type %q, got: %v", r, types)
		}
	}
}

// Sub-agent creates child session linked to parent.
func TestBehavior_ChildSessionLinking(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
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

	result, _ := env.RunAgent(t, "orchestrator", "delegate to coding")

	// Parent should link to child
	parent, _ := env.Sessions.GetMeta(result.SessionID)
	if parent == nil || len(parent.ChildIDs) == 0 {
		t.Fatal("parent should have child session")
	}

	// Child should link back
	child, err := env.Sessions.GetMeta(parent.ChildIDs[0])
	if err != nil || child == nil {
		t.Fatalf("GetMeta child: %v", err)
	}
	if child.ParentID != parent.ID {
		t.Errorf("child.ParentID = %q, want %q", child.ParentID, parent.ID)
	}
	if child.RootID != parent.RootID {
		t.Errorf("child.RootID = %q, want %q", child.RootID, parent.RootID)
	}
	if child.AgentID != "coding" {
		t.Errorf("child.AgentID = %q, want coding", child.AgentID)
	}
}

// =============================================================================
// Category 7: Context isolation — sub-agent has own session
// =============================================================================

// Multiple sub-agents should each get their own session.
func TestBehavior_ContextIsolation(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Orchestrator dispatches to coding
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "coding",
					"prompt": "task A",
				}},
			}},
			{Text: "coding done"},
			// Orchestrator dispatches to research
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-2", Input: map[string]any{
					"agent":  "research",
					"prompt": "task B",
				}},
			}},
			{Text: "research done"},
			// Orchestrator responds
			{Text: "Both tasks complete."},
		},
	})

	result, _ := env.RunAgent(t, "orchestrator", "do tasks A and B")

	// Should have 3 sessions total: orchestrator + 2 children
	sessions, _ := env.Sessions.List()
	if len(sessions) < 3 {
		t.Fatalf("expected >= 3 sessions, got %d", len(sessions))
	}

	parent, _ := env.Sessions.GetMeta(result.SessionID)
	if len(parent.ChildIDs) != 2 {
		t.Fatalf("parent should have 2 children, got %d", len(parent.ChildIDs))
	}

	// Children should have different agent IDs
	child1, _ := env.Sessions.GetMeta(parent.ChildIDs[0])
	child2, _ := env.Sessions.GetMeta(parent.ChildIDs[1])
	if child1.AgentID == child2.AgentID {
		t.Error("children should have different agent IDs")
	}
}

// =============================================================================
// Category 8: Bus event propagation
// =============================================================================

// Bus events should fire for tool calls during agent runs.
func TestBehavior_BusEvents(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "echo bus test",
				}},
			}},
			{Text: "done"},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to bus before running
	var toolEvents []string
	var mu sync.Mutex
	busCh := env.Bus.Subscribe(ctx, "tool.call")

	go func() {
		for ev := range busCh {
			mu.Lock()
			toolEvents = append(toolEvents, ev.Topic)
			mu.Unlock()
		}
	}()

	env.RunAgent(t, "orchestrator", "echo bus test")

	// Small delay to let bus deliver
	time.Sleep(50 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(toolEvents) == 0 {
		t.Error("bus should have received tool.call event")
	}
}

// =============================================================================
// Category 9: EA behavioral tests — Claire as Executive Assistant
// =============================================================================

// Claire delegates research to the research agent.
func TestBehavior_DelegateToResearch(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Claire delegates to research
			{ToolCalls: []testutil.MockToolCall{
				{Name: "agent", ID: "tc-1", Input: map[string]any{
					"agent":  "research",
					"prompt": "Find information about Go generics best practices",
				}},
			}},
			// Research agent searches the web
			{ToolCalls: []testutil.MockToolCall{
				{Name: "web_search", ID: "tc-2", Input: map[string]any{
					"query": "Go generics best practices 2026",
				}},
			}},
			// Research agent returns findings
			{Text: "Found several resources on Go generics best practices."},
			// Claire summarizes
			{Text: "The research agent found information on Go generics best practices."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Research Go generics best practices")

	if !hasEvent(events, agent.EventSubAgentStarted, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "research"
	}) {
		t.Error("should delegate to research agent")
	}

	if !hasEvent(events, agent.EventSubAgentCompleted, func(ev agent.StreamEvent) bool {
		return ev.AgentID == "research"
	}) {
		t.Error("research agent should complete")
	}
}

// Claire creates a new agent using eclaire_manage.
func TestBehavior_ClaireCreatesAgent(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "eclaire_manage", ID: "tc-1", Input: map[string]any{
					"operation":         "agent_create",
					"agent_id":          "qa-tester",
					"agent_name":        "QA Tester",
					"agent_description": "Runs test suites",
					"agent_role":        "complex",
					"agent_tools":       []any{"shell", "read", "write"},
					"agent_soul":        "You are a QA testing specialist.",
				}},
			}},
			{Text: "Created the qa-tester agent."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Create a QA tester agent")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Error("should use eclaire_manage tool")
	}

	// Agent files should exist on disk
	agentYaml := filepath.Join(dir, "agents", "qa-tester", "agent.yaml")
	if _, err := os.Stat(agentYaml); os.IsNotExist(err) {
		t.Error("agent.yaml should have been created")
	}
	soulMd := filepath.Join(dir, "agents", "qa-tester", "workspace", "SOUL.md")
	if _, err := os.Stat(soulMd); os.IsNotExist(err) {
		t.Error("SOUL.md should have been created")
	}
}

// Claire creates a new skill using eclaire_manage.
func TestBehavior_ClaireCreatesSkill(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "eclaire_manage", ID: "tc-1", Input: map[string]any{
					"operation":         "skill_create",
					"skill_name":        "commit",
					"skill_description": "Create conventional commits",
					"skill_body":        "When creating a commit, use conventional commit format.",
				}},
			}},
			{Text: "Created the commit skill."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Create a skill for conventional commits")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Error("should use eclaire_manage tool")
	}

	skillPath := filepath.Join(dir, "skills", "commit", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Error("SKILL.md should have been created")
	}
}

// Claire adds a cron entry using eclaire_manage — routes through unified job store.
func TestBehavior_ClaireManagesCron(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "eclaire_manage", ID: "tc-1", Input: map[string]any{
					"operation":     "cron_add",
					"cron_id":       "disk-check",
					"cron_schedule": "0 * * * *",
					"cron_agent":    "sysadmin",
					"cron_prompt":   "Check disk usage and report if over 90%",
				}},
			}},
			{Text: "Added the disk-check cron job."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Add an hourly cron job to check disk usage")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Error("should use eclaire_manage tool")
	}

	// Job should exist in the unified store (cron_add routes through job_add)
	if env.JobStore.Count() == 0 {
		t.Error("job store should have at least one entry after cron_add")
	}
}

// Memory written in one run is readable in the next.
func TestBehavior_MemoryPersistence(t *testing.T) {
	dir := t.TempDir()

	// Run 1: write memory
	env1 := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			{ToolCalls: []testutil.MockToolCall{
				{Name: "memory_write", ID: "tc-1", Input: map[string]any{
					"content": "User prefers vim keybindings",
					"type":    "curated",
				}},
			}},
			{Text: "Saved to memory."},
		},
	})
	env1.RunAgent(t, "orchestrator", "Remember that I prefer vim keybindings")

	// Verify memory file exists
	memPath := filepath.Join(dir, "workspace", "MEMORY.md")
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist: %v", err)
	}
	if !strings.Contains(string(data), "vim keybindings") {
		t.Error("memory should contain the preference")
	}
}

// Permission denied when using ReadOnly mode.
func TestBehavior_PermissionDeny(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Agent tries to use shell (TrustModify) in ReadOnly mode
			{ToolCalls: []testutil.MockToolCall{
				{Name: "shell", ID: "tc-1", Input: map[string]any{
					"command": "echo test",
				}},
			}},
			// Agent gets the error and responds
			{Text: "Permission denied for shell."},
		},
	})

	a, _ := env.Registry.Get("orchestrator")
	_, events := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:        "orchestrator",
		Agent:          a,
		Prompt:         "Run echo test",
		PermissionMode: tool.PermissionReadOnly,
	})

	// Should have a tool result with permission denied
	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "Permission denied")
	}) {
		t.Error("shell tool should be denied in ReadOnly mode")
	}
}

// Workspace boundary prevents writing outside allowed directories.
func TestBehavior_WorkspaceBoundaryDeny(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewTestEnv(dir, &testutil.MockModel{
		Responses: []testutil.MockResponse{
			// Agent tries to write outside workspace
			{ToolCalls: []testutil.MockToolCall{
				{Name: "write", ID: "tc-1", Input: map[string]any{
					"path":    "/etc/malicious",
					"content": "bad content",
				}},
			}},
			{Text: "Write was blocked."},
		},
	})

	_, events := env.RunAgent(t, "orchestrator", "Write to /etc/malicious")

	if !hasEvent(events, agent.EventToolResult, func(ev agent.StreamEvent) bool {
		return strings.Contains(ev.Output, "denied") || strings.Contains(ev.Output, "outside")
	}) {
		t.Error("write to /etc/malicious should be denied by workspace boundary")
	}

	// File should NOT exist
	if _, err := os.Stat("/etc/malicious"); err == nil {
		t.Error("/etc/malicious should not have been created")
	}
}
