package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AgentInstance tracks a single running instance of an agent definition.
// Keyed by session ID — each concurrent run of the same agent type gets its own instance.
type AgentInstance struct {
	SessionID string
	AgentID   string
	Status    Status
	StartedAt time.Time
	Cancel    context.CancelFunc // nil if not cancellable
}

// Registry manages agent definitions and running instances.
type Registry struct {
	agents    map[string]Agent            // definitions keyed by agent ID
	instances map[string]*AgentInstance   // running instances keyed by session ID
	mu        sync.RWMutex
}

// NewRegistry creates a new agent registry.
func NewRegistry() *Registry {
	return &Registry{
		agents:    make(map[string]Agent),
		instances: make(map[string]*AgentInstance),
	}
}

// Register adds an agent definition to the registry.
func (r *Registry) Register(a Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[a.ID()]; exists {
		return fmt.Errorf("agent %q already registered", a.ID())
	}

	r.agents[a.ID()] = a
	return nil
}

// Upsert adds or replaces an agent definition.
// Returns true if an existing agent was replaced.
func (r *Registry) Upsert(a Agent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, replaced := r.agents[a.ID()]
	r.agents[a.ID()] = a
	return replaced
}

// Get returns an agent definition by ID.
func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// RegisterInstance records a new running agent instance.
// sessionID must be unique per run (typically a UUID).
func (r *Registry) RegisterInstance(sessionID, agentID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[sessionID] = &AgentInstance{
		SessionID: sessionID,
		AgentID:   agentID,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		Cancel:    cancel,
	}
}

// UpdateInstance updates the status of a running instance.
func (r *Registry) UpdateInstance(sessionID string, status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inst, ok := r.instances[sessionID]; ok {
		inst.Status = status
	}
}

// RemoveInstance removes a completed instance.
func (r *Registry) RemoveInstance(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, sessionID)
}

// GetInstance returns an instance by session ID.
func (r *Registry) GetInstance(sessionID string) (*AgentInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[sessionID]
	if !ok {
		return nil, false
	}
	cp := *inst
	return &cp, true
}

// RunningInstances returns all running instances of a given agent type.
func (r *Registry) RunningInstances(agentID string) []*AgentInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*AgentInstance
	for _, inst := range r.instances {
		if inst.AgentID == agentID && inst.Status == StatusRunning {
			cp := *inst
			result = append(result, &cp)
		}
	}
	return result
}

// AllInstances returns all instances across all agent types.
func (r *Registry) AllInstances() []*AgentInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*AgentInstance, 0, len(r.instances))
	for _, inst := range r.instances {
		cp := *inst
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// SetStatus is a backward-compatible shim that updates all instances of the given
// agent ID. Prefer RegisterInstance/RemoveInstance for new code.
// TODO: Remove once all callers are migrated.
func (r *Registry) SetStatus(id string, s Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inst := range r.instances {
		if inst.AgentID == id {
			inst.Status = s
		}
	}
}

// Resolve finds the best agent for the given context by matching bindings.
func (r *Registry) Resolve(cwd, taskType string) (Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type match struct {
		agent    Agent
		priority int
	}

	var matches []match
	for _, a := range r.agents {
		for _, b := range a.Bindings() {
			if b.Match(cwd, taskType) {
				matches = append(matches, match{agent: a, priority: b.Priority})
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no agent matches cwd=%s task=%s", cwd, taskType)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].priority > matches[j].priority
	})

	return matches[0].agent, nil
}

// All returns info for all registered agent definitions with instance counts.
func (r *Registry) All() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]Info, 0, len(r.agents))
	for _, a := range r.agents {
		builtin := false
		if bi, ok := a.(interface{ IsBuiltIn() bool }); ok {
			builtin = bi.IsBuiltIn()
		}

		// Compute status from instances
		status := StatusIdle
		for _, inst := range r.instances {
			if inst.AgentID == a.ID() && inst.Status == StatusRunning {
				status = StatusRunning
				break
			}
		}

		infos = append(infos, Info{
			ID:          a.ID(),
			Name:        a.Name(),
			Description: a.Description(),
			Role:        a.Role(),
			Status:      status,
			Tools:       a.RequiredTools(),
			BuiltIn:     builtin,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	return infos
}

// HasRunning returns true if any agent instance is currently running.
func (r *Registry) HasRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, inst := range r.instances {
		if inst.Status == StatusRunning {
			return true
		}
	}
	return false
}

// HasBackgroundAgents returns true if any background agent definitions have running instances.
func (r *Registry) HasBackgroundAgents() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.agents {
		if _, ok := a.(BackgroundAgent); ok {
			for _, inst := range r.instances {
				if inst.AgentID == a.ID() && inst.Status == StatusRunning {
					return true
				}
			}
		}
	}
	return false
}

// SerializeAgents formats registered agents as an XML listing for the system prompt.
func SerializeAgents(agents []Info, selfID string) string {
	var sb strings.Builder
	sb.WriteString("<available_agents>\n")
	sb.WriteString("Use the agent tool to delegate to any of these specialists.\n")
	sb.WriteString("Multiple instances of the same agent can run in parallel.\n\n")

	n := 0
	for _, a := range agents {
		if a.ID == selfID {
			continue
		}
		sb.WriteString(fmt.Sprintf("<agent id=%q name=%q role=%q>\n%s\n</agent>\n",
			a.ID, a.Name, string(a.Role), a.Description))
		n++
	}

	if n == 0 {
		return ""
	}

	sb.WriteString("</available_agents>")
	return sb.String()
}
