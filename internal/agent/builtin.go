package agent

import "context"

// builtinAgent is a Go-defined agent with embedded workspace defaults.
type builtinAgent struct {
	id                string
	name              string
	description       string
	role              Role
	tools             []string
	embeddedWorkspace map[string]string
	builtin           bool
	skills            []string          // optional skill allowlist
	features          []SectionFeature  // opt-in prompt sections
}

func (a *builtinAgent) ID() string            { return a.id }
func (a *builtinAgent) Name() string          { return a.name }
func (a *builtinAgent) Description() string   { return a.description }
func (a *builtinAgent) Init(_ context.Context, _ AgentDeps) error { return nil }
func (a *builtinAgent) Shutdown(_ context.Context) error          { return nil }
func (a *builtinAgent) Role() Role            { return a.role }
func (a *builtinAgent) Bindings() []Binding   { return nil }
func (a *builtinAgent) RequiredTools() []string { return a.tools }
func (a *builtinAgent) CredentialScope() string { return a.id }
func (a *builtinAgent) IsBuiltIn() bool                      { return true }
func (a *builtinAgent) EmbeddedWorkspace() map[string]string { return a.embeddedWorkspace }
func (a *builtinAgent) SkillsAllowlist() []string            { return a.skills }
func (a *builtinAgent) SectionFeatures() []SectionFeature    { return a.features }

// SystemPrompt returns the SOUL.md content as a fallback.
// The context engine should be used instead for full prompt assembly.
func (a *builtinAgent) SystemPrompt() string {
	if soul, ok := a.embeddedWorkspace[FileSoul]; ok {
		return soul
	}
	return ""
}

func (a *builtinAgent) Handle(_ context.Context, _ Request) (Response, error) {
	return Response{Content: "use Runner for execution", Done: true}, nil
}
func (a *builtinAgent) Stream(_ context.Context, _ Request) (<-chan StreamPart, error) {
	ch := make(chan StreamPart, 1)
	ch <- StreamPart{Delta: "use Runner for execution", Done: true}
	close(ch)
	return ch, nil
}

// OrchestratorAgent is Claire — the Executive Assistant orchestrator.
func OrchestratorAgent() Agent {
	return &builtinAgent{
		id:          "orchestrator",
		name:        "Claire",
		description: "Executive Assistant — orchestrates specialist agents and manages daily workflow",
		role:        RoleOrchestrator,
		tools: []string{
			"shell", "read", "write", "edit", "multiedit", "view",
			"glob", "grep", "ls", "apply_patch",
			"fetch", "download", "web_search", "rss_feed",
			"job_output", "job_kill",
			"todos",
			"agent", "task_status",
			"memory_write", "memory_read",
			"eclaire_reminder", "eclaire_briefing", "eclaire_email",
			"eclaire_manage",
		},
		builtin: true,
		embeddedWorkspace: map[string]string{
			FileSoul: orchestratorSoul,
			FileAgents: orchestratorAgents,
			FileUser: orchestratorUser,
			FileBoot: orchestratorBoot,
			FileHeartbeat: orchestratorHeartbeat,
		},
	}
}

// CodingAgent is a full programming agent — as complete as a standalone coding tool.
func CodingAgent() Agent {
	return &builtinAgent{
		id:          "coding",
		name:        "Coding",
		description: "Full programming agent for writing, editing, debugging, and reviewing code",
		role:        RoleComplex,
		tools: []string{
			"shell", "read", "write", "edit", "multiedit", "view",
			"glob", "grep", "ls", "apply_patch",
			"fetch", "download",
			"job_output", "job_kill",
			"todos", "memory_write", "memory_read",
		},
		builtin: true,
		features: []SectionFeature{
			FeatureInstructionFiles,
			FeatureProjectContext,
		},
		embeddedWorkspace: map[string]string{
			FileSoul:   codingSoul,
			FileAgents: codingAgents,
		},
	}
}

// ResearchAgent gathers information from the web.
func ResearchAgent() Agent {
	return &builtinAgent{
		id:          "research",
		name:        "Research",
		description: "Web research agent for information gathering, analysis, and report writing",
		role:        RoleComplex,
		tools: []string{
			"web_search", "fetch", "download", "rss_feed",
			"read", "write", "edit",
			"shell", "todos", "memory_write", "memory_read",
		},
		builtin: true,
		embeddedWorkspace: map[string]string{
			FileSoul:   researchSoul,
			FileAgents: researchAgents,
		},
	}
}

// SysadminAgent handles system administration.
func SysadminAgent() Agent {
	return &builtinAgent{
		id:          "sysadmin",
		name:        "Sysadmin",
		description: "System administration agent for servers, deployments, monitoring, and infrastructure",
		role:        RoleComplex,
		tools: []string{
			"shell", "read", "write", "edit",
			"glob", "grep", "ls", "view",
			"fetch", "job_output", "job_kill",
			"todos", "memory_write", "memory_read",
		},
		builtin: true,
		embeddedWorkspace: map[string]string{
			FileSoul:   sysadminSoul,
			FileAgents: sysadminAgents,
		},
	}
}

// ConfigAgent modifies eclaire's own configuration.
func ConfigAgent() Agent {
	return &builtinAgent{
		id:          "config",
		name:        "Config",
		description: "Configuration agent that modifies eclaire's settings, agents, and model routing",
		role:        RoleSimple,
		tools: []string{
			"read", "write", "edit", "ls", "glob",
			"memory_write", "memory_read",
			"eclaire_manage",
		},
		builtin: true,
		embeddedWorkspace: map[string]string{
			FileSoul: configSoul,
		},
	}
}

// BuiltinAgents returns all built-in agents.
func BuiltinAgents() []Agent {
	return []Agent{
		OrchestratorAgent(),
		CodingAgent(),
		ResearchAgent(),
		SysadminAgent(),
		ConfigAgent(),
	}
}
