package chat

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// MessageList is a scrollable list of message items.
type MessageList struct {
	items        []MessageItem
	toolIndex    map[string]ToolMessageItem  // toolCallID → item, for result matching
	agentIndex   map[string]*AgentToolItem   // taskID → agent item, for nesting
	width        int
	height       int
	scrollOffset int  // lines from bottom (0 = follow)
	followMode   bool
}

// NewMessageList creates an empty message list.
func NewMessageList() *MessageList {
	return &MessageList{
		toolIndex:  make(map[string]ToolMessageItem),
		agentIndex: make(map[string]*AgentToolItem),
		followMode: true,
	}
}

// SetSize sets the viewport dimensions.
func (l *MessageList) SetSize(w, h int) {
	l.width = w
	l.height = h
}

// Add appends a message item to the list.
func (l *MessageList) Add(item MessageItem) {
	l.items = append(l.items, item)
}

// AddTool adds a tool item and indexes it for result matching.
func (l *MessageList) AddTool(toolCallID string, item ToolMessageItem) {
	l.items = append(l.items, item)
	l.toolIndex[toolCallID] = item
}

// AddAgentTool adds an agent tool item and indexes it by task ID.
func (l *MessageList) AddAgentTool(toolCallID, taskID string, item *AgentToolItem) {
	l.items = append(l.items, item)
	l.toolIndex[toolCallID] = item
	l.agentIndex[taskID] = item
}

// GetTool returns a tool item by call ID.
func (l *MessageList) GetTool(toolCallID string) (ToolMessageItem, bool) {
	item, ok := l.toolIndex[toolCallID]
	return item, ok
}

// GetAgent returns an agent tool item by task ID.
func (l *MessageList) GetAgent(taskID string) (*AgentToolItem, bool) {
	item, ok := l.agentIndex[taskID]
	return item, ok
}

// AddNestedTool adds a tool as a child of an agent tool item.
func (l *MessageList) AddNestedTool(taskID, toolCallID string, item ToolMessageItem) {
	if agent, ok := l.agentIndex[taskID]; ok {
		agent.AddNestedTool(item)
		l.toolIndex[toolCallID] = item
	}
}

// SetToolResult updates a tool item with its result.
func (l *MessageList) SetToolResult(toolCallID, output string, isError bool) {
	if item, ok := l.toolIndex[toolCallID]; ok {
		item.SetResult(output, isError)
	}
}

// ToggleExpandAll expands or collapses all tool items.
// Returns the new expanded state.
func (l *MessageList) ToggleExpandAll() bool {
	// Check if any are currently expanded — if so, collapse all; otherwise expand all
	anyExpanded := false
	for _, item := range l.items {
		if ti, ok := item.(ToolMessageItem); ok && ti.IsExpanded() {
			anyExpanded = true
			break
		}
	}
	newState := !anyExpanded
	for _, item := range l.items {
		if ti, ok := item.(ToolMessageItem); ok {
			if ti.IsExpanded() != newState {
				ti.ToggleExpanded()
			}
			// Also expand nested tools inside agent items
			if nc, ok := item.(NestedToolContainer); ok {
				for _, nested := range nc.NestedTools() {
					if nested.IsExpanded() != newState {
						nested.ToggleExpanded()
					}
				}
			}
		}
	}
	return newState
}

// Clear removes all items.
// Len returns the number of top-level items in the list.
func (l *MessageList) Len() int { return len(l.items) }

func (l *MessageList) Clear() {
	l.items = nil
	l.toolIndex = make(map[string]ToolMessageItem)
	l.agentIndex = make(map[string]*AgentToolItem)
	l.scrollOffset = 0
	l.followMode = true
}

// ScrollBy adjusts scroll offset by delta lines.
func (l *MessageList) ScrollBy(delta int) {
	l.scrollOffset += delta
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
		l.followMode = true
	}
	if l.scrollOffset > 0 {
		l.followMode = false
	}
}

// ScrollToBottom resets to follow mode.
func (l *MessageList) ScrollToBottom() {
	l.scrollOffset = 0
	l.followMode = true
}

// ScrollOffset returns current scroll offset.
func (l *MessageList) ScrollOffset() int { return l.scrollOffset }

// ScrollToTop scrolls to the beginning.
func (l *MessageList) ScrollToTop() {
	l.followMode = false
	// Set to a very large offset; Render will clamp it
	l.scrollOffset = 999999
}

// Render renders the visible portion of the message list.
func (l *MessageList) Render(streamingText string) string {
	width := l.width
	height := l.height
	if width <= 0 {
		width = 120 // default for tests
	}
	if height <= 0 {
		height = 9999 // show all for tests
	}

	// Render all items into lines
	var allLines []string
	for _, item := range l.items {
		rendered := item.Render(width)
		for _, line := range strings.Split(rendered, "\n") {
			allLines = append(allLines, wrapLine(line, width)...)
		}
		allLines = append(allLines, "") // gap between items
	}

	// Add streaming text
	if streamingText != "" {
		for _, line := range strings.Split(streamingText, "\n") {
			allLines = append(allLines, wrapLine(line, width)...)
		}
	}

	totalLines := len(allLines)

	// Follow mode: pin to bottom
	if l.followMode || totalLines <= height {
		l.scrollOffset = 0
	}

	// Clamp scroll offset
	maxScroll := totalLines - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if l.scrollOffset > maxScroll {
		l.scrollOffset = maxScroll
	}

	// Calculate visible range from bottom
	end := totalLines - l.scrollOffset
	if end > totalLines {
		end = totalLines
	}
	start := end - height
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for _, line := range allLines[start:end] {
		b.WriteString(line + "\n")
	}

	return b.String()
}

// wrapLine hard-wraps a line at maxWidth using ANSI-aware wrapping.
func wrapLine(line string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{line}
	}
	wrapped := ansi.Hardwrap(line, maxWidth, false)
	return strings.Split(wrapped, "\n")
}
