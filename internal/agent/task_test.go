package agent

import "testing"

func TestTaskRegistryCreate(t *testing.T) {
	r := NewTaskRegistry()
	task := r.Create("t1", "coding", "write code")

	if task.ID != "t1" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Status != TaskCreated {
		t.Errorf("Status = %q, want created", task.Status)
	}
	if task.AgentID != "coding" {
		t.Errorf("AgentID = %q", task.AgentID)
	}

	got, ok := r.Get("t1")
	if !ok {
		t.Fatal("should find task")
	}
	if got.Prompt != "write code" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
}

func TestTaskRegistryUpdateStatus(t *testing.T) {
	r := NewTaskRegistry()
	r.Create("t1", "coding", "test")

	if err := r.UpdateStatus("t1", TaskRunning, "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := r.Get("t1")
	if task.Status != TaskRunning {
		t.Errorf("Status = %q, want running", task.Status)
	}

	if err := r.UpdateStatus("t1", TaskCompleted, "done", ""); err != nil {
		t.Fatal(err)
	}
	task, _ = r.Get("t1")
	if task.Output != "done" {
		t.Errorf("Output = %q", task.Output)
	}

	if err := r.UpdateStatus("nonexistent", TaskFailed, "", "oops"); err == nil {
		t.Error("should error for nonexistent task")
	}
}

func TestTaskRegistryList(t *testing.T) {
	r := NewTaskRegistry()
	r.Create("t1", "coding", "a")
	r.Create("t2", "research", "b")
	r.Create("t3", "coding", "c")

	all := r.List()
	if len(all) != 3 {
		t.Errorf("got %d tasks, want 3", len(all))
	}

	// Set flow and test ListByFlow
	r.SetFlowID("t1", "flow-1")
	r.SetFlowID("t3", "flow-1")

	byFlow := r.ListByFlow("flow-1")
	if len(byFlow) != 2 {
		t.Errorf("got %d tasks for flow-1, want 2", len(byFlow))
	}
}

func TestTaskRegistryActive(t *testing.T) {
	r := NewTaskRegistry()
	r.Create("t1", "a", "x")
	r.Create("t2", "b", "y")

	r.UpdateStatus("t1", TaskRunning, "", "")
	r.UpdateStatus("t2", TaskCompleted, "done", "")

	active := r.Active()
	if len(active) != 1 {
		t.Errorf("got %d active, want 1", len(active))
	}
	if active[0].ID != "t1" {
		t.Errorf("active task ID = %q", active[0].ID)
	}
}
