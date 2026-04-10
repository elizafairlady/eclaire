package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
)

// Coordinator manages agent lifecycle and inter-agent communication.
type Coordinator struct {
	registry *Registry
	bus      *bus.Bus
	logger   *slog.Logger

	active map[string]context.CancelFunc // agentID -> cancel
	mu     sync.Mutex
}

// NewCoordinator creates a new Coordinator.
func NewCoordinator(registry *Registry, msgBus *bus.Bus, logger *slog.Logger) *Coordinator {
	return &Coordinator{
		registry: registry,
		bus:      msgBus,
		logger:   logger,
		active:   make(map[string]context.CancelFunc),
	}
}

// Spawn starts an agent in the background.
func (c *Coordinator) Spawn(ctx context.Context, agentID string, deps AgentDeps) error {
	a, ok := c.registry.Get(agentID)
	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}

	if err := a.Init(ctx, deps); err != nil {
		return fmt.Errorf("init agent %q: %w", agentID, err)
	}

	agentCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.active[agentID] = cancel
	c.mu.Unlock()

	c.registry.SetStatus(agentID, StatusRunning)
	c.bus.Publish(bus.TopicAgentStarted, bus.AgentEvent{
		AgentID: agentID,
		Name:    a.Name(),
		Status:  string(StatusRunning),
	})

	// If it's a background agent, start its heartbeat loop
	if bg, ok := a.(BackgroundAgent); ok {
		go c.runHeartbeat(agentCtx, bg)
	}

	c.logger.Info("agent spawned", "id", agentID)
	return nil
}

// Kill stops a running agent.
func (c *Coordinator) Kill(agentID string) error {
	c.mu.Lock()
	cancel, ok := c.active[agentID]
	if ok {
		delete(c.active, agentID)
	}
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %q not active", agentID)
	}

	cancel()
	c.registry.SetStatus(agentID, StatusIdle)
	c.bus.Publish(bus.TopicAgentCompleted, bus.AgentEvent{
		AgentID: agentID,
		Status:  string(StatusIdle),
	})

	a, _ := c.registry.Get(agentID)
	if a != nil {
		a.Shutdown(context.Background())
	}

	c.logger.Info("agent killed", "id", agentID)
	return nil
}

// Delegate sends a request from one agent to another and waits for the response.
func (c *Coordinator) Delegate(ctx context.Context, fromAgentID, toAgentID string, req Request) (Response, error) {
	a, ok := c.registry.Get(toAgentID)
	if !ok {
		return Response{}, fmt.Errorf("agent %q not found", toAgentID)
	}

	c.logger.Info("delegating",
		"from", fromAgentID,
		"to", toAgentID,
		"prompt", req.Prompt[:min(len(req.Prompt), 50)],
	)

	return a.Handle(ctx, req)
}

// Active returns the IDs of all active agents.
func (c *Coordinator) Active() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	ids := make([]string, 0, len(c.active))
	for id := range c.active {
		ids = append(ids, id)
	}
	return ids
}

func (c *Coordinator) runHeartbeat(ctx context.Context, a BackgroundAgent) {
	interval := time.Duration(a.HeartbeatInterval()) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.bus.Publish(bus.TopicAgentHeartbeat, bus.AgentEvent{
				AgentID: a.ID(),
				Status:  "heartbeat",
			})

			if err := a.Heartbeat(ctx); err != nil {
				c.logger.Error("heartbeat failed", "agent", a.ID(), "err", err)
				c.bus.Publish(bus.TopicAgentError, bus.AgentEvent{
					AgentID: a.ID(),
					Error:   err.Error(),
				})
				c.registry.SetStatus(a.ID(), StatusError)
			}
		}
	}
}
