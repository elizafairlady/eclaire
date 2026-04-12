package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFlowStoreSaveGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	store, err := NewFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}

	run := &FlowRun{
		ID:        "flow_test_1",
		Status:    FlowRunning,
		CreatedAt: time.Now(),
		FlowDef:   FlowDef{ID: "audit", Name: "Audit"},
		Input:     "test input",
	}

	if err := store.Save(run); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get("flow_test_1")
	if !ok {
		t.Fatal("expected to find saved flow run")
	}
	if got.Status != FlowRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
	if got.FlowDef.Name != "Audit" {
		t.Errorf("flow name = %q", got.FlowDef.Name)
	}
}

func TestFlowStoreList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	store, _ := NewFlowStore(path)

	now := time.Now()
	store.Save(&FlowRun{ID: "a", CreatedAt: now.Add(-time.Hour), Status: FlowCompleted})
	store.Save(&FlowRun{ID: "b", CreatedAt: now, Status: FlowRunning})

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("got %d runs, want 2", len(list))
	}
	// Most recent first
	if list[0].ID != "b" {
		t.Errorf("first = %q, want b (most recent)", list[0].ID)
	}
}

func TestFlowStoreClearStaleRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	store, _ := NewFlowStore(path)

	store.Save(&FlowRun{ID: "running1", Status: FlowRunning, CreatedAt: time.Now()})
	store.Save(&FlowRun{ID: "done1", Status: FlowCompleted, CreatedAt: time.Now()})
	store.Save(&FlowRun{ID: "running2", Status: FlowRunning, CreatedAt: time.Now()})

	cleared := store.ClearStaleRunning()
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2", cleared)
	}

	r1, _ := store.Get("running1")
	if r1.Status != FlowFailed {
		t.Errorf("running1 status = %q, want failed", r1.Status)
	}
	if r1.Error == "" {
		t.Error("should have stale error message")
	}

	d1, _ := store.Get("done1")
	if d1.Status != FlowCompleted {
		t.Error("completed run should not be affected")
	}
}

func TestFlowStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	store, _ := NewFlowStore(path)

	store.Save(&FlowRun{ID: "persist1", Status: FlowCompleted, CreatedAt: time.Now()})

	// Load into a new store
	store2, err := NewFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store2.Get("persist1")
	if !ok {
		t.Fatal("run not found after reload")
	}
	if got.Status != FlowCompleted {
		t.Errorf("status = %q after reload", got.Status)
	}
}

func TestFlowStoreGetMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	store, _ := NewFlowStore(path)
	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent run")
	}
}
