package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContextEngineAssemble(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md":   {Name: "SOUL.md", Content: "I am a test agent"},
			"AGENTS.md": {Name: "AGENTS.md", Content: "Follow these rules"},
			"USER.md":   {Name: "USER.md", Content: "User prefers Go"},
		},
		Memory: &Memory{
			Curated: "Remember: user likes terse code",
			Daily:   make(map[string]string),
		},
	}

	plan := engine.Assemble("test", ws, []string{"shell", "read", "write"}, 128000, "", PromptModeFull, nil, nil)

	if plan.SystemPrompt == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(plan.SystemPrompt, "I am a test agent") {
		t.Error("should contain SOUL.md content")
	}
	if !strings.Contains(plan.SystemPrompt, "Follow these rules") {
		t.Error("should contain AGENTS.md content")
	}
	if !strings.Contains(plan.SystemPrompt, "User prefers Go") {
		t.Error("should contain USER.md content")
	}
	if !strings.Contains(plan.SystemPrompt, "shell, read, write") {
		t.Error("should contain tool manifest")
	}
	if !strings.Contains(plan.SystemPrompt, "Remember: user likes terse code") {
		t.Error("should contain memory")
	}
}

func TestContextEngineAssembleEmpty(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "empty",
		Files:   make(map[string]WorkspaceFile),
		Memory:  &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("empty", ws, nil, 32000, "", PromptModeFull, nil, nil)

	if plan.SystemPrompt == "" {
		t.Error("should at least have runtime header")
	}
	if !strings.Contains(plan.SystemPrompt, "Runtime") {
		t.Error("should contain runtime header")
	}
}

func TestContextEngineSections(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md":   {Name: "SOUL.md", Content: "soul content"},
			"AGENTS.md": {Name: "AGENTS.md", Content: "agents content"},
		},
		Memory: &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, []string{"shell"}, 128000, "extra instructions", PromptModeFull, nil, nil)

	// Check sections exist with correct priorities
	found := make(map[string]int)
	for _, s := range plan.Sections {
		found[s.Name] = s.Priority
	}

	if found["runtime"] != 100 {
		t.Errorf("runtime priority = %d, want 100", found["runtime"])
	}
	if found["soul"] != 95 {
		t.Errorf("soul priority = %d, want 95", found["soul"])
	}
	if found["agents"] != 90 {
		t.Errorf("agents priority = %d, want 90", found["agents"])
	}
	if found["overrides"] != 40 {
		t.Errorf("overrides priority = %d, want 40", found["overrides"])
	}
}

func TestContextEngineTokenBudget(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "test",
		Files:   make(map[string]WorkspaceFile),
		Memory:  &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, nil, 100000, "", PromptModeFull, nil, nil)

	if plan.Budget.ContextWindow != 100000 {
		t.Errorf("ContextWindow = %d", plan.Budget.ContextWindow)
	}
	if plan.Budget.ReservedOutput <= 0 {
		t.Error("ReservedOutput should be positive")
	}
	if plan.Budget.Available <= 0 {
		t.Error("Available should be positive")
	}
	if plan.Budget.SystemPromptEst <= 0 {
		t.Error("SystemPromptEst should be positive")
	}
}

func TestContextEngineMemoryFullContent(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	// Create memory with 300 lines — all should be included (no truncation)
	var lines []string
	for i := range 300 {
		lines = append(lines, fmt.Sprintf("memory line %d", i))
	}

	ws := &Workspace{
		AgentID: "test",
		Files:   make(map[string]WorkspaceFile),
		Memory: &Memory{
			Curated: strings.Join(lines, "\n"),
			Daily:   make(map[string]string),
		},
	}

	plan := engine.Assemble("test", ws, nil, 128000, "", PromptModeFull, nil, nil)

	// Memory should contain ALL lines — no arbitrary truncation
	if strings.Contains(plan.SystemPrompt, "truncated") {
		t.Error("memory should not be truncated — no arbitrary line limits")
	}
	if !strings.Contains(plan.SystemPrompt, "memory line 299") {
		t.Error("memory should contain all lines including line 299")
	}
}

