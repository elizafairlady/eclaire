package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

// ShellExecutor is the single chokepoint for all os/exec calls in eclaire.
// Every shell command — foreground or background — must go through this.
// This is where deny-lists, rate limiting, audit logging, and hard security gates live.
type ShellExecutor struct {
	logger    *slog.Logger
	execCount atomic.Int64
	policy    *CommandPolicy
	limiter   *rateLimiter
	audit     *AuditLog
	mu        sync.Mutex // guards lazy init of policy/limiter
}

// DefaultExecutor is the global shell executor. Set once at startup.
var DefaultExecutor = &ShellExecutor{}

// SetLogger configures the executor's logger. Call during gateway init.
func (e *ShellExecutor) SetLogger(l *slog.Logger) {
	e.logger = l
}

// SetPolicy configures the command policy. Call during gateway init.
// If nil, DefaultCommandPolicy() is used.
func (e *ShellExecutor) SetPolicy(p *CommandPolicy) {
	e.policy = p
}

// SetAuditLog configures structured audit logging. Call during gateway init.
func (e *ShellExecutor) SetAuditLog(a *AuditLog) {
	e.audit = a
}

// AddSandboxWriteRoot dynamically adds a write root to the sandbox config.
// Called when a project root is detected on connection.
func (e *ShellExecutor) AddSandboxWriteRoot(root string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.policy == nil || e.policy.Sandbox == nil {
		return
	}
	// Check if already present
	for _, r := range e.policy.Sandbox.WriteRoots {
		if r == root {
			return
		}
	}
	e.policy.Sandbox.WriteRoots = append(e.policy.Sandbox.WriteRoots, root)
}

func (e *ShellExecutor) effectivePolicy() *CommandPolicy {
	if e.policy != nil {
		return e.policy
	}
	return DefaultCommandPolicy()
}

func (e *ShellExecutor) effectiveLimiter() *rateLimiter {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.limiter != nil {
		return e.limiter
	}
	e.limiter = newRateLimiter(DefaultRateLimit, time.Minute)
	return e.limiter
}

// ExecResult holds the output of a shell command.
type ExecResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Err       error
	Truncated bool // true if output was truncated due to size limits
}

// Run executes a shell command synchronously and returns its output.
// This is the ONLY place in the codebase where exec.CommandContext should be called.
// If the caller's context has no deadline and MaxTimeout > 0, a backstop timeout is applied.
func (e *ShellExecutor) Run(ctx context.Context, command, cwd string) ExecResult {
	policy := e.effectivePolicy()

	if err := policy.Validate(command); err != nil {
		e.logAudit(command, cwd, "denied", err.Error(), 0)
		return ExecResult{Err: err, ExitCode: 1}
	}

	// Rate limit check
	if !e.effectiveLimiter().allow() {
		err := fmt.Errorf("rate limit exceeded: max %d commands per minute", e.effectiveLimiter().limit)
		e.logAudit(command, cwd, "rate_limited", "", 0)
		return ExecResult{Err: err, ExitCode: 1}
	}

	// Backstop timeout: if the caller hasn't set a deadline and the policy has a max,
	// enforce it here so no command can hang forever.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && policy.MaxTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(policy.MaxTimeout)*time.Second)
		defer cancel()
	}

	e.execCount.Add(1)
	n := e.execCount.Load()
	start := time.Now()

	if e.logger != nil {
		e.logger.Info("shell exec", "n", n, "cmd", truncateCmd(command), "cwd", cwd)
	}

	var cmd *exec.Cmd
	if policy.Sandbox != nil {
		bin, args := buildSandboxedCommand(*policy.Sandbox, command, cwd)
		cmd = exec.CommandContext(ctx, bin, args...)
		// bwrap handles CWD via --chdir
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", command)
		if cwd != "" {
			cmd.Dir = cwd
		}
	}
	cmd.Env = sanitizeEnv(os.Environ())

	// Capture output with size limits
	maxOut := policy.MaxOutputBytes
	stdoutBuf := &limitedBuffer{max: maxOut}
	stderrBuf := &limitedBuffer{max: maxOut}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()
	elapsed := time.Since(start)

	truncated := stdoutBuf.truncated || stderrBuf.truncated
	result := ExecResult{
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		Err:       err,
		Truncated: truncated,
	}

	if truncated {
		result.Stderr += fmt.Sprintf("\n[TRUNCATED] Output exceeded %d byte limit.", maxOut)
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}

	status := "ok"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
		if e.logger != nil {
			e.logger.Warn("shell exec failed", "n", n, "err", err, "exit", result.ExitCode, "elapsed", elapsed)
		}
	}
	e.logAudit(command, cwd, status, errMsg, elapsed)

	return result
}

