package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"

	"charm.land/fantasy"
	"github.com/google/uuid"
)

// BackgroundJob tracks an async command.
type BackgroundJob struct {
	ID      string
	Command string
	CWD     string
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	done    chan struct{}
	err     error
	cmd     *exec.Cmd
}

// Done returns true if the job has finished.
func (j *BackgroundJob) Done() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

// Output returns the current stdout+stderr output.
func (j *BackgroundJob) Output() string {
	out := j.stdout.String()
	if j.stderr.Len() > 0 {
		out += "\nSTDERR:\n" + j.stderr.String()
	}
	if j.Done() && j.err != nil {
		out += fmt.Sprintf("\nError: %v", j.err)
	}
	return out
}

// JobManager manages background jobs.
type JobManager struct {
	jobs map[string]*BackgroundJob
	mu   sync.Mutex
}

// Jobs is the global job manager.
var Jobs = &JobManager{
	jobs: make(map[string]*BackgroundJob),
}

// Start launches a background command and returns a job ID.
func (m *JobManager) Start(command, cwd string) string {
	id := uuid.NewString()[:8]
	job := &BackgroundJob{
		ID:      id,
		Command: command,
		CWD:     cwd,
		done:    make(chan struct{}),
	}

	cmd := exec.Command("bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdout = &job.stdout
	cmd.Stderr = &job.stderr
	job.cmd = cmd

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	go func() {
		job.err = cmd.Run()
		close(job.done)
	}()

	return id
}

// Get returns a job by ID.
func (m *JobManager) Get(id string) (*BackgroundJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// Kill terminates a background job.
func (m *JobManager) Kill(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("job %q not found", id)
	}

	if j.cmd != nil && j.cmd.Process != nil {
		return j.cmd.Process.Kill()
	}
	return nil
}

// JobOutputTool creates the background job output retrieval tool.
func JobOutputTool() Tool {
	return NewTool("job_output", "Get output from a background job", TrustReadOnly, "shell",
		func(ctx context.Context, input struct {
			JobID string `json:"job_id" jsonschema:"description=Background job ID"`
		}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			job, ok := Jobs.Get(input.JobID)
			if !ok {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: job %q not found", input.JobID)}, nil
			}

			status := "running"
			if job.Done() {
				status = "completed"
			}

			output := job.Output()

			return fantasy.ToolResponse{Content: fmt.Sprintf("Job %s (%s):\n%s", job.ID, status, output)}, nil
		},
	)
}

// JobKillTool creates the background job termination tool.
func JobKillTool() Tool {
	return NewTool("job_kill", "Terminate a background job", TrustDangerous, "shell",
		func(ctx context.Context, input struct {
			JobID string `json:"job_id" jsonschema:"description=Background job ID to kill"`
		}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := Jobs.Kill(input.JobID); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			return fantasy.ToolResponse{Content: fmt.Sprintf("Killed job %s", input.JobID)}, nil
		},
	)
}
