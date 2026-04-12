package tool

import (
	"context"
	"fmt"
	"time"

	"charm.land/fantasy"
)

type shellInput struct {
	Command         string `json:"command" jsonschema:"description=The shell command to execute"`
	CWD             string `json:"cwd,omitempty" jsonschema:"description=Working directory (optional)"`
	Timeout         int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (default 120\\, max 600)"`
	RunInBackground bool   `json:"run_in_background,omitempty" jsonschema:"description=Run command in background\\, returns job ID for later retrieval"`
}

// ShellTool creates the shell execution tool.
func ShellTool() Tool {
	return NewTool("shell", "Execute a shell command and return its output. Use run_in_background for long-running commands.", TrustDangerous, "shell",
		func(ctx context.Context, input shellInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			policy := DefaultExecutor.effectivePolicy()

			if input.RunInBackground {
				jobID := Jobs.Start(input.Command, input.CWD)
				return fantasy.ToolResponse{Content: fmt.Sprintf("Background job started: %s\nUse job_output tool with job_id=%q to check output.", jobID, jobID)}, nil
			}

			timeout := 120
			if input.Timeout > 0 {
				timeout = input.Timeout
			}
			// Clamp to policy max
			timeout = policy.ClampTimeout(timeout)

			ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
			defer cancel()

			r := DefaultExecutor.Run(ctx, input.Command, input.CWD)
			return fantasy.ToolResponse{Content: r.FormatResult(timeout)}, nil
		},
	)
}