// StartBackground launches a command and returns a *exec.Cmd.
// The caller is responsible for capturing stdout/stderr and waiting.
// This is the ONLY place background exec.Command should be created.
func (e *ShellExecutor) StartBackground(command, cwd string) *exec.Cmd {
	policy := e.effectivePolicy()

	if err := policy.Validate(command); err != nil {
		if e.logger != nil {
			e.logger.Warn("background command denied", "cmd", truncateCmd(command), "err", err)
		}
		e.logAudit(command, cwd, "denied", err.Error(), 0)
		return exec.Command("false")
	}

	if !e.effectiveLimiter().allow() {
		if e.logger != nil {
			e.logger.Warn("background command rate limited", "cmd", truncateCmd(command))
		}
		e.logAudit(command, cwd, "rate_limited", "", 0)
		return exec.Command("false")
	}

	e.execCount.Add(1)
	n := e.execCount.Load()

	if e.logger != nil {
		e.logger.Info("shell exec (background)", "n", n, "cmd", truncateCmd(command), "cwd", cwd)
	}
	e.logAudit(command, cwd, "background", "", 0)

	var cmd *exec.Cmd
	if policy.Sandbox != nil {
		bin, args := buildSandboxedCommand(*policy.Sandbox, command, cwd)
		cmd = exec.Command(bin, args...)
	} else {
		cmd = exec.Command("bash", "-c", command)
		if cwd != "" {
			cmd.Dir = cwd
		}
	}
	cmd.Env = sanitizeEnv(os.Environ())
	return cmd
}

// ExecCount returns the total number of commands executed since startup.
func (e *ShellExecutor) ExecCount() int64 {
	return e.execCount.Load()
}

func (e *ShellExecutor) logAudit(command, cwd, status, errMsg string, elapsed time.Duration) {
	if e.audit != nil {
		e.audit.Log(AuditEntry{
			Time:    time.Now(),
			Command: command,
			CWD:     cwd,
			Status:  status,
			Error:   errMsg,
			Elapsed: elapsed,
		})
	}
}

// --- Command Policy ---

const (
	DefaultMaxCommandLen = 100_000     // 100KB max command length
	DefaultMaxOutputBytes = 1_048_576  // 1MB max output per stream
	DefaultMaxTimeout    = 600         // 10 minutes max timeout
	DefaultRateLimit     = 120         // commands per minute
)

// CommandPolicy defines validation rules for shell commands.
// Uses mvdan.cc/sh AST parsing for reliable command inspection instead of regex.
type CommandPolicy struct {
	// DeniedBinaries are command names that are never allowed.
	DeniedBinaries map[string]string // binary name → reason

	// MaxCommandLen is the maximum command string length in bytes.
	MaxCommandLen int

	// MaxOutputBytes caps stdout and stderr capture per stream.
	MaxOutputBytes int

	// MaxTimeout is the maximum allowed timeout in seconds.
	MaxTimeout int

	// Sandbox restricts filesystem access via bwrap. Nil = no sandboxing.
	Sandbox *SandboxConfig
}

// DefaultCommandPolicy returns the production command policy.
func DefaultCommandPolicy() *CommandPolicy {
	return &CommandPolicy{
		DeniedBinaries: defaultDeniedBinaries(),
		MaxCommandLen:  DefaultMaxCommandLen,
		MaxOutputBytes: DefaultMaxOutputBytes,
		MaxTimeout:     DefaultMaxTimeout,
	}
}

