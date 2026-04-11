package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/provider"
)

// Bootstrap size limits (from OpenClaw agents.defaults.bootstrapMaxChars).
const (
	MaxFileChars  = 20_000
	MaxTotalChars = 150_000
)

// PromptMode controls which sections are included in the system prompt.
type PromptMode string

const (
	PromptModeFull    PromptMode = "full"
	PromptModeMinimal PromptMode = "minimal"
	PromptModeNone    PromptMode = "none"
)

// PromptSection is a named section of the system prompt with priority.
// Higher priority sections survive compaction.
type PromptSection struct {
	Name     string
	Content  string
	Priority int
	Tokens   int64 // estimated
}

// TokenBudget tracks context window allocation.
type TokenBudget struct {
	ContextWindow   int64
	SystemPromptEst int64
	HistoryEst      int64
	ReservedOutput  int64
	Available       int64
}

// ShouldCompact returns true if history exceeds available space.
func (b *TokenBudget) ShouldCompact() bool {
	return b.HistoryEst > b.Available
}

// ContextPlan is the assembled context for an agent run.
type ContextPlan struct {
	SystemPrompt string
	Sections     []PromptSection
	Budget       TokenBudget
}

// ContextEngine assembles system prompts from workspace files and manages compaction.
type ContextEngine struct {
	router     *provider.Router
	workspaces *WorkspaceLoader
	skills     *SkillLoader
	registry   *Registry
}

// NewContextEngine creates a context engine.
func NewContextEngine(router *provider.Router, workspaces *WorkspaceLoader, skills *SkillLoader) *ContextEngine {
	return &ContextEngine{
		router:     router,
		workspaces: workspaces,
		skills:     skills,
	}
}

// SetRegistry gives the context engine access to the agent registry for dynamic agent listing.
func (e *ContextEngine) SetRegistry(r *Registry) {
	e.registry = r
}

// AssembleOpts configures context assembly beyond the required parameters.
type AssembleOpts struct {
	// ProjectRoot is the client's project directory (e.g. "/home/user/myproject").
	// Used for instruction file discovery, git context, and CWD display.
	// If empty, falls back to os.Getwd() (daemon CWD).
	ProjectRoot string

	// ProjectDir is the .eclaire/ directory path (e.g. "/home/user/myproject/.eclaire").
	// Used for project-level skill discovery. May be empty if no .eclaire/ exists.
	ProjectDir string
}

