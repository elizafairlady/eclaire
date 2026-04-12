package tool

import (
	"context"
	"testing"
	"time"
)

func TestExecutorRunEcho(t *testing.T) {
	e := &ShellExecutor{}
	ctx := context.Background()

	r := e.Run(ctx, "echo hello", "")
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", r.ExitCode)
	}
}

func TestExecutorRunFailure(t *testing.T) {
	e := &ShellExecutor{}
	ctx := context.Background()

	r := e.Run(ctx, "exit 42", "")
	if r.Err == nil {
		t.Fatal("expected error for exit 42")
	}
	if r.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", r.ExitCode)
	}
}

func TestExecutorRunTimeout(t *testing.T) {
	e := &ShellExecutor{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := e.Run(ctx, "sleep 10", "")
	if r.Err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecutorCountsExecutions(t *testing.T) {
	e := &ShellExecutor{}
	before := e.ExecCount()

	e.Run(context.Background(), "true", "")
	e.Run(context.Background(), "true", "")

	after := e.ExecCount()
	if after-before != 2 {
		t.Errorf("exec count increased by %d, want 2", after-before)
	}
}

func TestExecutorStartBackground(t *testing.T) {
	e := &ShellExecutor{}
	cmd := e.StartBackground("echo bg", "")
	if cmd == nil {
		t.Fatal("StartBackground returned nil")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestFormatResult(t *testing.T) {
	r := ExecResult{Stdout: "ok\n", Stderr: "warn\n"}
	s := r.FormatResult(120)
	if s != "ok\n\nSTDERR:\nwarn\n" {
		t.Errorf("FormatResult = %q", s)
	}
}
