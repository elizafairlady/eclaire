package tool

import (
	"context"
	"testing"

	"charm.land/fantasy"
)

func TestPermissionCheckerReadOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(ReadTool())
	pc := NewPermissionChecker(r)

	if pc.Check("any", "read", nil) != DecisionAllow {
		t.Error("ReadOnly tools should be auto-allowed")
	}
}

func TestPermissionCheckerDangerousInAllowMode(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	// In allow mode (default), dangerous tools are auto-allowed
	if pc.Check("agent1", "shell", nil) != DecisionAllow {
		t.Error("Dangerous tools should be auto-allowed in default PermissionAllow mode")
	}
}

func TestPermissionCheckerDifferentAgents(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	pc.Approve("agent1", "shell")

	// In allow mode, all agents pass
	if pc.Check("agent2", "shell", nil) != DecisionAllow {
		t.Error("should be allowed in default mode")
	}
}

func TestPermissionCheckerDangerousWithDangerousParams(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	pc := NewPermissionChecker(r)

	// Shell is TrustDangerous. In allow mode, everything is allowed
	if pc.Check("agent1", "shell", nil) != DecisionAllow {
		t.Error("dangerous tool should be allowed in default mode")
	}
	params := map[string]any{"command": "rm -rf /"}
	if pc.Check("agent1", "shell", params) != DecisionAllow {
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

func TestPermissionWrapperDeny(t *testing.T) {
	r := NewRegistry()
	r.Register(WriteTool())
	pc := NewPermissionChecker(r)

	inner := WriteTool()
	wrapped := WrapPermission(inner, pc, "test", PermissionReadOnly, nil, nil)

	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{Input: `{"path":"/tmp/test","content":"x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsError {
		t.Error("should be denied in ReadOnly mode")
	}
}

func TestPermissionWrapperAllow(t *testing.T) {
	r := NewRegistry()
	r.Register(ReadTool())
	pc := NewPermissionChecker(r)

	inner := ReadTool()
	wrapped := WrapPermission(inner, pc, "test", PermissionAllow, nil, nil)

	// Just verify it doesn't panic and delegates properly
	// The actual tool may fail on missing file, but permission should pass
	resp, _ := wrapped.Run(context.Background(), fantasy.ToolCall{Input: `{"path":"/nonexistent"}`})
	// ReadOnly tool in Allow mode should not get permission denied
	if resp.IsError && resp.Content == "Permission denied" {
		t.Error("should not be permission denied")
	}
}

func TestPermissionWrapperNilChecker(t *testing.T) {
	inner := ReadTool()
	wrapped := WrapPermission(inner, nil, "test", PermissionAllow, nil, nil)
	if wrapped != inner {
		t.Error("nil checker should return inner tool unchanged")
	}
}

func TestPermissionWrapperBoundaryDeny(t *testing.T) {
	r := NewRegistry()
	r.Register(WriteTool())
	pc := NewPermissionChecker(r)

	inner := WriteTool()
	roots := []string{"/home/user/project"}
	wrapped := WrapPermission(inner, pc, "test", PermissionAllow, roots, nil)

	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{Input: `{"path":"/etc/malicious","content":"x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsError {
		t.Error("should be denied by workspace boundary")
	}
}
