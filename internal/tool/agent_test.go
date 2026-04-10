package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/bus"
)

func TestAgentToolMetadata(t *testing.T) {
	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			return "result", "sess-1", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)
	info := tool.Info()
	if info.Name != "agent" {
		t.Errorf("name = %q, want agent", info.Name)
	}
}

func TestAgentToolNamedDispatch(t *testing.T) {
	var capturedAgent, capturedPrompt string

	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			capturedAgent = agentID
			capturedPrompt = prompt
			return "task completed", "sess-2", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)

	input := agentInput{Agent: "coding", Prompt: "write hello world"}
	inputJSON, _ := json.Marshal(input)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: string(inputJSON),
	})
	if err != nil {
		t.Fatal(err)
	}

	if capturedAgent != "coding" {
		t.Errorf("dispatched to %q, want coding", capturedAgent)
	}
	if capturedPrompt != "write hello world" {
		t.Errorf("prompt = %q, want 'write hello world'", capturedPrompt)
	}

	if !strings.Contains(resp.Content, "task completed") {
		t.Errorf("response should contain agent output, got: %s", resp.Content)
	}
}

func TestAgentToolMissingAgent(t *testing.T) {
	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			return "", "", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"prompt":"test"}`,
	})
	if !resp.IsError {
		t.Error("expected error for missing agent")
	}
}

func TestAgentToolMissingPrompt(t *testing.T) {
	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			return "", "", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"agent":"coding"}`,
	})
	if !resp.IsError {
		t.Error("expected error for missing prompt")
	}
}

func TestAgentToolExcludesSelf(t *testing.T) {
	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			return "done", "s", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)
	// The tool should work for any agent name — filtering is done by the agent's RequiredTools
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"agent":"orchestrator","prompt":"test"}`,
	})
	if resp.IsError {
		t.Error("should allow any agent name")
	}
}

func TestAgentToolLongContent(t *testing.T) {
	longContent := strings.Repeat("x", 10000)
	deps := SubAgentDeps{
		RunSubAgent: func(ctx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
			return longContent, "s", nil
		},
		Bus:    bus.New(),
		Logger: slog.Default(),
	}

	tool := AgentTool(deps)
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"agent":"coding","prompt":"test"}`,
	})

	// Full content returned inside structured completion format, no truncation
	if !strings.Contains(resp.Content, longContent) {
		t.Error("structured result should contain the full agent output")
	}
	if !strings.Contains(resp.Content, "<<<BEGIN_AGENT_RESULT>>>") {
		t.Error("result should have agent result markers")
	}
	if !strings.Contains(resp.Content, "Action required:") {
		t.Error("result should have reply instruction")
	}
}
