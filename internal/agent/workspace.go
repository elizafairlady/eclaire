package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Workspace file names.
const (
	FileSoul      = "SOUL.md"
	FileAgents    = "AGENTS.md"
	FileHeartbeat = "HEARTBEAT.md"
	FileBoot      = "BOOT.md"
	FileMemory    = "MEMORY.md"
	FileUser      = "USER.md"
	FileTools     = "TOOLS.md"
)

// WorkspaceFile is a single workspace document with provenance.
type WorkspaceFile struct {
	Name     string // e.g. "SOUL.md"
	Content  string
	Source   string // "embedded", "global", "agent", "project"
	Priority int    // higher overrides lower
}

// Memory holds an agent's memory state.
type Memory struct {
	Curated   string            // MEMORY.md content
	Daily     map[string]string // "2026-04-08" -> daily log content
	LastFlush time.Time
}

// Workspace is the assembled workspace for an agent.
type Workspace struct {
	AgentID string
	Files   map[string]WorkspaceFile
	Memory  *Memory
}

// Get returns the content of a workspace file, or empty string.
func (w *Workspace) Get(name string) string {
	if f, ok := w.Files[name]; ok {
		return f.Content
	}
	return ""
}

// WorkspaceLoader loads and assembles workspaces from disk with layering.
type WorkspaceLoader struct {
	globalDir  string // ~/.eclaire/workspace/
	agentsDir  string // ~/.eclaire/agents/
	projectDir string // .eclaire/ in cwd (may be empty)
}

// NewWorkspaceLoader creates a loader.
func NewWorkspaceLoader(globalDir, agentsDir, projectDir string) *WorkspaceLoader {
	return &WorkspaceLoader{
		globalDir:  globalDir,
		agentsDir:  agentsDir,
		projectDir: projectDir,
	}
}

// Load assembles a workspace for an agent by layering:
// 1. embedded defaults (from built-in agent)
// 2. global workspace (~/.eclaire/workspace/)
// 3. agent-specific workspace (~/.eclaire/agents/<id>/workspace/)
// 4. project workspace overlay (.eclaire/workspace/) — uses l.projectDir (daemon startup)
//
// For per-connection project dirs, use LoadWithProject instead.
func (l *WorkspaceLoader) Load(agentID string, embedded map[string]string) (*Workspace, error) {
	return l.LoadWithProject(agentID, embedded, l.projectDir)
}

// LoadWithProject assembles a workspace using the given projectDir for layer 4.
// projectDir is the .eclaire/ directory path (e.g. "/home/user/myproject/.eclaire").
// If empty, layer 4 is skipped (no project workspace overlay).
func (l *WorkspaceLoader) LoadWithProject(agentID string, embedded map[string]string, projectDir string) (*Workspace, error) {
	ws := &Workspace{
		AgentID: agentID,
		Files:   make(map[string]WorkspaceFile),
		Memory:  &Memory{Daily: make(map[string]string)},
	}

	// Layer 1: embedded defaults (priority 0)
	for name, content := range embedded {
		ws.Files[name] = WorkspaceFile{
			Name:     name,
			Content:  content,
			Source:   "embedded",
			Priority: 0,
		}
	}

	// Layer 2: global workspace (priority 10)
	l.loadDir(ws, l.globalDir, "global", 10)

	// Layer 3: agent-specific workspace (priority 20)
	agentWSDir := filepath.Join(l.agentsDir, agentID, "workspace")
	l.loadDir(ws, agentWSDir, "agent", 20)

	// Layer 4: project workspace overlay (priority 30)
	// Only loads if .eclaire/ exists at the project root.
	if projectDir != "" {
		projectWSDir := filepath.Join(projectDir, "workspace")
		l.loadDir(ws, projectWSDir, "project", 30)

		// Also check project agent-specific overlay
		projectAgentWSDir := filepath.Join(projectDir, "agents", agentID, "workspace")
		l.loadDir(ws, projectAgentWSDir, "project", 35)
	}

	// Load memory
	l.loadMemory(ws)

	return ws, nil
}

func (l *WorkspaceLoader) loadDir(ws *Workspace, dir, source string, priority int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory doesn't exist, skip silently
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Only override if higher priority
		if existing, ok := ws.Files[name]; ok && existing.Priority >= priority {
			continue
		}

		ws.Files[name] = WorkspaceFile{
			Name:     name,
			Content:  string(data),
			Source:   source,
			Priority: priority,
		}
	}
}

func (l *WorkspaceLoader) loadMemory(ws *Workspace) {
	// Load curated MEMORY.md
	if f, ok := ws.Files[FileMemory]; ok {
		ws.Memory.Curated = f.Content
	}

	// Load daily memory logs
	memDirs := []string{
		filepath.Join(l.globalDir, "memory"),
	}
	if l.projectDir != "" {
		memDirs = append(memDirs, filepath.Join(l.projectDir, "workspace", "memory"))
	}

	today := time.Now().Format("2006-01-02")
	for _, dir := range memDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			date := strings.TrimSuffix(name, ".md")
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			ws.Memory.Daily[date] = string(data)
		}
	}
	_ = today // available for callers
}

// AppendMemory appends content to the curated MEMORY.md or a daily log.
func (l *WorkspaceLoader) AppendMemory(agentID, content, memType string) error {
	var path string
	switch memType {
	case "curated":
		path = filepath.Join(l.globalDir, FileMemory)
	case "daily":
		memDir := filepath.Join(l.globalDir, "memory")
		os.MkdirAll(memDir, 0o700)
		path = filepath.Join(memDir, time.Now().Format("2006-01-02")+".md")
	default:
		return fmt.Errorf("unknown memory type: %s", memType)
	}

	os.MkdirAll(filepath.Dir(path), 0o700)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open memory file: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(content + "\n")
	return err
}

// LoadStandingOrders reads all .md files from ~/.eclaire/standing_orders/ and
// concatenates them. Standing orders are persistent instructions injected every session.
func (l *WorkspaceLoader) LoadStandingOrders() string {
	dir := filepath.Join(filepath.Dir(l.globalDir), "standing_orders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			parts = append(parts, content)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BootRanToday checks if BOOT.md has already been executed today.
func (l *WorkspaceLoader) BootRanToday() bool {
	path := filepath.Join(l.globalDir, ".boot_ran")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	today := time.Now().Format("2006-01-02")
	return strings.TrimSpace(string(data)) == today
}

// MarkBootRan records that BOOT.md was executed today.
func (l *WorkspaceLoader) MarkBootRan() error {
	path := filepath.Join(l.globalDir, ".boot_ran")
	os.MkdirAll(filepath.Dir(path), 0o700)
	return os.WriteFile(path, []byte(time.Now().Format("2006-01-02")), 0o644)
}
