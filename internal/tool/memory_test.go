package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestMemoryWriteCurated(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o700)

	tool := MemoryWriteTool(wsDir)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"content":"important fact about Go","type":"curated"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Saved") {
		t.Errorf("response = %q", resp.Content)
	}

	data, _ := os.ReadFile(filepath.Join(wsDir, "MEMORY.md"))
	if !strings.Contains(string(data), "important fact about Go") {
		t.Errorf("MEMORY.md = %q", string(data))
	}
}

func TestMemoryWriteDaily(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o700)

	tool := MemoryWriteTool(wsDir)
	tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"content":"today's note","type":"daily"}`,
	})

	entries, _ := os.ReadDir(filepath.Join(wsDir, "memory"))
	if len(entries) != 1 {
		t.Fatalf("got %d daily files, want 1", len(entries))
	}
}

func TestMemoryWriteInvalidType(t *testing.T) {
	dir := t.TempDir()
	tool := MemoryWriteTool(dir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"content":"x","type":"invalid"}`,
	})
	if !strings.Contains(resp.Content, "Error") {
		t.Error("should error on invalid type")
	}
}

func TestMemoryWriteEmpty(t *testing.T) {
	dir := t.TempDir()
	tool := MemoryWriteTool(dir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"content":"","type":"curated"}`,
	})
	if !strings.Contains(resp.Content, "Error") {
		t.Error("should error on empty content")
	}
}

func TestMemoryReadCurated(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o700)
	os.WriteFile(filepath.Join(wsDir, "MEMORY.md"), []byte("stored fact\n"), 0o644)

	tool := MemoryReadTool(wsDir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{}`,
	})
	if !strings.Contains(resp.Content, "stored fact") {
		t.Errorf("response = %q", resp.Content)
	}
}

func TestMemoryReadSearch(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o700)
	os.WriteFile(filepath.Join(wsDir, "MEMORY.md"), []byte("Go is great\nPython is fine\n"), 0o644)

	tool := MemoryReadTool(wsDir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"query":"python"}`,
	})
	if !strings.Contains(resp.Content, "Python") {
		t.Errorf("search should find Python, got: %q", resp.Content)
	}
}

func TestMemoryReadNoMatch(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	os.MkdirAll(wsDir, 0o700)
	os.WriteFile(filepath.Join(wsDir, "MEMORY.md"), []byte("only Go here\n"), 0o644)

	tool := MemoryReadTool(wsDir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"query":"rust"}`,
	})
	if !strings.Contains(resp.Content, "No memory") {
		t.Errorf("should report no match, got: %q", resp.Content)
	}
}

func TestMemoryReadEmpty(t *testing.T) {
	dir := t.TempDir()
	tool := MemoryReadTool(dir)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{}`,
	})
	if !strings.Contains(resp.Content, "No memory") {
		t.Errorf("should report empty, got: %q", resp.Content)
	}
}
