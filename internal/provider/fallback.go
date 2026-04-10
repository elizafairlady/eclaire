package provider

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/config"
)

// FallbackChain tries multiple provider+model combos with exponential backoff.
type FallbackChain struct {
	entries    []config.RouteEntry
	providers  map[string]*Provider
	maxRetries int
	logger     *slog.Logger
}

// NewFallbackChain creates a new fallback chain.
func NewFallbackChain(entries []config.RouteEntry, providers map[string]*Provider, logger *slog.Logger) *FallbackChain {
	return &FallbackChain{
		entries:    entries,
		providers:  providers,
		maxRetries: 2,
		logger:     logger,
	}
}

// Execute tries the function with each entry in order.
// For each entry, retries with exponential backoff before moving to the next.
func (f *FallbackChain) Execute(ctx context.Context, fn func(fantasy.LanguageModel) error) error {
	var lastErr error

	for _, entry := range f.entries {
		p, ok := f.providers[entry.Provider]
		if !ok {
			continue
		}

		for attempt := range f.maxRetries + 1 {
			lm, err := p.LanguageModel(ctx, entry.Model)
			if err != nil {
				lastErr = err
				break // skip to next provider
			}

			if err := fn(lm); err != nil {
				lastErr = err
				f.logger.Warn("attempt failed",
					"provider", entry.Provider,
					"model", entry.Model,
					"attempt", attempt+1,
					"err", err,
				)

				if attempt < f.maxRetries {
					backoff := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(backoff):
					}
				}
				continue
			}

			// Success - mark provider healthy
			p.MarkHealthy(true)
			return nil
		}

		// All retries exhausted for this entry, mark unhealthy
		p.MarkHealthy(false)
		f.logger.Warn("provider exhausted, trying fallback",
			"provider", entry.Provider,
			"model", entry.Model,
		)
	}

	return fmt.Errorf("all fallbacks exhausted: %w", lastErr)
}
