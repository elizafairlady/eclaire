package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SandboxConfig controls bwrap-based command sandboxing.
type SandboxConfig struct {
	Enabled        bool     // enable bwrap sandboxing
	WriteRoots     []string // directories with read-write access (workspace roots)
	AllowNetwork   bool     // allow network access (default true — agents need git/curl)
	AllowNewPID    bool     // new PID namespace (hides host processes)
	ReadOnlyDirs   []string // system dirs to bind read-only (nil = use defaults)
	ExtraReadOnly  []string // additional read-only bind mounts
	ExtraReadWrite []string // additional read-write bind mounts
}

// DefaultSandboxConfig returns a sandbox config that restricts filesystem writes
// to workspace roots while keeping network and most reads available.
func DefaultSandboxConfig(writeRoots []string) SandboxConfig {
	return SandboxConfig{
		Enabled:      true,
		WriteRoots:   writeRoots,
		AllowNetwork: true,
	}
}

// bwrapPath caches the resolved bwrap binary location.
var bwrapPath string

func init() {
	bwrapPath, _ = exec.LookPath("bwrap")
}

// SetBwrapPath overrides the auto-detected bwrap binary path.
// Call this before any sandbox operations if the config specifies a custom path.
func SetBwrapPath(path string) {
	bwrapPath = path
}

// BwrapAvailable returns true if bubblewrap is installed or a custom path is set.
func BwrapAvailable() bool {
	return bwrapPath != ""
}

// buildSandboxedCommand wraps a shell command in bwrap with filesystem restrictions.
// Returns the bwrap binary and its argument list.
// If bwrap is unavailable, returns bash -c as fallback.
func buildSandboxedCommand(cfg SandboxConfig, command, cwd string) (string, []string) {
	if !cfg.Enabled || !BwrapAvailable() {
		return "bash", []string{"-c", command}
	}

	args := []string{
		// Unshare user namespace for unprivileged sandboxing
		"--unshare-user",
		// Die with parent — sandbox dies if eclaire dies
		"--die-with-parent",
	}

	if cfg.AllowNewPID {
		args = append(args, "--unshare-pid")
	}

	if !cfg.AllowNetwork {
		args = append(args, "--unshare-net")
	}

	// Bind system directories read-only
	roDirs := cfg.ReadOnlyDirs
	if roDirs == nil {
		roDirs = DefaultReadOnlyDirs()
	}
	for _, dir := range roDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", dir, dir)
		}
	}

	// /tmp as tmpfs
	args = append(args, "--tmpfs", "/tmp")

	// /dev basics
	args = append(args, "--dev", "/dev")

	// /proc (needed by many tools — go, git, etc.)
	args = append(args, "--proc", "/proc")

	// Home directory: read-only by default
	home, _ := os.UserHomeDir()
	if home != "" {
		args = append(args, "--ro-bind", home, home)
	}

	// Workspace roots: read-write (overlay on top of read-only home)
	for _, root := range cfg.WriteRoots {
		root = filepath.Clean(root)
		if root == "" || root == "/" {
			continue
		}
		// Resolve symlinks so the real path is mounted
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			args = append(args, "--bind", root, root)
		}
	}

	// Extra mounts
	for _, dir := range cfg.ExtraReadOnly {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", dir, dir)
		}
	}
	for _, dir := range cfg.ExtraReadWrite {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			args = append(args, "--bind", dir, dir)
		}
	}

	// Set CWD inside the sandbox
	if cwd != "" {
		args = append(args, "--chdir", cwd)
	}

	// The actual command
	args = append(args, "bash", "-c", command)

	return bwrapPath, args
}

// DefaultReadOnlyDirs returns the default system directories to bind read-only.
// Used when SandboxConfig.ReadOnlyDirs is nil.
func DefaultReadOnlyDirs() []string {
	candidates := []string{
		"/usr",
		"/bin",
		"/sbin",
		"/lib",
		"/lib64",
		"/etc",
		"/var/lib",   // package databases
		"/var/cache",  // package caches
		"/run",        // runtime state (dbus, etc.)
	}

	// Go toolchain
	goroot := os.Getenv("GOROOT")
	if goroot != "" {
		candidates = append(candidates, goroot)
	}
	gopath := os.Getenv("GOPATH")
	if gopath != "" {
		for _, p := range strings.Split(gopath, ":") {
			candidates = append(candidates, p)
		}
	}

	return candidates
}
