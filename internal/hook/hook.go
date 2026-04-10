package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Event types matching Claw Code's HookEvent enum.
type Event string

const (
	PreToolUse         Event = "PreToolUse"
	PostToolUse        Event = "PostToolUse"
	PostToolUseFailure Event = "PostToolUseFailure"
)

// Definition is a single hook from configuration.
type Definition struct {
	Event   Event  `yaml:"event" json:"event"`
	Matcher string `yaml:"matcher" json:"matcher"`
	Command string `yaml:"command" json:"command"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// Payload is the JSON sent to hook commands on stdin.
type Payload struct {
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
	ToolInput     string `json:"tool_input"`
	ToolOutput    string `json:"tool_output,omitempty"`
	ToolError     string `json:"tool_error,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
}

// Result is the JSON expected back from hook commands on stdout.
type Result struct {
	Denied       bool   `json:"denied,omitempty"`
	DenyMessage  string `json:"deny_message,omitempty"`
	UpdatedInput string `json:"updated_input,omitempty"`
	Message      string `json:"message,omitempty"`
}

// Runner executes hook commands.
type Runner struct {
	hooks []Definition
}

// NewRunner creates a hook runner from configuration.
func NewRunner(hooks []Definition) *Runner {
	if len(hooks) == 0 {
		return nil
	}
	return &Runner{hooks: hooks}
}

// RunPre executes all matching pre-tool-use hooks.
// Returns: updated input (empty = no change), denied, deny message, error.
func (r *Runner) RunPre(ctx context.Context, toolName, input, agentID, sessionID string) (string, bool, string, error) {
	if r == nil {
		return "", false, "", nil
	}

	payload := Payload{
		HookEventName: string(PreToolUse),
		ToolName:      toolName,
		ToolInput:     input,
		AgentID:       agentID,
		SessionID:     sessionID,
	}

	var updatedInput string
	for _, def := range r.hooks {
		if def.Event != PreToolUse || !matchTool(def.Matcher, toolName) {
			continue
		}
		result, err := r.execute(ctx, def, payload)
		if err != nil {
			return "", false, "", fmt.Errorf("pre-hook %q: %w", def.Command, err)
		}
		if result.Denied {
			msg := result.DenyMessage
			if msg == "" {
				msg = "Tool use denied by hook"
			}
			return "", true, msg, nil
		}
		if result.UpdatedInput != "" {
			updatedInput = result.UpdatedInput
		}
	}
	return updatedInput, false, "", nil
}

// RunPost executes all matching post-tool-use hooks.
// Returns additional message (empty = none) and error.
func (r *Runner) RunPost(ctx context.Context, toolName, input, output, agentID, sessionID string) (string, error) {
	if r == nil {
		return "", nil
	}

	payload := Payload{
		HookEventName: string(PostToolUse),
		ToolName:      toolName,
		ToolInput:     input,
		ToolOutput:    output,
		AgentID:       agentID,
		SessionID:     sessionID,
	}

	var messages []string
	for _, def := range r.hooks {
		if def.Event != PostToolUse || !matchTool(def.Matcher, toolName) {
			continue
		}
		result, err := r.execute(ctx, def, payload)
		if err != nil {
			return "", fmt.Errorf("post-hook %q: %w", def.Command, err)
		}
		if result.Message != "" {
			messages = append(messages, result.Message)
		}
	}

	if len(messages) == 0 {
		return "", nil
	}
	msg := messages[0]
	for _, m := range messages[1:] {
		msg += "\n" + m
	}
	return msg, nil
}

// RunPostFailure executes all matching post-tool-use-failure hooks.
func (r *Runner) RunPostFailure(ctx context.Context, toolName, input, errMsg, agentID, sessionID string) error {
	if r == nil {
		return nil
	}

	payload := Payload{
		HookEventName: string(PostToolUseFailure),
		ToolName:      toolName,
		ToolInput:     input,
		ToolError:     errMsg,
		AgentID:       agentID,
		SessionID:     sessionID,
	}

	for _, def := range r.hooks {
		if def.Event != PostToolUseFailure || !matchTool(def.Matcher, toolName) {
			continue
		}
		if _, err := r.execute(ctx, def, payload); err != nil {
			return fmt.Errorf("post-failure-hook %q: %w", def.Command, err)
		}
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, def Definition, payload Payload) (*Result, error) {
	timeout := 10 * time.Second
	if def.Timeout != "" {
		if d, err := time.ParseDuration(def.Timeout); err == nil {
			timeout = d
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", def.Command)
	cmd.Stdin = bytes.NewReader(payloadJSON)
	cmd.Env = append(os.Environ(),
		"HOOK_EVENT="+payload.HookEventName,
		"HOOK_TOOL_NAME="+payload.ToolName,
		"HOOK_TOOL_INPUT="+payload.ToolInput,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hook %q: %w (stderr: %s)", def.Command, err, stderr.String())
	}

	if stdout.Len() == 0 {
		return &Result{}, nil
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		// Non-JSON output treated as message
		return &Result{Message: stdout.String()}, nil
	}
	return &result, nil
}

// matchTool checks if a tool name matches a glob pattern.
func matchTool(pattern, toolName string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}
	matched, _ := filepath.Match(pattern, toolName)
	return matched
}
