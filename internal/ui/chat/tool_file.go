package chat

import (
	"encoding/json"
	"fmt"
)

// ReadToolItem renders read tool calls.
// Header: ● Read path
// Body: file content (collapsed)
type ReadToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*ReadToolItem)(nil)

func NewReadToolItem(id, input string) *ReadToolItem {
	return &ReadToolItem{
		baseToolItem: baseToolItem{id: id, name: "read", input: input, status: ToolRunning},
	}
}

func (r *ReadToolItem) Height(width int) int { return countLines(r.Render(width)) }

func (r *ReadToolItem) Render(width int) string {
	path := extractPath(r.input)
	header := ToolHeader(r.status, "Read", r.compact, path)
	if r.compact || r.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(r.output, defaultCollapsedLines, r.expanded)
	return "  " + JoinToolParts(header, body)
}

// WriteToolItem renders write tool calls.
// Header: ● Write path
// Body: "Wrote N bytes" or error
type WriteToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*WriteToolItem)(nil)

func NewWriteToolItem(id, input string) *WriteToolItem {
	return &WriteToolItem{
		baseToolItem: baseToolItem{id: id, name: "write", input: input, status: ToolRunning},
	}
}

func (w *WriteToolItem) Height(width int) int { return countLines(w.Render(width)) }

func (w *WriteToolItem) Render(width int) string {
	path := extractPath(w.input)
	header := ToolHeader(w.status, "Write", w.compact, path)
	if w.compact || w.output == "" {
		return "  " + header
	}
	return "  " + JoinToolParts(header, "  "+w.output)
}

// EditToolItem renders edit tool calls.
// Header: ● Edit path
// Body: context or "Applied"
type EditToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*EditToolItem)(nil)

func NewEditToolItem(id, input string) *EditToolItem {
	return &EditToolItem{
		baseToolItem: baseToolItem{id: id, name: "edit", input: input, status: ToolRunning},
	}
}

func (e *EditToolItem) Height(width int) int { return countLines(e.Render(width)) }

func (e *EditToolItem) Render(width int) string {
	path := extractPath(e.input)
	header := ToolHeader(e.status, "Edit", e.compact, path)
	if e.compact || e.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(e.output, defaultCollapsedLines, e.expanded)
	return "  " + JoinToolParts(header, body)
}

// ViewToolItem renders view tool calls.
type ViewToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*ViewToolItem)(nil)

func NewViewToolItem(id, input string) *ViewToolItem {
	return &ViewToolItem{
		baseToolItem: baseToolItem{id: id, name: "view", input: input, status: ToolRunning},
	}
}

func (v *ViewToolItem) Height(width int) int { return countLines(v.Render(width)) }

func (v *ViewToolItem) Render(width int) string {
	path := extractPath(v.input)
	var params struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	json.Unmarshal([]byte(v.input), &params)
	detail := path
	if params.Offset > 0 || params.Limit > 0 {
		detail = fmt.Sprintf("%s [%d:%d]", path, params.Offset, params.Offset+params.Limit)
	}

	header := ToolHeader(v.status, "View", v.compact, detail)
	if v.compact || v.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(v.output, defaultCollapsedLines, v.expanded)
	return "  " + JoinToolParts(header, body)
}

// MultiEditToolItem renders multiedit tool calls.
type MultiEditToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*MultiEditToolItem)(nil)

func NewMultiEditToolItem(id, input string) *MultiEditToolItem {
	return &MultiEditToolItem{
		baseToolItem: baseToolItem{id: id, name: "multiedit", input: input, status: ToolRunning},
	}
}

func (m *MultiEditToolItem) Height(width int) int { return countLines(m.Render(width)) }

func (m *MultiEditToolItem) Render(width int) string {
	path := extractPath(m.input)
	header := ToolHeader(m.status, "MultiEdit", m.compact, path)
	if m.compact || m.output == "" {
		return "  " + header
	}
	return "  " + JoinToolParts(header, "  "+m.output)
}

// extractPath pulls "path" or "file_path" from JSON input.
func extractPath(input string) string {
	var params struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal([]byte(input), &params) == nil {
		if params.Path != "" {
			return params.Path
		}
		return params.FilePath
	}
	return ""
}
