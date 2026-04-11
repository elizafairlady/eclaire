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
// The active map is keyed by session ID, not agent ID — multiple instances
// of the same agent type can run simultaneously.
type Coordinator struct {
	registry *Registry
	bus      *bus.Bus
	logger   *slog.Logger

	active map[string]context.CancelFunc // sessionID -> cancel
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

// Spawn starts an agent instance in the background.
// Returns a session ID that can be used to kill this specific instance.
func (c *Coordinator) Spawn(ctx context.Context, agentID string, deps AgentDeps) (string, error) {
	a, ok := c.registry.Get(agentID)
	if !ok {
		return "", fmt.Errorf("agent %q not found", agentID)
	}

	if err := a.Init(ctx, deps); err != nil {
		return "", fmt.Errorf("init agent %q: %w", agentID, err)
	}

	agentCtx, cancel := context.WithCancel(ctx)

	// Generate a unique session ID for this instance
	sessionID := fmt.Sprintf("spawn_%s_%d", agentID, time.Now().UnixNano())

	c.mu.Lock()
	c.active[sessionID] = cancel
	c.mu.Unlock()

	c.registry.RegisterInstance(sessionID, agentID, cancel)
	c.bus.Publish(bus.TopicAgentStarted, bus.AgentEvent{
		AgentID: agentID,
		Name:    a.Name(),
		Status:  string(StatusRunning),
	})

	// If it's a background agent, start its heartbeat loop
	if bg, ok := a.(BackgroundAgent); ok {
		go c.runHeartbeat(agentCtx, sessionID, bg)
	}

	c.logger.Info("agent spawned", "id", agentID, "session", sessionID)
	return sessionID, nil
}

// Kill stops a specific agent instance by session ID.
func (c *Coordinator) Kill(sessionID string) error {
	c.mu.Lock()
	cancel, ok := c.active[sessionID]
	if ok {
		delete(c.active, sessionID)
	}
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("instance %q not active", sessionID)
	}

	cancel()
	inst, _ := c.registry.GetInstance(sessionID)
	c.registry.RemoveInstance(sessionID)

	agentID := sessionID
	if inst != nil {
		agentID = inst.AgentID
	}
	c.bus.Publish(bus.TopicAgentCompleted, bus.AgentEvent{
		AgentID: agentID,
		Status:  string(StatusIdle),
	})

	a, _ := c.registry.Get(agentID)
	if a != nil {
		a.Shutdown(context.Background())
	}

	c.logger.Info("agent killed", "session", sessionID, "agent", agentID)
	return nil
}

// KillAll stops all running instances of a given agent type.
func (c *Coordinator) KillAll(agentID string) int {
	instances := c.registry.RunningInstances(agentID)
	killed := 0
	for _, inst := range instances {
		if err := c.Kill(inst.SessionID); err == nil {
			killed++
		}
	}
	return killed
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

// Active returns the session IDs of all active instances.
func (c *Coordinator) Active() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	ids := make([]string, 0, len(c.active))
	for id := range c.active {
		ids = append(ids, id)
	}
	return ids
}

func (c *Coordinator) runHeartbeat(ctx context.Context, sessionID string, a BackgroundAgent) {
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
				c.registry.UpdateInstance(sessionID, StatusError)
			}
		}
	}
}
