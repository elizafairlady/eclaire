package persist

import (
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
)

// RebuildMessages converts session events into fantasy messages for session continuation.
func RebuildMessages(events []SessionEvent) []fantasy.Message {
	var result []fantasy.Message

	for _, ev := range events {
		switch ev.Type {
		case EventUserMessage:
			var msg MessageData
			json.Unmarshal(ev.Data, &msg)
			result = append(result, fantasy.NewUserMessage(msg.Content))

		case EventAssistantMessage:
			var msg MessageData
			json.Unmarshal(ev.Data, &msg)
			result = append(result, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: []fantasy.MessagePart{fantasy.TextPart{Text: msg.Content}},
			})

		case EventToolCall:
			var tc ToolCallData
			json.Unmarshal(ev.Data, &tc)
			// Tool calls are part of the assistant's message — append as tool call part
			// to the last assistant message, or create a new one
			if len(result) > 0 && result[len(result)-1].Role == fantasy.MessageRoleAssistant {
				result[len(result)-1].Content = append(result[len(result)-1].Content,
					fantasy.ToolCallPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.Name,
						Input:      tc.Input,
					})
			} else {
				result = append(result, fantasy.Message{
					Role: fantasy.MessageRoleAssistant,
					Content: []fantasy.MessagePart{
						fantasy.ToolCallPart{
							ToolCallID: tc.ToolCallID,
							ToolName:   tc.Name,
							Input:      tc.Input,
						},
					},
				})
			}

		case EventToolResult:
			var tc ToolCallData
			json.Unmarshal(ev.Data, &tc)
			result = append(result, fantasy.Message{
				Role: fantasy.MessageRoleTool,
				Content: []fantasy.MessagePart{
					fantasy.ToolResultPart{
						ToolCallID: tc.ToolCallID,
						Output:     fantasy.ToolResultOutputContentText{Text: tc.Output},
					},
				},
			})

		case EventChildSpawned:
			var cd ChildData
			json.Unmarshal(ev.Data, &cd)
			// Represent child spawn as system context
			result = append(result, fantasy.NewSystemMessage(
				fmt.Sprintf("[Sub-agent %s started: %s]", cd.ChildAgentID, cd.Title)))

		case EventChildCompleted:
			var cd ChildData
			json.Unmarshal(ev.Data, &cd)
			result = append(result, fantasy.NewSystemMessage(
				fmt.Sprintf("[Sub-agent %s completed: %s]", cd.ChildAgentID, cd.Result)))

		case EventCompaction:
			// Compaction replaces all prior messages with a summary
			var msg MessageData
			json.Unmarshal(ev.Data, &msg)
			result = []fantasy.Message{
				fantasy.NewUserMessage("[Session summary]\n" + msg.Content),
			}

		case EventMemoryFlush:
			var msg MessageData
			json.Unmarshal(ev.Data, &msg)
			result = append(result, fantasy.NewSystemMessage(
				"[Memory saved before compaction]"))

		// Skip: assistant_delta, system_message, step_finish, todo_update
		}
	}

	return result
}

// ToFantasyMessages is a convenience wrapper for backward compatibility.
func ToFantasyMessages(events []SessionEvent) []fantasy.Message {
	return RebuildMessages(events)
}
