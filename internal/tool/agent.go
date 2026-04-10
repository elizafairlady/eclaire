package tool

import (
	"context"
	"fmt"
	"log/slog"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/bus"
)

type agentInput struct {
	Agent  string `json:"agent" jsonschema:"description=The specialist agent to delegate to (e.g. coding, research, sysadmin, config)."`
	Prompt string `json:"prompt" jsonschema:"description=The task for the sub-agent to perform. Be specific and provide all necessary context."`
}

// SubAgentDeps holds everything the agent tool needs to run sub-agents.
type SubAgentDeps struct {
	// RunSubAgent runs a named agent with emit forwarding.
	// Returns (content, sessionID, error).
	RunSubAgent func(parentCtx context.Context, agentID, prompt, parentSessionID string) (string, string, error)

	Bus    *bus.Bus
	Logger *slog.Logger
}

// AgentTool creates the sub-agent delegation tool.
// Uses NewParallelAgentTool so fantasy runs concurrent sub-agent calls in parallel goroutines.
// Each call blocks in its own goroutine — the gateway handles concurrency.
//
// Returns structured completion events matching OpenClaw's pattern:
// - Markers around untrusted child output (<<<BEGIN_AGENT_RESULT>>>)
// - Reply instruction forcing parent to synthesize
// - Task metadata (agent, status, task label)
func AgentTool(deps SubAgentDeps) Tool {
	at := fantasy.NewParallelAgentTool("agent",
		"Run a specialist agent to perform a task. The agent runs with its own context window and tools. "+
			"Available agents: coding (programming), research (web research), sysadmin (system tasks), config (eclaire settings). "+
			"You can run multiple agents in parallel by calling this tool multiple times in one turn.",
		func(ctx context.Context, input agentInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.Agent == "" {
				return fantasy.NewTextErrorResponse("agent is required — specify which specialist to delegate to (coding, research, sysadmin, config)"), nil
			}
			if input.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			deps.Logger.Info("sub-agent dispatching",
				"agent", input.Agent,
				"prompt_len", len(input.Prompt),
			)

			taskLabel := input.Prompt
			if len(taskLabel) > 120 {
				taskLabel = taskLabel[:120] + "…"
			}

			content, sessionID, err := deps.RunSubAgent(ctx, input.Agent, input.Prompt, "")
			if err != nil {
				deps.Logger.Error("sub-agent failed",
					"agent", input.Agent,
					"err", err,
				)
				result := fmt.Sprintf("[Task Completion]\nAgent: %s\nStatus: error\nTask: %s\nError: %v\n\nAction required: Report this failure to your owner and suggest alternatives or retry with different instructions.",
					input.Agent, taskLabel, err)
				return fantasy.NewTextErrorResponse(result), nil
			}

			deps.Logger.Info("sub-agent completed",
				"agent", input.Agent,
				"session", sessionID,
				"output_len", len(content),
			)

			// Return structured completion with markers and reply instruction.
			// The markers tell the parent that content inside is DATA from a child agent,
			// not instructions to follow. The reply instruction forces the parent to
			// synthesize results into a user-facing response.
			result := fmt.Sprintf(`[Task Completion]
Agent: %s
Status: completed
Task: %s

<<<BEGIN_AGENT_RESULT>>>
%s
<<<END_AGENT_RESULT>>>

Action required: Read the agent's result above carefully. Synthesize the findings into a clear, complete response for your owner. Present the full substance organized by topic — do not just echo a summary line. If the result is incomplete or confused, note what is missing. If you delegated to multiple agents, combine ALL results into one coherent response before replying to your owner.`,
				input.Agent, taskLabel, content)
			return fantasy.NewTextResponse(result), nil
		},
	)

	return Wrap(at, TrustModify, "agent")
}
