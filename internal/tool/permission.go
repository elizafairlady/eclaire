package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"charm.land/fantasy"
)

// Decision is the outcome of a permission check.
type Decision int

const (
	DecisionAllow  Decision = iota
	DecisionPrompt          // require human approval
	DecisionDeny
)

// PermissionMode controls how strictly tools are gated.
type PermissionMode int

const (
	PermissionAllow     PermissionMode = iota // all tools auto-allowed (default)
	PermissionWriteOnly                        // ReadOnly + Modify, Dangerous denied
	PermissionReadOnly                         // only ReadOnly tools
)

// Approver requests human approval for tool execution.
// Blocks until the user responds.
type Approver interface {
	Request(ctx context.Context, agentID, action, description string, details any) (ApprovalResult, error)
}

// ApprovalResult is the human's response to an approval request.
type ApprovalResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// PermissionChecker gates tool execution based on trust tiers.
type PermissionChecker struct {
	registry  *Registry
	approved  map[string]struct{}
	mu        sync.Mutex
}

// NewPermissionChecker creates a new checker.
func NewPermissionChecker(registry *Registry) *PermissionChecker {
	return &PermissionChecker{
		registry: registry,
		approved: make(map[string]struct{}),
	}
}

// Check determines whether a tool call should proceed (legacy, uses PermissionAllow).
func (p *PermissionChecker) Check(agentID, toolName string, params any) Decision {
	return p.CheckWithMode(agentID, toolName, params, PermissionAllow)
}

// CheckWithMode determines whether a tool call should proceed under the given mode.
func (p *PermissionChecker) CheckWithMode(agentID, toolName string, params any, mode PermissionMode) Decision {
	tier := p.registry.EffectiveTier(agentID, toolName)

	// Mode-based gating runs first
	switch mode {
	case PermissionReadOnly:
		if tier != TrustReadOnly {
			return DecisionDeny
		}
	case PermissionWriteOnly:
		if tier == TrustDangerous {
			return DecisionPrompt // prompt for dangerous tools, don't outright deny
		}
	}

	// PermissionAllow and PermissionWriteOnly auto-approve matching tiers
	switch tier {
	case TrustReadOnly:
		return DecisionAllow
	case TrustModify:
		if mode == PermissionAllow || mode == PermissionWriteOnly {
			return DecisionAllow
		}
		key := fmt.Sprintf("%s:%s", agentID, toolName)
		p.mu.Lock()
		_, ok := p.approved[key]
		p.mu.Unlock()
		if ok {
			return DecisionAllow
		}
		return DecisionPrompt
	case TrustDangerous:
		if mode == PermissionAllow {
			return DecisionAllow
		}
		if toolName == "shell" && isDangerous(params) {
			return DecisionDeny
		}
		return DecisionPrompt
	default:
		return DecisionPrompt
	}
}

// Approve marks a tool as approved for the session.
func (p *PermissionChecker) Approve(agentID, toolName string) {
	key := fmt.Sprintf("%s:%s", agentID, toolName)
	p.mu.Lock()
	p.approved[key] = struct{}{}
	p.mu.Unlock()
}

// CheckWorkspaceBoundary verifies that a file-writing tool's target path
// is within one of the allowed root directories.
func CheckWorkspaceBoundary(toolName string, input string, roots []string) (bool, string) {
	writeTools := map[string]bool{
		"write": true, "edit": true, "multiedit": true,
		"apply_patch": true, "download": true,
	}
	if !writeTools[toolName] {
		return true, ""
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return true, ""
	}

	var targetPath string
	for _, key := range []string{"path", "file_path", "file"} {
		if p, ok := params[key].(string); ok && p != "" {
			targetPath = p
			break
		}
	}
	if targetPath == "" {
		return true, ""
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return false, fmt.Sprintf("cannot resolve path: %v", err)
	}
	absPath = filepath.Clean(absPath)

	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) || absPath == absRoot {
			return true, ""
		}
	}

	return false, fmt.Sprintf("write to %q is outside workspace boundaries %v", targetPath, roots)
}

