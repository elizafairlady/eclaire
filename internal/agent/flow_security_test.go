package agent

import (
	"testing"

	"github.com/elizafairlady/eclaire/internal/tool"
)

func TestRunConfigZeroValuePermission(t *testing.T) {
	cfg := RunConfig{}
	if cfg.PermissionMode != tool.PermissionWriteOnly {
		t.Fatalf("RunConfig{} zero-value PermissionMode = %d, want PermissionWriteOnly (%d)",
			cfg.PermissionMode, tool.PermissionWriteOnly)
	}
}

func TestPermissionAllowRequiresExplicitSet(t *testing.T) {
	// PermissionAllow should require explicit assignment — it must never be a default
	cfg := RunConfig{PermissionMode: tool.PermissionAllow}
	if cfg.PermissionMode != tool.PermissionAllow {
		t.Fatal("explicit PermissionAllow should work")
	}
	if cfg.PermissionMode == 0 {
		t.Fatal("PermissionAllow must not be zero")
	}
}
