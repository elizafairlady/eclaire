package ui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
)

// markdownRenderer caches glamour renderers by width for efficient reuse.
type markdownRenderer struct {
	mu    sync.Mutex
	cache map[int]*glamour.TermRenderer
}

func newMarkdownRenderer() *markdownRenderer {
	return &markdownRenderer{
		cache: make(map[int]*glamour.TermRenderer),
	}
}

// Render converts markdown to styled terminal output.
// Falls back to raw content on error.
func (r *markdownRenderer) Render(content string, width int) string {
	if content == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}

	r.mu.Lock()
	renderer, ok := r.cache[width]
	if !ok {
		renderer, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
		)
		r.cache[width] = renderer
	}
	r.mu.Unlock()

	if renderer == nil {
		return content
	}

	result, err := renderer.Render(content)
	if err != nil {
		return content
	}

	// Trim trailing whitespace that glamour adds
	return strings.TrimRight(result, "\n ")
}
