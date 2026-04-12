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

	shsyntax "mvdan.cc/sh/v3/syntax"
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

// isApproved checks if a tool call is covered by any approved key.
// Supports exact match ("coding:shell") and pattern match ("coding:shell:go").
func (p *PermissionChecker) isApproved(key, toolName string, params any) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Exact tool-level approval (e.g. "coding:shell")
	if _, ok := p.approved[key]; ok {
		return true
	}

	// Pattern-based approval for shell commands (e.g. "coding:shell:go test")
	if toolName == "shell" {
		pattern := ExtractShellPattern(params)
		if pattern != "" {
			// Check exact pattern: "coding:shell:go test"
			if _, ok := p.approved[key+":"+pattern]; ok {
				return true
			}
			// Check binary-only: "coding:shell:go" (covers all subcommands)
			parts := strings.SplitN(pattern, " ", 2)
			if len(parts) == 2 {
				if _, ok := p.approved[key+":"+parts[0]]; ok {
					return true
				}
			}
		}
	}

	return false
}

// ApprovePattern stores a pattern-scoped approval (e.g. "coding:shell:go").
func (p *PermissionChecker) ApprovePattern(agentID, toolName, pattern string) {
	key := fmt.Sprintf("%s:%s:%s", agentID, toolName, pattern)
	p.mu.Lock()
	p.approved[key] = struct{}{}
	p.mu.Unlock()
}

// ExtractShellPattern parses the command from shell tool params and returns
// a pattern like "go test" (binary + subcommand) for pattern-based approvals.
// Falls back to just the binary name if no subcommand.
func ExtractShellPattern(params any) string {
	var input string
	switch v := params.(type) {
	case string:
		input = v
	default:
		return ""
	}
	if input == "" {
		return ""
	}

	var parsed map[string]any
	if json.Unmarshal([]byte(input), &parsed) != nil {
		return ""
	}
	cmd, ok := parsed["command"].(string)
	if !ok || cmd == "" {
		return ""
	}

	return ExtractCommandPattern(cmd)
}

// ExtractCommandPattern extracts "binary subcommand" from a shell command string.
// Examples: "go test ./..." → "go test", "git status" → "git status",
// "ls -la" → "ls", "cd /tmp && go build" → "go build" (last command in chain).
func ExtractCommandPattern(cmd string) string {
	parser := shsyntax.NewParser(shsyntax.KeepComments(false), shsyntax.Variant(shsyntax.LangBash))
	prog, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return ""
	}

	// Collect all simple commands — use the last one (after cd/env preambles)
	var lastBinary, lastSubcmd string
	shsyntax.Walk(prog, func(node shsyntax.Node) bool {
		call, ok := node.(*shsyntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Find the real binary — skip preamble commands (cd, env, sudo, etc.)
		preamble := map[string]bool{"cd": true, "env": true, "sudo": true, "nice": true, "nohup": true, "time": true}
		startIdx := 0
		for startIdx < len(call.Args) {
			word := shellWordToString(call.Args[startIdx])
			if word == "" || !preamble[filepath.Base(word)] {
				break
			}
			startIdx++
		}
		if startIdx >= len(call.Args) {
			return true
		}

		binary := filepath.Base(shellWordToString(call.Args[startIdx]))
		if binary == "" {
			return true
		}

		lastBinary = binary
		lastSubcmd = ""

		// Only extract subcommand for tools that have them.
		// POSIX/common commands that take arguments (not subcommands)
		// should be approved at the binary level only.
		if startIdx+1 < len(call.Args) && hasSubcommands(binary) {
			arg := shellWordToString(call.Args[startIdx+1])
			if arg != "" && !strings.HasPrefix(arg, "-") {
				lastSubcmd = arg
			}
		}
		return false
	})

	if lastBinary == "" {
		return ""
	}
	if lastSubcmd != "" {
		return lastBinary + " " + lastSubcmd
	}
	return lastBinary
}

// shellWordToString extracts a plain string from a shell word (literals only).
func shellWordToString(w *shsyntax.Word) string {
	if w == nil {
		return ""
	}
	var buf strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *shsyntax.Lit:
			buf.WriteString(p.Value)
		case *shsyntax.SglQuoted:
			buf.WriteString(p.Value)
		case *shsyntax.DblQuoted:
			for _, qp := range p.Parts {
				if lit, ok := qp.(*shsyntax.Lit); ok {
					buf.WriteString(lit.Value)
				} else {
					return ""
				}
			}
		default:
			return ""
		}
	}
	return buf.String()
}

// hasSubcommands returns true for tools where the first argument is a subcommand
// (go build, git status, docker run) vs tools where it's just an argument
// (echo hello, cat file.txt, grep pattern).
func hasSubcommands(binary string) bool {
	switch binary {
	case "go", "git", "docker", "podman", "kubectl", "helm",
		"npm", "npx", "yarn", "pnpm", "bun", "deno",
		"cargo", "rustup", "mix", "elixir",
		"pip", "pip3", "poetry", "uv", "conda",
		"apt", "apt-get", "dnf", "yum", "pacman", "emerge", "xbps-install",
		"brew", "port", "snap", "flatpak", "nix",
		"systemctl", "journalctl", "rc-service", "rc-update",
		"openrc", "sv",
		"ip", "ss", "tc", "nmcli", "networkctl",
		"gh", "glab",
		"ecl",
		"make":
		return true
	}
	return false
}

// CheckWithMode determines whether a tool call should proceed under the given mode.
func (p *PermissionChecker) CheckWithMode(agentID, toolName string, params any, mode PermissionMode) Decision {
	tier := p.registry.EffectiveTier(agentID, toolName)

	// Check session-level approvals — exact tool match or pattern match
	key := fmt.Sprintf("%s:%s", agentID, toolName)
	approved := p.isApproved(key, toolName, params)

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

