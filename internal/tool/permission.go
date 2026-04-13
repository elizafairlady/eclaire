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

	return extractPatternFromProg(prog)
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

// extractPatternFromProg walks a parsed shell program and extracts the command
// pattern from the FIRST simple command (not last — chains like "git; rm" should
// return "git", not "rm"). Handles bash -c, skips preambles, ignores expansions.
func extractPatternFromProg(prog *shsyntax.File) string {
	if prog == nil || len(prog.Stmts) == 0 {
		return ""
	}
	return extractPatternFromStmt(prog.Stmts[0])
}

func extractPatternFromStmt(stmt *shsyntax.Stmt) string {
	if stmt == nil || stmt.Cmd == nil {
		return ""
	}

	switch cmd := stmt.Cmd.(type) {
	case *shsyntax.CallExpr:
		return extractPatternFromCall(cmd)
	case *shsyntax.BinaryCmd:
		// For chains (&&, ||, ;, |): use the first meaningful command.
		// Skip preamble-only stmts (cd /dir && real_command).
		if pattern := extractPatternFromStmt(cmd.X); pattern != "" {
			return pattern
		}
		return extractPatternFromStmt(cmd.Y)
	case *shsyntax.Subshell:
		if len(cmd.Stmts) > 0 {
			return extractPatternFromStmt(cmd.Stmts[0])
		}
	case *shsyntax.Block:
		if len(cmd.Stmts) > 0 {
			return extractPatternFromStmt(cmd.Stmts[0])
		}
	}
	return ""
}

func extractPatternFromCall(call *shsyntax.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}

	// Skip preamble commands. If the entire call is a preamble (cd /tmp),
	// return empty so BinaryCmd can try the next command in the chain.
	preambleOnly := map[string]bool{"cd": true, "env": true, "nice": true, "nohup": true, "time": true}
	passthrough := map[string]bool{"sudo": true} // sudo passes to next arg as the real binary
	startIdx := 0
	for startIdx < len(call.Args) {
		word := shellWordToString(call.Args[startIdx])
		base := filepath.Base(word)
		if word == "" {
			break
		}
		if preambleOnly[base] {
			return "" // cd, env, etc. with args are not meaningful patterns
		}
		if passthrough[base] {
			startIdx++
			continue
		}
		break
	}
	if startIdx >= len(call.Args) {
		return ""
	}

	binary := shellWordToString(call.Args[startIdx])
	if binary == "" {
		return "" // variable expansion, command substitution, etc.
	}
	binary = filepath.Base(binary)

	// Handle "bash -c 'actual command'" — extract pattern from the inner command
	if (binary == "bash" || binary == "sh" || binary == "zsh") && startIdx+1 < len(call.Args) {
		for i := startIdx + 1; i < len(call.Args); i++ {
			arg := shellWordToString(call.Args[i])
			if arg == "-c" && i+1 < len(call.Args) {
				inner := shellWordToString(call.Args[i+1])
				if inner != "" {
					return ExtractCommandPattern(inner) // recurse on inner command
				}
			}
		}
	}

	// Extract subcommand for tools that have them
	if startIdx+1 < len(call.Args) && hasSubcommands(binary) {
		arg := shellWordToString(call.Args[startIdx+1])
		if arg != "" && !strings.HasPrefix(arg, "-") {
			return binary + " " + arg
		}
	}
	return binary
}

// subcommandBinaries is the set of binaries where the first non-flag argument
// is a subcommand (go build, git status, docker run) vs tools where it's just
// an argument (echo hello, cat file.txt, grep pattern).
// Use AddSubcommandBinaries to extend this set from config.
var subcommandBinaries = map[string]struct{}{
	"go": {}, "git": {}, "docker": {}, "podman": {}, "kubectl": {}, "helm": {},
	"npm": {}, "npx": {}, "yarn": {}, "pnpm": {}, "bun": {}, "deno": {},
	"cargo": {}, "rustup": {}, "mix": {}, "elixir": {},
	"pip": {}, "pip3": {}, "poetry": {}, "uv": {}, "conda": {},
	"apt": {}, "apt-get": {}, "dnf": {}, "yum": {}, "pacman": {}, "emerge": {}, "xbps-install": {},
	"brew": {}, "port": {}, "snap": {}, "flatpak": {}, "nix": {},
	"systemctl": {}, "journalctl": {}, "rc-service": {}, "rc-update": {},
	"openrc": {}, "sv": {},
	"ip": {}, "ss": {}, "tc": {}, "nmcli": {}, "networkctl": {},
	"gh": {}, "glab": {},
	"ecl": {},
	"make": {},
}

// AddSubcommandBinaries adds additional binaries to the subcommand set.
// Call at startup from config before any permission checks.
func AddSubcommandBinaries(binaries []string) {
	for _, b := range binaries {
		subcommandBinaries[b] = struct{}{}
	}
}

func hasSubcommands(binary string) bool {
	_, ok := subcommandBinaries[binary]
	return ok
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

