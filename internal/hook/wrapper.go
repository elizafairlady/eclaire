package hook

import (
	"context"
	"log/slog"

	"charm.land/fantasy"
)

// SessionFromContextFunc extracts a session ID from context.
// Injected to avoid import cycle with agent package.
type SessionFromContextFunc func(ctx context.Context) string

// ToolWrapper wraps a fantasy.AgentTool with pre/post hook execution.
type ToolWrapper struct {
	inner      fantasy.AgentTool
	runner     *Runner
	agentID    string
	logger     *slog.Logger
	sessionFn  SessionFromContextFunc
}

// WrapTool creates a hook-aware wrapper around a tool.
// Returns inner unchanged if runner is nil.
func WrapTool(inner fantasy.AgentTool, runner *Runner, agentID string, logger *slog.Logger, sessionFn SessionFromContextFunc) fantasy.AgentTool {
	if runner == nil {
		return inner
	}
	return &ToolWrapper{
		inner:     inner,
		runner:    runner,
		agentID:   agentID,
		logger:    logger,
		sessionFn: sessionFn,
	}
}

func (w *ToolWrapper) Info() fantasy.ToolInfo {
	return w.inner.Info()
}

func (w *ToolWrapper) ProviderOptions() fantasy.ProviderOptions {
	return w.inner.ProviderOptions()
}

func (w *ToolWrapper) SetProviderOptions(opts fantasy.ProviderOptions) {
	w.inner.SetProviderOptions(opts)
}

func (w *ToolWrapper) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	toolName := w.inner.Info().Name
	var sessionID string
	if w.sessionFn != nil {
		sessionID = w.sessionFn(ctx)
	}

	// Pre-hooks
	updatedInput, denied, denyMsg, err := w.runner.RunPre(ctx, toolName, call.Input, w.agentID, sessionID)
	if err != nil {
		w.logger.Warn("pre-hook error", "tool", toolName, "err", err)
		// Non-fatal: continue with original input
	}
	if denied {
		return fantasy.NewTextErrorResponse(denyMsg), nil
	}
	if updatedInput != "" {
		call.Input = updatedInput
	}

	// Execute actual tool
	resp, toolErr := w.inner.Run(ctx, call)

	// Post-hooks
	if toolErr != nil {
		w.runner.RunPostFailure(ctx, toolName, call.Input, toolErr.Error(), w.agentID, sessionID)
	} else if resp.IsError {
		w.runner.RunPostFailure(ctx, toolName, call.Input, resp.Content, w.agentID, sessionID)
	} else {
		extraMsg, err := w.runner.RunPost(ctx, toolName, call.Input, resp.Content, w.agentID, sessionID)
		if err != nil {
			w.logger.Warn("post-hook error", "tool", toolName, "err", err)
		}
		if extraMsg != "" {
			resp.Content += "\n\n[Hook] " + extraMsg
		}
	}

	return resp, toolErr
}