// Assemble builds the system prompt for an agent run, filtered by mode.
// Empty mode is treated as PromptModeFull.
// skillsAllowlist optionally filters skills to only those named (nil = all).
// features enables opt-in composable sections (instruction files, project context, etc.).
// opts may be nil for default behavior.
func (e *ContextEngine) Assemble(agentID string, workspace *Workspace, toolNames []string, contextWindow int64, overrides string, mode PromptMode, skillsAllowlist []string, features []SectionFeature, opts *AssembleOpts) *ContextPlan {
	if mode == PromptModeNone {
		return &ContextPlan{
			SystemPrompt: "You are Claire, a personal AI assistant operating inside eclaire.",
			Budget:       TokenBudget{ContextWindow: contextWindow, Available: contextWindow},
		}
	}

	// Resolve effective CWD: prefer client's project root, fallback to daemon CWD
	effectiveCWD := ""
	if opts != nil && opts.ProjectRoot != "" {
		effectiveCWD = opts.ProjectRoot
	}
	if effectiveCWD == "" {
		effectiveCWD, _ = os.Getwd()
	}

	featureSet := make(map[SectionFeature]bool, len(features))
	for _, f := range features {
		featureSet[f] = true
	}

	var sections []PromptSection
	totalChars := 0

	// addSection enforces per-file and total bootstrap limits.
	// Per-file limit: 20k chars (OpenClaw default). Total: 150k chars.
	addSection := func(name string, priority int, content string) {
		if content == "" {
			return
		}
		if len(content) > MaxFileChars {
			content = content[:MaxFileChars] + "\n[truncated at 20k chars]"
		}
		if totalChars+len(content) > MaxTotalChars {
			return // total budget exhausted
		}
		totalChars += len(content)
		sections = append(sections, PromptSection{
			Name:     name,
			Priority: priority,
			Content:  content,
		})
	}

	// [100] Runtime header
	if sectionIncluded("runtime", mode) {
		addSection("runtime", 100, buildRuntimeHeader(effectiveCWD))
	}

	// [95] SOUL.md
	if sectionIncluded("soul", mode) {
		addSection("soul", 95, workspace.Get(FileSoul))
	}

	// [92] Output style (opt-in via FeatureOutputStyle)
	if featureSet[FeatureOutputStyle] && sectionIncluded("output_style", mode) {
		addSection("output_style", 92, buildOutputStyleSection())
	}

	// [90] AGENTS.md
	if sectionIncluded("agents", mode) {
		addSection("agents", 90, workspace.Get(FileAgents))
	}

	// [89] Dynamic agent registry — lists all available agents from the registry
	if e.registry != nil && sectionIncluded("agents", mode) {
		agents := e.registry.All()
		if serialized := SerializeAgents(agents, agentID); serialized != "" {
			addSection("agents_registry", 89, serialized)
		}
	}

	// [88] Task guidance (opt-in via FeatureTaskGuidance)
	if featureSet[FeatureTaskGuidance] && sectionIncluded("task_guidance", mode) {
		addSection("task_guidance", 88, buildTaskGuidanceSection())
	}

	// [86] Action guidance (opt-in via FeatureActionGuidance)
	if featureSet[FeatureActionGuidance] && sectionIncluded("action_guidance", mode) {
		addSection("action_guidance", 86, buildActionGuidanceSection())
	}

	// [85] USER.md
	if sectionIncluded("user", mode) {
		addSection("user", 85, workspace.Get(FileUser))
	}

	// [80] Tool manifest
	if len(toolNames) > 0 && sectionIncluded("tools", mode) {
		addSection("tools", 80, "# Available Tools\n"+strings.Join(toolNames, ", "))
	}

	// [75] Instruction files (opt-in via FeatureInstructionFiles)
	if featureSet[FeatureInstructionFiles] && sectionIncluded("instruction_files", mode) {
		if files := DiscoverInstructionFiles(effectiveCWD); len(files) > 0 {
			addSection("instruction_files", 75, renderInstructionFiles(files))
		}
	}

	// [73] Project context (opt-in via FeatureProjectContext)
	if featureSet[FeatureProjectContext] && sectionIncluded("project_context", mode) {
		if ctx := buildProjectContext(effectiveCWD); ctx != "" {
			addSection("project_context", 73, ctx)
		}
	}

	// [70] TOOLS.md
	if sectionIncluded("tools_doc", mode) {
		addSection("tools_doc", 70, workspace.Get(FileTools))
	}

	// [65] Skills
	if e.skills != nil && sectionIncluded("skills", mode) {
		projectDir := ""
		if opts != nil {
			projectDir = opts.ProjectDir
		}
		skills := e.skills.LoadWithProject(agentID, skillsAllowlist, projectDir)
		if len(skills) > 0 {
			addSection("skills", 65, SerializeSkills(skills))
		}
	}

	// [60] MEMORY.md — full content, no arbitrary line limit
	if workspace.Memory != nil && workspace.Memory.Curated != "" && sectionIncluded("memory", mode) {
		addSection("memory", 60, "# Memory\n"+workspace.Memory.Curated)
	}

	// [55] Standing orders
	if e.workspaces != nil && sectionIncluded("standing_orders", mode) {
		if orders := e.workspaces.LoadStandingOrders(); orders != "" {
			addSection("standing_orders", 55, "# Standing Orders\n"+orders)
		}
	}

	// [50] Daily memory (today only)
	if workspace.Memory != nil && sectionIncluded("daily_memory", mode) {
		today := time.Now().Format("2006-01-02")
		if daily, ok := workspace.Memory.Daily[today]; ok {
			addSection("daily_memory", 50, "# Today's Notes ("+today+")\n"+daily)
		}
	}

	// [45] HEARTBEAT.md
	if sectionIncluded("heartbeat", mode) {
		addSection("heartbeat", 45, workspace.Get(FileHeartbeat))
	}

	// [40] Per-run overrides
	if overrides != "" && sectionIncluded("overrides", mode) {
		addSection("overrides", 40, overrides)
	}

	// Estimate tokens for each section
	for i := range sections {
		sections[i].Tokens = EstimateTokens(sections[i].Content)
	}

	// Build system prompt
	var parts []string
	var totalTokens int64
	for _, s := range sections {
		parts = append(parts, s.Content)
		totalTokens += s.Tokens
	}

	// Calculate budget
	reserved := contextWindow / 4
	if reserved > 32000 {
		reserved = 32000
	}
	available := contextWindow - totalTokens - reserved
	if available < 0 {
		available = 0
	}

	return &ContextPlan{
		SystemPrompt: strings.Join(parts, "\n\n---\n\n"),
		Sections:     sections,
		Budget: TokenBudget{
			ContextWindow:   contextWindow,
			SystemPromptEst: totalTokens,
			ReservedOutput:  reserved,
			Available:       available,
		},
	}
}

