package chat

import (
	"encoding/json"
)

// GlobToolItem renders glob tool calls.
// Header: ● Glob pattern
type GlobToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*GlobToolItem)(nil)

func NewGlobToolItem(id, input string) *GlobToolItem {
	return &GlobToolItem{
		baseToolItem: baseToolItem{id: id, name: "glob", input: input, status: ToolRunning},
	}
}

func (g *GlobToolItem) Height(width int) int { return countLines(g.Render(width)) }

func (g *GlobToolItem) Render(width int) string {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	json.Unmarshal([]byte(g.input), &params)
	detail := params.Pattern
	if params.Path != "" {
		detail += " in " + params.Path
	}
	header := ToolHeader(g.status, "Glob", g.compact, detail)
	if g.compact || g.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(g.output, defaultCollapsedLines, g.expanded)
	return "  " + JoinToolParts(header, body)
}

// GrepToolItem renders grep tool calls.
// Header: ● Grep pattern path
type GrepToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*GrepToolItem)(nil)

func NewGrepToolItem(id, input string) *GrepToolItem {
	return &GrepToolItem{
		baseToolItem: baseToolItem{id: id, name: "grep", input: input, status: ToolRunning},
	}
}

func (g *GrepToolItem) Height(width int) int { return countLines(g.Render(width)) }

func (g *GrepToolItem) Render(width int) string {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	json.Unmarshal([]byte(g.input), &params)
	detail := params.Pattern
	if params.Path != "" {
		detail += " " + params.Path
	}
	if params.Include != "" {
		detail += " (" + params.Include + ")"
	}
	header := ToolHeader(g.status, "Grep", g.compact, detail)
	if g.compact || g.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(g.output, defaultCollapsedLines, g.expanded)
	return "  " + JoinToolParts(header, body)
}

// LsToolItem renders ls tool calls.
// Header: ● Ls directory
type LsToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*LsToolItem)(nil)

func NewLsToolItem(id, input string) *LsToolItem {
	return &LsToolItem{
		baseToolItem: baseToolItem{id: id, name: "ls", input: input, status: ToolRunning},
	}
}

func (l *LsToolItem) Height(width int) int { return countLines(l.Render(width)) }

func (l *LsToolItem) Render(width int) string {
	path := extractPath(l.input)
	header := ToolHeader(l.status, "Ls", l.compact, path)
	if l.compact || l.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(l.output, defaultCollapsedLines, l.expanded)
	return "  " + JoinToolParts(header, body)
}
