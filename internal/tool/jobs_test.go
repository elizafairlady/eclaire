package tool

import (
	"testing"
	"time"
)

func TestJobManagerStartAndGet(t *testing.T) {
	mgr := &JobManager{jobs: make(map[string]*BackgroundJob)}
	id := mgr.Start("echo hello", "")

	if id == "" {
		t.Fatal("job ID should not be empty")
	}

	job, ok := mgr.Get(id)
	if !ok {
		t.Fatal("job not found")
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for !job.Done() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if !job.Done() {
		t.Fatal("job should be done")
	}

	output := job.Output()
	if output == "" {
		t.Error("output should not be empty")
	}
}

func TestJobManagerKill(t *testing.T) {
	mgr := &JobManager{jobs: make(map[string]*BackgroundJob)}
	id := mgr.Start("sleep 60", "")

	time.Sleep(100 * time.Millisecond)
	if err := mgr.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	job, _ := mgr.Get(id)
	deadline := time.Now().Add(5 * time.Second)
	for !job.Done() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !job.Done() {
		t.Error("killed job should be done")
	}
}

func TestJobManagerGetNotFound(t *testing.T) {
	mgr := &JobManager{jobs: make(map[string]*BackgroundJob)}
	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent job")
	}
}

func TestJobManagerKillNotFound(t *testing.T) {
	mgr := &JobManager{jobs: make(map[string]*BackgroundJob)}
	err := mgr.Kill("nonexistent")
	if err == nil {
		t.Error("should error on kill of nonexistent job")
	}
}
