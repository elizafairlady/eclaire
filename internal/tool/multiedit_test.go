package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestMultiEditTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte("hello world\nfoo bar\n"), 0o644)

	tool := MultiEditTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `","edits":[{"old_string":"hello","new_string":"hi"},{"old_string":"foo","new_string":"baz"}]}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(resp.Content, "2/2") {
		t.Errorf("expected 2/2 edits applied, got: %s", resp.Content)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "hi world") {
		t.Error("first edit not applied")
	}
	if !strings.Contains(string(data), "baz bar") {
		t.Error("second edit not applied")
	}
}

func TestMultiEditToolPartialFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	tool := MultiEditTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `","edits":[{"old_string":"hello","new_string":"hi"},{"old_string":"nonexistent","new_string":"x"}]}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(resp.Content, "1/2") {
		t.Errorf("expected 1/2, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Failures") {
		t.Error("should report failures")
	}
}

func TestMultiEditToolCreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	tool := MultiEditTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `","edits":[{"old_string":"","new_string":"new content"}]}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(resp.Content, "Created") {
		t.Errorf("expected Created, got: %s", resp.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("file content = %q", string(data))
	}
}