// Validate checks a command against the policy. Returns nil if allowed.
// Parses the command as a shell AST and inspects every simple command
// for denied binaries, dangerous arguments, and pipe-to-shell patterns.
func (p *CommandPolicy) Validate(command string) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}
	if strings.ContainsRune(command, 0) {
		return fmt.Errorf("command contains null byte")
	}
	if len(command) > p.MaxCommandLen {
		return fmt.Errorf("command exceeds max length (%d > %d bytes)", len(command), p.MaxCommandLen)
	}

	// Parse the command into a shell AST
	parser := syntax.NewParser(syntax.KeepComments(false), syntax.Variant(syntax.LangBash))
	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		// Unparseable command — let bash handle the error, don't block.
		// This avoids false positives on unusual but valid syntax.
		return nil
	}

	// Walk the AST looking for denied patterns
	var walkErr error
	syntax.Walk(prog, func(node syntax.Node) bool {
		if walkErr != nil {
			return false
		}

		switch n := node.(type) {
		case *syntax.CallExpr:
			walkErr = p.checkCallExpr(n)
		case *syntax.BinaryCmd:
			walkErr = p.checkPipeToShell(n, command)
		}
		return walkErr == nil
	})

	return walkErr
}

// checkCallExpr inspects a simple command (binary + args) for policy violations.
func (p *CommandPolicy) checkCallExpr(call *syntax.CallExpr) error {
	if len(call.Args) == 0 {
		return nil
	}

	// Extract the command name from the first word
	name := wordToString(call.Args[0])
	if name == "" {
		return nil
	}

	// Strip path prefix — "/usr/bin/rm" → "rm"
	base := filepath.Base(name)

	// Check denied binaries
	if reason, denied := p.DeniedBinaries[base]; denied {
		return fmt.Errorf("command %q denied: %s", base, reason)
	}

	// Check context-dependent rules
	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		args[i] = wordToString(a)
	}

	return checkDangerousArgs(base, args)
}

// checkDangerousArgs applies context-dependent rules for commands that are
// allowed in some forms but dangerous in others.
func checkDangerousArgs(binary string, args []string) error {
	switch binary {
	case "rm":
		return checkRm(args)
	case "dd":
		return checkDd(args)
	case "chmod":
		return checkChmod(args)
	case "chown":
		return checkChown(args)
	case "crontab":
		for _, a := range args {
			if a == "-r" {
				return fmt.Errorf("crontab -r denied: removes all cron jobs")
			}
		}
	case "iptables", "ip6tables":
		for _, a := range args {
			if a == "-F" || a == "--flush" {
				return fmt.Errorf("iptables flush denied: drops all firewall rules")
			}
		}
	}
	return nil
}

// checkRm denies recursive deletion of root or near-root paths.
func checkRm(args []string) error {
	hasRecursive := false
	for _, a := range args[1:] {
		if a == "-r" || a == "-rf" || a == "-fr" || a == "-R" ||
			strings.Contains(a, "r") && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			hasRecursive = true
		}
		if a == "--recursive" {
			hasRecursive = true
		}
	}
	if !hasRecursive {
		return nil
	}
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		clean := filepath.Clean(a)
		if clean == "/" || clean == "/." {
			return fmt.Errorf("rm -r / denied: would destroy the filesystem")
		}
		// Deny deletion of top-level system directories
		if isDangerousRmTarget(clean) {
			return fmt.Errorf("rm -r %q denied: system directory", clean)
		}
	}
	return nil
}

func isDangerousRmTarget(path string) bool {
	danger := map[string]bool{
		"/bin": true, "/sbin": true, "/usr": true, "/etc": true,
		"/var": true, "/boot": true, "/lib": true, "/lib64": true,
		"/dev": true, "/proc": true, "/sys": true,
	}
	return danger[path]
}

