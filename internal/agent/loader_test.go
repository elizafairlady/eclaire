package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-agent.yaml")

	yaml := `id: test-agent
name: "Test Agent"
description: "A test agent"
role: complex
bindings:
  - type: task
    pattern: "test"
    priority: 5
tools:
  - shell
  - fs
system_prompt: "You are a test agent."
credential_scope: "test"
`
	os.WriteFile(path, []byte(yaml), 0o644)

	a, err := LoadAgentFile(path)
	if err != nil {
		t.Fatalf("LoadAgentFile: %v", err)
	}

	if a.ID() != "test-agent" {
		t.Errorf("ID = %q, want %q", a.ID(), "test-agent")
	}
	if a.Name() != "Test Agent" {
		t.Errorf("Name = %q, want %q", a.Name(), "Test Agent")
	}
	if a.Role() != RoleComplex {
		t.Errorf("Role = %q, want %q", a.Role(), RoleComplex)
	}
	if len(a.Bindings()) != 1 {
		t.Fatalf("Bindings len = %d, want 1", len(a.Bindings()))
	}
	if a.Bindings()[0].Priority != 5 {
		t.Errorf("Binding priority = %d, want 5", a.Bindings()[0].Priority)
	}
	if len(a.RequiredTools()) != 2 {
		t.Errorf("RequiredTools len = %d, want 2", len(a.RequiredTools()))
	}
	if a.CredentialScope() != "test" {
		t.Errorf("CredentialScope = %q, want %q", a.CredentialScope(), "test")
	}

	// Test system prompt
	ya, ok := a.(*yamlAgent)
	if !ok {
		t.Fatal("expected *yamlAgent")
	}
	if ya.SystemPrompt() == "" {
		t.Error("SystemPrompt should not be empty")
	}
}

func TestLoadAgentFileDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")

	yaml := `name: "Minimal"
`
	os.WriteFile(path, []byte(yaml), 0o644)

	a, err := LoadAgentFile(path)
	if err != nil {
		t.Fatalf("LoadAgentFile: %v", err)
	}

	// ID should default to filename
	if a.ID() != "minimal" {
		t.Errorf("ID = %q, want %q", a.ID(), "minimal")
	}
	if a.Role() != RoleSimple {
		t.Errorf("Role = %q, want %q", a.Role(), RoleSimple)
	}
	if a.CredentialScope() != "minimal" {
		t.Errorf("CredentialScope = %q, want %q", a.CredentialScope(), "minimal")
	}
}

func TestLoadAgentsDir(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "agent-a.yaml"), []byte(`id: agent-a
name: A
`), 0o644)
	os.WriteFile(filepath.Join(dir, "agent-b.yml"), []byte(`id: agent-b
name: B
`), 0o644)
	os.WriteFile(filepath.Join(dir, "not-yaml.txt"), []byte(`ignored`), 0o644)

	agents, err := LoadAgentsDir(dir)
	if err != nil {
		t.Fatalf("LoadAgentsDir: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("got %d agents, want 2", len(agents))
	}
}

func TestLoadAgentsDirNotExist(t *testing.T) {
	agents, err := LoadAgentsDir("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil agents, got %v", agents)
	}
}

func TestLoadAgentFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	os.WriteFile(path, []byte(`{{{invalid yaml`), 0o644)

	_, err := LoadAgentFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadAgentDir(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "my-agent")
	wsDir := filepath.Join(agentDir, "workspace")
	os.MkdirAll(wsDir, 0o755)

	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(`id: my-agent
name: "My Agent"
role: complex
tools:
  - shell
  - read
model: "qwen3:32b"
`), 0o644)
	os.WriteFile(filepath.Join(wsDir, "SOUL.md"), []byte("I am my-agent"), 0o644)
	os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte("Operating instructions"), 0o644)

	a, err := LoadAgentDir(agentDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}

	if a.ID() != "my-agent" {
		t.Errorf("ID = %q, want %q", a.ID(), "my-agent")
	}
	if a.Name() != "My Agent" {
		t.Errorf("Name = %q, want %q", a.Name(), "My Agent")
	}
	if a.Role() != RoleComplex {
		t.Errorf("Role = %q, want %q", a.Role(), RoleComplex)
	}

	ya := a.(*yamlAgent)

	// Check workspace files loaded
	ws := ya.EmbeddedWorkspace()
	if ws["SOUL.md"] != "I am my-agent" {
		t.Errorf("SOUL.md = %q, want %q", ws["SOUL.md"], "I am my-agent")
	}
	if ws["AGENTS.md"] != "Operating instructions" {
		t.Errorf("AGENTS.md = %q, want %q", ws["AGENTS.md"], "Operating instructions")
	}

	// Check IsBuiltIn
	if ya.IsBuiltIn() {
		t.Error("yamlAgent should not be built-in")
	}

	// Check sourceDir
	if ya.sourceDir != agentDir {
		t.Errorf("sourceDir = %q, want %q", ya.sourceDir, agentDir)
	}
}

func TestLoadAgentsDirMixed(t *testing.T) {
	dir := t.TempDir()

	// Flat YAML agent
	os.WriteFile(filepath.Join(dir, "flat.yaml"), []byte(`id: flat-agent
name: Flat
`), 0o644)

	// Directory-based agent
	agentDir := filepath.Join(dir, "dir-agent")
	os.MkdirAll(filepath.Join(agentDir, "workspace"), 0o755)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(`id: dir-agent
name: Dir
`), 0o644)
	os.WriteFile(filepath.Join(agentDir, "workspace", "SOUL.md"), []byte("soul"), 0o644)

	// Directory without agent.yaml — should be skipped
	os.MkdirAll(filepath.Join(dir, "not-an-agent"), 0o755)

	agents, err := LoadAgentsDir(dir)
	if err != nil {
		t.Fatalf("LoadAgentsDir: %v", err)
	}

	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}

	ids := make(map[string]bool)
	for _, a := range agents {
		ids[a.ID()] = true
	}
	if !ids["flat-agent"] {
		t.Error("missing flat-agent")
	}
	if !ids["dir-agent"] {
		t.Error("missing dir-agent")
	}
}

func TestYamlAgentModelOverride(t *testing.T) {
	var a Agent = &yamlAgent{
		def: AgentDef{
			IDField: "test",
			Model:   "qwen3:32b",
		},
	}

	co, ok := a.(ConfigOverrides)
	if !ok {
		t.Fatal("yamlAgent should implement ConfigOverrides")
	}
	if co.ModelOverride() != "qwen3:32b" {
		t.Errorf("ModelOverride() = %q, want %q", co.ModelOverride(), "qwen3:32b")
	}

	// Empty model returns empty string
	var a2 Agent = &yamlAgent{def: AgentDef{IDField: "test2"}}
	co2 := a2.(ConfigOverrides)
	if co2.ModelOverride() != "" {
		t.Errorf("ModelOverride() = %q, want empty", co2.ModelOverride())
	}
}
