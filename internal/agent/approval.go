package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/google/uuid"
)

// ApprovalRequest is sent when an agent needs human approval.
type ApprovalRequest struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	Action      string `json:"action"`
	Description string `json:"description"`
	Details     any    `json:"details,omitempty"`
}

// ApprovalResult is the human's response.
type ApprovalResult struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// ApprovalGate manages human-in-the-loop approval flows.
type ApprovalGate struct {
	bus     *bus.Bus
	pending map[string]chan ApprovalResult
	mu      sync.Mutex
}

// NewApprovalGate creates a new approval gate.
func NewApprovalGate(msgBus *bus.Bus) *ApprovalGate {
	return &ApprovalGate{
		bus:     msgBus,
		pending: make(map[string]chan ApprovalResult),
	}
}

// Request sends an approval request and blocks until the user responds.
func (g *ApprovalGate) Request(ctx context.Context, agentID, action, description string, details any) (ApprovalResult, error) {
	req := ApprovalRequest{
		ID:          uuid.NewString()[:8],
		AgentID:     agentID,
		Action:      action,
		Description: description,
		Details:     details,
	}

	ch := make(chan ApprovalResult, 1)
	g.mu.Lock()
	g.pending[req.ID] = ch
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, req.ID)
		g.mu.Unlock()
	}()

	// Publish the request for the CLI to pick up
	g.bus.Publish(bus.TopicApprovalRequest, req)

	select {
	case result := <-ch:
		g.bus.Publish(bus.TopicApprovalResult, map[string]any{
			"request_id": req.ID,
			"approved":   result.Approved,
		})
		return result, nil
	case <-ctx.Done():
		return ApprovalResult{}, fmt.Errorf("approval timed out")
	}
}

// Respond completes a pending approval request.
func (g *ApprovalGate) Respond(requestID string, result ApprovalResult) error {
	g.mu.Lock()
	ch, ok := g.pending[requestID]
	g.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending approval with ID %q", requestID)
	}

	ch <- result
	return nil
}

// PendingCount returns the number of pending approvals.
func (g *ApprovalGate) PendingCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.pending)
}
