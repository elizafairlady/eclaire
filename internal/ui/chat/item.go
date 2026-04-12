// Package chat implements structured message rendering for the TUI.
// Each tool type has its own renderer. Agent tools render as trees with
// compact nested children. Output is collapsed by default, expandable.
package chat

import "strings"

// MessageItem is a renderable item in the chat message list.
type MessageItem interface {
	ID() string
	Render(width int) string
	Height(width int) int
}

// ToolStatus tracks the state of a tool call.
type ToolStatus int

const (
	ToolPending ToolStatus = iota
	ToolRunning
	ToolSuccess
	ToolError
)

// ToolMessageItem is a tool call with status tracking, result, and expand/collapse.
type ToolMessageItem interface {
	MessageItem
	ToolName() string
	ToolInput() string
	SetResult(output string, isError bool)
	Status() ToolStatus
	SetStatus(status ToolStatus)
	IsExpanded() bool
	ToggleExpanded()
	SetCompact(compact bool)
	IsCompact() bool
}

// NestedToolContainer is a tool item that contains child tool calls (e.g. agent).
type NestedToolContainer interface {
	ToolMessageItem
	AddNestedTool(tool ToolMessageItem)
	NestedTools() []ToolMessageItem
}

// UserMessageItem is a user's message.
type UserMessageItem struct {
	id      string
	content string
}

func NewUserMessage(id, content string) *UserMessageItem {
	return &UserMessageItem{id: id, content: content}
}

func (u *UserMessageItem) ID() string              { return u.id }
func (u *UserMessageItem) Height(width int) int     { return countLines(u.Render(width)) }
func (u *UserMessageItem) Render(width int) string {
	return "\033[1myou\033[0m\n" + u.content
}

// AssistantMessageItem is the assistant's text response.
type AssistantMessageItem struct {
	id       string
	content  string
	renderFn func(content string, width int) string // markdown renderer

	// Render cache — avoids re-running glamour every frame.
	// Invalidated by SetContent() (streaming updates) or width change.
	cachedRender string
	cachedWidth  int
}

func NewAssistantMessage(id, content string, renderFn func(string, int) string) *AssistantMessageItem {
	return &AssistantMessageItem{id: id, content: content, renderFn: renderFn}
}

func (a *AssistantMessageItem) ID() string      { return a.id }
func (a *AssistantMessageItem) Content() string  { return a.content }
func (a *AssistantMessageItem) Height(width int) int { return countLines(a.Render(width)) }

func (a *AssistantMessageItem) SetContent(s string) {
	a.content = s
	a.cachedRender = ""
	a.cachedWidth = 0
}

func (a *AssistantMessageItem) Render(width int) string {
	if a.cachedWidth == width && a.cachedRender != "" {
		return a.cachedRender
	}

	var rendered string
	if a.renderFn != nil {
		rendered = a.renderFn(a.content, width)
	} else {
		rendered = a.content
	}

	// Per-line left padding (2 spaces) — consistent indent for assistant text
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	rendered = strings.Join(lines, "\n")

	a.cachedRender = rendered
	a.cachedWidth = width
	return rendered
}

// SystemMessageItem is a system/error message.
type SystemMessageItem struct {
	id      string
	content string
}

func NewSystemMessage(id, content string) *SystemMessageItem {
	return &SystemMessageItem{id: id, content: content}
}

func (s *SystemMessageItem) ID() string          { return s.id }
func (s *SystemMessageItem) Height(width int) int { return countLines(s.Render(width)) }
func (s *SystemMessageItem) Render(width int) string {
	return "  \033[3m" + s.content + "\033[0m" // italic
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
