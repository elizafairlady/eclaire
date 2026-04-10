package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
)

type taskStatusInput struct {
	SessionID string `json:"session_id" jsonschema:"description=The session ID of the child task to check."`
}

// TaskStatusTool creates a tool that checks the status of a child session.
func TaskStatusTool(sessions *persist.SessionStore) Tool {
	at := fantasy.NewAgentTool("task_status",
		"Check the status of a sub-agent task by its session ID. "+
			"Returns the session metadata including status, token usage, and child sessions.",
		func(ctx context.Context, input taskStatusInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.SessionID == "" {
				return fantasy.NewTextErrorResponse("session_id is required"), nil
			}

			meta, err := sessions.GetMeta(input.SessionID)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("session not found: %v", err)), nil
			}

			result := map[string]any{
				"id":        meta.ID,
				"agent_id":  meta.AgentID,
				"status":    meta.Status,
				"title":     meta.Title,
				"tokens_in": meta.TokensIn,
				"tokens_out": meta.TokensOut,
				"messages":  meta.MessageCount,
			}
			if meta.ParentID != "" {
				result["parent_id"] = meta.ParentID
			}
			if len(meta.ChildIDs) > 0 {
				result["child_ids"] = meta.ChildIDs
			}

			data, _ := json.MarshalIndent(result, "", "  ")
			return fantasy.NewTextResponse(string(data)), nil
		},
	)

	return Wrap(at, TrustReadOnly, "agent")
}
