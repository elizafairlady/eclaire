package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/bus"
)

// Coordinator manages agent lifecycle.
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

// Spawn registers an agent instance for lifecycle tracking.
// Execution is handled by Runner — Coordinator only tracks what's running.
func (c *Coordinator) Spawn(ctx context.Context, agentID string) (string, error) {
	a, ok := c.registry.Get(agentID)
	if !ok {
		return "", fmt.Errorf("agent %q not found", agentID)
	}

	_, cancel := context.WithCancel(ctx)

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
