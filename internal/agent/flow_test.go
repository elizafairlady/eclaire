package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowDefLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-flow.yaml")
	os.WriteFile(path, []byte(`id: test
name: Test Flow
description: A test flow
steps:
  - name: step1
    agent: coding
    prompt: "Do step 1: {{.Input}}"
  - name: step2
    agent: research
    prompt: "Do step 2 based on: {{.PrevOutput}}"
`), 0o644)

	def, err := LoadFlowFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "test" {
		t.Errorf("ID = %q", def.ID)
	}
	if def.Name != "Test Flow" {
		t.Errorf("Name = %q", def.Name)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("Steps = %d, want 2", len(def.Steps))
	}
	if def.Steps[0].Agent != "coding" {
		t.Errorf("Step[0].Agent = %q", def.Steps[0].Agent)
	}
	if def.Steps[1].Agent != "research" {
		t.Errorf("Step[1].Agent = %q", def.Steps[1].Agent)
	}
}

func TestFlowDefLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")
	os.WriteFile(path, []byte(`steps:
  - name: only
    agent: coding
    prompt: do it
`), 0o644)

	def, err := LoadFlowFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "minimal" {
		t.Errorf("ID should default to filename, got %q", def.ID)
	}
}

func TestFlowDefLoadNoSteps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	os.WriteFile(path, []byte(`id: empty
name: Empty
steps: []
`), 0o644)

	_, err := LoadFlowFile(path)
	if err == nil {
		t.Error("should error for empty steps")
	}
}

func TestLoadFlowsDir(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(`id: a
steps:
  - name: s1
    agent: coding
    prompt: do a
`), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yml"), []byte(`id: b
steps:
  - name: s1
    agent: research
    prompt: do b
`), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a flow"), 0o644)

	flows, err := LoadFlowsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 2 {
		t.Errorf("got %d flows, want 2", len(flows))
	}
}

func TestLoadFlowsDirMissing(t *testing.T) {
	flows, err := LoadFlowsDir("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 0 {
		t.Errorf("got %d flows, want 0", len(flows))
	}
}

func TestTemplatePrompt(t *testing.T) {
	tests := []struct {
		tmpl       string
		input      string
		prevOutput string
		want       string
	}{
		{"Do {{.Input}}", "task A", "", "Do task A"},
		{"Based on: {{.PrevOutput}}", "", "step 1 result", "Based on: step 1 result"},
		{"{{.Input}} then {{.PrevOutput}}", "start", "mid", "start then mid"},
		{"no templates here", "", "", "no templates here"},
	}
	for _, tt := range tests {
		got, err := templatePrompt(tt.tmpl, tt.input, tt.prevOutput)
		if err != nil {
			t.Errorf("templatePrompt(%q): %v", tt.tmpl, err)
			continue
		}
		if !strings.Contains(got, tt.want) {
			t.Errorf("templatePrompt(%q) = %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}
