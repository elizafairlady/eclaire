package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentDef is the YAML definition of an agent.
type AgentDef struct {
	IDField          string    `yaml:"id"`
	NameField        string    `yaml:"name"`
	DescriptionField string    `yaml:"description"`
	RoleField        string    `yaml:"role"`
	BindingsField    []Binding `yaml:"bindings"`
	Tools            []string  `yaml:"tools"`
	SystemPrompt     string    `yaml:"system_prompt"`
	CredScope        string    `yaml:"credential_scope"`
	Model            string    `yaml:"model,omitempty"`
	Skills           []string  `yaml:"skills,omitempty"`
}

// LoadAgentsDir loads agent definitions from a directory.
// Supports both directory-based agents (<dir>/<id>/agent.yaml + workspace/)
// and flat YAML files (<dir>/<id>.yaml) for backward compatibility.
func LoadAgentsDir(dir string) ([]Agent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir %s: %w", dir, err)
	}

	var agents []Agent
	for _, entry := range entries {
		if entry.IsDir() {
			// Directory-based agent: <dir>/<id>/agent.yaml
			agentYAML := filepath.Join(dir, entry.Name(), "agent.yaml")
			if _, err := os.Stat(agentYAML); err != nil {
				continue // not an agent directory
			}
			a, err := LoadAgentDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", entry.Name(), err)
			}
			agents = append(agents, a)
			continue
		}

		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		a, err := LoadAgentFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// LoadAgentDir loads a directory-based agent with workspace files.
func LoadAgentDir(dir string) (Agent, error) {
	yamlPath := filepath.Join(dir, "agent.yaml")
	a, err := LoadAgentFile(yamlPath)
	if err != nil {
		return nil, err
	}
	ya := a.(*yamlAgent)
	ya.sourceDir = dir
	ya.workspace = loadDirWorkspace(filepath.Join(dir, "workspace"))
	return ya, nil
}

// LoadAgentFile loads a single YAML agent definition.
func LoadAgentFile(path string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def AgentDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if def.IDField == "" {
		def.IDField = filepath.Base(path[:len(path)-len(filepath.Ext(path))])
	}
	if def.NameField == "" {
		def.NameField = def.IDField
	}
	if def.RoleField == "" {
		def.RoleField = string(RoleSimple)
	}
	if def.CredScope == "" {
		def.CredScope = def.IDField
	}

	return &yamlAgent{def: def}, nil
}

// loadDirWorkspace reads .md files from a workspace directory.
func loadDirWorkspace(wsDir string) map[string]string {
	ws := make(map[string]string)
	entries, err := os.ReadDir(wsDir)
	if err != nil {
		return ws
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(wsDir, entry.Name()))
		if err != nil {
			continue
		}
		ws[entry.Name()] = string(data)
	}
	return ws
}

// yamlAgent wraps AgentDef to implement the Agent interface.
type yamlAgent struct {
	def       AgentDef
	workspace map[string]string // from <dir>/workspace/*.md
	sourceDir string
}

func (a *yamlAgent) ID() string          { return a.def.IDField }
func (a *yamlAgent) Name() string        { return a.def.NameField }
func (a *yamlAgent) Description() string { return a.def.DescriptionField }
func (a *yamlAgent) Role() Role          { return Role(a.def.RoleField) }
func (a *yamlAgent) Bindings() []Binding { return a.def.BindingsField }
func (a *yamlAgent) RequiredTools() []string {
	if a.def.Tools == nil {
		return []string{}
	}
	return a.def.Tools
}
func (a *yamlAgent) CredentialScope() string           { return a.def.CredScope }
func (a *yamlAgent) SystemPrompt() string              { return a.def.SystemPrompt }
func (a *yamlAgent) IsBuiltIn() bool                   { return false }
func (a *yamlAgent) EmbeddedWorkspace() map[string]string { return a.workspace }
func (a *yamlAgent) ModelOverride() string             { return a.def.Model }
func (a *yamlAgent) SkillsAllowlist() []string         { return a.def.Skills }

// InstructionFile is a discovered project instruction file (CLAUDE.md, etc.).
type InstructionFile struct {
	Path    string
	Content string
}

// DiscoverInstructionFiles walks from cwd to root, collecting instruction files.
// Files are collected in root-first order. Duplicates (by content hash) are removed.
// Reference: Claw Code rust/crates/runtime/src/prompt.rs discover_instruction_files()
func DiscoverInstructionFiles(cwd string) []InstructionFile {
	// Build ancestor chain from root to cwd
	var dirs []string
	cursor := cwd
	for {
		dirs = append(dirs, cursor)
		parent := filepath.Dir(cursor)
		if parent == cursor {
			break
		}
		cursor = parent
	}
	// Reverse so we walk root → cwd
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	var files []InstructionFile
	for _, dir := range dirs {
		candidates := []string{
			filepath.Join(dir, "CLAUDE.md"),
			filepath.Join(dir, "CLAUDE.local.md"),
			filepath.Join(dir, ".eclaire", "CLAUDE.md"),
			filepath.Join(dir, ".eclaire", "instructions.md"),
		}
		for _, path := range candidates {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(content))
			if text == "" {
				continue
			}
			files = append(files, InstructionFile{
				Path:    path,
				Content: text,
			})
		}
	}

	return dedupeInstructionFiles(files)
}

// dedupeInstructionFiles removes files with identical content (by SHA-256 hash).
// Keeps the first occurrence (closest to root).
func dedupeInstructionFiles(files []InstructionFile) []InstructionFile {
	seen := make(map[[32]byte]bool, len(files))
	var result []InstructionFile
	for _, f := range files {
		normalized := strings.TrimSpace(f.Content)
		hash := sha256.Sum256([]byte(normalized))
		if seen[hash] {
			continue
		}
		seen[hash] = true
		result = append(result, f)
	}
	return result
}