// PermissionWrapper wraps a tool with permission enforcement and interactive approval.
type PermissionWrapper struct {
	inner    fantasy.AgentTool
	checker  *PermissionChecker
	agentID  string
	mode     PermissionMode
	roots    []string
	approver Approver
	mu       sync.Mutex // protects roots for dynamic extension
}

// WrapPermission creates a permission-enforcing wrapper around a tool.
func WrapPermission(inner fantasy.AgentTool, checker *PermissionChecker, agentID string, mode PermissionMode, roots []string, approver Approver) fantasy.AgentTool {
	if checker == nil {
		return inner
	}
	return &PermissionWrapper{
		inner:    inner,
		checker:  checker,
		agentID:  agentID,
		mode:     mode,
		roots:    roots,
		approver: approver,
	}
}

func (w *PermissionWrapper) Info() fantasy.ToolInfo                    { return w.inner.Info() }
func (w *PermissionWrapper) ProviderOptions() fantasy.ProviderOptions  { return w.inner.ProviderOptions() }
func (w *PermissionWrapper) SetProviderOptions(o fantasy.ProviderOptions) { w.inner.SetProviderOptions(o) }

func (w *PermissionWrapper) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	toolName := w.inner.Info().Name

	// Permission mode check
	decision := w.checker.CheckWithMode(w.agentID, toolName, nil, w.mode)
	if decision == DecisionDeny {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("Permission denied: tool %q not allowed in current permission mode", toolName),
		), nil
	}
	if decision == DecisionPrompt {
		if w.approver != nil {
			result, err := w.approver.Request(ctx, w.agentID, "tool_use",
				fmt.Sprintf("Agent %q wants to use tool %q", w.agentID, toolName),
				map[string]any{"tool": toolName, "input": call.Input})
			if err != nil || !result.Approved {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("Permission denied: tool %q was not approved by user", toolName),
				), nil
			}
			// User approved — fall through
		} else {
			return fantasy.NewTextErrorResponse(
				fmt.Sprintf("Permission denied: tool %q requires approval but no approver available", toolName),
			), nil
		}
	}

	// Workspace boundary check for file-writing tools
	w.mu.Lock()
	roots := make([]string, len(w.roots))
	copy(roots, w.roots)
	w.mu.Unlock()

	if ok, reason := CheckWorkspaceBoundary(toolName, call.Input, roots); !ok {
		if w.approver != nil {
			result, err := w.approver.Request(ctx, w.agentID, "write_outside_workspace",
				reason,
				map[string]any{"tool": toolName, "input": call.Input})
			if err != nil || !result.Approved {
				return fantasy.NewTextErrorResponse(reason), nil
			}
			// User approved — extend roots for this session
			w.extendRoots(call.Input)
		} else {
			return fantasy.NewTextErrorResponse(reason), nil
		}
	}

	return w.inner.Run(ctx, call)
}

// extendRoots adds the directory of the approved write to the session roots.
func (w *PermissionWrapper) extendRoots(input string) {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return
	}
	for _, key := range []string{"path", "file_path", "file"} {
		if p, ok := params[key].(string); ok && p != "" {
			absPath, err := filepath.Abs(p)
			if err != nil {
				return
			}
			dir := filepath.Dir(filepath.Clean(absPath))
			w.mu.Lock()
			w.roots = append(w.roots, dir)
			w.mu.Unlock()
			return
		}
	}
}

func isDangerous(params any) bool {
	if m, ok := params.(map[string]any); ok {
		if cmd, ok := m["command"].(string); ok {
			dangerous := []string{"rm -rf", "sudo", "mkfs", "dd if=", "> /dev/"}
			for _, d := range dangerous {
				if strings.Contains(cmd, d) {
					return true
				}
			}
		}
	}
	return false
}
