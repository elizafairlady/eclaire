package agent

import (
	"path/filepath"
	"strings"
)

// BindingType determines how an agent binds to work.
type BindingType string

const (
	BindProject   BindingType = "project"
	BindDirectory BindingType = "directory"
	BindGlob      BindingType = "glob"
	BindTask      BindingType = "task"
)

// Binding maps an agent to a project, directory, or task type.
type Binding struct {
	Type     BindingType `yaml:"type" json:"type"`
	Pattern  string      `yaml:"pattern" json:"pattern"`
	Priority int         `yaml:"priority" json:"priority"`
}

// Match checks if a binding matches the given context.
func (b Binding) Match(cwd, taskType string) bool {
	switch b.Type {
	case BindProject:
		return strings.HasPrefix(cwd, b.Pattern)
	case BindDirectory:
		return cwd == b.Pattern
	case BindGlob:
		matched, _ := filepath.Match(b.Pattern, filepath.Base(cwd))
		return matched
	case BindTask:
		return taskType == b.Pattern || b.Pattern == "*"
	default:
		return false
	}
}
