package provider

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"charm.land/fantasy"
)

// ProviderType identifies the kind of LLM provider.
type ProviderType string

const (
	ProviderOllama     ProviderType = "ollama"
	ProviderOpenRouter ProviderType = "openrouter"
)

// ContextWindowQuerier is optionally implemented by providers that can
// report a model's context window from the upstream API.
type ContextWindowQuerier interface {
	QueryContextWindow(ctx context.Context, model string) (int64, error)
}

// Provider wraps a fantasy.Provider with health tracking and context window cache.
type Provider struct {
	ID       string
	Type     ProviderType
	provider fantasy.Provider
	healthy  atomic.Bool
	baseURL  string // for API queries (Ollama)

	ctxWindowCache map[string]int64
	ctxWindowMu    sync.Mutex
}

// NewProvider creates a Provider wrapper.
func NewProvider(id string, ptype ProviderType, fp fantasy.Provider) *Provider {
	p := &Provider{
		ID:             id,
		Type:           ptype,
		provider:       fp,
		ctxWindowCache: make(map[string]int64),
	}
	p.healthy.Store(true)
	return p
}

// SetBaseURL sets the base URL for API queries (used by Ollama).
func (p *Provider) SetBaseURL(url string) {
	p.baseURL = url
}

// ContextWindow returns the context window for a model, querying the provider if needed.
// Results are cached per model.
func (p *Provider) ContextWindow(ctx context.Context, modelID string) int64 {
	p.ctxWindowMu.Lock()
	if cached, ok := p.ctxWindowCache[modelID]; ok {
		p.ctxWindowMu.Unlock()
		return cached
	}
	p.ctxWindowMu.Unlock()

	// Try querying the provider
	if q, ok := p.provider.(ContextWindowQuerier); ok {
		if cw, err := q.QueryContextWindow(ctx, modelID); err == nil && cw > 0 {
			p.ctxWindowMu.Lock()
			p.ctxWindowCache[modelID] = cw
			p.ctxWindowMu.Unlock()
			return cw
		}
	}

	// Provider-specific fallback
	if p.baseURL != "" && p.Type == ProviderOllama {
		if cw := queryOllamaContextWindow(ctx, p.baseURL, modelID); cw > 0 {
			p.ctxWindowMu.Lock()
			p.ctxWindowCache[modelID] = cw
			p.ctxWindowMu.Unlock()
			return cw
		}
	}

	return 0 // unknown
}

// LanguageModel gets a language model by ID.
func (p *Provider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	lm, err := p.provider.LanguageModel(ctx, modelID)
	if err != nil {
		p.healthy.Store(false)
		return nil, fmt.Errorf("provider %s: %w", p.ID, err)
	}
	return lm, nil
}

// Healthy returns whether the provider is considered healthy.
func (p *Provider) Healthy() bool {
	return p.healthy.Load()
}

// MarkHealthy sets the provider health status.
func (p *Provider) MarkHealthy(h bool) {
	p.healthy.Store(h)
}
