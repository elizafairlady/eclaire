//go:build live

package agent_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/testutil"
)

// =============================================================================
// Live LLM behavioral tests (run with OPENROUTER_API_KEY)
// =============================================================================

func TestLive_DirectShell(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "orchestrator", "Run this shell command and tell me the output: echo 'eclaire live test'")

	// Agent should have used shell tool
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "shell"
	}) {
		t.Error("orchestrator should use shell tool")
	}

	// Response should mention the output
	if !strings.Contains(strings.ToLower(result.Content), "eclaire") {
		t.Errorf("response should mention output, got: %s", result.Content)
	}
}

func TestLive_ReadFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "test.txt")
	os.WriteFile(target, []byte("The password is swordfish."), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "orchestrator", "Read "+target+" and tell me what it says.")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "read"
	}) {
		t.Error("should use read tool")
	}

	if !strings.Contains(result.Content, "swordfish") {
		t.Errorf("response should mention file content, got: %s", result.Content)
	}
}

func TestLive_WriteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "created.txt")

	env := testutil.NewLiveTestEnv(t, dir)

	env.RunAgent(t, "orchestrator", "Create a file at "+target+" with the content 'Hello from the live test'. Use the write tool.")

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if !strings.Contains(string(data), "Hello from the live test") {
		t.Errorf("file content = %q", string(data))
	}
}

func TestLive_MultiStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "greet.txt")
	os.WriteFile(target, []byte("Hello World"), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	env.RunAgent(t, "orchestrator",
		"Read the file "+target+", change 'World' to 'eclaire', then verify the change by reading it again.")

	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "eclaire") {
		t.Errorf("file should contain 'eclaire' after edit, got: %s", string(data))
	}
}

func TestLive_Delegation(t *testing.T) {
	dir := t.TempDir()

	env := testutil.NewLiveTestEnv(t, dir)

	_, events := env.RunAgent(t, "orchestrator",
		"Delegate to the coding agent: write a Go file at "+filepath.Join(dir, "hello.go")+
			" with a main package that prints 'hello from coding agent'. "+
			"Use the agent tool with agent='coding'.")

	// Should have delegation events
	if !hasEvent(events, agent.EventSubAgentStarted, nil) {
		t.Error("should have sub_agent_started event")
	}
	if !hasEvent(events, agent.EventSubAgentCompleted, nil) {
		t.Error("should have sub_agent_completed event")
	}

	// File should exist
	if _, err := os.Stat(filepath.Join(dir, "hello.go")); os.IsNotExist(err) {
		t.Error("hello.go should have been created by coding agent")
	}
}

// --- Live tests for features added in recent sessions ---

// Test that Claire can create an agent using eclaire_manage with a real LLM.
func TestLive_CreateAgent(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "orchestrator",
		"Use the eclaire_manage tool to create a new agent. "+
			"Set operation to 'agent_create', agent_id to 'live-test-agent', "+
			"agent_name to 'Live Test', agent_role to 'simple', "+
			"and agent_soul to 'You are a test agent created by a live test.'")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "eclaire_manage"
	}) {
		t.Errorf("should have used eclaire_manage tool. Content: %s", result.Content)
	}

	// Verify files created on disk
	agentYaml := filepath.Join(dir, "agents", "live-test-agent", "agent.yaml")
	if _, err := os.Stat(agentYaml); os.IsNotExist(err) {
		t.Error("agent.yaml should have been created")
	}
}

// Test that Claire can write memory and it persists.
func TestLive_Memory(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	env.RunAgent(t, "orchestrator",
		"Use the memory_write tool to save this to curated memory: 'Live test memory entry — eclaire works.'")

	memPath := filepath.Join(dir, "workspace", "MEMORY.md")
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist: %v", err)
	}
	if !strings.Contains(string(data), "eclaire") {
		t.Errorf("memory should contain our entry, got: %s", string(data))
	}
}

// Test that the system prompt includes Claire's identity.
func TestLive_ClaireIdentity(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, _ := env.RunAgent(t, "orchestrator",
		"What is your name? Answer in one word.")

	lower := strings.ToLower(result.Content)
	if !strings.Contains(lower, "claire") {
		t.Errorf("Claire should identify herself. Got: %s", result.Content)
	}
}

