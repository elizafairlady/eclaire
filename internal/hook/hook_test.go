package hook

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchTool(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"*", "shell", true},
		{"shell", "shell", true},
		{"shell", "read", false},
		{"sh*", "shell", true},
		{"", "anything", true},
		{"read", "read", true},
		{"read", "write", false},
		{"*_search", "web_search", true},
	}
	for _, tt := range tests {
		if got := matchTool(tt.pattern, tt.name); got != tt.want {
			t.Errorf("matchTool(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunPreDeny(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "deny.sh", `echo '{"denied": true, "deny_message": "not allowed"}'`)

	runner := NewRunner([]Definition{{
		Event:   PreToolUse,
		Matcher: "shell",
		Command: script,
	}})

	_, denied, msg, err := runner.RunPre(context.Background(), "shell", `{"command":"ls"}`, "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if !denied {
		t.Error("expected denied")
	}
	if msg != "not allowed" {
		t.Errorf("deny message = %q, want %q", msg, "not allowed")
	}
}

func TestRunPreModifyInput(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "modify.sh", `echo '{"updated_input": "{\"command\":\"ls -la\"}"}'`)

	runner := NewRunner([]Definition{{
		Event:   PreToolUse,
		Matcher: "*",
		Command: script,
	}})

	input, denied, _, err := runner.RunPre(context.Background(), "shell", `{"command":"ls"}`, "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if denied {
		t.Error("should not be denied")
	}
	if input != `{"command":"ls -la"}` {
		t.Errorf("updated input = %q", input)
	}
}

func TestRunPreAllow(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "allow.sh", `echo '{}'`)

	runner := NewRunner([]Definition{{
		Event:   PreToolUse,
		Matcher: "shell",
		Command: script,
	}})

	_, denied, _, err := runner.RunPre(context.Background(), "shell", `{"command":"ls"}`, "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if denied {
		t.Error("should not be denied")
	}
}

func TestRunPreNoMatch(t *testing.T) {
	runner := NewRunner([]Definition{{
		Event:   PreToolUse,
		Matcher: "shell",
		Command: "false", // would fail if called
	}})

	_, denied, _, err := runner.RunPre(context.Background(), "read", `{"path":"/etc/passwd"}`, "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if denied {
		t.Error("should not be denied — no matching hook")
	}
}

func TestRunPost(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "post.sh", `echo '{"message": "audit logged"}'`)

	runner := NewRunner([]Definition{{
		Event:   PostToolUse,
		Matcher: "*",
		Command: script,
	}})

	msg, err := runner.RunPost(context.Background(), "shell", `{"command":"ls"}`, "file1\nfile2", "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "audit logged" {
		t.Errorf("message = %q, want %q", msg, "audit logged")
	}
}

func TestRunPostNoMatch(t *testing.T) {
	runner := NewRunner([]Definition{{
		Event:   PostToolUse,
		Matcher: "write",
		Command: "false",
	}})

	msg, err := runner.RunPost(context.Background(), "read", `{}`, "output", "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "" {
		t.Errorf("expected empty message for non-matching hook, got %q", msg)
	}
}

func TestEnvVars(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "env.sh", `echo "{\"message\": \"$HOOK_EVENT:$HOOK_TOOL_NAME\"}"`)

	runner := NewRunner([]Definition{{
		Event:   PostToolUse,
		Matcher: "shell",
		Command: script,
	}})

	msg, err := runner.RunPost(context.Background(), "shell", `{}`, "output", "test", "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "PostToolUse:shell" {
		t.Errorf("message = %q, want %q", msg, "PostToolUse:shell")
	}
}

func TestNilRunnerSafe(t *testing.T) {
	var r *Runner
	_, denied, _, err := r.RunPre(context.Background(), "shell", "{}", "", "")
	if err != nil || denied {
		t.Error("nil runner should be safe no-op")
	}
	msg, err := r.RunPost(context.Background(), "shell", "{}", "out", "", "")
	if err != nil || msg != "" {
		t.Error("nil runner should be safe no-op")
	}
	if err := r.RunPostFailure(context.Background(), "shell", "{}", "err", "", ""); err != nil {
		t.Error("nil runner should be safe no-op")
	}
}

func TestNewRunnerNilForEmpty(t *testing.T) {
	r := NewRunner(nil)
	if r != nil {
		t.Error("NewRunner(nil) should return nil")
	}
	r = NewRunner([]Definition{})
	if r != nil {
		t.Error("NewRunner([]) should return nil")
	}
}
