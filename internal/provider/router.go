package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/config"
)

// Router selects the right provider+model based on an agent's Role.
type Router struct {
	providers map[string]*Provider
	routes    map[string][]config.RouteEntry
	logger    *slog.Logger
	mu        sync.RWMutex
}

// NewRouter creates a Router from config.
func NewRouter(cfg *config.Config, logger *slog.Logger) (*Router, error) {
	r := &Router{
		providers: make(map[string]*Provider),
		routes:    make(map[string][]config.RouteEntry),
		logger:    logger,
	}

	// Initialize providers
	for id, pc := range cfg.Providers {
		p, err := r.createProvider(id, pc)
		if err != nil {
			logger.Warn("failed to create provider", "id", id, "err", err)
			continue
		}
		r.providers[id] = p
		logger.Info("provider initialized", "id", id, "type", pc.Type)
	}

	// Load routing table
	for role, entries := range cfg.Routing {
		r.routes[string(role)] = entries
	}

	return r, nil
}

func (r *Router) createProvider(id string, pc config.ProviderConfig) (*Provider, error) {
	switch ProviderType(pc.Type) {
	case ProviderOllama:
		baseURL := pc.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return NewOllama(id, baseURL)
	case ProviderOpenRouter:
		if pc.APIKey == "" {
			return nil, fmt.Errorf("openrouter requires api_key")
		}
		return NewOpenRouter(id, pc.APIKey)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", pc.Type)
	}
}

// Resolve returns a fantasy.LanguageModel for the given role,
// walking the fallback chain if the preferred provider is unavailable.
// If the role doesn't match any route, it's treated as a direct model
// name and resolved against all healthy providers.
func (r *Router) Resolve(ctx context.Context, role string) (fantasy.LanguageModel, error) {
	r.mu.RLock()
	entries, ok := r.routes[role]
	r.mu.RUnlock()

	// RoleOrchestrator falls back to "complex" if no explicit route
	if !ok && role == "orchestrator" {
		entries, ok = r.routes["complex"]
	}

	if ok && len(entries) > 0 {
		for _, entry := range entries {
			p, ok := r.providers[entry.Provider]
			if !ok {
				r.logger.Warn("provider not found in route", "provider", entry.Provider)
				continue
			}

			if !p.Healthy() {
				r.logger.Debug("skipping unhealthy provider", "provider", entry.Provider)
				continue
			}

			lm, err := p.LanguageModel(ctx, entry.Model)
			if err != nil {
				r.logger.Warn("provider failed",
					"provider", entry.Provider,
					"model", entry.Model,
					"err", err,
				)
				continue
			}

			r.logger.Debug("resolved model",
				"role", role,
				"provider", entry.Provider,
				"model", entry.Model,
			)
			return lm, nil
		}
		return nil, fmt.Errorf("all providers failed for role %q", role)
	}

	// No route found — treat role as a direct model name
	return r.resolveByModel(ctx, role)
}

// resolveByModel tries to resolve a specific model name against all healthy providers.
func (r *Router) resolveByModel(ctx context.Context, model string) (fantasy.LanguageModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, p := range r.providers {
		if !p.Healthy() {
			continue
		}
		lm, err := p.LanguageModel(ctx, model)
		if err != nil {
			r.logger.Debug("provider cannot serve model",
				"provider", id,
				"model", model,
				"err", err,
			)
			continue
		}
		r.logger.Debug("resolved model directly",
			"provider", id,
			"model", model,
		)
		return lm, nil
	}
	return nil, fmt.Errorf("no provider can serve model %q", model)
}

// ModelResolution holds the resolved model and its configured context window.
type ModelResolution struct {
	Model         fantasy.LanguageModel
	ContextWindow int64  // 0 means not configured — caller should use a sensible default
	ProviderID    string // "ollama", "openrouter", etc.
}

// ResolveWithContext returns a LanguageModel and its context window for the given role.
// The context window is queried from the provider (e.g. Ollama /api/show) and cached.
// Config context_window is used as an override if set.
func (r *Router) ResolveWithContext(ctx context.Context, role string) (*ModelResolution, error) {
	r.mu.RLock()
	entries, ok := r.routes[role]
	r.mu.RUnlock()

	if !ok && role == "orchestrator" {
		entries, ok = r.routes["complex"]
	}

	if ok && len(entries) > 0 {
		for _, entry := range entries {
			p, ok := r.providers[entry.Provider]
			if !ok {
				continue
			}
			if !p.Healthy() {
				continue
			}
			lm, err := p.LanguageModel(ctx, entry.Model)
			if err != nil {
				continue
			}

			// Get context window: config override > provider query
			contextWindow := entry.ContextWindow
			if contextWindow <= 0 {
				contextWindow = p.ContextWindow(ctx, entry.Model)
			}

			r.logger.Debug("resolved model with context",
				"role", role,
				"provider", entry.Provider,
				"model", entry.Model,
				"context_window", contextWindow,
			)

			return &ModelResolution{
				Model:         lm,
				ContextWindow: contextWindow,
				ProviderID:    entry.Provider,
			}, nil
		}
		return nil, fmt.Errorf("all providers failed for role %q", role)
	}

	// No route — try direct model name
	lm, err := r.resolveByModel(ctx, role)
	if err != nil {
		return nil, err
	}
	return &ModelResolution{Model: lm}, nil
}

// NewAgent creates a fantasy.Agent wired to the right model for the given role.
func (r *Router) NewAgent(ctx context.Context, role string, systemPrompt string, tools []fantasy.AgentTool) (fantasy.Agent, error) {
	lm, err := r.Resolve(ctx, role)
	if err != nil {
		return nil, err
	}

	opts := []fantasy.AgentOption{}
	if systemPrompt != "" {
		opts = append(opts, fantasy.WithSystemPrompt(systemPrompt))
	}
	if len(tools) > 0 {
		opts = append(opts, fantasy.WithTools(tools...))
	}

	return fantasy.NewAgent(lm, opts...), nil
}
