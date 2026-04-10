package tool

import (
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	s := ShellTool()
	r.Register(s)

	got, ok := r.Get("shell")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Info().Name != "shell" {
		t.Errorf("got name %q", got.Info().Name)
	}
	if got.TrustTier() != TrustDangerous {
		t.Errorf("got tier %d, want %d", got.TrustTier(), TrustDangerous)
	}
	if got.Category() != "shell" {
		t.Errorf("got category %q", got.Category())
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	r.Register(ReadTool())
	r.Register(FetchTool())

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("got %d tools, want 3", len(all))
	}
	// Should be sorted by name
	if all[0].Info().Name > all[1].Info().Name {
		t.Error("tools should be sorted by name")
	}
}

func TestRegistryForAgent(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())
	r.Register(ReadTool())
	r.Register(WriteTool())
	r.Register(FetchTool())

	// Specific tools
	tools := r.ForAgent("coder", []string{"shell", "read"})
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	// Empty required = all tools
	all := r.ForAgent("coder", nil)
	if len(all) != 4 {
		t.Fatalf("got %d tools, want 4", len(all))
	}
}

func TestRegistryOverride(t *testing.T) {
	r := NewRegistry()
	r.Register(ShellTool())

	// Default tier for shell is Dangerous
	if r.EffectiveTier("any", "shell") != TrustDangerous {
		t.Error("default shell tier should be Dangerous")
	}

	// Override for specific agent
	r.SetOverride("auto-fixer", "shell", TrustReadOnly)
	if r.EffectiveTier("auto-fixer", "shell") != TrustReadOnly {
		t.Error("overridden tier should be ReadOnly")
	}
	// Other agents still have default
	if r.EffectiveTier("other", "shell") != TrustDangerous {
		t.Error("non-overridden agent should still be Dangerous")
	}
}

func TestRegistryEffectiveTierUnknown(t *testing.T) {
	r := NewRegistry()
	if r.EffectiveTier("any", "nonexistent") != TrustDangerous {
		t.Error("unknown tool should be Dangerous")
	}
}
