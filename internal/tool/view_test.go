package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestViewTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

	tool := ViewTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have line numbers
	if !strings.Contains(resp.Content, "1\tline1") {
		t.Error("should have line numbers")
	}
	if !strings.Contains(resp.Content, "3\tline3") {
		t.Error("should show all lines")
	}
}

func TestViewToolOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line content")
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)

	tool := ViewTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `","offset":50,"limit":10}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	outputLines := strings.Split(resp.Content, "\n")
	if len(outputLines) != 10 {
		t.Errorf("got %d lines, want 10", len(outputLines))
	}
	if !strings.HasPrefix(strings.TrimSpace(outputLines[0]), "50") {
		t.Errorf("first line should start with 50, got: %s", outputLines[0])
	}
}

func TestViewToolBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")
	os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0xFF}, 0o644)

	tool := ViewTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"` + path + `"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(resp.Content, "Binary file") {
		t.Errorf("should detect binary, got: %s", resp.Content)
	}
}

func TestViewToolNotExist(t *testing.T) {
	tool := ViewTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"path":"/nonexistent/file.txt"}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Error") {
		t.Error("should return error for nonexistent file")
	}
}
