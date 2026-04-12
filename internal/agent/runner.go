package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/hook"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/provider"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// emitKeyType is the context key for the parent's emit function.
type emitKeyType struct{}

// sessionKeyType is the context key for the current session ID.
type sessionKeyType struct{}

// projectContextKeyType is the context key for project context (inherited by sub-agents).
type projectContextKeyType struct{}

// ProjectContext carries per-connection project context through the context chain.
type ProjectContext struct {
	Root string // project directory (for git context, instruction files)
	Dir  string // .eclaire/ path (for workspace/skills); empty if no .eclaire/
}

var emitKey = emitKeyType{}
var sessionKey = sessionKeyType{}
var projectContextKey = projectContextKeyType{}

// EmitFromContext retrieves the emit function from a context, if present.
func EmitFromContext(ctx context.Context) (func(StreamEvent) error, bool) {
	fn, ok := ctx.Value(emitKey).(func(StreamEvent) error)
	return fn, ok
}

// SessionFromContext retrieves the current session ID from context.
func SessionFromContext(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey).(string)
	return s
}

// ProjectFromContext retrieves the project context, if present.
func ProjectFromContext(ctx context.Context) ProjectContext {
	pc, _ := ctx.Value(projectContextKey).(ProjectContext)
	return pc
}

// ContextWithEmit stores an emit function in the context.
func ContextWithEmit(ctx context.Context, emit func(StreamEvent) error) context.Context {
	return context.WithValue(ctx, emitKey, emit)
}

// ContextWithSession stores the current session ID in the context.
func ContextWithSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionKey, sessionID)
}

// ContextWithProject stores project context for sub-agent inheritance.
func ContextWithProject(ctx context.Context, pc ProjectContext) context.Context {
	return context.WithValue(ctx, projectContextKey, pc)
}

// StreamEvent is the typed payload for stream envelopes.
type StreamEvent struct {
	Type       string    `json:"type"`
	Delta      string    `json:"delta,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Input      string    `json:"input,omitempty"`
	Output     string    `json:"output,omitempty"`
	Usage      *UsageInfo `json:"usage,omitempty"`
	Error      string    `json:"error,omitempty"`
	Nested     bool      `json:"nested,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	Provider   string    `json:"provider,omitempty"`
}

// UsageInfo tracks token consumption.
type UsageInfo struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Stream event type constants.
const (
	EventTextDelta  = "text_delta"
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventStepFinish = "step_finish"
	EventError      = "error"

	EventSubAgentStarted   = "sub_agent_started"
	EventSubAgentToolCall  = "sub_agent_tool_call"
	EventSubAgentToolResult = "sub_agent_tool_result"
	EventSubAgentCompleted = "sub_agent_completed"
)

// ModelResolver resolves a model and context window for a given role.
// provider.Router and testutil.MockAgentFactory implement this interface.
type ModelResolver interface {
	ResolveWithContext(ctx context.Context, role string) (*provider.ModelResolution, error)
}

// Runner executes agents via ConversationRuntime.
type Runner struct {
	Router        ModelResolver
	Tools         *tool.Registry
	Sessions      *persist.SessionStore
	Bus           *bus.Bus
	Logger        *slog.Logger
	Workspaces    *WorkspaceLoader
	ContextEngine *ContextEngine
	Registry      *Registry
	HookRunner    *hook.Runner
	PermChecker   *tool.PermissionChecker
	Approver      tool.Approver
	WorkspaceRoot string
	EclaireDir    string
	SystemEvents  *SystemEventQueue
	flushState    *memoryFlushState
}

