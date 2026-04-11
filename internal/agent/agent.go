package agent

import (
	"context"
	"log/slog"

	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/config"
)

// Role determines which LLM provider/model an agent gets.
type Role string

const (
	RoleSimple       Role = "simple"
	RoleComplex      Role = "complex"
	RoleEmbed        Role = "embed"
	RoleOrchestrator Role = "orchestrator" // defaults to complex if no explicit route
	RoleAdversary    Role = "adversary"    // red-team QA, routed to adversarial model
)

// SectionFeature is an opt-in prompt section that agents can declare.
type SectionFeature string

const (
	// FeatureInstructionFiles walks the directory tree for CLAUDE.md / .eclaire/instructions.md.
	FeatureInstructionFiles SectionFeature = "instruction_files"
	// FeatureProjectContext injects git status, diff (staged+unstaged), recent commits.
	FeatureProjectContext SectionFeature = "project_context"
	// FeatureOutputStyle adds configurable output style guidance.
	FeatureOutputStyle SectionFeature = "output_style"
	// FeatureTaskGuidance adds task-handling directives (read before modify, verify, etc.).
	FeatureTaskGuidance SectionFeature = "task_guidance"
	// FeatureActionGuidance adds action/tool usage guidance (reversibility, blast radius).
	FeatureActionGuidance SectionFeature = "action_guidance"
)

// SectionFeatured is optionally implemented by agents that want composable prompt sections.
type SectionFeatured interface {
	SectionFeatures() []SectionFeature
}

// Request is an input to an agent.
type Request struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	CWD       string `json:"cwd,omitempty"`
}

// Response is an agent's output.
type Response struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
}

// StreamPart is a chunk of streaming output.
type StreamPart struct {
	Delta    string `json:"delta,omitempty"`
	ToolCall string `json:"tool_call,omitempty"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// AgentDeps is injected into agents during Init.
type AgentDeps struct {
	Bus    *bus.Bus
	Config *config.Store
	Logger *slog.Logger
}

// Agent is the core interface for all agents.
type Agent interface {
	ID() string
	Name() string
	Description() string

	Init(ctx context.Context, deps AgentDeps) error
	Shutdown(ctx context.Context) error

	Handle(ctx context.Context, req Request) (Response, error)
	Stream(ctx context.Context, req Request) (<-chan StreamPart, error)

	Role() Role
	Bindings() []Binding
	RequiredTools() []string
	CredentialScope() string
}

// ConfigOverrides is optionally implemented by agents with per-agent model config.
type ConfigOverrides interface {
	ModelOverride() string // "" means use role default
}

// BackgroundAgent runs autonomously on a heartbeat interval.
type BackgroundAgent interface {
	Agent
	HeartbeatInterval() int // seconds
	Heartbeat(ctx context.Context) error
}

// Status tracks an agent's current state.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusRunning  Status = "running"
	StatusWaiting  Status = "waiting"
	StatusError    Status = "error"
	StatusSpawning Status = "spawning"
)

// Info is a snapshot of an agent's state for display.
type Info struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Role        Role     `json:"role"`
	Status      Status   `json:"status"`
	Tools       []string `json:"tools"`
	BuiltIn     bool     `json:"built_in"`
	Error       string   `json:"error,omitempty"`
}
