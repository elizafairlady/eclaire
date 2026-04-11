package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	MaxSkills       = 150
	MaxSkillsBudget = 30000 // bytes for serialized skills section
)

// SkillMeta is the YAML frontmatter of a SKILL.md file.
type SkillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Skill is a discovered skill with provenance.
type Skill struct {
	Meta     SkillMeta
	FilePath string // absolute path to SKILL.md
	Source   string // "global", "agent", "project"
	Priority int
}

// SkillLoader discovers and assembles skills from a 3-level hierarchy.
type SkillLoader struct {
	globalDir  string // ~/.eclaire/skills/
	agentsDir  string // ~/.eclaire/agents/
	projectDir string // .eclaire/ in cwd (may be empty)
}

// NewSkillLoader creates a skill loader.
func NewSkillLoader(globalDir, agentsDir, projectDir string) *SkillLoader {
	return &SkillLoader{
		globalDir:  globalDir,
		agentsDir:  agentsDir,
		projectDir: projectDir,
	}
}

// Load discovers all skills for the given agent using the loader's static projectDir.
// For per-connection project dirs, use LoadWithProject instead.
func (l *SkillLoader) Load(agentID string, allowlist []string) []Skill {
	return l.LoadWithProject(agentID, allowlist, l.projectDir)
}

// LoadWithProject discovers all skills for the given agent, applying the 3-level
// hierarchy with deduplication (higher priority wins).
// projectDir overrides the static projectDir for layer 3 (project skills).
// If allowlist is non-nil and non-empty, only skills with matching names are returned.
func (l *SkillLoader) LoadWithProject(agentID string, allowlist []string, projectDir string) []Skill {
	skills := make(map[string]Skill)

	// Priority 10: global skills
	for _, s := range scanSkillsDir(l.globalDir, "global", 10) {
		skills[s.Meta.Name] = s
	}

	// Priority 20: agent-specific skills
	if agentID != "" && l.agentsDir != "" {
		agentSkillsDir := filepath.Join(l.agentsDir, agentID, "skills")
		for _, s := range scanSkillsDir(agentSkillsDir, "agent", 20) {
			if existing, ok := skills[s.Meta.Name]; !ok || s.Priority > existing.Priority {
				skills[s.Meta.Name] = s
			}
		}
	}

	// Priority 30: project skills
	if projectDir != "" {
		projectSkillsDir := filepath.Join(projectDir, "skills")
		for _, s := range scanSkillsDir(projectSkillsDir, "project", 30) {
			if existing, ok := skills[s.Meta.Name]; !ok || s.Priority > existing.Priority {
				skills[s.Meta.Name] = s
			}
		}
	}

	// Collect and sort
	var result []Skill
	for _, s := range skills {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Meta.Name < result[j].Meta.Name
	})

	// Apply allowlist
	if len(allowlist) > 0 {
		allowed := make(map[string]bool, len(allowlist))
		for _, name := range allowlist {
			allowed[name] = true
		}
		var filtered []Skill
		for _, s := range result {
			if allowed[s.Meta.Name] {
				filtered = append(filtered, s)
			}
		}
		result = filtered
	}

	// Enforce limit
	if len(result) > MaxSkills {
		result = result[:MaxSkills]
	}

	return result
}

// scanSkillsDir reads a directory of skill subdirectories.
// Each subdirectory must contain a SKILL.md file.
func scanSkillsDir(dir, source string, priority int) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}
		meta, err := ParseSkillMeta(string(data))
		if err != nil {
			continue
		}
		absPath, _ := filepath.Abs(skillFile)
		skills = append(skills, Skill{
			Meta:     meta,
			FilePath: absPath,
			Source:   source,
			Priority: priority,
		})
	}
	return skills
}

// ParseSkillMeta extracts YAML frontmatter from a SKILL.md file's content.
// Frontmatter is delimited by "---" on lines by itself.
func ParseSkillMeta(content string) (SkillMeta, error) {
	lines := strings.SplitN(content, "---", 3)
	if len(lines) < 3 {
		return SkillMeta{}, fmt.Errorf("no frontmatter found")
	}

	yamlBlock := strings.TrimSpace(lines[1])
	if yamlBlock == "" {
		return SkillMeta{}, fmt.Errorf("empty frontmatter")
	}

	var meta SkillMeta
	if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err != nil {
		return SkillMeta{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	if meta.Name == "" {
		return SkillMeta{}, fmt.Errorf("skill name is required")
	}

	return meta, nil
}

// SerializeSkills formats skills as an XML listing for the system prompt.
func SerializeSkills(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<available_skills>\n")
	sb.WriteString("If a task matches a skill, read its SKILL.md file for instructions before proceeding.\n\n")

	home, _ := os.UserHomeDir()

	for _, s := range skills {
		path := s.FilePath
		if home != "" {
			path = strings.Replace(path, home, "~", 1)
		}
		sb.WriteString(fmt.Sprintf("<skill name=%q file_path=%q>\n%s\n</skill>\n",
			s.Meta.Name, path, s.Meta.Description))
	}

	sb.WriteString("</available_skills>")

	return sb.String()
}
