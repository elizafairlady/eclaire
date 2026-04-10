package agent

import (
	"context"
	"testing"
)

// stubAgent is a minimal Agent for testing.
type stubAgent struct {
	id       string
	name     string
	role     Role
	bindings []Binding
	tools    []string
}

func (a *stubAgent) ID() string                                              { return a.id }
func (a *stubAgent) Name() string                                            { return a.name }
func (a *stubAgent) Description() string                                     { return a.name }
func (a *stubAgent) Init(_ context.Context, _ AgentDeps) error               { return nil }
func (a *stubAgent) Shutdown(_ context.Context) error                        { return nil }
func (a *stubAgent) Handle(_ context.Context, _ Request) (Response, error)   { return Response{}, nil }
func (a *stubAgent) Stream(_ context.Context, _ Request) (<-chan StreamPart, error) {
	return nil, nil
}
func (a *stubAgent) Role() Role              { return a.role }
func (a *stubAgent) Bindings() []Binding     { return a.bindings }
func (a *stubAgent) RequiredTools() []string { return a.tools }
func (a *stubAgent) CredentialScope() string { return a.id }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	a := &stubAgent{id: "test-1", name: "Test", role: RoleSimple}
	if err := r.Register(a); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("test-1")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.ID() != "test-1" {
		t.Errorf("got ID %q, want %q", got.ID(), "test-1")
	}
}

func TestRegistryDuplicateRegister(t *testing.T) {
	r := NewRegistry()

	a := &stubAgent{id: "dup", name: "Dup"}
	r.Register(a)
	if err := r.Register(a); err == nil {
		t.Error("expected error on duplicate register")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent agent")
	}
}

func TestRegistryResolve(t *testing.T) {
	r := NewRegistry()

	r.Register(&stubAgent{
		id:   "low-priority",
		role: RoleSimple,
		bindings: []Binding{
			{Type: BindTask, Pattern: "*", Priority: 1},
		},
	})
	r.Register(&stubAgent{
		id:   "high-priority",
		role: RoleComplex,
		bindings: []Binding{
			{Type: BindTask, Pattern: "*", Priority: 10},
		},
	})

	agent, err := r.Resolve("", "anything")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if agent.ID() != "high-priority" {
		t.Errorf("got %q, want %q", agent.ID(), "high-priority")
	}
}

func TestRegistryResolveNoMatch(t *testing.T) {
	r := NewRegistry()

	r.Register(&stubAgent{
		id:       "specific",
		bindings: []Binding{{Type: BindTask, Pattern: "deploy"}},
	})

	_, err := r.Resolve("", "review")
	if err == nil {
		t.Error("expected error when no binding matches")
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()

	r.Register(&stubAgent{id: "b-agent", name: "B"})
	r.Register(&stubAgent{id: "a-agent", name: "A"})

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("got %d agents, want 2", len(all))
	}
	// Should be sorted by ID
	if all[0].ID != "a-agent" {
		t.Errorf("first agent should be a-agent, got %s", all[0].ID)
	}
}

func TestRegistrySetStatus(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubAgent{id: "s1"})

	r.SetStatus("s1", StatusRunning)
	all := r.All()
	if all[0].Status != StatusRunning {
		t.Errorf("got status %q, want %q", all[0].Status, StatusRunning)
	}
}

func TestRegistryUpsertNew(t *testing.T) {
	r := NewRegistry()
	replaced := r.Upsert(&stubAgent{id: "new", name: "New"})
	if replaced {
		t.Error("should not be replaced (new agent)")
	}
	a, ok := r.Get("new")
	if !ok {
		t.Fatal("should find agent after upsert")
	}
	if a.Name() != "New" {
		t.Errorf("Name = %q", a.Name())
	}
}

func TestRegistryUpsertReplace(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubAgent{id: "test", name: "Original"})
	replaced := r.Upsert(&stubAgent{id: "test", name: "Updated"})
	if !replaced {
		t.Error("should be replaced")
	}
	a, _ := r.Get("test")
	if a.Name() != "Updated" {
		t.Errorf("Name = %q, want Updated", a.Name())
	}
}

func TestRegistryUpsertPreservesStatus(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubAgent{id: "test"})
	r.SetStatus("test", StatusRunning)

	r.Upsert(&stubAgent{id: "test", name: "Updated"})

	all := r.All()
	for _, a := range all {
		if a.ID == "test" && a.Status != StatusRunning {
			t.Errorf("status = %q, want running (preserved)", a.Status)
		}
	}
}

func TestRegistryHasBackgroundAgents(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubAgent{id: "normal"})

	if r.HasBackgroundAgents() {
		t.Error("no background agents registered")
	}
}
