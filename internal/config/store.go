package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
)

// Store manages global and per-project configuration.
type Store struct {
	global  *Config
	project *Config
	merged  *Config

	globalDir  string
	projectDir string
}

// Load creates a Store by reading global config from ~/.eclaire/
// and project config from cwd/.eclaire/ (if it exists).
func Load(cwd string) (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	globalDir := filepath.Join(home, ".eclaire")
	s := &Store{
		globalDir: globalDir,
	}

	s.global, err = LoadFile(filepath.Join(globalDir, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("global config: %w", err)
	}
	if s.global == nil {
		s.global = Defaults()
	}

	projectDir := filepath.Join(cwd, ".eclaire")
	if info, serr := os.Stat(projectDir); serr == nil && info.IsDir() {
		s.projectDir = projectDir
		s.project, err = LoadFile(filepath.Join(projectDir, "config.yaml"))
		if err != nil {
			return nil, fmt.Errorf("project config: %w", err)
		}
	}

	s.merged = merge(s.global, s.project)
	return s, nil
}

// Merged returns the effective configuration.
func (s *Store) Merged() *Config { return s.merged }

// GlobalDir returns ~/.eclaire/.
func (s *Store) GlobalDir() string { return s.globalDir }

// ProjectDir returns .eclaire/ relative to cwd, or empty if none.
func (s *Store) ProjectDir() string { return s.projectDir }

// SocketPath returns the gateway socket path.
func (s *Store) SocketPath() string {
	if s.merged.Gateway.SocketPath != "" {
		return s.merged.Gateway.SocketPath
	}
	return filepath.Join(s.globalDir, "gateway.sock")
}

// PIDPath returns the gateway PID file path.
func (s *Store) PIDPath() string {
	return filepath.Join(s.globalDir, "gateway.pid")
}

// LogDir returns the log directory path.
func (s *Store) LogDir() string {
	return filepath.Join(s.globalDir, "logs")
}

// SkillsDir returns the global skills directory.
func (s *Store) SkillsDir() string {
	return filepath.Join(s.globalDir, "skills")
}

// FlowsDir returns the global flows directory.
func (s *Store) FlowsDir() string {
	return filepath.Join(s.globalDir, "flows")
}

// AgentsDir returns the global agents directory.
func (s *Store) AgentsDir() string {
	return filepath.Join(s.globalDir, "agents")
}

// SessionsDir returns the sessions directory.
func (s *Store) SessionsDir() string {
	return filepath.Join(s.globalDir, "sessions")
}

// WorkspaceDir returns the global workspace directory.
func (s *Store) WorkspaceDir() string {
	return filepath.Join(s.globalDir, "workspace")
}

// CronPath returns the cron.yaml path.
func (s *Store) CronPath() string {
	return filepath.Join(s.globalDir, "cron.yaml")
}

// HeartbeatDir returns the heartbeat data directory.
func (s *Store) HeartbeatDir() string {
	return filepath.Join(s.globalDir, "heartbeat")
}

// JobsPath returns the unified jobs store path.
func (s *Store) JobsPath() string {
	return filepath.Join(s.globalDir, "jobs.json")
}

// RunsDir returns the per-job run log directory.
func (s *Store) RunsDir() string {
	return filepath.Join(s.globalDir, "runs")
}

// FlowStatePath returns the flow run store path.
func (s *Store) FlowStatePath() string {
	return filepath.Join(s.globalDir, "flows_state.json")
}

// NotificationsPath returns the notifications JSONL path.
func (s *Store) NotificationsPath() string {
	return filepath.Join(s.globalDir, "notifications.jsonl")
}

// RemindersPath returns the reminders.json path.
func (s *Store) RemindersPath() string {
	return filepath.Join(s.globalDir, "reminders.json")
}

// BriefingsDir returns the briefings directory.
func (s *Store) BriefingsDir() string {
	return filepath.Join(s.WorkspaceDir(), "briefings")
}

// CredentialsDir returns the credentials directory.
func (s *Store) CredentialsDir() string {
	return filepath.Join(s.globalDir, "credentials")
}

// EmailCredentialsPath returns the email credentials YAML path.
func (s *Store) EmailCredentialsPath() string {
	return filepath.Join(s.CredentialsDir(), "email.yaml")
}

// EnsureDirs creates required directories under ~/.eclaire/.
func (s *Store) EnsureDirs() error {
	dirs := []string{
		s.globalDir,
		s.AgentsDir(),
		s.SessionsDir(),
		s.LogDir(),
		s.WorkspaceDir(),
		s.SkillsDir(),
		s.FlowsDir(),
		s.HeartbeatDir(),
		s.RunsDir(),
		filepath.Join(s.globalDir, "credentials"),
		filepath.Join(s.globalDir, "cache"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// merge overlays project config onto global config.
func merge(global, project *Config) *Config {
	if project == nil {
		return global
	}

	m := *global

	// Merge maps
	if len(project.Providers) > 0 {
		if m.Providers == nil {
			m.Providers = make(map[string]ProviderConfig)
		}
		maps.Copy(m.Providers, project.Providers)
	}
	if len(project.Routing) > 0 {
		if m.Routing == nil {
			m.Routing = make(map[string][]RouteEntry)
		}
		maps.Copy(m.Routing, project.Routing)
	}
	if len(project.MCP) > 0 {
		if m.MCP == nil {
			m.MCP = make(map[string]MCPConfig)
		}
		maps.Copy(m.MCP, project.MCP)
	}
	if len(project.LSP) > 0 {
		if m.LSP == nil {
			m.LSP = make(map[string]LSPConfig)
		}
		maps.Copy(m.LSP, project.LSP)
	}

	// Merge overrides (append)
	if len(project.Tools.Overrides) > 0 {
		m.Tools.Overrides = append(m.Tools.Overrides, project.Tools.Overrides...)
	}

	// Scalar overrides
	if project.Gateway.IdleTimeout != "" {
		m.Gateway.IdleTimeout = project.Gateway.IdleTimeout
	}
	if project.Gateway.LogLevel != "" {
		m.Gateway.LogLevel = project.Gateway.LogLevel
	}
	if project.Agents.DefaultRole != "" {
		m.Agents.DefaultRole = project.Agents.DefaultRole
	}
	if len(project.Agents.AgentFiles) > 0 {
		m.Agents.AgentFiles = append(m.Agents.AgentFiles, project.Agents.AgentFiles...)
	}

	// Hooks merge (append) — project hooks run in addition to global
	if len(project.Hooks) > 0 {
		m.Hooks = append(m.Hooks, project.Hooks...)
	}

	return &m
}
