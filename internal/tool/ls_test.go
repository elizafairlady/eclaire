package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestLsTool(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("package src"), 0o644)

	tool := LsTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + dir + `"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(resp.Content, "main.go") {
		t.Error("output should contain main.go")
	}
	if !strings.Contains(resp.Content, "src/") {
		t.Error("output should contain src/")
	}
}

func TestLsToolDepthLimit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "d", "deep.txt"), []byte(""), 0o644)

	tool := LsTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + dir + `","depth":2}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(resp.Content, "deep.txt") {
		t.Error("depth=2 should not show files at depth 4")
	}
}

func TestLsToolNotExist(t *testing.T) {
	tool := LsTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"/nonexistent/path"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Error") {
		t.Error("should return error for nonexistent path")
	}
}

func TestLsToolCategory(t *testing.T) {
	tool := LsTool()
	if tool.TrustTier() != TrustReadOnly {
		t.Error("ls should be ReadOnly")
	}
	if tool.Category() != "fs" {
		t.Errorf("category = %q, want fs", tool.Category())
	}
}
