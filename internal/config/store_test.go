package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMerge(t *testing.T) {
	global := &Config{
		Gateway: GatewayConfig{
			IdleTimeout: "10m",
			LogLevel:    "info",
		},
		Providers: map[string]ProviderConfig{
			"ollama": {Type: "ollama", BaseURL: "http://localhost:11434"},
		},
		Routing: map[string][]RouteEntry{
			"simple": {{Provider: "ollama", Model: "qwen:7b"}},
		},
		Agents: AgentsConfig{DefaultRole: "simple"},
	}

	project := &Config{
		Gateway: GatewayConfig{
			LogLevel: "debug",
		},
		Providers: map[string]ProviderConfig{
			"openrouter": {Type: "openrouter", APIKey: "key"},
		},
		Agents: AgentsConfig{DefaultRole: "complex"},
	}

	merged := merge(global, project)

	// Gateway: project overrides log_level
	if merged.Gateway.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", merged.Gateway.LogLevel, "debug")
	}
	// Gateway: global idle_timeout preserved
	if merged.Gateway.IdleTimeout != "10m" {
		t.Errorf("IdleTimeout = %q, want %q", merged.Gateway.IdleTimeout, "10m")
	}
	// Providers: both present
	if len(merged.Providers) != 2 {
		t.Errorf("Providers len = %d, want 2", len(merged.Providers))
	}
	if _, ok := merged.Providers["ollama"]; !ok {
		t.Error("ollama provider should be present")
	}
	if _, ok := merged.Providers["openrouter"]; !ok {
		t.Error("openrouter provider should be present")
	}
	// Agents: project overrides
	if merged.Agents.DefaultRole != "complex" {
		t.Errorf("DefaultRole = %q, want %q", merged.Agents.DefaultRole, "complex")
	}
}

func TestMergeNilProject(t *testing.T) {
	global := Defaults()
	merged := merge(global, nil)
	if merged != global {
		t.Error("merge with nil project should return global")
	}
}

func TestStoreEnsureDirs(t *testing.T) {
	tmpHome := t.TempDir()
	s := &Store{globalDir: filepath.Join(tmpHome, ".eclaire")}

	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	dirs := []string{
		s.globalDir,
		s.AgentsDir(),
		s.SessionsDir(),
		s.LogDir(),
	}
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("dir %s not created: %v", d, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}
}

func TestStorePaths(t *testing.T) {
	s := &Store{
		globalDir: "/home/test/.eclaire",
		merged:    Defaults(),
	}

	if got := s.SocketPath(); got != "/home/test/.eclaire/gateway.sock" {
		t.Errorf("SocketPath = %q", got)
	}
	if got := s.PIDPath(); got != "/home/test/.eclaire/gateway.pid" {
		t.Errorf("PIDPath = %q", got)
	}
	if got := s.AgentsDir(); got != "/home/test/.eclaire/agents" {
		t.Errorf("AgentsDir = %q", got)
	}
}
