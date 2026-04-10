package chat

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Max collapsed output lines before "click to expand".
const defaultCollapsedLines = 10
const nestedCollapsedLines = 3

// Icons by status.
const (
	IconPending = "●"
	IconRunning = "●"
	IconSuccess = "✓"
	IconError   = "✗"
)

var (
	iconPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B8B8B"))
	iconRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4A017"))
	iconSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#50C878"))
	iconErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))
	toolNameStyle    = lipgloss.NewStyle().Bold(true)
	toolParamStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B8B8B"))
	toolBodyStyle    = lipgloss.NewStyle().PaddingLeft(2)
	truncHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Italic(true)
	nestedPrefix     = "    │ "
	agentLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4A017")).Bold(true)
	taskTagStyle     = lipgloss.NewStyle().Background(lipgloss.Color("#333333")).Foreground(lipgloss.Color("#D4A017")).Padding(0, 1)
)

// ToolIcon returns the styled icon for a tool status.
func ToolIcon(status ToolStatus) string {
	switch status {
	case ToolPending:
		return iconPendingStyle.Render(IconPending)
	case ToolRunning:
		return iconRunningStyle.Render(IconRunning)
	case ToolSuccess:
		return iconSuccessStyle.Render(IconSuccess)
	case ToolError:
		return iconErrorStyle.Render(IconError)
	default:
		return iconPendingStyle.Render(IconPending)
	}
}

// ToolHeader renders "● ToolName params..." with status icon.
func ToolHeader(status ToolStatus, name string, compact bool, params ...string) string {
	icon := ToolIcon(status)
	styledName := toolNameStyle.Render(name)

	if compact {
		if len(params) > 0 && params[0] != "" {
			param := params[0]
			if len(param) > 60 {
				param = param[:60] + "…"
			}
			return icon + " " + styledName + " " + toolParamStyle.Render(param)
		}
		return icon + " " + styledName
	}

	header := icon + " " + styledName
	if len(params) > 0 && params[0] != "" {
		header += " " + toolParamStyle.Render(params[0])
	}
	return header
}

// CollapsedOutput renders output with collapse at maxLines.
// If expanded, shows all. Otherwise truncates with hint.
func CollapsedOutput(content string, maxLines int, expanded bool) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	if expanded || len(lines) <= maxLines {
		return toolBodyStyle.Render(content)
	}

	visible := strings.Join(lines[:maxLines], "\n")
	hidden := len(lines) - maxLines
	hint := truncHintStyle.Render(fmt.Sprintf("  … (%d lines hidden)", hidden))
	return toolBodyStyle.Render(visible) + "\n" + hint
}

// JoinToolParts joins header and body with a newline separator.
func JoinToolParts(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "\n")
}