// Test that the coding agent can do multi-step work: glob → read → respond.
func TestLive_CodingAgentMultiTool(t *testing.T) {
	dir := t.TempDir()

	// Create a Go file for the coding agent to analyze
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte(`package main

import "fmt"

func greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func main() {
	fmt.Println(greet("world"))
}
`), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "coding",
		"Find the Go files in "+dir+"/src/ using glob, read them, and tell me what the greet function does.")

	// Should have used glob and read
	usedGlob := hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "glob"
	})
	usedRead := hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "read"
	})

	if !usedGlob && !usedRead {
		t.Error("coding agent should use glob or read to find and read files")
	}

	if !strings.Contains(strings.ToLower(result.Content), "greet") {
		t.Errorf("response should discuss the greet function, got: %s", result.Content)
	}
}

// Test that skills appear in the system prompt and the agent can see them.
func TestLive_SkillsVisible(t *testing.T) {
	dir := t.TempDir()

	// Create a skill
	skillDir := filepath.Join(dir, "skills", "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: test-skill
description: A test skill that says hello
---
When this skill is activated, respond with "SKILL_ACTIVATED_OK".
`), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	result, _ := env.RunAgent(t, "orchestrator",
		"List the available skills you can see. Do you see a skill called 'test-skill'? "+
			"If yes, read its SKILL.md file and follow the instructions inside it.")

	lower := strings.ToLower(result.Content)
	if !strings.Contains(lower, "test-skill") && !strings.Contains(lower, "skill_activated") {
		t.Errorf("agent should see or activate the test skill. Got: %s", result.Content)
	}
}

// Test session continuation — run twice with the same session.
func TestLive_SessionContinue(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	// First run
	result1, _ := env.RunAgent(t, "orchestrator",
		"Remember the code word 'pineapple'. Confirm you've noted it.")

	if !strings.Contains(strings.ToLower(result1.Content), "pineapple") {
		t.Logf("first run response: %s", result1.Content)
	}

	// Second run — continue the session
	a, _ := env.Registry.Get("orchestrator")
	events2, _ := env.Sessions.ReadEvents(result1.SessionID)
	history := persist.RebuildMessages(events2)

	result2, _ := env.RunAgentWithConfig(t, agent.RunConfig{
		AgentID:   "orchestrator",
		Agent:     a,
		Prompt:    "What was the code word I asked you to remember?",
		SessionID: result1.SessionID,
		History:   history,
	})

	if !strings.Contains(strings.ToLower(result2.Content), "pineapple") {
		t.Errorf("continued session should remember 'pineapple'. Got: %s", result2.Content)
	}
}

// Test that the sysadmin agent can check system health.
func TestLive_SysadminAgent(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "sysadmin",
		"Check disk usage with 'df -h /' and report the percentage used.")

	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "shell"
	}) {
		t.Error("sysadmin should use shell tool")
	}

	if !strings.Contains(result.Content, "%") {
		t.Errorf("response should contain disk usage percentage. Got: %s", result.Content)
	}
}

// =============================================================================
// Realistic live tests — real user tasks, no hand-holding
// =============================================================================

// Ask Claire to write a real program, compile it, and verify it works.
func TestLive_WriteAndRunProgram(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "orchestrator",
		"Write a Go program in "+dir+" that prints the first 10 fibonacci numbers, one per line. "+
			"Compile and run it to verify the output is correct. "+
			"Tell me the output.")

	// Should have used tools — we don't care which ones, as long as it got the job done
	toolCalls := countEvents(events, agent.EventToolCall) + countEvents(events, agent.EventSubAgentToolCall)
	if toolCalls == 0 {
		t.Error("should have used tools to write/compile/run the program")
	}

	// The output should contain fibonacci numbers (first 10: 0 1 1 2 3 5 8 13 21 34)
	if !strings.Contains(result.Content, "34") {
		t.Errorf("output should contain fib numbers including 34. Got: %s", result.Content)
	}

	// Actually find and run the program ourselves to verify it works
	goFiles, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	if len(goFiles) == 0 {
		goFiles, _ = filepath.Glob(filepath.Join(dir, "**", "*.go"))
	}
	if len(goFiles) == 0 {
		t.Fatal("no .go file was written to disk")
	}

	// Compile it
	buildOut, err := exec.CommandContext(context.Background(), "go", "build", "-o", filepath.Join(dir, "fib"), goFiles[0]).CombinedOutput()
	if err != nil {
		t.Fatalf("program doesn't compile: %v\n%s", err, buildOut)
	}

	// Run it
	runOut, err := exec.CommandContext(context.Background(), filepath.Join(dir, "fib")).CombinedOutput()
	if err != nil {
		t.Fatalf("program doesn't run: %v\n%s", err, runOut)
	}
	output := string(runOut)
	if !strings.Contains(output, "34") {
		t.Errorf("program output should contain 34. Got:\n%s", output)
	}
}

// Ask the coding agent to find and fix a bug in real code.
func TestLive_FindAndFixBug(t *testing.T) {
	dir := t.TempDir()

	// Write a buggy program
	os.WriteFile(filepath.Join(dir, "buggy.go"), []byte(`package main

import "fmt"

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes); i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func main() {
	fmt.Println(reverse("hello"))
}
`), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "coding",
		"The file "+filepath.Join(dir, "buggy.go")+" has an off-by-one error in the reverse function. "+
			"Find the bug, fix it, and verify it works by running the program.")

	// Should have read the file
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "read" || ev.ToolName == "view"
	}) {
		t.Error("coding agent should read the buggy file")
	}

	// Should have edited/written the fix
	if !hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "edit" || ev.ToolName == "write"
	}) {
		t.Error("coding agent should fix the file")
	}

	// Read the fixed file back
	data, err := os.ReadFile(filepath.Join(dir, "buggy.go"))
	if err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
	t.Logf("Fixed file content:\n%s", string(data))

	// Actually compile and run the fixed program
	buildOut, berr := exec.CommandContext(context.Background(), "go", "run", filepath.Join(dir, "buggy.go")).CombinedOutput()
	if berr != nil {
		t.Fatalf("fixed program doesn't compile/run: %v\nOutput: %s\nFile:\n%s", berr, buildOut, data)
	}
	output := strings.TrimSpace(string(buildOut))
	if output != "olleh" {
		t.Errorf("fixed program should output 'olleh', got %q", output)
	}

	t.Logf("Agent response: %s", result.Content)
}

// Ask Claire to investigate what's in a directory and summarize it.
func TestLive_InvestigateProject(t *testing.T) {
	dir := t.TempDir()

	// Set up a small fake project
	os.MkdirAll(filepath.Join(dir, "cmd"), 0o755)
	os.MkdirAll(filepath.Join(dir, "internal", "api"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/myapp\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "api", "handler.go"), []byte("package api\n\nfunc Handle() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My App\nA sample application.\n"), 0o644)

	env := testutil.NewLiveTestEnv(t, dir)

	result, events := env.RunAgent(t, "orchestrator",
		"Look at the project in "+dir+" and tell me: what language is it, what's the module name, "+
			"and what's the directory structure? Use tools to find out — don't guess.")

	// Should have used file exploration tools
	usedTools := hasEvent(events, agent.EventToolCall, func(ev agent.StreamEvent) bool {
		return ev.ToolName == "glob" || ev.ToolName == "read" || ev.ToolName == "ls" || ev.ToolName == "shell"
	})
	if !usedTools {
		// Check sub-agent tools too
		usedTools = hasEvent(events, agent.EventSubAgentToolCall, func(ev agent.StreamEvent) bool {
			return ev.ToolName == "glob" || ev.ToolName == "read" || ev.ToolName == "ls"
		})
	}
	if !usedTools {
		t.Error("should use exploration tools to investigate the project")
	}

	lower := strings.ToLower(result.Content)
	if !strings.Contains(lower, "go") {
		t.Errorf("should identify it as a Go project. Got: %s", result.Content)
	}
	if !strings.Contains(lower, "myapp") && !strings.Contains(lower, "example.com") {
		t.Errorf("should mention the module name. Got: %s", result.Content)
	}
}

// Ask Claire to create a skill and verify the file is well-formed.
func TestLive_SelfImprovementCreateSkill(t *testing.T) {
	dir := t.TempDir()
	env := testutil.NewLiveTestEnv(t, dir)

	result, _ := env.RunAgent(t, "orchestrator",
		"I want you to create a new skill for yourself. "+
			"Use eclaire_manage with operation 'skill_create'. "+
			"The skill should be called 'summarize', with description 'Summarize long documents into key points', "+
			"and the body should contain instructions for how to summarize effectively.")

	// Verify the skill file exists and is parseable
	skillPath := filepath.Join(dir, "skills", "summarize", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("SKILL.md should exist: %v (response: %s)", err, result.Content)
	}

	content := string(data)
	if !strings.Contains(content, "name: summarize") {
		t.Errorf("SKILL.md should have correct frontmatter. Content:\n%s", content)
	}
	if !strings.Contains(content, "Summarize") {
		t.Errorf("SKILL.md should contain description. Content:\n%s", content)
	}

	// Verify it parses as a valid skill
	meta, err := agent.ParseSkillMeta(content)
	if err != nil {
		t.Errorf("SKILL.md should be parseable: %v. Content:\n%s", err, content)
	}
	if meta.Name != "summarize" {
		t.Errorf("skill name = %q, want summarize", meta.Name)
	}
}