func TestContextEngineCompactPrompt(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	sections := []PromptSection{
		{Name: "runtime", Priority: 100, Content: "runtime", Tokens: 10},
		{Name: "soul", Priority: 95, Content: "soul", Tokens: 10},
		{Name: "memory", Priority: 60, Content: "memory", Tokens: 10},
		{Name: "daily", Priority: 50, Content: "daily", Tokens: 10},
		{Name: "overrides", Priority: 40, Content: "overrides", Tokens: 10},
	}

	// Budget for 30 tokens — should keep runtime + soul + memory, drop daily + overrides
	result := engine.CompactPrompt(sections, 30)

	if !strings.Contains(result, "runtime") {
		t.Error("should keep runtime (priority 100)")
	}
	if !strings.Contains(result, "soul") {
		t.Error("should keep soul (priority 95)")
	}
	if !strings.Contains(result, "memory") {
		t.Error("should keep memory (priority 60)")
	}
	if strings.Contains(result, "overrides") {
		t.Error("should drop overrides (priority 40)")
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens("hello world"); got != 2 {
		t.Errorf("EstimateTokens('hello world') = %d, want ~2", got)
	}
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("EstimateTokens('') = %d, want 0", got)
	}
}

func TestTokenBudgetShouldCompact(t *testing.T) {
	b := TokenBudget{Available: 1000, HistoryEst: 500}
	if b.ShouldCompact() {
		t.Error("500 < 1000, should not compact")
	}

	b.HistoryEst = 1500
	if !b.ShouldCompact() {
		t.Error("1500 > 1000, should compact")
	}
}

// --- PromptMode tests ---

func TestSectionIncluded(t *testing.T) {
	tests := []struct {
		name string
		mode PromptMode
		want bool
	}{
		// Full mode includes everything
		{"runtime", PromptModeFull, true},
		{"soul", PromptModeFull, true},
		{"agents", PromptModeFull, true},
		{"user", PromptModeFull, true},
		{"tools", PromptModeFull, true},
		{"tools_doc", PromptModeFull, true},
		{"skills", PromptModeFull, true},
		{"memory", PromptModeFull, true},
		{"daily_memory", PromptModeFull, true},
		{"standing_orders", PromptModeFull, true},
		{"heartbeat", PromptModeFull, true},
		{"overrides", PromptModeFull, true},

		// Empty mode behaves as full
		{"user", "", true},
		{"memory", "", true},

		// Minimal mode whitelist
		{"runtime", PromptModeMinimal, true},
		{"soul", PromptModeMinimal, true},
		{"agents", PromptModeMinimal, true},
		{"tools", PromptModeMinimal, true},
		{"tools_doc", PromptModeMinimal, true},
		{"skills", PromptModeMinimal, true},
		{"standing_orders", PromptModeMinimal, true},

		// Minimal mode excludes
		{"user", PromptModeMinimal, false},
		{"memory", PromptModeMinimal, false},
		{"daily_memory", PromptModeMinimal, false},
		{"heartbeat", PromptModeMinimal, false},
		{"overrides", PromptModeMinimal, false},
	}

	for _, tt := range tests {
		got := sectionIncluded(tt.name, tt.mode)
		if got != tt.want {
			t.Errorf("sectionIncluded(%q, %q) = %v, want %v", tt.name, tt.mode, got, tt.want)
		}
	}
}

func TestAssembleMinimalMode(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md":   {Name: "SOUL.md", Content: "I am a test agent"},
			"AGENTS.md": {Name: "AGENTS.md", Content: "Follow these rules"},
			"USER.md":   {Name: "USER.md", Content: "User prefers Go"},
			"TOOLS.md":  {Name: "TOOLS.md", Content: "Tool docs here"},
		},
		Memory: &Memory{
			Curated: "Remember: user likes terse code",
			Daily:   map[string]string{time.Now().Format("2006-01-02"): "did stuff today"},
		},
	}

	plan := engine.Assemble("test", ws, []string{"shell", "read"}, 128000, "extra overrides", PromptModeMinimal, nil, nil)

	// Should include
	if !strings.Contains(plan.SystemPrompt, "Runtime") {
		t.Error("minimal should include runtime")
	}
	if !strings.Contains(plan.SystemPrompt, "I am a test agent") {
		t.Error("minimal should include SOUL.md")
	}
	if !strings.Contains(plan.SystemPrompt, "Follow these rules") {
		t.Error("minimal should include AGENTS.md")
	}
	if !strings.Contains(plan.SystemPrompt, "shell, read") {
		t.Error("minimal should include tool manifest")
	}
	if !strings.Contains(plan.SystemPrompt, "Tool docs here") {
		t.Error("minimal should include TOOLS.md")
	}

	// Should exclude
	if strings.Contains(plan.SystemPrompt, "User prefers Go") {
		t.Error("minimal should NOT include USER.md")
	}
	if strings.Contains(plan.SystemPrompt, "user likes terse code") {
		t.Error("minimal should NOT include MEMORY.md")
	}
	if strings.Contains(plan.SystemPrompt, "did stuff today") {
		t.Error("minimal should NOT include daily memory")
	}
	if strings.Contains(plan.SystemPrompt, "extra overrides") {
		t.Error("minimal should NOT include overrides")
	}
}

