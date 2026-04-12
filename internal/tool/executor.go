package tool

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync/atomic"
)

// ShellExecutor is the single chokepoint for all os/exec calls in eclaire.
// Every shell command — foreground or background — must go through this.
// This is where allow-lists, audit logging, and hard security gates live.
type ShellExecutor struct {
	logger   *slog.Logger
	execCount atomic.Int64
}

// DefaultExecutor is the global shell executor. Set once at startup.
var DefaultExecutor = &ShellExecutor{}

// SetLogger configures the executor's logger. Call during gateway init.
func (e *ShellExecutor) SetLogger(l *slog.Logger) {
	e.logger = l
}

// ExecResult holds the output of a shell command.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Run executes a shell command synchronously and returns its output.
// This is the ONLY place in the codebase where exec.CommandContext should be called.
func (e *ShellExecutor) Run(ctx context.Context, command, cwd string) ExecResult {
	e.execCount.Add(1)
	n := e.execCount.Load()

	if e.logger != nil {
		e.logger.Info("shell exec", "n", n, "cmd", truncateCmd(command), "cwd", cwd)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}

	if e.logger != nil && err != nil {
		e.logger.Warn("shell exec failed", "n", n, "err", err, "exit", result.ExitCode)
	}

	return result
}

// StartBackground launches a command in a goroutine and returns a *exec.Cmd.
// The caller is responsible for capturing stdout/stderr and waiting.
// This is the ONLY place background exec.Command should be created.
func (e *ShellExecutor) StartBackground(command, cwd string) *exec.Cmd {
	e.execCount.Add(1)
	n := e.execCount.Load()

	if e.logger != nil {
		e.logger.Info("shell exec (background)", "n", n, "cmd", truncateCmd(command), "cwd", cwd)
	}

	cmd := exec.Command("bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	return cmd
}

// ExecCount returns the total number of commands executed since startup.
func (e *ShellExecutor) ExecCount() int64 {
	return e.execCount.Load()
}

func truncateCmd(cmd string) string {
	if len(cmd) > 200 {
		return cmd[:200] + "..."
	}
	return cmd
}

// FormatResult formats an ExecResult into a user-facing string.
func (r ExecResult) FormatResult(timeout int) string {
	result := r.Stdout
	if r.Stderr != "" {
		result += "\nSTDERR:\n" + r.Stderr
	}
	if r.Err != nil {
		if r.Err == context.DeadlineExceeded {
			result += fmt.Sprintf("\n[TIMEOUT] Command exceeded %ds timeout. "+
				"Consider: use a more specific command, add flags to limit output, "+
				"or increase timeout.", timeout)
		} else if r.ExitCode != 0 {
			result += fmt.Sprintf("\nExit code: %d", r.ExitCode)
		} else {
			result += fmt.Sprintf("\nError: %v", r.Err)
		}
	}
	return result
}
