package tool

import (
	"fmt"
	"sort"
	"sync"

	"charm.land/fantasy"
)

// Registry manages available tools and permission overrides.
type Registry struct {
	tools     map[string]Tool
	overrides map[string]TrustTier // "agentID:toolName" -> tier
	mu        sync.RWMutex
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]Tool),
		overrides: make(map[string]TrustTier),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Info().Name] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ForAgent returns fantasy-compatible tools filtered for a specific agent.
func (r *Registry) ForAgent(agentID string, required []string) []fantasy.AgentTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []fantasy.AgentTool
	if len(required) == 0 {
		// Return all tools
		for _, t := range r.tools {
			out = append(out, t)
		}
	} else {
		for _, name := range required {
			if t, ok := r.tools[name]; ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// SetOverride sets a trust tier override for a specific agent+tool pair.
func (r *Registry) SetOverride(agentID, toolName string, tier TrustTier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[fmt.Sprintf("%s:%s", agentID, toolName)] = tier
}

// EffectiveTier returns the effective trust tier for an agent+tool,
// checking overrides first, then falling back to the tool's default.
func (r *Registry) EffectiveTier(agentID, toolName string) TrustTier {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", agentID, toolName)
	if tier, ok := r.overrides[key]; ok {
		return tier
	}

	if t, ok := r.tools[toolName]; ok {
		return t.TrustTier()
	}
	return TrustDangerous // unknown tools are dangerous
}

// All returns all registered tools sorted by name.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Info().Name < out[j].Info().Name
	})
	return out
}
