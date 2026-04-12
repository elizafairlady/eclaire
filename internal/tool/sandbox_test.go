package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSandboxedCommandDisabled(t *testing.T) {
	cfg := SandboxConfig{Enabled: false}
	bin, args := buildSandboxedCommand(cfg, "echo hello", "/tmp")
	if bin != "bash" {
		t.Errorf("disabled sandbox should use bash, got %q", bin)
	}
	if len(args) != 2 || args[0] != "-c" || args[1] != "echo hello" {
		t.Errorf("args = %v", args)
	}
}

func TestBuildSandboxedCommandEnabled(t *testing.T) {
	if !BwrapAvailable() {
		t.Skip("bwrap not available")
	}

	dir := t.TempDir()
	cfg := SandboxConfig{
		Enabled:      true,
		WriteRoots:   []string{dir},
		AllowNetwork: true,
	}

	bin, args := buildSandboxedCommand(cfg, "echo hello", dir)
	if !strings.HasSuffix(bin, "bwrap") {
		t.Errorf("expected bwrap binary, got %q", bin)
	}

	// Should contain --bind for the write root
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--bind "+dir) {
		t.Errorf("should bind write root %q, args: %s", dir, argStr)
	}

	// Should contain --chdir
	if !strings.Contains(argStr, "--chdir "+dir) {
		t.Errorf("should set chdir, args: %s", argStr)
	}

	// Should end with bash -c command
	if args[len(args)-3] != "bash" || args[len(args)-2] != "-c" || args[len(args)-1] != "echo hello" {
		t.Errorf("should end with bash -c command, got: %v", args[len(args)-3:])
	}
}

func TestSandboxedExecution(t *testing.T) {
	if !BwrapAvailable() {
		t.Skip("bwrap not available")
	}

	dir := t.TempDir()
	e := &ShellExecutor{}
	sandbox := DefaultSandboxConfig([]string{dir})
	e.SetPolicy(&CommandPolicy{
		DeniedBinaries: nil,
		MaxCommandLen:  DefaultMaxCommandLen,
		MaxOutputBytes: DefaultMaxOutputBytes,
		MaxTimeout:     DefaultMaxTimeout,
		Sandbox:        &sandbox,
	})

	// Basic command should work
	r := e.Run(context.Background(), "echo sandboxed", dir)
	if r.Err != nil {
		t.Fatalf("sandboxed echo failed: %v\nstderr: %s", r.Err, r.Stderr)
	}
	if strings.TrimSpace(r.Stdout) != "sandboxed" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestSandboxBlocksWriteOutsideRoot(t *testing.T) {
	if !BwrapAvailable() {
		t.Skip("bwrap not available")
	}

	writeDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "should_not_exist.txt")

	e := &ShellExecutor{}
	sandbox := DefaultSandboxConfig([]string{writeDir})
	e.SetPolicy(&CommandPolicy{
		DeniedBinaries: nil,
		MaxCommandLen:  DefaultMaxCommandLen,
		MaxOutputBytes: DefaultMaxOutputBytes,
		MaxTimeout:     DefaultMaxTimeout,
		Sandbox:        &sandbox,
	})

	// Try to write outside the sandbox write root
	r := e.Run(context.Background(), "echo evil > "+outsideFile, writeDir)

	// The write should fail (read-only filesystem) or the file should not exist
	if _, err := os.Stat(outsideFile); err == nil {
		t.Error("sandbox should prevent writes outside write roots")
	}
	_ = r // exit code may vary
}

func TestSandboxAllowsWriteInsideRoot(t *testing.T) {
	if !BwrapAvailable() {
		t.Skip("bwrap not available")
	}

	dir := t.TempDir()
	testFile := filepath.Join(dir, "sandbox_test.txt")

	e := &ShellExecutor{}
	sandbox := DefaultSandboxConfig([]string{dir})
	e.SetPolicy(&CommandPolicy{
		DeniedBinaries: nil,
		MaxCommandLen:  DefaultMaxCommandLen,
		MaxOutputBytes: DefaultMaxOutputBytes,
		MaxTimeout:     DefaultMaxTimeout,
		Sandbox:        &sandbox,
	})

	r := e.Run(context.Background(), "echo allowed > "+testFile, dir)
	if r.Err != nil {
		t.Fatalf("write inside sandbox should work: %v\nstderr: %s", r.Err, r.Stderr)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if strings.TrimSpace(string(data)) != "allowed" {
		t.Errorf("file content = %q", string(data))
	}
}
