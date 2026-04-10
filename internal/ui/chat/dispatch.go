package chat

// NewToolItem creates the appropriate ToolMessageItem for a tool call.
func NewToolItem(toolCallID, toolName, input string) ToolMessageItem {
	switch toolName {
	case "shell":
		return NewShellToolItem(toolCallID, input)
	case "read":
		return NewReadToolItem(toolCallID, input)
	case "write":
		return NewWriteToolItem(toolCallID, input)
	case "edit":
		return NewEditToolItem(toolCallID, input)
	case "multiedit":
		return NewMultiEditToolItem(toolCallID, input)
	case "view":
		return NewViewToolItem(toolCallID, input)
	case "glob":
		return NewGlobToolItem(toolCallID, input)
	case "grep":
		return NewGrepToolItem(toolCallID, input)
	case "ls":
		return NewLsToolItem(toolCallID, input)
	case "web_search":
		return NewWebSearchToolItem(toolCallID, input)
	case "agent":
		return NewAgentToolItem(toolCallID, input)
	default:
		return NewGenericToolItem(toolCallID, toolName, input)
	}
}