// buildTaskGuidanceSection returns Claw Code-style task handling directives.
func buildTaskGuidanceSection() string {
	return `# Doing Tasks
 - Read relevant code before changing it and keep changes tightly scoped to the request.
 - Do not add speculative abstractions, compatibility shims, or unrelated cleanup.
 - Do not create files unless they are required to complete the task.
 - If an approach fails, diagnose the failure before switching tactics.
 - Be careful not to introduce security vulnerabilities such as command injection, XSS, or SQL injection.
 - Report outcomes faithfully: if verification fails or was not run, say so explicitly.`
}

// buildActionGuidanceSection returns Claw Code-style action/reversibility guidance.
func buildActionGuidanceSection() string {
	return `# Executing Actions with Care
Carefully consider reversibility and blast radius. Local, reversible actions like editing files or running tests are usually fine. Actions that affect shared systems, publish state, delete data, or otherwise have high blast radius should be explicitly authorized by the user or durable workspace instructions.`
}

// buildOutputStyleSection returns output style directives.
func buildOutputStyleSection() string {
	return `# Output Style
 - Go straight to the point. Lead with the answer or action, not the reasoning.
 - Skip filler words, preamble, and unnecessary transitions.
 - Do not restate what was said. Do not summarize unless asked.
 - If you can say it in one sentence, don't use three.`
}

// buildProjectContext returns git project context (branch, status, diff, recent commits).
func buildProjectContext(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if we're in a git repo
	if _, err := runGitCmd(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return ""
	}

	var parts []string
	parts = append(parts, "# Project Context")

	if branch, err := runGitCmd(ctx, dir, "branch", "--show-current"); err == nil && branch != "" {
		parts = append(parts, "- Branch: "+branch)
	}

	// Full git status — no line limit
	if status, err := runGitCmd(ctx, dir, "--no-optional-locks", "status", "--short", "--branch"); err == nil && status != "" {
		parts = append(parts, "\nGit status:\n"+status)
	}

	// Recent commits (last 5)
	if log, err := runGitCmd(ctx, dir, "log", "--oneline", "-5"); err == nil && log != "" {
		parts = append(parts, "\nRecent commits (last 5):\n"+log)
	}

	// Full diff stat — no line limit
	if diff, err := runGitCmd(ctx, dir, "diff", "--stat"); err == nil && diff != "" {
		parts = append(parts, "\nUnstaged changes:\n"+diff)
	}

	if diff, err := runGitCmd(ctx, dir, "diff", "--cached", "--stat"); err == nil && diff != "" {
		parts = append(parts, "\nStaged changes:\n"+diff)
	}

	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// renderInstructionFiles formats discovered instruction files for the prompt.
func renderInstructionFiles(files []InstructionFile) string {
	var sb strings.Builder
	sb.WriteString("# Instruction Files\n")
	sb.WriteString(fmt.Sprintf("Discovered %d instruction file(s) from the project directory hierarchy.\n", len(files)))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("\n## %s\n%s\n", f.Path, f.Content))
	}
	return sb.String()
}

