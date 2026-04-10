package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMeta(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    SkillMeta
		wantErr bool
	}{
		{
			name:    "valid",
			content: "---\nname: commit\ndescription: Create git commits\n---\nInstructions here",
			want:    SkillMeta{Name: "commit", Description: "Create git commits"},
		},
		{
			name:    "missing name",
			content: "---\ndescription: Something\n---\nInstructions",
			wantErr: true,
		},
		{
			name:    "no frontmatter",
			content: "Just plain text",
			wantErr: true,
		},
		{
			name:    "empty frontmatter",
			content: "---\n---\nContent",
			wantErr: true,
		},
		{
			name:    "malformed yaml",
			content: "---\n{{{bad yaml\n---\nContent",
			wantErr: true,
		},
		{
			name:    "extra fields ignored",
			content: "---\nname: test\ndescription: desc\nextra: ignored\n---\nContent",
			want:    SkillMeta{Name: "test", Description: "desc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSkillMeta(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
		})
	}
}

func writeSkillDir(t *testing.T, base, name, skillName, desc string) {
	t.Helper()
	dir := filepath.Join(base, name)
	os.MkdirAll(dir, 0o755)
	content := "---\nname: " + skillName + "\ndescription: " + desc + "\n---\nInstructions for " + skillName
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func TestScanSkillsDir(t *testing.T) {
	dir := t.TempDir()

	writeSkillDir(t, dir, "commit", "commit", "Create commits")
	writeSkillDir(t, dir, "review", "review-pr", "Review PRs")

	// Invalid: no SKILL.md
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)

	// Not a dir — should be skipped
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("not a skill"), 0o644)

	skills := scanSkillsDir(dir, "global", 10)
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}

	names := make(map[string]bool)
	for _, s := range skills {
		names[s.Meta.Name] = true
		if s.Source != "global" {
			t.Errorf("source = %q, want global", s.Source)
		}
		if s.Priority != 10 {
			t.Errorf("priority = %d, want 10", s.Priority)
		}
	}
	if !names["commit"] {
		t.Error("missing commit skill")
	}
	if !names["review-pr"] {
		t.Error("missing review-pr skill")
	}
}

func TestSkillLoaderHierarchy(t *testing.T) {
	base := t.TempDir()
	globalDir := filepath.Join(base, "skills")
	projectDir := filepath.Join(base, "project")

	// Same skill at both levels — project should win
	writeSkillDir(t, globalDir, "commit", "commit", "Global commit skill")
	writeSkillDir(t, filepath.Join(projectDir, "skills"), "commit", "commit", "Project commit skill")

	loader := NewSkillLoader(globalDir, "", projectDir)
	skills := loader.Load("", nil)

	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1 (deduplicated)", len(skills))
	}
	if skills[0].Source != "project" {
		t.Errorf("source = %q, want project (higher priority)", skills[0].Source)
	}
	if skills[0].Meta.Description != "Project commit skill" {
		t.Errorf("description = %q, want project version", skills[0].Meta.Description)
	}
}

func TestSkillLoaderAllowlist(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "skills")

	writeSkillDir(t, globalDir, "commit", "commit", "Commits")
	writeSkillDir(t, globalDir, "review", "review", "Reviews")
	writeSkillDir(t, globalDir, "deploy", "deploy", "Deploys")

	loader := NewSkillLoader(globalDir, "", "")
	skills := loader.Load("", []string{"commit", "deploy"})

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2 (filtered)", len(skills))
	}

	names := make(map[string]bool)
	for _, s := range skills {
		names[s.Meta.Name] = true
	}
	if !names["commit"] || !names["deploy"] {
		t.Errorf("expected commit and deploy, got %v", names)
	}
	if names["review"] {
		t.Error("review should be filtered out")
	}
}

func TestSkillLoaderEmptyDirs(t *testing.T) {
	loader := NewSkillLoader("/nonexistent", "/nonexistent", "")
	skills := loader.Load("test", nil)
	if len(skills) != 0 {
		t.Errorf("got %d skills, want 0", len(skills))
	}
}

func TestSerializeSkills(t *testing.T) {
	skills := []Skill{
		{Meta: SkillMeta{Name: "commit", Description: "Create commits"}, FilePath: "/home/user/.eclaire/skills/commit/SKILL.md"},
		{Meta: SkillMeta{Name: "review", Description: "Review PRs"}, FilePath: "/home/user/.eclaire/skills/review/SKILL.md"},
	}

	result := SerializeSkills(skills)

	if !strings.Contains(result, "<available_skills>") {
		t.Error("should contain <available_skills> tag")
	}
	if !strings.Contains(result, "</available_skills>") {
		t.Error("should contain </available_skills> closing tag")
	}
	if !strings.Contains(result, `name="commit"`) {
		t.Error("should contain commit skill")
	}
	if !strings.Contains(result, `name="review"`) {
		t.Error("should contain review skill")
	}
	if !strings.Contains(result, "Create commits") {
		t.Error("should contain commit description")
	}
}

func TestSerializeSkillsEmpty(t *testing.T) {
	result := SerializeSkills(nil)
	if result != "" {
		t.Errorf("empty skills should return empty string, got %q", result)
	}
}

func TestSerializeSkillsNoTruncation(t *testing.T) {
	// Generate many skills — all should be serialized (no arbitrary truncation)
	var skills []Skill
	for i := range 200 {
		skills = append(skills, Skill{
			Meta:     SkillMeta{Name: strings.Repeat("x", 50) + string(rune('a'+i%26)), Description: strings.Repeat("description text ", 10)},
			FilePath: "/some/very/long/path/to/skill/SKILL.md",
		})
	}

	result := SerializeSkills(skills)
	if strings.Contains(result, "truncated") {
		t.Error("should NOT contain truncation — no arbitrary skill limits")
	}
	// All 200 skills should be present
	for i := range 200 {
		name := strings.Repeat("x", 50) + string(rune('a'+i%26))
		if !strings.Contains(result, name) {
			t.Errorf("skill %q should be present in serialized output", name)
			break
		}
	}
}

func TestSkillLoaderAgentSpecific(t *testing.T) {
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")

	writeSkillDir(t, filepath.Join(agentsDir, "coding", "skills"), "lint", "lint", "Run linter")

	loader := NewSkillLoader("", agentsDir, "")

	// Should find the skill for coding agent
	skills := loader.Load("coding", nil)
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}
	if skills[0].Meta.Name != "lint" {
		t.Errorf("name = %q, want lint", skills[0].Meta.Name)
	}

	// Should not find it for a different agent
	skills = loader.Load("research", nil)
	if len(skills) != 0 {
		t.Errorf("got %d skills for research agent, want 0", len(skills))
	}
}
