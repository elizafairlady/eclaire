package chat

import (
	"encoding/json"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	searchTitleStyle   = lipgloss.NewStyle().Bold(true)
	searchURLStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#5F87AF"))
	searchSnippetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B8B8B"))
)

// WebSearchToolItem renders web search results as a clean list of titles + excerpts.
type WebSearchToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*WebSearchToolItem)(nil)

func NewWebSearchToolItem(id, input string) *WebSearchToolItem {
	return &WebSearchToolItem{
		baseToolItem: baseToolItem{
			id:     id,
			name:   "web_search",
			input:  input,
			status: ToolRunning,
		},
	}
}

func (w *WebSearchToolItem) Height(width int) int { return countLines(w.Render(width)) }

func (w *WebSearchToolItem) Render(width int) string {
	var params struct {
		Query string `json:"query"`
	}
	json.Unmarshal([]byte(w.input), &params)

	header := ToolHeader(w.status, "Search", w.compact, params.Query)
	if w.compact || w.output == "" {
		return "  " + header
	}

	// Parse the search results (format: **Title**\nURL\nSnippet separated by \n\n)
	results := parseSearchResults(w.output)
	if len(results) == 0 {
		return "  " + JoinToolParts(header, "  "+w.output)
	}

	maxResults := 5
	if w.expanded {
		maxResults = len(results)
	}

	var lines []string
	for i, r := range results {
		if i >= maxResults {
			remaining := len(results) - maxResults
			lines = append(lines, truncHintStyle.Render(
				"  … ("+strings.Repeat(" ", 0)+string(rune('0'+remaining/10))+string(rune('0'+remaining%10))+" more results)"))
			break
		}
		title := searchTitleStyle.Render(r.title)
		url := searchURLStyle.Render(r.url)
		line := "  " + title + "  " + url
		if r.snippet != "" {
			snippetWidth := width - 6
			if snippetWidth > 100 {
				snippetWidth = 100
			}
			snippet := r.snippet
			if len(snippet) > snippetWidth && snippetWidth > 0 {
				snippet = snippet[:snippetWidth] + "…"
			}
			line += "\n  " + searchSnippetStyle.Render(snippet)
		}
		lines = append(lines, line)
	}

	body := strings.Join(lines, "\n")
	return "  " + JoinToolParts(header, body)
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

func parseSearchResults(output string) []searchResult {
	blocks := strings.Split(output, "\n\n")
	var results []searchResult
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.SplitN(block, "\n", 3)
		r := searchResult{}
		if len(lines) >= 1 {
			r.title = strings.TrimPrefix(strings.TrimSuffix(lines[0], "**"), "**")
		}
		if len(lines) >= 2 {
			r.url = strings.TrimSpace(lines[1])
		}
		if len(lines) >= 3 {
			r.snippet = strings.TrimSpace(lines[2])
		}
		if r.title != "" {
			results = append(results, r)
		}
	}
	return results
}