// RunConfig configures a single agent run.
type RunConfig struct {
	AgentID       string
	Agent         Agent
	Prompt        string
	SessionID     string            // empty = create new
	ParentSessID  string            // for sub-agent sessions
	History       []fantasy.Message // for session continuation
	ContextWindow int64
	PromptMode    PromptMode        // default "" treated as "full"
	Title          string              // explicit session title; empty = auto-derive from prompt
	Compaction     CompactionConfig   // zero value = disabled
	PermissionMode tool.PermissionMode // zero value = PermissionAllow
	WorkspaceRoots []string           // override workspace root dirs; empty = use runner defaults
	ProjectRoot    string             // client's project root dir (for git context, instruction files)
	ProjectDir     string             // client's .eclaire/ dir (for project workspace/skills); empty if no .eclaire/
}

// CompactionConfig controls automatic conversation compaction.
type CompactionConfig struct {
	Enabled       bool
	ThresholdToks int64 // cumulative input tokens before compaction triggers (default 100000)
	PreserveCount int   // number of recent messages to preserve (default 4)
}

// DefaultCompactionConfig returns sensible defaults matching Claw Code's behavior.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:       true,
		ThresholdToks: 100_000,
		PreserveCount: 4,
	}
}

// RunResult is returned after an agent run completes.
type RunResult struct {
	SessionID   string
	TotalUsage  fantasy.Usage
	Steps       int
	Content     string
	Compactions int // number of compaction cycles that occurred
}

