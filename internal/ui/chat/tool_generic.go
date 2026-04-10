package chat

import "strings"

// GenericToolItem is the fallback renderer for tools without a specific implementation.
// Header: ● ToolName params
// Body: output (collapsed)
type GenericToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*GenericToolItem)(nil)

func NewGenericToolItem(id, name, input string) *GenericToolItem {
	return &GenericToolItem{
		baseToolItem: baseToolItem{
			id:     id,
			name:   name,
			input:  input,
			status: ToolRunning,
		},
	}
}

func (g *GenericToolItem) Height(width int) int { return countLines(g.Render(width)) }

func (g *GenericToolItem) Render(width int) string {
	prettyName := prettyToolName(g.name)
	header := ToolHeader(g.status, prettyName, g.compact)
	if g.compact || g.output == "" {
		return "  " + header
	}
	body := CollapsedOutput(g.output, defaultCollapsedLines, g.expanded)
	return "  " + JoinToolParts(header, body)
}

// prettyToolName converts snake_case to Title Case.
func prettyToolName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
