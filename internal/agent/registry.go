package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry manages all registered agents.
type Registry struct {
	agents map[string]Agent
	status map[string]Status
	mu     sync.RWMutex
}

// NewRegistry creates a new agent registry.
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]Agent),
		status: make(map[string]Status),
	}
}

// Register adds an agent to the registry.
func (r *Registry) Register(a Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[a.ID()]; exists {
		return fmt.Errorf("agent %q already registered", a.ID())
	}

	r.agents[a.ID()] = a
	r.status[a.ID()] = StatusIdle
	return nil
}

// Upsert adds or replaces an agent in the registry.
// Returns true if an existing agent was replaced.
func (r *Registry) Upsert(a Agent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, replaced := r.agents[a.ID()]
	r.agents[a.ID()] = a
	if !replaced {
		r.status[a.ID()] = StatusIdle
	}
	return replaced
}

// Get returns an agent by ID.
func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// SetStatus updates an agent's status.
func (r *Registry) SetStatus(id string, s Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status[id] = s
}

// Resolve finds the best agent for the given context by matching bindings.
// Returns the highest-priority match.
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

// All returns info for all registered agents.
func (r *Registry) All() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]Info, 0, len(r.agents))
	for _, a := range r.agents {
		builtin := false
		if bi, ok := a.(interface{ IsBuiltIn() bool }); ok {
			builtin = bi.IsBuiltIn()
		}
		infos = append(infos, Info{
			ID:          a.ID(),
			Name:        a.Name(),
			Description: a.Description(),
			Role:        a.Role(),
			Status:      r.status[a.ID()],
			Tools:       a.RequiredTools(),
			BuiltIn:     builtin,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	return infos
}

// HasRunning returns true if any agent has StatusRunning.
func (r *Registry) HasRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.status {
		if s == StatusRunning {
			return true
		}
	}
	return false
}

// SerializeAgents formats registered agents as an XML listing for the system prompt.
// Excludes the calling agent (selfID) from the list.
func SerializeAgents(agents []Info, selfID string) string {
	var sb strings.Builder
	sb.WriteString("<available_agents>\n")
	sb.WriteString("Use the agent tool to delegate to any of these specialists.\n\n")

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

// HasBackgroundAgents returns true if any background agents are running.
func (r *Registry) HasBackgroundAgents() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.agents {
		if _, ok := a.(BackgroundAgent); ok {
			if r.status[a.ID()] == StatusRunning {
				return true
			}
		}
	}
	return false
}
