package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileNotExist(t *testing.T) {
	cfg, err := LoadFile("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for nonexistent file")
	}
}

func TestLoadFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `gateway:
  idle_timeout: "5m"
  log_level: "debug"

providers:
  ollama:
    type: ollama
    base_url: "http://localhost:11434"
  openrouter:
    type: openrouter
    api_key: "test-key"

routing:
  simple:
    - provider: ollama
      model: "qwen:7b"
  complex:
    - provider: openrouter
      model: "anthropic/claude-sonnet"

agents:
  default_role: "complex"
`
	os.WriteFile(path, []byte(yaml), 0o644)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if cfg.Gateway.IdleTimeout != "5m" {
		t.Errorf("IdleTimeout = %q, want %q", cfg.Gateway.IdleTimeout, "5m")
	}
	if cfg.Gateway.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.Gateway.LogLevel, "debug")
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("Providers len = %d, want 2", len(cfg.Providers))
	}
	if cfg.Providers["ollama"].Type != "ollama" {
		t.Errorf("Providers[ollama].Type = %q", cfg.Providers["ollama"].Type)
	}
	if cfg.Providers["openrouter"].APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", cfg.Providers["openrouter"].APIKey, "test-key")
	}
	if len(cfg.Routing["simple"]) != 1 {
		t.Errorf("Routing[simple] len = %d, want 1", len(cfg.Routing["simple"]))
	}
	if cfg.Agents.DefaultRole != "complex" {
		t.Errorf("DefaultRole = %q, want %q", cfg.Agents.DefaultRole, "complex")
	}
}

func TestLoadFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte(`{{{not yaml`), 0o644)

	_, err := LoadFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-123")

	if got := expandEnv("$TEST_API_KEY"); got != "secret-123" {
		t.Errorf("expandEnv($TEST_API_KEY) = %q, want %q", got, "secret-123")
	}
	if got := expandEnv("literal"); got != "literal" {
		t.Errorf("expandEnv(literal) = %q, want %q", got, "literal")
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Gateway.IdleTimeout != "10m" {
		t.Errorf("default IdleTimeout = %q", cfg.Gateway.IdleTimeout)
	}
	if cfg.Agents.DefaultRole != "simple" {
		t.Errorf("default role = %q", cfg.Agents.DefaultRole)
	}
}
