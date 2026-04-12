package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// ToolRateConfig defines rate limits for a specific tool tier.
type ToolRateConfig struct {
	Dangerous int // max calls per minute for dangerous tools (default 30)
	Modify    int // max calls per minute for modify tools (default 60)
}

// DefaultToolRateConfig returns sensible rate limits.
func DefaultToolRateConfig() ToolRateConfig {
	return ToolRateConfig{Dangerous: 30, Modify: 60}
}

// PermissionChecker gates tool execution based on trust tiers.
type PermissionChecker struct {
	registry   *Registry
	approved   map[string]struct{}
	toolRates  map[string]*rateLimiter // key: agentID:toolName
	rateConfig ToolRateConfig
	auditLog   *slog.Logger
	mu         sync.Mutex
}

// NewPermissionChecker creates a new checker.
func NewPermissionChecker(registry *Registry) *PermissionChecker {
	return &PermissionChecker{
		registry:   registry,
		approved:   make(map[string]struct{}),
		toolRates:  make(map[string]*rateLimiter),
		rateConfig: DefaultToolRateConfig(),
	}
}

// SetRateConfig configures per-tool rate limits.
func (p *PermissionChecker) SetRateConfig(cfg ToolRateConfig) {
	p.mu.Lock()
	p.rateConfig = cfg
	p.mu.Unlock()
}

// SetAuditLogger configures the permission audit logger.
func (p *PermissionChecker) SetAuditLogger(l *slog.Logger) {
	p.mu.Lock()
	p.auditLog = l
	p.mu.Unlock()
}

// Check determines whether a tool call should proceed (legacy, uses PermissionAllow).
func (p *PermissionChecker) Check(agentID, toolName string, params any) Decision {
	return p.CheckWithMode(agentID, toolName, params, PermissionAllow)
}

// LoadApprovals bulk-adds approval keys (e.g. from persisted session metadata).
func (p *PermissionChecker) LoadApprovals(keys []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		p.approved[k] = struct{}{}
	}
}

// ApprovedKeys returns a copy of all currently approved keys.
func (p *PermissionChecker) ApprovedKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make([]string, 0, len(p.approved))
	for k := range p.approved {
		keys = append(keys, k)
	}
	return keys
}

// CheckWithMode determines whether a tool call should proceed under the given mode.
func (p *PermissionChecker) CheckWithMode(agentID, toolName string, params any, mode PermissionMode) Decision {
	tier := p.registry.EffectiveTier(agentID, toolName)

	// Check session-level approvals first — if the user said "always", honor it
	key := fmt.Sprintf("%s:%s", agentID, toolName)
	p.mu.Lock()
	_, approved := p.approved[key]
	p.mu.Unlock()

	var decision Decision
	if approved {
		decision = DecisionAllow
	} else {
		// Mode-based gating
		switch mode {
		case PermissionAllow:
			decision = DecisionAllow
		case PermissionReadOnly:
			if tier != TrustReadOnly {
				decision = DecisionDeny
			} else {
				decision = DecisionAllow
			}
		case PermissionWriteOnly:
			switch tier {
			case TrustReadOnly, TrustModify:
				decision = DecisionAllow
			case TrustDangerous:
				decision = DecisionPrompt
			default:
				decision = DecisionPrompt
			}
		default:
			decision = DecisionPrompt
		}
	}

	// Per-tool rate limiting for Dangerous and Modify tiers
	if decision == DecisionAllow && (tier == TrustDangerous || tier == TrustModify) {
		if !p.checkToolRate(key, tier) {
			p.logAudit(agentID, toolName, "rate_limited", tier)
			return DecisionDeny
		}
	}

	p.logAudit(agentID, toolName, decisionName(decision), tier)
	return decision
}

func (p *PermissionChecker) checkToolRate(key string, tier TrustTier) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	rl, ok := p.toolRates[key]
	if !ok {
		limit := p.rateConfig.Modify
		if tier == TrustDangerous {
			limit = p.rateConfig.Dangerous
		}
		rl = newRateLimiter(limit, time.Minute)
		p.toolRates[key] = rl
	}
	return rl.allow()
}

func (p *PermissionChecker) logAudit(agentID, toolName, decision string, tier TrustTier) {
	p.mu.Lock()
	logger := p.auditLog
	p.mu.Unlock()

	if logger != nil {
		logger.Info("permission_check",
			"agent", agentID,
			"tool", toolName,
			"decision", decision,
			"tier", tierName(tier),
		)
	}
}

func decisionName(d Decision) string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionPrompt:
		return "prompt"
	case DecisionDeny:
		return "deny"
	default:
		return "unknown"
	}
}

func tierName(t TrustTier) string {
	switch t {
	case TrustReadOnly:
		return "readonly"
	case TrustModify:
		return "modify"
	case TrustDangerous:
		return "dangerous"
	default:
		return "unknown"
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
// Resolves symlinks to prevent escaping via symlinked directories.
func CheckWorkspaceBoundary(toolName string, input string, roots []string) (bool, string) {
	writeTools := map[string]bool{
		"write": true, "edit": true, "multiedit": true,
		"apply_patch": true, "download": true,
		"shell": true, // CWD parameter can write anywhere
	}
	if !writeTools[toolName] {
		return true, ""
	}

	// For shell tool, check CWD parameter
	paramKeys := []string{"path", "file_path", "file"}
	if toolName == "shell" {
		paramKeys = []string{"cwd"}
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return true, ""
	}

	var targetPath string
	for _, key := range paramKeys {
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

	// Resolve symlinks on the target path. Walk up to the nearest existing
	// parent directory to handle paths where the leaf doesn't exist yet.
	realPath := resolveRealPath(absPath)

	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		realRoot := resolveRealPath(absRoot)

		// Check resolved paths only — unresolved paths would bypass symlink detection
		if strings.HasPrefix(realPath, realRoot+string(os.PathSeparator)) || realPath == realRoot {
			return true, ""
		}
	}

	return false, fmt.Sprintf("write to %q is outside workspace boundaries %v", targetPath, roots)
}

// resolveRealPath resolves symlinks for a path, walking up to the nearest
// existing ancestor if the path doesn't exist yet (e.g., new file creation).
func resolveRealPath(p string) string {
	// Try resolving the full path first
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	// Path doesn't exist — walk up to find the nearest existing parent
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	for dir != p {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, base)
		}
		base = filepath.Join(filepath.Base(dir), base)
		p = dir
		dir = filepath.Dir(dir)
	}
	return p // give up, return as-is
}

