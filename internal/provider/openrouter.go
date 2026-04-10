package provider

import (
	"charm.land/fantasy/providers/openrouter"
)

// NewOpenRouter creates an OpenRouter provider.
func NewOpenRouter(id, apiKey string) (*Provider, error) {
	fp, err := openrouter.New(
		openrouter.WithAPIKey(apiKey),
	)
	if err != nil {
		return nil, err
	}

	return NewProvider(id, ProviderOpenRouter, fp), nil
}
