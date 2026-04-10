package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

type sessionReadInput struct {
	SessionID string `json:"session_id" jsonschema:"description=The session ID to read the transcript of."`
	Last      int    `json:"last,omitempty" jsonschema:"description=Number of recent messages to return. Default 5."`
}

// SessionReadTool creates a tool that reads a session's conversation transcript.
// Returns the last N assistant messages and tool results, formatted as text.
// This is how the orchestrator reads completed child session results on resume.
func SessionReadTool(sessions *persist.SessionStore) Tool {
	at := fantasy.NewAgentTool("session_read",
		"Read the conversation transcript of a session by ID. "+
			"Returns recent assistant messages and tool results. "+
			"Use this to read the output of completed child agent sessions.",
		func(ctx context.Context, input sessionReadInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.SessionID == "" {
				return fantasy.NewTextErrorResponse("session_id is required"), nil
			}

			last := input.Last
			if last <= 0 {
				last = 5
			}
			if last > 20 {
				last = 20
			}

			// Get session metadata for context
			meta, err := sessions.GetMeta(input.SessionID)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("session not found: %v", err)), nil
			}

			// Read session events
			events, err := sessions.ReadEvents(input.SessionID)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to read session: %v", err)), nil
			}

			// Extract assistant messages and tool results
			type entry struct {
				Type    string
				Content string
			}
			var entries []entry

			for _, ev := range events {
				switch ev.Type {
				case persist.EventAssistantMessage:
					var msg persist.MessageData
					if json.Unmarshal(ev.Data, &msg) == nil && msg.Content != "" {
						entries = append(entries, entry{Type: "assistant", Content: msg.Content})
					}
				case persist.EventToolResult:
					var tc persist.ToolCallData
					if json.Unmarshal(ev.Data, &tc) == nil && tc.Output != "" {
						// Truncate very large tool outputs
						output := tc.Output
						if len(output) > 2000 {
							output = output[:2000] + "\n[truncated]"
						}
						entries = append(entries, entry{Type: "tool_result:" + tc.Name, Content: output})
					}
				}
			}

			// Take last N entries
			if len(entries) > last {
				entries = entries[len(entries)-last:]
			}

			if len(entries) == 0 {
				return fantasy.NewTextResponse("No messages found in session " + input.SessionID), nil
			}

			// Format output
			var sb strings.Builder
			if meta != nil {
				fmt.Fprintf(&sb, "Session: %s (agent: %s, status: %s, messages: %d)\n\n",
					meta.ID, meta.AgentID, meta.Status, meta.MessageCount)
			}
			for i, e := range entries {
				fmt.Fprintf(&sb, "--- [%s] ---\n%s\n", e.Type, e.Content)
				if i < len(entries)-1 {
					sb.WriteString("\n")
				}
			}

			return fantasy.NewTextResponse(sb.String()), nil
		},
	)

	return Wrap(at, TrustReadOnly, "agent")
}
