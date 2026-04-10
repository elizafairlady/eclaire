package agent

import (
	"strings"
	"testing"
)

func TestBuiltinAgents(t *testing.T) {
	agents := BuiltinAgents()
	if len(agents) != 5 {
		t.Fatalf("got %d built-in agents, want 5", len(agents))
	}

	ids := map[string]bool{}
	for _, a := range agents {
		if a.ID() == "" {
			t.Error("agent ID should not be empty")
		}
		if ids[a.ID()] {
			t.Errorf("duplicate agent ID: %s", a.ID())
		}
		ids[a.ID()] = true

		if a.Name() == "" {
			t.Errorf("agent %s: Name should not be empty", a.ID())
		}
		if a.Description() == "" {
			t.Errorf("agent %s: Description should not be empty", a.ID())
		}
		if a.Role() == "" {
			t.Errorf("agent %s: Role should not be empty", a.ID())
		}
		if len(a.RequiredTools()) == 0 {
			t.Errorf("agent %s: should have at least one tool", a.ID())
		}

		ba, ok := a.(interface{ SystemPrompt() string })
		if !ok {
			t.Errorf("agent %s: should have SystemPrompt method", a.ID())
			continue
		}
		if ba.SystemPrompt() == "" {
			t.Errorf("agent %s: SystemPrompt should not be empty", a.ID())
		}

		bi, ok := a.(interface{ IsBuiltIn() bool })
		if !ok || !bi.IsBuiltIn() {
			t.Errorf("agent %s: should be built-in", a.ID())
		}
	}
}

func TestOrchestratorHasAgentTool(t *testing.T) {
	o := OrchestratorAgent()
	tools := o.RequiredTools()
	found := false
	for _, tool := range tools {
		if tool == "agent" {
			found = true
		}
	}
	if !found {
		t.Error("orchestrator should have 'agent' tool for delegation")
	}
}

func TestOrchestratorHasTaskStatusTool(t *testing.T) {
	o := OrchestratorAgent()
	tools := o.RequiredTools()
	found := false
	for _, tool := range tools {
		if tool == "task_status" {
			found = true
		}
	}
	if !found {
		t.Error("orchestrator should have 'task_status' tool")
	}
}

func TestCodingAgentHasDevTools(t *testing.T) {
	c := CodingAgent()
	tools := c.RequiredTools()
	required := []string{"shell", "read", "write", "edit", "glob", "grep"}
	for _, r := range required {
		found := false
		for _, t := range tools {
			if t == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("coding agent missing tool: %s", r)
		}
	}
}

func TestConfigAgentIsLocal(t *testing.T) {
	c := ConfigAgent()
	if c.Role() != RoleSimple {
		t.Errorf("config agent role = %q, want simple (local model)", c.Role())
	}
}

func TestOrchestratorUsesOrchestratorRole(t *testing.T) {
	o := OrchestratorAgent()
	if o.Role() != RoleOrchestrator {
		t.Errorf("orchestrator role = %q, want orchestrator (frontier model)", o.Role())
	}
}

func TestOrchestratorIsClaire(t *testing.T) {
	o := OrchestratorAgent()
	if o.Name() != "Claire" {
		t.Errorf("orchestrator Name = %q, want Claire", o.Name())
	}

	ba := o.(interface{ EmbeddedWorkspace() map[string]string })
	ws := ba.EmbeddedWorkspace()

	soul := ws[FileSoul]
	if !strings.Contains(soul, "Claire") {
		t.Error("orchestrator SOUL.md should contain Claire")
	}
	if !strings.Contains(soul, "Executive Assistant") {
		t.Error("orchestrator SOUL.md should mention Executive Assistant")
	}
}

func TestOrchestratorHasBootMD(t *testing.T) {
	o := OrchestratorAgent()
	ba := o.(interface{ EmbeddedWorkspace() map[string]string })
	ws := ba.EmbeddedWorkspace()

	boot := ws[FileBoot]
	if boot == "" {
		t.Fatal("orchestrator should have BOOT.md in embedded workspace")
	}
	if !strings.Contains(boot, "Startup") {
		t.Error("BOOT.md should contain startup checklist")
	}
}

func TestOrchestratorHasHeartbeatMD(t *testing.T) {
	o := OrchestratorAgent()
	ba := o.(interface{ EmbeddedWorkspace() map[string]string })
	ws := ba.EmbeddedWorkspace()

	hb := ws[FileHeartbeat]
	if hb == "" {
		t.Fatal("orchestrator should have HEARTBEAT.md in embedded workspace")
	}
	if !strings.Contains(hb, "Periodic") {
		t.Error("HEARTBEAT.md should contain periodic check instructions")
	}
}

func TestOrchestratorHasUserProfile(t *testing.T) {
	o := OrchestratorAgent()
	ba := o.(interface{ EmbeddedWorkspace() map[string]string })
	ws := ba.EmbeddedWorkspace()

	user := ws[FileUser]
	if user == "" {
		t.Fatal("orchestrator should have USER.md in embedded workspace")
	}
	if !strings.Contains(user, "Owner Profile") {
		t.Error("USER.md should be an owner profile template")
	}
}