// checkDd denies writes to raw block devices.
func checkDd(args []string) error {
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "of=/dev/sd") ||
			strings.HasPrefix(a, "of=/dev/nvme") ||
			strings.HasPrefix(a, "of=/dev/vd") ||
			strings.HasPrefix(a, "of=/dev/hd") {
			return fmt.Errorf("dd to block device denied: %s", a)
		}
	}
	return nil
}

// checkChmod denies recursive 777 on root.
func checkChmod(args []string) error {
	hasRecursive := false
	has777 := false
	hasRoot := false
	for _, a := range args[1:] {
		if a == "-R" || a == "--recursive" || (strings.HasPrefix(a, "-") && strings.Contains(a, "R")) {
			hasRecursive = true
		}
		if a == "777" || a == "a+rwx" {
			has777 = true
		}
		clean := filepath.Clean(a)
		if clean == "/" {
			hasRoot = true
		}
	}
	if hasRecursive && has777 && hasRoot {
		return fmt.Errorf("chmod -R 777 / denied")
	}
	return nil
}

// checkChown denies recursive chown on root.
func checkChown(args []string) error {
	hasRecursive := false
	hasRoot := false
	for _, a := range args[1:] {
		if a == "-R" || a == "--recursive" || (strings.HasPrefix(a, "-") && strings.Contains(a, "R")) {
			hasRecursive = true
		}
		if filepath.Clean(a) == "/" {
			hasRoot = true
		}
	}
	if hasRecursive && hasRoot {
		return fmt.Errorf("chown -R / denied")
	}
	return nil
}

// checkPipeToShell detects `curl ... | bash` and similar patterns.
func (p *CommandPolicy) checkPipeToShell(bin *syntax.BinaryCmd, _ string) error {
	if bin.Op != syntax.Pipe && bin.Op != syntax.PipeAll {
		return nil
	}

	// Check if the RHS is a shell interpreter
	rightName := firstCommandName(bin.Y)
	shells := map[string]bool{
		"bash": true, "sh": true, "zsh": true, "dash": true,
		"ksh": true, "csh": true, "fish": true,
	}
	if !shells[rightName] {
		return nil
	}

	// Check if the LHS is a network fetch command
	leftName := firstCommandName(bin.X)
	fetchers := map[string]bool{
		"curl": true, "wget": true, "fetch": true,
	}
	if fetchers[leftName] {
		return fmt.Errorf("pipe from %s to %s denied: remote code execution", leftName, rightName)
	}

	return nil
}

// firstCommandName extracts the command name from a statement.
func firstCommandName(stmt *syntax.Stmt) string {
	if stmt == nil || stmt.Cmd == nil {
		return ""
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	return filepath.Base(wordToString(call.Args[0]))
}

// wordToString converts a syntax.Word to a plain string.
// Returns empty string for words containing expansions that can't be
// statically resolved (we err on the side of allowing those).
func wordToString(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var buf strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			buf.WriteString(p.Value)
		case *syntax.SglQuoted:
			buf.WriteString(p.Value)
		case *syntax.DblQuoted:
			// Only extract if it's simple literal content
			for _, qp := range p.Parts {
				if lit, ok := qp.(*syntax.Lit); ok {
					buf.WriteString(lit.Value)
				} else {
					return "" // contains expansion — can't resolve statically
				}
			}
		default:
			return "" // variable expansion, command substitution, etc.
		}
	}
	return buf.String()
}

