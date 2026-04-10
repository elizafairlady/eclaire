package ui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestMarkdownRendererBasic(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("# Hello\n\nSome **bold** text", 80)
	if out == "" {
		t.Error("empty output")
	}
	// Glamour should transform the markdown — output should differ from input
	if out == "# Hello\n\nSome **bold** text" {
		t.Error("markdown was not transformed")
	}
	if !strings.Contains(out, "Hello") {
		t.Error("should contain heading text")
	}
}

func TestMarkdownRendererPlainText(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("just plain text", 80)
	clean := stripANSI(out)
	if !strings.Contains(clean, "plain text") {
		t.Errorf("plain text should pass through, got %q", clean)
	}
}

func TestMarkdownRendererEmptyString(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("", 80)
	if out != "" {
		t.Errorf("empty input should return empty, got %q", out)
	}
}

func TestMarkdownRendererCache(t *testing.T) {
	r := newMarkdownRenderer()
	// Render twice at same width — should reuse renderer
	r.Render("# Test", 80)
	r.Render("# Test 2", 80)

	r.mu.Lock()
	count := len(r.cache)
	r.mu.Unlock()

	if count != 1 {
		t.Errorf("cache should have 1 entry for width 80, got %d", count)
	}

	// Different width creates new entry
	r.Render("# Test 3", 120)
	r.mu.Lock()
	count = len(r.cache)
	r.mu.Unlock()

	if count != 2 {
		t.Errorf("cache should have 2 entries, got %d", count)
	}
}

func TestMarkdownRendererCodeBlock(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("```go\nfunc main() {}\n```", 80)
	if !strings.Contains(out, "func") && !strings.Contains(out, "main") {
		t.Errorf("code block should contain function text, got %q", out)
	}
}
