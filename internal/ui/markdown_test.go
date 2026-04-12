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

// Verify the custom style doesn't pad lines to full width with colored spaces.
// The stock "dark" theme pads every line with \033[38;5;252m \033[m sequences.
func TestMarkdownRendererNoLinePadding(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("Short line.", 80)

	// Each line should not have trailing ANSI-colored spaces
	for i, line := range strings.Split(out, "\n") {
		clean := stripANSI(line)
		trimmed := strings.TrimRight(clean, " ")
		// Allow at most a few trailing spaces (glamour may add 1-2 for formatting)
		// but not padding to full 80-char width
		padding := len(clean) - len(trimmed)
		if padding > 5 {
			t.Errorf("line %d has %d trailing spaces (full-width padding detected): %q", i, padding, line)
		}
	}
}

func TestMarkdownRendererCustomColors(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.Render("# Heading\n\nSome text", 80)

	// Heading should use eclaire's Secondary color (#f9e2af = RGB 249,226,175)
	// which glamour renders as \033[38;2;249;226;175m
	if !strings.Contains(out, "38;2;249;226;175") {
		t.Logf("output: %q", out)
		t.Error("heading should use eclaire's Secondary color (249,226,175)")
	}

	// Body text should use eclaire's FgBase color (#cdd6f4 = RGB 205,214,244)
	if !strings.Contains(out, "38;2;205;214;244") {
		t.Error("body text should use eclaire's FgBase color (205,214,244)")
	}
}
