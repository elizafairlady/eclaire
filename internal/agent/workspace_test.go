package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceLoadEmbedded(t *testing.T) {
	dir := t.TempDir()
	loader := NewWorkspaceLoader(filepath.Join(dir, "workspace"), filepath.Join(dir, "agents"), "")

	embedded := map[string]string{
		"SOUL.md":   "I am the orchestrator",
		"AGENTS.md": "Delegation rules here",
	}

	ws, err := loader.Load("orchestrator", embedded)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if ws.Get("SOUL.md") != "I am the orchestrator" {
		t.Errorf("SOUL.md = %q", ws.Get("SOUL.md"))
	}
	if ws.Get("AGENTS.md") != "Delegation rules here" {
		t.Errorf("AGENTS.md = %q", ws.Get("AGENTS.md"))
	}
	if ws.Get("NONEXISTENT.md") != "" {
		t.Error("nonexistent file should return empty")
	}
}

func TestWorkspaceLayering(t *testing.T) {
	dir := t.TempDir()
	globalWS := filepath.Join(dir, "workspace")
	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(globalWS, 0o700)

	// Write global SOUL.md
	os.WriteFile(filepath.Join(globalWS, "SOUL.md"), []byte("global soul"), 0o644)
	// Write global USER.md
	os.WriteFile(filepath.Join(globalWS, "USER.md"), []byte("global user"), 0o644)

	// Write agent-specific SOUL.md (should override global)
	agentWS := filepath.Join(agentsDir, "coding", "workspace")
	os.MkdirAll(agentWS, 0o700)
	os.WriteFile(filepath.Join(agentWS, "SOUL.md"), []byte("coding soul"), 0o644)

	loader := NewWorkspaceLoader(globalWS, agentsDir, "")
	ws, err := loader.Load("coding", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Agent-specific should override global
	if ws.Get("SOUL.md") != "coding soul" {
		t.Errorf("SOUL.md = %q, want 'coding soul'", ws.Get("SOUL.md"))
	}
	// Global USER.md should be inherited
	if ws.Get("USER.md") != "global user" {
		t.Errorf("USER.md = %q, want 'global user'", ws.Get("USER.md"))
	}
}

func TestWorkspaceProjectOverlay(t *testing.T) {
	dir := t.TempDir()
	globalWS := filepath.Join(dir, "global", "workspace")
	agentsDir := filepath.Join(dir, "global", "agents")
	projectDir := filepath.Join(dir, "project", ".eclaire")
	os.MkdirAll(globalWS, 0o700)
	os.MkdirAll(filepath.Join(projectDir, "workspace"), 0o700)

	os.WriteFile(filepath.Join(globalWS, "AGENTS.md"), []byte("global agents"), 0o644)
	os.WriteFile(filepath.Join(projectDir, "workspace", "AGENTS.md"), []byte("project agents"), 0o644)

	loader := NewWorkspaceLoader(globalWS, agentsDir, projectDir)
	ws, err := loader.Load("orchestrator", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Project should override global
	if ws.Get("AGENTS.md") != "project agents" {
		t.Errorf("AGENTS.md = %q, want 'project agents'", ws.Get("AGENTS.md"))
	}
}

func TestWorkspaceEmbeddedOverriddenByDisk(t *testing.T) {
	dir := t.TempDir()
	globalWS := filepath.Join(dir, "workspace")
	os.MkdirAll(globalWS, 0o700)
	os.WriteFile(filepath.Join(globalWS, "SOUL.md"), []byte("disk soul"), 0o644)

	loader := NewWorkspaceLoader(globalWS, filepath.Join(dir, "agents"), "")
	ws, err := loader.Load("test", map[string]string{"SOUL.md": "embedded soul"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Disk should override embedded
	if ws.Get("SOUL.md") != "disk soul" {
		t.Errorf("SOUL.md = %q, want 'disk soul'", ws.Get("SOUL.md"))
	}
}

func TestWorkspaceMemoryAppend(t *testing.T) {
	dir := t.TempDir()
	globalWS := filepath.Join(dir, "workspace")
	os.MkdirAll(globalWS, 0o700)

	loader := NewWorkspaceLoader(globalWS, filepath.Join(dir, "agents"), "")

	// Append curated memory
	err := loader.AppendMemory("test", "important fact", "curated")
	if err != nil {
		t.Fatalf("AppendMemory curated: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(globalWS, "MEMORY.md"))
	if string(data) != "important fact\n" {
		t.Errorf("MEMORY.md = %q", string(data))
	}

	// Append again
	loader.AppendMemory("test", "another fact", "curated")
	data, _ = os.ReadFile(filepath.Join(globalWS, "MEMORY.md"))
	if string(data) != "important fact\nanother fact\n" {
		t.Errorf("MEMORY.md = %q", string(data))
	}

	// Append daily
	err = loader.AppendMemory("test", "today's note", "daily")
	if err != nil {
		t.Fatalf("AppendMemory daily: %v", err)
	}
}

func TestWorkspaceMemoryAppendInvalidType(t *testing.T) {
	dir := t.TempDir()
	loader := NewWorkspaceLoader(filepath.Join(dir, "ws"), filepath.Join(dir, "agents"), "")
	err := loader.AppendMemory("test", "content", "invalid")
	if err == nil {
		t.Error("expected error for invalid memory type")
	}
}

func TestWorkspaceBootRan(t *testing.T) {
	dir := t.TempDir()
	globalWS := filepath.Join(dir, "workspace")
	os.MkdirAll(globalWS, 0o700)

	loader := NewWorkspaceLoader(globalWS, filepath.Join(dir, "agents"), "")

	if loader.BootRanToday() {
		t.Error("boot should not have run yet")
	}

	loader.MarkBootRan()

	if !loader.BootRanToday() {
		t.Error("boot should be marked as run")
	}
}

func TestWorkspaceGetEmpty(t *testing.T) {
	ws := &Workspace{Files: make(map[string]WorkspaceFile)}
	if ws.Get("anything") != "" {
		t.Error("empty workspace should return empty string")
	}
}

func TestWorkspaceNoDir(t *testing.T) {
	loader := NewWorkspaceLoader("/nonexistent", "/nonexistent", "")
	ws, err := loader.Load("test", nil)
	if err != nil {
		t.Fatalf("Load should not error on missing dirs: %v", err)
	}
	if len(ws.Files) != 0 {
		t.Errorf("should have no files, got %d", len(ws.Files))
	}
}
