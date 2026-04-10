package chat

import (
	"encoding/json"
	"strings"
)

// ShellToolItem renders shell/bash tool calls.
// Header: ● Shell command...
// Body: output (collapsed)
type ShellToolItem struct {
	baseToolItem
}

var _ ToolMessageItem = (*ShellToolItem)(nil)

func NewShellToolItem(id, input string) *ShellToolItem {
	return &ShellToolItem{
		baseToolItem: baseToolItem{
			id:     id,
			name:   "shell",
			input:  input,
			status: ToolRunning,
		},
	}
}

func (s *ShellToolItem) Height(width int) int { return countLines(s.Render(width)) }

func (s *ShellToolItem) Render(width int) string {
	// Extract command from JSON input
	cmd := s.extractCommand()

	header := ToolHeader(s.status, "Shell", s.compact, cmd)
	if s.compact || s.output == "" {
		return "  " + header
	}

	body := CollapsedOutput(s.output, defaultCollapsedLines, s.expanded)
	return "  " + JoinToolParts(header, body)
}

func (s *ShellToolItem) extractCommand() string {
	var params struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(s.input), &params) == nil && params.Command != "" {
		cmd := strings.ReplaceAll(params.Command, "\n", " ")
		return cmd
	}
	return ""
}