// CompactPrompt removes low-priority sections when the system prompt is too large.
func (e *ContextEngine) CompactPrompt(sections []PromptSection, maxTokens int64) string {
	// Sort by priority descending — keep highest priority
	// Drop lowest priority sections until under budget
	var kept []PromptSection
	var total int64

	// Copy and sort by priority (highest first)
	sorted := make([]PromptSection, len(sections))
	copy(sorted, sections)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, s := range sorted {
		if total+s.Tokens <= maxTokens {
			kept = append(kept, s)
			total += s.Tokens
		}
	}

	// Rebuild in original priority order
	var parts []string
	for _, s := range kept {
		parts = append(parts, s.Content)
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// CompactionResult holds the output of structured conversation compaction.
type CompactionResult struct {
	Summary          string            // formatted summary for continuation
	CompactedHistory []fantasy.Message // [continuation_message, ...preserved_tail]
	RemovedCount     int
}

// Compact performs structured compaction of conversation history.
// Reference: Claw Code rust/crates/runtime/src/compact.rs
//
// Algorithm:
// 1. Split messages into compactable range and preserved tail
// 2. Analyze compactable messages: count by role, extract tool names, collect user requests, timeline
// 3. Merge with prior summary if present
// 4. Build continuation message with formatted summary
func (e *ContextEngine) Compact(ctx context.Context, messages []fantasy.Message, preserveCount int) (*CompactionResult, error) {
	if len(messages) <= preserveCount {
		return nil, nil // nothing to compact
	}

	// Split: compactable (older) and preserved (tail)
	splitIdx := len(messages) - preserveCount
	older := messages[:splitIdx]
	tail := messages[splitIdx:]

	// Analyze the compactable messages
	analysis := analyzeMessages(older)

	// Check for prior summary in the first message
	var priorSummary string
	if len(older) > 0 {
		for _, part := range older[0].Content {
			if tp, ok := part.(fantasy.TextPart); ok && strings.HasPrefix(tp.Text, "[Session summary]") {
				priorSummary = tp.Text
				break
			}
		}
	}

	// Build structured summary
	var sb strings.Builder
	sb.WriteString("[Session summary]\n")

	if priorSummary != "" {
		sb.WriteString("\n## Previously Compacted\n")
		// Extract just the content after the header
		cleaned := strings.TrimPrefix(priorSummary, "[Session summary]\n")
		sb.WriteString(cleaned)
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Conversation Statistics\n")
	sb.WriteString(fmt.Sprintf("- Messages compacted: %d (user: %d, assistant: %d, tool: %d)\n",
		analysis.totalMessages, analysis.userCount, analysis.assistantCount, analysis.toolCount))

	if len(analysis.toolNames) > 0 {
		sb.WriteString(fmt.Sprintf("- Tools used: %s\n", strings.Join(analysis.toolNames, ", ")))
	}

	if len(analysis.userRequests) > 0 {
		sb.WriteString("\n## Recent User Requests\n")
		for _, req := range analysis.userRequests {
			sb.WriteString(fmt.Sprintf("- %s\n", req))
		}
	}

	if len(analysis.keyFiles) > 0 {
		sb.WriteString("\n## Key Files Referenced\n")
		for _, f := range analysis.keyFiles {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	if len(analysis.timeline) > 0 {
		sb.WriteString("\n## Timeline\n")
		for _, entry := range analysis.timeline {
			sb.WriteString(fmt.Sprintf("- %s\n", entry))
		}
	}

	if analysis.pendingWork != "" {
		sb.WriteString("\n## Pending/Current Work\n")
		sb.WriteString(analysis.pendingWork + "\n")
	}

	summary := sb.String()

	// Build continuation message
	continuationMsg := fantasy.Message{
		Role: fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: summary + "\n\nRecent messages are preserved verbatim. Continue the conversation from where it left off."},
		},
	}

	// Build compacted history: [continuation_message, ...preserved_tail]
	compacted := make([]fantasy.Message, 0, 1+len(tail))
	compacted = append(compacted, continuationMsg)
	compacted = append(compacted, tail...)

	return &CompactionResult{
		Summary:          summary,
		CompactedHistory: compacted,
		RemovedCount:     len(older),
	}, nil
}

// Summarize is a thin wrapper around Compact for backward compatibility.
func (e *ContextEngine) Summarize(ctx context.Context, messages []fantasy.Message) (string, error) {
	result, err := e.Compact(ctx, messages, 0)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Summary, nil
}

type messageAnalysis struct {
	totalMessages  int
	userCount      int
	assistantCount int
	toolCount      int
	toolNames      []string
	userRequests   []string // last 3 user requests
	keyFiles       []string // file paths mentioned
	timeline       []string // chronological summary entries
	pendingWork    string   // inferred from last assistant messages
}

// analyzeMessages extracts structured information from conversation messages.
func analyzeMessages(messages []fantasy.Message) messageAnalysis {
	var a messageAnalysis
	toolNameSet := make(map[string]bool)
	fileSet := make(map[string]bool)
	var allUserTexts []string

	for _, msg := range messages {
		a.totalMessages++

		switch msg.Role {
		case fantasy.MessageRoleUser:
			a.userCount++
			for _, part := range msg.Content {
				if tp, ok := part.(fantasy.TextPart); ok && tp.Text != "" {
					allUserTexts = append(allUserTexts, tp.Text)
					// Timeline entry
					preview := tp.Text
					if len(preview) > 120 {
						preview = preview[:120]
					}
					a.timeline = append(a.timeline, "[user] "+preview)
				}
			}

		case fantasy.MessageRoleAssistant:
			a.assistantCount++
			for _, part := range msg.Content {
				if tp, ok := part.(fantasy.TextPart); ok && tp.Text != "" {
					preview := tp.Text
					if len(preview) > 120 {
						preview = preview[:120]
					}
					a.timeline = append(a.timeline, "[assistant] "+preview)
				}
				if tc, ok := part.(fantasy.ToolCallPart); ok {
					toolNameSet[tc.ToolName] = true
					preview := tc.ToolName
					if tc.Input != "" && len(tc.Input) < 80 {
						preview += " " + tc.Input
					}
					a.timeline = append(a.timeline, "[tool_call] "+preview)
				}
			}

		case fantasy.MessageRoleTool:
			a.toolCount++
			for _, part := range msg.Content {
				if tr, ok := part.(fantasy.ToolResultPart); ok {
					output := toolResultOutputString(tr.Output)
					// Extract file paths from tool results
					extractFilePaths(output, fileSet)
				}
			}
		}
	}

	// Collect tool names
	for name := range toolNameSet {
		a.toolNames = append(a.toolNames, name)
	}

	// Last 3 user requests
	if len(allUserTexts) > 3 {
		allUserTexts = allUserTexts[len(allUserTexts)-3:]
	}
	a.userRequests = allUserTexts

	// Key files (max 20)
	count := 0
	for f := range fileSet {
		if count >= 20 {
			break
		}
		a.keyFiles = append(a.keyFiles, f)
		count++
	}

	// Infer pending work from last assistant text
	if a.assistantCount > 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == fantasy.MessageRoleAssistant {
				for _, part := range messages[i].Content {
					if tp, ok := part.(fantasy.TextPart); ok && tp.Text != "" {
						a.pendingWork = tp.Text
						if len(a.pendingWork) > 300 {
							a.pendingWork = a.pendingWork[:300]
						}
						break
					}
				}
				break
			}
		}
	}

	// Cap timeline at 30 entries
	if len(a.timeline) > 30 {
		a.timeline = a.timeline[len(a.timeline)-30:]
	}

	return a
}

// toolResultOutputString extracts text from a ToolResultOutputContent.
func toolResultOutputString(output fantasy.ToolResultOutputContent) string {
	if output == nil {
		return ""
	}
	switch c := output.(type) {
	case fantasy.ToolResultOutputContentText:
		return c.Text
	case fantasy.ToolResultOutputContentError:
		return fmt.Sprintf("Error: %v", c.Error)
	default:
		return ""
	}
}

// extractFilePaths finds file-like paths in text and adds them to the set.
func extractFilePaths(text string, paths map[string]bool) {
	for _, word := range strings.Fields(text) {
		// Simple heuristic: looks like a file path if it contains / and doesn't start with http
		if strings.Contains(word, "/") && !strings.HasPrefix(word, "http") && len(word) > 2 && len(word) < 200 {
			// Clean up trailing punctuation
			word = strings.TrimRight(word, ".,;:!?\"')")
			paths[word] = true
		}
	}
}

// EstimateTokens provides a rough token count (4 chars per token heuristic).
func EstimateTokens(text string) int64 {
	return int64(len(text)) / 4
}

// sectionIncluded returns true if the named section should appear in the given mode.
// Uses a whitelist for minimal mode — new sections are excluded by default.
func sectionIncluded(name string, mode PromptMode) bool {
	if mode == "" || mode == PromptModeFull {
		return true
	}
	switch name {
	case "runtime", "soul", "agents", "tools", "tools_doc", "skills", "standing_orders":
		return true
	default:
		return false
	}
}

func buildRuntimeHeader(cwd string) string {
	home, _ := os.UserHomeDir()
	header := fmt.Sprintf("# Runtime\n- Date: %s\n- OS: %s/%s\n- CWD: %s\n- Home: %s\n- Writes outside home directory will prompt for user approval",
		time.Now().Format("2006-01-02 15:04 MST"),
		runtime.GOOS, runtime.GOARCH,
		cwd, home,
	)

	// Git context — detect repo and add branch/status/diff info
	if git := buildGitContext(cwd); git != "" {
		header += "\n" + git
	}

	return header
}

// buildGitContext returns git repo info for the given directory, or empty string.
func buildGitContext(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Check if we're in a git repo
	if _, err := runGitCmd(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return ""
	}

	var parts []string

	if branch, err := runGitCmd(ctx, dir, "branch", "--show-current"); err == nil && branch != "" {
		parts = append(parts, "- Git branch: "+branch)
	}

	if status, err := runGitCmd(ctx, dir, "status", "--short"); err == nil && status != "" {
		parts = append(parts, "- Git status:\n"+indent(strings.TrimSpace(status), "  "))
	}

	if diff, err := runGitCmd(ctx, dir, "diff", "--stat"); err == nil && diff != "" {
		parts = append(parts, "- Git diff:\n"+indent(strings.TrimSpace(diff), "  "))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
