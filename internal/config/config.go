package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
type Config struct {
	Gateway   GatewayConfig              `yaml:"gateway"`
	Providers map[string]ProviderConfig  `yaml:"providers"`
	Routing   map[string][]RouteEntry    `yaml:"routing"`
	MCP       map[string]MCPConfig       `yaml:"mcp"`
	LSP       map[string]LSPConfig       `yaml:"lsp"`
	Tools     ToolsConfig                `yaml:"tools"`
	Agents    AgentsConfig               `yaml:"agents"`
	Hooks     []HookConfig               `yaml:"hooks"`
}

// HookConfig defines a tool lifecycle hook.
type HookConfig struct {
	Event   string `yaml:"event"`
	Matcher string `yaml:"matcher"`
	Command string `yaml:"command"`
	Timeout string `yaml:"timeout,omitempty"`
}

// GatewayConfig controls daemon behavior.
type GatewayConfig struct {
	IdleTimeout       string `yaml:"idle_timeout"`
	SocketPath        string `yaml:"socket_path"`
	LogLevel          string `yaml:"log_level"`
	HeartbeatInterval string `yaml:"heartbeat_interval"`
	DailyResetHour    int    `yaml:"daily_reset_hour"`
}

// ProviderConfig defines an LLM provider.
type ProviderConfig struct {
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// RouteEntry maps a role to a specific provider+model with priority.
type RouteEntry struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ContextWindow int64  `yaml:"context_window,omitempty"`
	Priority int    `yaml:"priority"`
}

// MCPConfig defines an MCP server connection.
type MCPConfig struct {
	Type    string   `yaml:"type"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	URL     string   `yaml:"url"`
}

// LSPConfig defines an LSP server.
type LSPConfig struct {
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args"`
	Filetypes   []string `yaml:"filetypes"`
	RootMarkers []string `yaml:"root_markers"`
}

// ToolsConfig holds tool permission overrides.
type ToolsConfig struct {
	Overrides []TierOverride `yaml:"overrides"`
}

// TierOverride changes a tool's trust tier for a specific agent.
type TierOverride struct {
	AgentID string `yaml:"agent_id"`
	Tool    string `yaml:"tool"`
	Tier    int    `yaml:"tier"`
}

// AgentsConfig holds agent defaults and file paths.
type AgentsConfig struct {
	DefaultRole string   `yaml:"default_role"`
	AgentFiles  []string `yaml:"agent_files"`
}

// Defaults returns a Config with sensible defaults.
func Defaults() *Config {
	return &Config{
		Gateway: GatewayConfig{
			IdleTimeout: "10m",
			LogLevel:    "info",
		},
		Providers: make(map[string]ProviderConfig),
		Routing:   make(map[string][]RouteEntry),
		MCP:       make(map[string]MCPConfig),
		LSP:       make(map[string]LSPConfig),
		Agents: AgentsConfig{
			DefaultRole: "simple",
		},
	}
}

// LoadFile reads and parses a YAML config file.
// Returns nil config (not an error) if file does not exist.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	resolveEnvVars(&cfg)
	return &cfg, nil
}

// resolveEnvVars expands $ENV_VAR references in string fields.
func resolveEnvVars(cfg *Config) {
	for k, p := range cfg.Providers {
		p.APIKey = expandEnv(p.APIKey)
		p.BaseURL = expandEnv(p.BaseURL)
		cfg.Providers[k] = p
	}
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(strings.TrimPrefix(s, "$"))
	}
	return s
}
