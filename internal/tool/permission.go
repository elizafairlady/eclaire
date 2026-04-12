package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	PermissionWriteOnly PermissionMode = iota // ReadOnly + Modify auto-allowed, Dangerous requires approval (SAFE DEFAULT)
	PermissionReadOnly                        // only ReadOnly tools
	PermissionAllow     PermissionMode = 99   // all tools auto-allowed — NEVER use in production
)

// Approver requests human approval for tool execution.
// Blocks until the user responds.
type Approver interface {
	Request(ctx context.Context, agentID, action, description string, details any) (ApprovalResult, error)
}

// ApprovalResult is the human's response to an approval request.
type ApprovalResult struct {
	Approved bool   `json:"approved"`
	Persist  bool   `json:"persist,omitempty"` // true = approve for rest of session ("always")
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

	// Check session-level approvals first ��� if the user said "always", honor it
	key := fmt.Sprintf("%s:%s", agentID, toolName)
	p.mu.Lock()
	_, approved := p.approved[key]
	p.mu.Unlock()
	if approved {
		return DecisionAllow
	}

	// Mode-based gating
	switch mode {
	case PermissionAllow:
		return DecisionAllow
	case PermissionReadOnly:
		if tier != TrustReadOnly {
			return DecisionDeny
		}
		return DecisionAllow
	case PermissionWriteOnly:
		switch tier {
		case TrustReadOnly, TrustModify:
			return DecisionAllow
		case TrustDangerous:
			return DecisionPrompt
		default:
			return DecisionPrompt
		}
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