func TestAssembleNoneMode(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md": {Name: "SOUL.md", Content: "big soul content"},
		},
		Memory: &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, []string{"shell"}, 128000, "", PromptModeNone, nil, nil)

	if !strings.Contains(plan.SystemPrompt, "Claire") {
		t.Error("none mode should contain Claire identity string")
	}
	if len(plan.Sections) != 0 {
		t.Errorf("none mode should have 0 sections, got %d", len(plan.Sections))
	}
	if strings.Contains(plan.SystemPrompt, "big soul content") {
		t.Error("none mode should not include workspace content")
	}
	if plan.Budget.ContextWindow != 128000 {
		t.Errorf("none mode ContextWindow = %d, want 128000", plan.Budget.ContextWindow)
	}
}

func TestAssembleDefaultMode(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"USER.md": {Name: "USER.md", Content: "User info here"},
		},
		Memory: &Memory{Daily: make(map[string]string)},
	}

	// Empty string should behave like full
	plan := engine.Assemble("test", ws, nil, 128000, "", "", nil, nil)

	if !strings.Contains(plan.SystemPrompt, "User info here") {
		t.Error("empty mode should include USER.md (full behavior)")
	}
}

func TestMinimalModeSectionNames(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md":   {Name: "SOUL.md", Content: "soul"},
			"AGENTS.md": {Name: "AGENTS.md", Content: "agents"},
			"USER.md":   {Name: "USER.md", Content: "user"},
			"TOOLS.md":  {Name: "TOOLS.md", Content: "tools doc"},
		},
		Memory: &Memory{
			Curated: "memory content",
			Daily:   map[string]string{time.Now().Format("2006-01-02"): "daily"},
		},
	}

	plan := engine.Assemble("test", ws, []string{"shell"}, 128000, "overrides", PromptModeMinimal, nil, nil)

	names := make(map[string]bool)
	for _, s := range plan.Sections {
		names[s.Name] = true
	}

	for _, want := range []string{"runtime", "soul", "agents", "tools", "tools_doc"} {
		if !names[want] {
			t.Errorf("minimal should include section %q", want)
		}
	}
	for _, deny := range []string{"user", "memory", "daily_memory", "heartbeat", "overrides"} {
		if names[deny] {
			t.Errorf("minimal should NOT include section %q", deny)
		}
	}
}

// --- Phase B: Bootstrap limits, standing orders, heartbeat, git context ---

func TestBootstrapFileTruncation(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	// Create a SOUL.md exceeding 20k chars
	bigContent := strings.Repeat("x", MaxFileChars+500)
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md": {Name: "SOUL.md", Content: bigContent},
		},
		Memory: &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, nil, 128000, "", PromptModeFull, nil, nil)

	if !strings.Contains(plan.SystemPrompt, "[truncated at 20k chars]") {
		t.Error("should truncate SOUL.md at 20k chars")
	}

	// Find the soul section and verify length
	for _, s := range plan.Sections {
		if s.Name == "soul" {
			if len(s.Content) > MaxFileChars+100 {
				t.Errorf("soul section too large: %d chars (max %d + marker)", len(s.Content), MaxFileChars)
			}
			return
		}
	}
	t.Error("soul section not found")
}

func TestBootstrapTotalLimit(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	// Create multiple files that together exceed 150k
	bigContent := strings.Repeat("y", MaxFileChars) // 20k each
	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"SOUL.md":   {Name: "SOUL.md", Content: bigContent},
			"AGENTS.md": {Name: "AGENTS.md", Content: bigContent},
			"USER.md":   {Name: "USER.md", Content: bigContent},
			"TOOLS.md":  {Name: "TOOLS.md", Content: bigContent},
		},
		Memory: &Memory{
			Curated: strings.Repeat("z", MaxFileChars),
			Daily: map[string]string{
				time.Now().Format("2006-01-02"): strings.Repeat("w", MaxFileChars),
			},
		},
	}

	plan := engine.Assemble("test", ws, []string{"shell"}, 128000, strings.Repeat("v", MaxFileChars), PromptModeFull, nil, nil)

	// Count total chars across sections
	totalChars := 0
	for _, s := range plan.Sections {
		totalChars += len(s.Content)
	}

	if totalChars > MaxTotalChars+1000 { // small margin for markers
		t.Errorf("total chars %d exceeds limit %d", totalChars, MaxTotalChars)
	}
}

