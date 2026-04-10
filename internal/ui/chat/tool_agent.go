package chat

import (
	"encoding/json"
	"strings"
)

// maxVisibleNestedTools limits how many nested tools show to prevent flooding.
const maxVisibleNestedTools = 5

// AgentToolItem renders agent delegation as a tree with nested compact children.
//
// ● Agent coding
//   Task  Write a reverse string function...
//   ├── ✓ Write /tmp/reverse.go
//   ├── ✓ Shell go build -o /tmp/reverse /tmp/reverse.go
//   ╰── ✓ Shell /tmp/reverse
type AgentToolItem struct {
	baseToolItem
	nestedTools []ToolMessageItem
	prompt      string // extracted from input
}

var (
	_ ToolMessageItem     = (*AgentToolItem)(nil)
	_ NestedToolContainer = (*AgentToolItem)(nil)
)

func NewAgentToolItem(id, input string) *AgentToolItem {
	var params struct {
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
	}
	json.Unmarshal([]byte(input), &params)

	return &AgentToolItem{
		baseToolItem: baseToolItem{
			id:     id,
			name:   "agent",
			input:  input,
			status: ToolRunning,
		},
		prompt: params.Prompt,
	}
}

func (a *AgentToolItem) NestedTools() []ToolMessageItem { return a.nestedTools }

func (a *AgentToolItem) AddNestedTool(tool ToolMessageItem) {
	tool.SetCompact(true)
	a.nestedTools = append(a.nestedTools, tool)
}

func (a *AgentToolItem) Height(width int) int { return countLines(a.Render(width)) }

func (a *AgentToolItem) Render(width int) string {
	var params struct {
		Agent string `json:"agent"`
	}
	json.Unmarshal([]byte(a.input), &params)

	header := "  " + ToolHeader(a.status, "Agent", false, params.Agent)

	// Task tag with prompt (word-wrapped to available width)
	taskLine := ""
	if a.prompt != "" {
		prompt := a.prompt
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		maxPromptLen := width - 16 // "  Task  " + some margin
		if maxPromptLen > 0 && len(prompt) > maxPromptLen {
			prompt = prompt[:maxPromptLen] + "…"
		}
		taskLine = "  " + taskTagStyle.Render("Task") + "  " + toolParamStyle.Render(prompt)
	}

	if a.compact {
		return header
	}

	var b strings.Builder
	b.WriteString(header)
	if taskLine != "" {
		b.WriteString("\n" + taskLine)
	}

	// Render nested tools as tree
	tools := a.nestedTools
	startIdx := 0
	if len(tools) > maxVisibleNestedTools {
		startIdx = len(tools) - maxVisibleNestedTools
		b.WriteString("\n" + nestedPrefix + toolParamStyle.Render(
			strings.Repeat("·", 3)+" ("+strings.Repeat(" ", 0)+
				string(rune('0'+len(tools)-maxVisibleNestedTools))+
				" earlier tools hidden)"))
	}

	for i := startIdx; i < len(tools); i++ {
		connector := "  ├── "
		if i == len(tools)-1 {
			connector = "  ╰── "
		}
		// Render the nested tool without its own "  " prefix
		rendered := tools[i].Render(width - 8) // account for tree prefix
		// Strip leading whitespace from the nested tool's render
		rendered = strings.TrimLeft(rendered, " ")
		b.WriteString("\n" + connector + rendered)
	}

	// Show final result body if complete and expanded
	if a.output != "" && a.expanded {
		body := CollapsedOutput(a.output, defaultCollapsedLines, true)
		b.WriteString("\n" + body)
	}

	return b.String()
}
