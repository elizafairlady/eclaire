package agent

import (
	"fmt"
	"sync"
	"time"
)

// TaskStatus tracks a task's lifecycle.
type TaskStatus string

const (
	TaskCreated   TaskStatus = "created"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskStopped   TaskStatus = "stopped"
)

// Task is a tracked unit of work.
type Task struct {
	ID        string     `json:"id"`
	FlowID    string     `json:"flow_id,omitempty"`
	AgentID   string     `json:"agent_id"`
	Prompt    string     `json:"prompt"`
	Status    TaskStatus `json:"status"`
	SessionID string     `json:"session_id,omitempty"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TaskRegistry tracks all background work in-memory.
type TaskRegistry struct {
	tasks map[string]*Task
	mu    sync.RWMutex
}

// NewTaskRegistry creates an empty task registry.
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{tasks: make(map[string]*Task)}
}

// Create adds a new task with status Created.
func (r *TaskRegistry) Create(id, agentID, prompt string) *Task {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	t := &Task{
		ID:        id,
		AgentID:   agentID,
		Prompt:    prompt,
		Status:    TaskCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.tasks[id] = t
	return t
}

// Get returns a task by ID.
func (r *TaskRegistry) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	return t, ok
}

// UpdateStatus transitions a task's status.
func (r *TaskRegistry) UpdateStatus(id string, status TaskStatus, output, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if output != "" {
		t.Output = output
	}
	if errMsg != "" {
		t.Error = errMsg
	}
	return nil
}

// SetFlowID associates a task with a flow.
func (r *TaskRegistry) SetFlowID(taskID, flowID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[taskID]; ok {
		t.FlowID = flowID
	}
}

// SetSessionID records the session for a task.
func (r *TaskRegistry) SetSessionID(taskID, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[taskID]; ok {
		t.SessionID = sessionID
	}
}

// List returns all tasks, most recent first.
func (r *TaskRegistry) List() []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, t)
	}
	return out
}

// ListByFlow returns tasks for a specific flow.
func (r *TaskRegistry) ListByFlow(flowID string) []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*Task
	for _, t := range r.tasks {
		if t.FlowID == flowID {
			out = append(out, t)
		}
	}
	return out
}

// Active returns tasks that are currently running.
func (r *TaskRegistry) Active() []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*Task
	for _, t := range r.tasks {
		if t.Status == TaskRunning {
			out = append(out, t)
		}
	}
	return out
}
