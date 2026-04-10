package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"charm.land/fantasy"
)

type shellInput struct {
	Command         string `json:"command" jsonschema:"description=The shell command to execute"`
	CWD             string `json:"cwd,omitempty" jsonschema:"description=Working directory (optional)"`
	Timeout         int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (default 120)"`
	RunInBackground bool   `json:"run_in_background,omitempty" jsonschema:"description=Run command in background, returns job ID for later retrieval"`
}

// ShellTool creates the shell execution tool.
func ShellTool() Tool {
	return NewTool("shell", "Execute a shell command and return its output. Use run_in_background for long-running commands.", TrustDangerous, "shell",
		func(ctx context.Context, input shellInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.RunInBackground {
				jobID := Jobs.Start(input.Command, input.CWD)
				return fantasy.ToolResponse{Content: fmt.Sprintf("Background job started: %s\nUse job_output tool with job_id=%q to check output.", jobID, jobID)}, nil
			}

			timeout := 120 * time.Second
			if input.Timeout > 0 {
				timeout = time.Duration(input.Timeout) * time.Second
			}

			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
			if input.CWD != "" {
				cmd.Dir = input.CWD
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			result := stdout.String()
			if stderr.Len() > 0 {
				result += "\nSTDERR:\n" + stderr.String()
			}

			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					result += fmt.Sprintf("\n[TIMEOUT] Command exceeded %ds timeout. "+
						"Consider: use a more specific command, add flags to limit output, "+
						"or increase timeout.", int(timeout.Seconds()))
				} else if exitErr, ok := err.(*exec.ExitError); ok {
					result += fmt.Sprintf("\nExit code: %d", exitErr.ExitCode())
				} else {
					result += fmt.Sprintf("\nError: %v", err)
				}
			}

			return fantasy.ToolResponse{Content: result}, nil
		},
	)
}