// Run executes the agent loop using ConversationRuntime (our own agentic loop).
// Reference: Claw Code ConversationRuntime.run_turn()
func (r *Runner) Run(ctx context.Context, cfg RunConfig, emit func(StreamEvent) error) (*RunResult, error) {
	// Create or load session
	var sessionID string
	if cfg.SessionID != "" {
		sessionID = cfg.SessionID
	} else {
		title := cfg.Title
		if title == "" {
			title = cfg.Prompt
		}
		meta, err := r.Sessions.Create(cfg.AgentID, title, cfg.ParentSessID)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		sessionID = meta.ID
	}

	// Register this instance in the registry for observability and lifecycle tracking.
	// Each concurrent run of the same agent type gets its own instance keyed by session ID.
	if r.Registry != nil {
		r.Registry.RegisterInstance(sessionID, cfg.AgentID, nil)
		defer r.Registry.RemoveInstance(sessionID)
	}

	// Get tools for this agent (no wrappers — runtime handles hooks + permissions directly)
	agentTools := r.Tools.ForAgent(cfg.AgentID, cfg.Agent.RequiredTools())

	// Store emit, session, and project context so sub-agent tools can inherit them
	ctx = ContextWithEmit(ctx, emit)
	ctx = ContextWithSession(ctx, sessionID)
	if cfg.ProjectRoot != "" || cfg.ProjectDir != "" {
		ctx = ContextWithProject(ctx, ProjectContext{Root: cfg.ProjectRoot, Dir: cfg.ProjectDir})
	}

	// Resolve model: try agent's role via routing table first.
	// If the agent has a ModelOverride, try that as a routing key first,
	// then fall back to using it as a direct model identifier.
	role := string(cfg.Agent.Role())
	var modelOverride string
	if co, ok := cfg.Agent.(ConfigOverrides); ok {
		modelOverride = co.ModelOverride()
	}

	resolveRole := role
	if modelOverride != "" {
		resolveRole = modelOverride
	}

	resolution, err := r.Router.ResolveWithContext(ctx, resolveRole)
	if err != nil && modelOverride != "" && resolveRole != role {
		// ModelOverride didn't match a route — fall back to the agent's role
		resolution, err = r.Router.ResolveWithContext(ctx, role)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	lm := resolution.Model
	contextWindow := resolution.ContextWindow
	providerID := resolution.ProviderID
	if contextWindow <= 0 {
		contextWindow = cfg.ContextWindow
	}
	if contextWindow <= 0 {
		contextWindow = 128000
	}

	// Assemble system prompt with the resolved context window
	systemPrompt := r.assemblePrompt(cfg, agentTools, contextWindow, sessionID)

	// Build workspace roots for permission checking
	var roots []string
	if len(cfg.WorkspaceRoots) > 0 {
		roots = cfg.WorkspaceRoots
	} else {
		// Default: ~/.eclaire/ only (main session scope)
		if r.EclaireDir != "" {
			roots = append(roots, r.EclaireDir)
		}
	}

	// Create ConversationRuntime
	rt := &ConversationRuntime{
		Model:          lm,
		SystemPrompt:   systemPrompt,
		Tools:          agentTools,
		HookRunner:     r.HookRunner,
		PermChecker:    r.PermChecker,
		Approver:       r.Approver,
		PermMode:       cfg.PermissionMode,
		WorkspaceRoots: roots,
		AgentID:        cfg.AgentID,
		Logger:         r.Logger,
		ContextWindow:  contextWindow,
	}

	// Record user message
	r.Sessions.Append(sessionID, persist.EventUserMessage, persist.MessageData{
		Content: cfg.Prompt,
	})

	// Build initial messages: system prompt + history + user prompt
	messages := []fantasy.Message{
		fantasy.NewSystemMessage(systemPrompt),
	}
	messages = append(messages, cfg.History...)
	messages = append(messages, fantasy.NewUserMessage(cfg.Prompt))

	// Wrap emit to also persist events to session
	persistEmit := func(ev StreamEvent) error {
		switch ev.Type {
		case EventToolCall:
			r.Sessions.Append(sessionID, persist.EventToolCall, persist.ToolCallData{
				Name:       ev.ToolName,
				ToolCallID: ev.ToolCallID,
				Input:      ev.Input,
				AgentID:    cfg.AgentID,
			})
			r.Bus.Publish(bus.TopicToolCall, bus.ToolEvent{
				AgentID:  cfg.AgentID,
				ToolName: ev.ToolName,
				Input:    ev.Input,
			})
		case EventToolResult:
			r.Sessions.Append(sessionID, persist.EventToolResult, persist.ToolCallData{
				Name:       ev.ToolName,
				ToolCallID: ev.ToolCallID,
				Output:     ev.Output,
				AgentID:    cfg.AgentID,
			})
			r.Bus.Publish(bus.TopicToolResult, bus.ToolEvent{
				AgentID:  cfg.AgentID,
				ToolName: ev.ToolName,
				Output:   ev.Output,
			})
		case EventStepFinish:
			ev.Provider = providerID
			if ev.Usage != nil {
				r.Sessions.Append(sessionID, persist.EventStepFinish, persist.StepData{
					TokensIn:  ev.Usage.InputTokens,
					TokensOut: ev.Usage.OutputTokens,
				})
			}
		}
		emit(ev) // best-effort: client may be disconnected
		return nil
	}

	// Execute the turn
	summary, _, err := rt.RunTurn(ctx, messages, persistEmit)
	if err != nil {
		emit(StreamEvent{Type: EventError, Error: err.Error(), AgentID: cfg.AgentID})
		r.Sessions.Append(sessionID, persist.EventSystemMessage, persist.MessageData{
			Content: fmt.Sprintf("Error: %v", err),
		})
		// Don't mark persistent sessions (main/project) as error — they survive across runs.
		if meta, merr := r.Sessions.GetMeta(sessionID); merr == nil && !isPersistentSession(meta) {
			r.Sessions.UpdateStatus(sessionID, "error")
		}
		return nil, fmt.Errorf("agent run: %w", err)
	}

	// Persist final assistant message
	if summary.Text != "" {
		r.Sessions.Append(sessionID, persist.EventAssistantMessage, persist.MessageData{
			Content: summary.Text,
			AgentID: cfg.AgentID,
		})
	}

	// Mark ephemeral sessions as completed. Persistent sessions (main, project)
	// stay "active" so they can be resumed — FindByProject() relies on this.
	if meta, merr := r.Sessions.GetMeta(sessionID); merr == nil && !isPersistentSession(meta) {
		r.Sessions.UpdateStatus(sessionID, "completed")
	}

	return &RunResult{
		SessionID:  sessionID,
		TotalUsage: summary.Usage,
		Steps:      summary.Iterations,
		Content:    summary.Text,
	}, nil
}

// assemblePrompt builds the system prompt for a run using the context engine.
// sessionID is used to drain pending system events into the prompt.
func (r *Runner) assemblePrompt(cfg RunConfig, agentTools []fantasy.AgentTool, contextWindow int64, sessionID string) string {
	if r.ContextEngine == nil || r.Workspaces == nil {
		if ya, ok := cfg.Agent.(interface{ SystemPrompt() string }); ok {
			return ya.SystemPrompt()
		}
		return ""
	}

	var embedded map[string]string
	if ba, ok := cfg.Agent.(interface{ EmbeddedWorkspace() map[string]string }); ok {
		embedded = ba.EmbeddedWorkspace()
	}
	ws, err := r.Workspaces.LoadWithProject(cfg.AgentID, embedded, cfg.ProjectDir)
	if err != nil {
		r.Logger.Warn("failed to load workspace", "agent", cfg.AgentID, "err", err)
		if ya, ok := cfg.Agent.(interface{ SystemPrompt() string }); ok {
			return ya.SystemPrompt()
		}
		return ""
	}

	var toolNames []string
	for _, t := range agentTools {
		toolNames = append(toolNames, t.Info().Name)
	}
	var skillsAllowlist []string
	if sa, ok := cfg.Agent.(interface{ SkillsAllowlist() []string }); ok {
		skillsAllowlist = sa.SkillsAllowlist()
	}
	var sectionFeatures []SectionFeature
	if sf, ok := cfg.Agent.(SectionFeatured); ok {
		sectionFeatures = sf.SectionFeatures()
	}

	// Drain pending system events for this session and inject as overrides.
	// Reference: OpenClaw src/auto-reply/reply/session-system-events.ts
	var overrides string
	if r.SystemEvents != nil && sessionID != "" {
		drained := r.SystemEvents.Drain(sessionID)
		if len(drained) > 0 {
			overrides = FormatDrained(drained)
		}
	}

	var assembleOpts *AssembleOpts
	if cfg.ProjectRoot != "" || cfg.ProjectDir != "" {
		assembleOpts = &AssembleOpts{ProjectRoot: cfg.ProjectRoot, ProjectDir: cfg.ProjectDir}
	}
	plan := r.ContextEngine.Assemble(cfg.AgentID, ws, toolNames, contextWindow, overrides, cfg.PromptMode, skillsAllowlist, sectionFeatures, assembleOpts)
	return plan.SystemPrompt
}

// shouldCompactStop returns true when remaining context budget is critically low.
func shouldCompactStop(contextWindow int64, totalUsage *fantasy.Usage) bool {
	if contextWindow <= 0 {
		return false
	}
	used := totalUsage.InputTokens + totalUsage.OutputTokens
	remaining := contextWindow - used
	threshold := int64(float64(contextWindow) * 0.2)
	if contextWindow > 200000 {
		threshold = 40000
	}
	return remaining <= threshold
}

// RunWithCompaction runs the agent loop with automatic compaction.
// When cumulative input tokens exceed the threshold and the context budget is low,
// it summarizes older messages, persists a compaction event, rebuilds history, and re-runs.
func (r *Runner) RunWithCompaction(ctx context.Context, cfg RunConfig, emit func(StreamEvent) error) (*RunResult, error) {
	if !cfg.Compaction.Enabled {
		return r.Run(ctx, cfg, emit)
	}

	threshold := cfg.Compaction.ThresholdToks
	if threshold <= 0 {
		threshold = 100_000
	}
	preserveCount := cfg.Compaction.PreserveCount
	if preserveCount <= 0 {
		preserveCount = 4
	}

	var cumulativeUsage fantasy.Usage
	totalCompactions := 0
	const maxCompactions = 5

	for {
		result, err := r.Run(ctx, cfg, emit)
		if err != nil {
			if result != nil {
				result.Compactions = totalCompactions
			}
			return result, err
		}

		cumulativeUsage.InputTokens += result.TotalUsage.InputTokens
		cumulativeUsage.OutputTokens += result.TotalUsage.OutputTokens

		// Check if we need to compact
		needsCompaction := cumulativeUsage.InputTokens >= threshold &&
			totalCompactions < maxCompactions &&
			shouldCompactStop(cfg.ContextWindow, &result.TotalUsage)

		if !needsCompaction {
			result.Compactions = totalCompactions
			return result, nil
		}

		totalCompactions++
		r.Logger.Info("compacting conversation",
			"session", result.SessionID,
			"input_tokens", cumulativeUsage.InputTokens,
			"compaction", totalCompactions,
		)

		// Read and rebuild messages for structured compaction
		events, err := r.Sessions.ReadEvents(result.SessionID)
		if err != nil {
			result.Compactions = totalCompactions
			return result, fmt.Errorf("read events for compaction: %w", err)
		}
		allMessages := persist.RebuildMessages(events)

		if len(allMessages) <= preserveCount {
			result.Compactions = totalCompactions
			return result, nil
		}

		// Flush important context to daily memory before compaction.
		// Best-effort: failure does not block compaction.
		// Reference: OpenClaw src/auto-reply/reply/memory-flush.ts
		fs := r.ensureFlushState()
		if fs.shouldFlush(result.SessionID, allMessages) {
			r.Logger.Info("flushing memory before compaction",
				"session", result.SessionID,
				"messages", len(allMessages),
			)
			if ferr := r.flushMemory(ctx, cfg, result.SessionID, allMessages, emit); ferr != nil {
				r.Logger.Warn("memory flush failed, continuing with compaction", "err", ferr)
			}
		}

		// Structured compaction: analyze messages, build summary, preserve tail
		compactResult, cerr := r.ContextEngine.Compact(ctx, allMessages, preserveCount)
		if cerr != nil {
			r.Logger.Error("compaction failed", "err", cerr)
			result.Compactions = totalCompactions
			return result, nil // don't fail — return what we have
		}
		if compactResult == nil {
			result.Compactions = totalCompactions
			return result, nil
		}

		// Persist compaction event
		r.Sessions.Append(result.SessionID, persist.EventCompaction, persist.MessageData{
			Content: compactResult.Summary,
		})

		emit(StreamEvent{
			Type:    "compaction",
			AgentID: cfg.AgentID,
			Output:  fmt.Sprintf("Compacted %d messages into structured summary", compactResult.RemovedCount),
		})

		r.Bus.Publish(bus.TopicCompaction, bus.AgentEvent{
			AgentID: cfg.AgentID,
			Status:  "compacted",
		})

		// Continue with compacted history
		cfg.History = compactResult.CompactedHistory
		cfg.SessionID = result.SessionID
		cfg.Prompt = "Continue where you left off. The conversation was compacted to save context space."
	}
}

// RunSubAgent runs a named agent as a child, forwarding events through the parent's emit.
// It creates a child session, wraps the emit to set Nested/AgentID, and runs the agent loop.
func (r *Runner) RunSubAgent(parentCtx context.Context, agentID, prompt, parentSessionID string) (string, string, error) {
	// Look up the agent
	a, ok := r.Registry.Get(agentID)
	if !ok {
		return "", "", fmt.Errorf("agent %q not found", agentID)
	}

	// Get parent session from context if not provided
	if parentSessionID == "" {
		parentSessionID = SessionFromContext(parentCtx)
	}

	// Get parent emit from context
	parentEmit, hasEmit := EmitFromContext(parentCtx)

	// Emit sub_agent_started
	taskID := fmt.Sprintf("task_%s_%d", agentID, time.Now().UnixNano())
	if hasEmit {
		parentEmit(StreamEvent{
			Type:    EventSubAgentStarted,
			AgentID: agentID,
			TaskID:  taskID,
			Output:  prompt,
		})
	}
	r.Bus.Publish(bus.TopicSubAgentStarted, bus.SubAgentEvent{
		TaskID:  taskID,
		AgentID: agentID,
		Status:  "started",
	})

	// Create wrapped emit that marks events as nested
	wrappedEmit := func(ev StreamEvent) error {
		if !hasEmit {
			return nil
		}
		// Forward tool calls and results as nested sub-agent events
		switch ev.Type {
		case EventToolCall:
			return parentEmit(StreamEvent{
				Type:       EventSubAgentToolCall,
				ToolName:   ev.ToolName,
				ToolCallID: ev.ToolCallID,
				Input:      ev.Input,
				Nested:     true,
				AgentID:    agentID,
				TaskID:     taskID,
			})
		case EventToolResult:
			return parentEmit(StreamEvent{
				Type:       EventSubAgentToolResult,
				ToolName:   ev.ToolName,
				ToolCallID: ev.ToolCallID,
				Output:     ev.Output,
				Nested:     true,
				AgentID:    agentID,
				TaskID:     taskID,
			})
		case EventTextDelta:
			// Don't forward text deltas from sub-agents — the final result
			// is returned as the tool result to the parent agent
			return nil
		case EventStepFinish:
			// Forward step finish so parent can track token usage
			ev.Nested = true
			ev.AgentID = agentID
			ev.TaskID = taskID
			return parentEmit(ev)
		default:
			ev.Nested = true
			ev.AgentID = agentID
			ev.TaskID = taskID
			return parentEmit(ev)
		}
	}

	// Inherit project context from parent so sub-agents get the right workspace/git context
	pc := ProjectFromContext(parentCtx)

	// Run the agent with minimal prompt — sub-agents don't need memory/user/daily
	cfg := RunConfig{
		AgentID:        agentID,
		Agent:          a,
		Prompt:         prompt,
		ParentSessID:   parentSessionID,
		PromptMode:     PromptModeMinimal,
		PermissionMode: tool.PermissionWriteOnly,
		ProjectRoot:    pc.Root,
		ProjectDir:     pc.Dir,
	}

	result, err := r.Run(parentCtx, cfg, wrappedEmit)

	// Emit sub_agent_completed
	status := "completed"
	content := ""
	sessionID := ""
	if err != nil {
		status = "error"
	} else if result != nil {
		content = result.Content
		sessionID = result.SessionID
	}

	if hasEmit {
		parentEmit(StreamEvent{
			Type:    EventSubAgentCompleted,
			AgentID: agentID,
			TaskID:  taskID,
			Output:  content,
		})
	}
	r.Bus.Publish(bus.TopicSubAgentCompleted, bus.SubAgentEvent{
		TaskID:          taskID,
		AgentID:         agentID,
		ParentSessionID: parentSessionID,
		SessionID:       sessionID,
		Status:          status,
		Result:          content,
	})

	if err != nil {
		return "", "", fmt.Errorf("sub-agent %q: %w", agentID, err)
	}

	return content, sessionID, nil
}

// isPersistentSession returns true for main and project sessions which should
// stay "active" across runs rather than being marked completed.
func isPersistentSession(meta *persist.SessionMeta) bool {
	return meta.Kind == "main" || meta.Kind == "project"
}

func toolResultString(content fantasy.ToolResultOutputContent) string {
	if content == nil {
		return ""
	}
	switch c := content.(type) {
	case fantasy.ToolResultOutputContentText:
		return c.Text
	case fantasy.ToolResultOutputContentError:
		return fmt.Sprintf("Error: %v", c.Error)
	case fantasy.ToolResultOutputContentMedia:
		return c.Text
	default:
		return fmt.Sprintf("%v", content)
	}
}


