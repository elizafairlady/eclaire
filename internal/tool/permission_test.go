package tool

import (
	"testing"
)

func TestPermissionCheckerReadOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(ReadTool())
	pc := NewPermissionChecker(r)

	if pc.CheckWithMode("any", "read", nil, PermissionWriteOnly) != DecisionAllow {
		t.Error("ReadOnly tools should be auto-allowed in WriteOnly mode")
	}
}

func TestPermissionCheckerDangerousInAllowMode(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	// In PermissionAllow mode, dangerous tools are auto-allowed
	if pc.CheckWithMode("agent1", "shell", nil, PermissionAllow) != DecisionAllow {
		t.Error("Dangerous tools should be auto-allowed in PermissionAllow mode")
	}
}

func TestPermissionCheckerDifferentAgents(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	pc.Approve("agent1", "shell")

	// In PermissionAllow mode, all agents pass
	if pc.CheckWithMode("agent2", "shell", nil, PermissionAllow) != DecisionAllow {
		t.Error("should be allowed in PermissionAllow mode")
	}
}

func TestPermissionCheckerDangerousWithDangerousParams(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	// Shell is TrustDangerous. In PermissionAllow mode, everything is allowed
	if pc.CheckWithMode("agent1", "shell", nil, PermissionAllow) != DecisionAllow {
		t.Error("dangerous tool should be allowed in PermissionAllow mode")
	}
	params := map[string]any{"command": "rm -rf /"}
	if pc.CheckWithMode("agent1", "shell", params, PermissionAllow) != DecisionAllow {
		t.Error("in PermissionAllow mode, even dangerous params are allowed")
	}

	// Under WriteOnly mode, all dangerous tools prompt (user decides)
	if pc.CheckWithMode("agent1", "shell", params, PermissionWriteOnly) != DecisionPrompt {
		t.Error("dangerous commands should prompt in WriteOnly mode")
	}
	if pc.CheckWithMode("agent1", "shell", nil, PermissionWriteOnly) != DecisionPrompt {
		t.Error("normal shell should prompt in WriteOnly mode")
	}
}

func TestPermissionModeAllow(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	r.Register(ReadTool())
	pc := NewPermissionChecker(r)

	if pc.CheckWithMode("a", "read", nil, PermissionAllow) != DecisionAllow {
		t.Error("PermissionAllow should allow read")
	}
	if pc.CheckWithMode("a", "shell", nil, PermissionAllow) != DecisionAllow {
		t.Error("PermissionAllow should allow shell")
	}
}

func TestPermissionModeReadOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	r.Register(ReadTool())
	r.Register(WriteTool())
	pc := NewPermissionChecker(r)

	if pc.CheckWithMode("a", "read", nil, PermissionReadOnly) != DecisionAllow {
		t.Error("ReadOnly mode should allow read tool")
	}
	if pc.CheckWithMode("a", "write", nil, PermissionReadOnly) != DecisionDeny {
		t.Error("ReadOnly mode should deny write tool")
	}
	if pc.CheckWithMode("a", "shell", nil, PermissionReadOnly) != DecisionDeny {
		t.Error("ReadOnly mode should deny shell tool")
	}
}

func TestPermissionModeWriteOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	r.Register(ReadTool())
	r.Register(WriteTool())
	pc := NewPermissionChecker(r)

	if pc.CheckWithMode("a", "read", nil, PermissionWriteOnly) != DecisionAllow {
		t.Error("WriteOnly mode should allow read")
	}
	if pc.CheckWithMode("a", "write", nil, PermissionWriteOnly) != DecisionAllow {
		t.Error("WriteOnly mode should allow write (Modify tier)")
	}
	// Shell is now TrustDangerous by default — should prompt (not deny)
	if pc.CheckWithMode("a", "shell", nil, PermissionWriteOnly) != DecisionPrompt {
		t.Error("WriteOnly mode should prompt (not deny) dangerous shell")
	}
}

func TestWorkspaceBoundaryAllowed(t *testing.T) {
	roots := []string{"/home/user/project", "/home/user/.eclaire"}

	ok, _ := CheckWorkspaceBoundary("write", `{"path":"/home/user/project/file.go"}`, roots)
	if !ok {
		t.Error("write inside workspace should be allowed")
	}

	ok, _ = CheckWorkspaceBoundary("edit", `{"file_path":"/home/user/.eclaire/workspace/SOUL.md"}`, roots)
	if !ok {
		t.Error("edit inside eclaire dir should be allowed")
	}
}

func TestWorkspaceBoundaryDenied(t *testing.T) {
	roots := []string{"/home/user/project"}

	ok, reason := CheckWorkspaceBoundary("write", `{"path":"/etc/passwd"}`, roots)
	if ok {
		t.Error("write to /etc/passwd should be denied")
	}
	if reason == "" {
		t.Error("should have a reason")
	}
}

func TestWorkspaceBoundaryTraversal(t *testing.T) {
	roots := []string{"/home/user/project"}

	ok, _ := CheckWorkspaceBoundary("write", `{"path":"/home/user/project/../../etc/passwd"}`, roots)
	if ok {
		t.Error("path traversal should be caught")
	}
}

func TestWorkspaceBoundaryNonWriteTool(t *testing.T) {
	roots := []string{"/home/user/project"}

	ok, _ := CheckWorkspaceBoundary("read", `{"path":"/etc/passwd"}`, roots)
	if !ok {
		t.Error("read tool should not be boundary-checked")
	}
}

func TestWorkspaceBoundaryNoPath(t *testing.T) {
	roots := []string{"/home/user/project"}

	ok, _ := CheckWorkspaceBoundary("write", `{"content":"hello"}`, roots)
	if !ok {
		t.Error("no path field should pass through")
	}
}

func TestAddSubcommandBinaries(t *testing.T) {
	// Custom binary should not have subcommands by default
	if hasSubcommands("mytool") {
		t.Fatal("mytool should not have subcommands by default")
	}
	AddSubcommandBinaries([]string{"mytool"})
	if !hasSubcommands("mytool") {
		t.Error("mytool should have subcommands after AddSubcommandBinaries")
	}
	// Cleanup
	delete(subcommandBinaries, "mytool")
}

func TestHasSubcommandsDefaults(t *testing.T) {
	// Verify some defaults are present
	for _, bin := range []string{"go", "git", "docker", "npm", "cargo", "kubectl"} {
		if !hasSubcommands(bin) {
			t.Errorf("%q should have subcommands", bin)
		}
	}
	// And some that should not
	for _, bin := range []string{"echo", "cat", "grep", "ls"} {
		if hasSubcommands(bin) {
			t.Errorf("%q should NOT have subcommands", bin)
		}
	}
}

// PermissionWrapper tests removed — PermissionWrapper was dead code (never used in production).
// Workspace boundary checks are now enforced in ConversationRuntime.executeToolCall().
// See permission_security_test.go for the active permission path tests.