func TestStandingOrders(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	agentsDir := filepath.Join(dir, "agents")
	ordersDir := filepath.Join(dir, "standing_orders")
	os.MkdirAll(wsDir, 0o755)
	os.MkdirAll(agentsDir, 0o755)
	os.MkdirAll(ordersDir, 0o755)

	// Write standing order files
	os.WriteFile(filepath.Join(ordersDir, "always-go.md"), []byte("Always use Go 1.26"), 0o644)
	os.WriteFile(filepath.Join(ordersDir, "no-mocks.md"), []byte("Never mock the database"), 0o644)
	os.WriteFile(filepath.Join(ordersDir, "ignored.txt"), []byte("not a markdown file"), 0o644)

	loader := NewWorkspaceLoader(wsDir, agentsDir, "")
	engine := NewContextEngine(nil, loader, nil)

	ws := &Workspace{
		AgentID: "test",
		Files:   make(map[string]WorkspaceFile),
		Memory:  &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, nil, 128000, "", PromptModeFull, nil, nil)

	if !strings.Contains(plan.SystemPrompt, "Always use Go 1.26") {
		t.Error("should contain first standing order")
	}
	if !strings.Contains(plan.SystemPrompt, "Never mock the database") {
		t.Error("should contain second standing order")
	}
	if strings.Contains(plan.SystemPrompt, "not a markdown file") {
		t.Error("should not include non-md files")
	}

	// Verify section exists with correct priority
	for _, s := range plan.Sections {
		if s.Name == "standing_orders" {
			if s.Priority != 55 {
				t.Errorf("standing_orders priority = %d, want 55", s.Priority)
			}
			return
		}
	}
	t.Error("standing_orders section not found")
}

func TestStandingOrdersInMinimalMode(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	agentsDir := filepath.Join(dir, "agents")
	ordersDir := filepath.Join(dir, "standing_orders")
	os.MkdirAll(wsDir, 0o755)
	os.MkdirAll(agentsDir, 0o755)
	os.MkdirAll(ordersDir, 0o755)

	os.WriteFile(filepath.Join(ordersDir, "rule.md"), []byte("Persistent rule"), 0o644)

	loader := NewWorkspaceLoader(wsDir, agentsDir, "")
	engine := NewContextEngine(nil, loader, nil)

	ws := &Workspace{
		AgentID: "test",
		Files:   make(map[string]WorkspaceFile),
		Memory:  &Memory{Daily: make(map[string]string)},
	}

	plan := engine.Assemble("test", ws, nil, 128000, "", PromptModeMinimal, nil, nil)

	if !strings.Contains(plan.SystemPrompt, "Persistent rule") {
		t.Error("standing orders should be included in minimal mode")
	}
}

func TestHeartbeatSection(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	ws := &Workspace{
		AgentID: "test",
		Files: map[string]WorkspaceFile{
			"HEARTBEAT.md": {Name: "HEARTBEAT.md", Content: "Check system health every 30 minutes"},
		},
		Memory: &Memory{Daily: make(map[string]string)},
	}

	// Full mode should include heartbeat
	plan := engine.Assemble("test", ws, nil, 128000, "", PromptModeFull, nil, nil)
	if !strings.Contains(plan.SystemPrompt, "Check system health") {
		t.Error("full mode should include HEARTBEAT.md")
	}

	// Check priority
	for _, s := range plan.Sections {
		if s.Name == "heartbeat" {
			if s.Priority != 45 {
				t.Errorf("heartbeat priority = %d, want 45", s.Priority)
			}
			break
		}
	}

	// Minimal mode should exclude heartbeat
	plan = engine.Assemble("test", ws, nil, 128000, "", PromptModeMinimal, nil, nil)
	if strings.Contains(plan.SystemPrompt, "Check system health") {
		t.Error("minimal mode should NOT include HEARTBEAT.md")
	}
}

func TestGitContextInNonRepo(t *testing.T) {
	// A temp dir is not a git repo
	dir := t.TempDir()
	result := buildGitContext(dir)
	if result != "" {
		t.Errorf("non-git dir should return empty, got: %s", result)
	}
}

func TestLoadStandingOrdersEmpty(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspace")
	agentsDir := filepath.Join(dir, "agents")
	os.MkdirAll(wsDir, 0o755)

	loader := NewWorkspaceLoader(wsDir, agentsDir, "")
	result := loader.LoadStandingOrders()
	if result != "" {
		t.Errorf("no standing_orders dir should return empty, got: %s", result)
	}
}