// defaultDeniedBinaries returns the set of commands that are never allowed.
func defaultDeniedBinaries() map[string]string {
	return map[string]string{
		// System lifecycle
		"shutdown": "system shutdown",
		"reboot":   "system reboot",
		"halt":     "system halt",
		"poweroff": "system poweroff",

		// Filesystem creation/destruction
		"mkfs":       "filesystem formatting",
		"mkfs.ext4":  "filesystem formatting",
		"mkfs.btrfs": "filesystem formatting",
		"mkfs.xfs":   "filesystem formatting",
		"mkswap":     "swap formatting",

		// Kernel module manipulation
		"insmod":  "kernel module loading",
		"rmmod":   "kernel module unloading",
		"modprobe": "kernel module manipulation",

		// Partition table manipulation
		"fdisk":   "partition table editing",
		"parted":  "partition table editing",
		"gdisk":   "partition table editing",
		"sfdisk":  "partition table editing",
		"sgdisk":  "partition table editing",
		"cfdisk":  "partition table editing",

		// Init system manipulation (all init systems)
		"init":       "init system manipulation",
		"telinit":    "runlevel change",
		"systemctl":  "systemd service management",
		"openrc":     "OpenRC service management",
		"rc-update":  "OpenRC service management",
		"rc-service": "OpenRC service management",
		"sv":         "runit service management",
		"runsv":      "runit service management",
		"runsvdir":   "runit service management",
		"xbps-remove": "Void Linux package removal",
	}
}

// ClampTimeout returns the timeout clamped to the policy's max.
func (p *CommandPolicy) ClampTimeout(requested int) int {
	if p.MaxTimeout > 0 && requested > p.MaxTimeout {
		return p.MaxTimeout
	}
	return requested
}

// --- Rate Limiter (token bucket) ---

type rateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	tokens   int
	lastFill time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:    limit,
		window:   window,
		tokens:   limit,
		lastFill: time.Now(),
	}
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastFill)

	// Refill tokens proportionally to elapsed time
	if elapsed > 0 {
		refill := int(elapsed.Seconds() / r.window.Seconds() * float64(r.limit))
		if refill > 0 {
			r.tokens += refill
			if r.tokens > r.limit {
				r.tokens = r.limit
			}
			r.lastFill = now
		}
	}

	if r.tokens <= 0 {
		return false
	}
	r.tokens--
	return true
}

// --- Audit Log ---

// AuditEntry is a structured record of a shell command execution.
type AuditEntry struct {
	Time    time.Time     `json:"time"`
	Command string        `json:"command"`
	CWD     string        `json:"cwd,omitempty"`
	Status  string        `json:"status"` // ok, error, denied, rate_limited, background
	Error   string        `json:"error,omitempty"`
	Elapsed time.Duration `json:"elapsed_ns,omitempty"`
}

// AuditLog writes structured audit entries.
type AuditLog struct {
	logger *slog.Logger
}

// NewAuditLog creates an audit log backed by a structured logger.
func NewAuditLog(logger *slog.Logger) *AuditLog {
	return &AuditLog{logger: logger}
}

// Log records an audit entry.
func (a *AuditLog) Log(entry AuditEntry) {
	a.logger.Info("shell_audit",
		"command", truncateCmd(entry.Command),
		"cwd", entry.CWD,
		"status", entry.Status,
		"error", entry.Error,
		"elapsed_ms", entry.Elapsed.Milliseconds(),
	)
}

// --- Output limiting ---

// limitedBuffer captures output up to a max size, then discards the rest.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if lb.truncated {
		return len(p), nil // silently discard
	}
	remaining := lb.max - lb.buf.Len()
	if remaining <= 0 {
		lb.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		lb.buf.Write(p[:remaining])
		lb.truncated = true
		return len(p), nil
	}
	return lb.buf.Write(p)
}

func (lb *limitedBuffer) String() string {
	return lb.buf.String()
}

// Ensure limitedBuffer implements io.Writer.
var _ io.Writer = (*limitedBuffer)(nil)

// --- Env sanitization ---

// sanitizeEnv strips environment variables that enable library injection
// (LD_PRELOAD, DYLD_INSERT_LIBRARIES, etc.) to prevent privilege escalation
// from a compromised agent.
func sanitizeEnv(env []string) []string {
	blocked := map[string]bool{
		"LD_PRELOAD":            true,
		"LD_LIBRARY_PATH":       true,
		"DYLD_INSERT_LIBRARIES": true,
		"DYLD_LIBRARY_PATH":     true,
	}
	var clean []string
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if !blocked[key] {
			clean = append(clean, e)
		}
	}
	return clean
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
