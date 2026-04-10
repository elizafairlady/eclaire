package tool

import (
	"context"

	"charm.land/fantasy"
)

// TrustTier controls when a tool requires human approval.
type TrustTier int

const (
	TrustReadOnly  TrustTier = 0 // auto-approved
	TrustModify    TrustTier = 1 // confirm on first use per session
	TrustDangerous TrustTier = 2 // always confirm
)

// Tool extends fantasy.AgentTool with eclaire-specific metadata.
type Tool interface {
	fantasy.AgentTool
	TrustTier() TrustTier
	Category() string
}

// wrapper adapts a fantasy.AgentTool with eclaire metadata.
type wrapper struct {
	fantasy.AgentTool
	tier     TrustTier
	category string
}

func (w *wrapper) TrustTier() TrustTier { return w.tier }
func (w *wrapper) Category() string     { return w.category }

// Wrap creates an eclaire Tool from a fantasy.AgentTool.
func Wrap(at fantasy.AgentTool, tier TrustTier, category string) Tool {
	return &wrapper{AgentTool: at, tier: tier, category: category}
}

// NewTool creates a typed eclaire Tool using fantasy's generic tool builder.
func NewTool[T any](name, description string, tier TrustTier, category string, fn func(context.Context, T, fantasy.ToolCall) (fantasy.ToolResponse, error)) Tool {
	at := fantasy.NewAgentTool(name, description, fn)
	return Wrap(at, tier, category)
}
