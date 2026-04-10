package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy/providers/openaicompat"
)

// NewOllama creates an Ollama provider using the OpenAI-compatible API.
func NewOllama(id, baseURL string) (*Provider, error) {
	fp, err := openaicompat.New(
		openaicompat.WithBaseURL(baseURL+"/v1"),
		openaicompat.WithName("ollama"),
		openaicompat.WithAPIKey("ollama"), // Ollama doesn't need a real key
	)
	if err != nil {
		return nil, err
	}

	p := NewProvider(id, ProviderOllama, fp)
	p.SetBaseURL(baseURL)
	return p, nil
}

// queryOllamaContextWindow calls Ollama's /api/show endpoint to get the model's context length.
func queryOllamaContextWindow(ctx context.Context, baseURL, model string) int64 {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"name": model})
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	var result struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return 0
	}

	// Look for <family>.context_length in model_info
	for key, val := range result.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			switch v := val.(type) {
			case float64:
				return int64(v)
			case json.Number:
				if n, err := v.Int64(); err == nil {
					return n
				}
			}
		}
	}

	// Fallback: check for num_ctx in parameters
	var paramsResult struct {
		Parameters string `json:"parameters"`
	}
	// Re-parse if needed — parameters is a string like "num_ctx 4096"
	if paramsResult.Parameters != "" {
		for _, line := range strings.Split(paramsResult.Parameters, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "num_ctx") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					var n int64
					fmt.Sscanf(parts[1], "%d", &n)
					return n
				}
			}
		}
	}

	return 0
}
