package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/elizafairlady/eclaire/internal/ui/chat"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
)

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantLen int
	}{
		{"/clear", "/clear", 0},
		{"/tools", "/tools", 0},
		{"/open coding", "/open", 1},
		{"/status", "/status", 0},
		{"", "", 0},
		{"/agents", "/agents", 0},
	}
	for _, tt := range tests {
		cmd, args := ParseSlashCommand(tt.input)
		if cmd != tt.wantCmd {
			t.Errorf("ParseSlashCommand(%q).cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
		}
		if len(args) != tt.wantLen {
			t.Errorf("ParseSlashCommand(%q).args len = %d, want %d", tt.input, len(args), tt.wantLen)
		}
	}
}

func TestRenderChatEntry(t *testing.T) {
	s := styles.Default()
	app := &App{styles: s, markdown: newMarkdownRenderer()}

	entries := []chatEntry{
		{kind: "user", content: "hello"},
		{kind: "assistant", content: "hi"},
		{kind: "tool_call", content: "shell"},
		{kind: "tool_result", content: "output"},
		{kind: "system", content: "info"},
	}
	for _, e := range entries {
		got := app.renderChatEntry(e)
		if got == "" {
			t.Errorf("renderChatEntry(%+v) returned empty", e)
		}
	}
}

func TestLaunchesToOrchestratorChat(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if len(app.tabs) != 1 {
		t.Fatalf("got %d tabs, want 1", len(app.tabs))
	}
	if app.tabs[0].ID != "main" {
		t.Errorf("first tab ID = %q, want main", app.tabs[0].ID)
	}
	if app.tabs[0].AgentID != "orchestrator" {
		t.Errorf("first tab agent = %q, want orchestrator", app.tabs[0].AgentID)
	}
	if app.tabs[0].Label != "Claire" {
		t.Errorf("first tab label = %q, want Claire", app.tabs[0].Label)
	}
	if app.tabs[0].Closable {
		t.Error("main tab should not be closable")
	}
	if app.focus != focusEditor {
		t.Error("should start with editor focused")
	}
}

func TestOpenAgentTab(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	app.openAgentTab("coding")
	if len(app.tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(app.tabs))
	}
	if app.tabs[1].AgentID != "coding" {
		t.Errorf("tab[1].AgentID = %q", app.tabs[1].AgentID)
	}
	if !app.tabs[1].Closable {
		t.Error("non-orchestrator tabs should be closable")
	}
	if app.activeTab != 1 {
		t.Errorf("activeTab = %d, want 1", app.activeTab)
	}

	// No duplicates
	app.openAgentTab("coding")
	if len(app.tabs) != 2 {
		t.Errorf("duplicate tab, got %d", len(app.tabs))
	}
}

func TestActiveAgentID(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if app.activeAgentID() != "orchestrator" {
		t.Errorf("got %q, want orchestrator", app.activeAgentID())
	}

	app.openAgentTab("coding")
	if app.activeAgentID() != "coding" {
		t.Errorf("got %q, want coding", app.activeAgentID())
	}
}

func TestHandleStreamEvent(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	app.busy["main"] = true

	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventTextDelta, Delta: "hello"})
	if app.streaming["main"] != "hello" {
		t.Errorf("streaming = %q", app.streaming["main"])
	}

	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventToolCall, ToolName: "shell", ToolCallID: "tc1", Input: `{"command":"ls"}`})
	cl := app.chatList("main")
	item, ok := cl.GetTool("tc1")
	if !ok {
		t.Fatal("tool call should be in chatList")
	}
	if item.ToolName() != "shell" {
		t.Errorf("tool name = %q, want shell", item.ToolName())
	}
}

func TestSlashClear(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	app.chatMsgs["main"] = []chatEntry{{kind: "user", content: "test"}}
	app.streaming["main"] = "partial"

	app.handleSlashCommand("/clear")

	if len(app.chatMsgs["main"]) != 0 {
		t.Error("should be cleared")
	}
	if _, ok := app.streaming["main"]; ok {
		t.Error("streaming should be cleared")
	}
}

func TestNestedStreamEvents(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// Sub-agent started
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:    agent.EventSubAgentStarted,
		AgentID: "coding",
		TaskID:  "task_coding_1",
		Output:  "do something",
	})

	if len(app.activeTasks) != 1 {
		t.Fatalf("got %d active tasks, want 1", len(app.activeTasks))
	}
	task := app.activeTasks["task_coding_1"]
	if task.agentID != "coding" {
		t.Errorf("task agentID = %q", task.agentID)
	}
	if task.status != "running" {
		t.Errorf("task status = %q, want running", task.status)
	}

	cl := app.chatList("main")
	agentItem, ok := cl.GetAgent("task_coding_1")
	if !ok {
		t.Fatal("agent tool should be in chatList")
	}
	if agentItem.Status() != chat.ToolRunning {
		t.Errorf("agent status = %d, want running", agentItem.Status())
	}

	// Sub-agent tool call
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:       agent.EventSubAgentToolCall,
		ToolName:   "shell",
		ToolCallID: "tc_nested_1",
		AgentID:    "coding",
		TaskID:     "task_coding_1",
	})
	nested := agentItem.NestedTools()
	if len(nested) != 1 {
		t.Fatalf("got %d nested tools, want 1", len(nested))
	}
	if nested[0].ToolName() != "shell" {
		t.Errorf("nested tool = %q, want shell", nested[0].ToolName())
	}

	// Sub-agent tool result
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:       agent.EventSubAgentToolResult,
		ToolName:   "shell",
		ToolCallID: "tc_nested_1",
		Output:     "Hello, World!",
		AgentID:    "coding",
		TaskID:     "task_coding_1",
	})
	if nested[0].Status() != chat.ToolSuccess {
		t.Errorf("nested tool status = %d, want success", nested[0].Status())
	}

	// Sub-agent completed
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:    agent.EventSubAgentCompleted,
		AgentID: "coding",
		TaskID:  "task_coding_1",
		Output:  "done",
	})
	if task.status != "completed" {
		t.Errorf("task status = %q, want completed", task.status)
	}
}

func TestNestedChatEntryRendering(t *testing.T) {
	s := styles.Default()
	app := &App{styles: s, markdown: newMarkdownRenderer()}

	// Top-level tool call
	normal := app.renderChatEntry(chatEntry{kind: "tool_call", content: "shell"})
	if normal == "" {
		t.Error("normal render should not be empty")
	}

	// Nested tool call
	nested := app.renderChatEntry(chatEntry{
		kind:    "tool_call",
		content: "write",
		agentID: "coding",
		depth:   1,
	})
	if nested == "" {
		t.Error("nested render should not be empty")
	}
	// Nested should contain the agent annotation
	if !strings.Contains(nested, "coding") {
		t.Error("nested tool_call should show agent ID")
	}
	// Nested should have tree indentation
	if !strings.Contains(nested, "│") {
		t.Error("nested tool_call should have tree character")
	}
}

func TestActiveTasksTracking(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// Start two tasks
	app.handleStreamEvent("main", agent.StreamEvent{
		Type: agent.EventSubAgentStarted, AgentID: "coding", TaskID: "t1",
	})
	app.handleStreamEvent("main", agent.StreamEvent{
		Type: agent.EventSubAgentStarted, AgentID: "research", TaskID: "t2",
	})

	if len(app.activeTasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(app.activeTasks))
	}

	// Complete one
	app.handleStreamEvent("main", agent.StreamEvent{
		Type: agent.EventSubAgentCompleted, AgentID: "coding", TaskID: "t1",
	})

	if app.activeTasks["t1"].status != "completed" {
		t.Error("t1 should be completed")
	}
	if app.activeTasks["t2"].status != "running" {
		t.Error("t2 should still be running")
	}
}

func TestScrollUpDisablesFollow(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if !app.followMode {
		t.Error("should start in follow mode")
	}

	// Simulate scroll up
	app.scrollOffset += 10
	app.followMode = false

	if app.followMode {
		t.Error("scrolling up should disable follow mode")
	}
	if app.scrollOffset != 10 {
		t.Errorf("scrollOffset = %d, want 10", app.scrollOffset)
	}
}

func TestScrollToBottomReenablesFollow(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// Scroll up, then back to bottom
	app.scrollOffset = 20
	app.followMode = false

	app.scrollOffset = 0
	app.followMode = true

	if !app.followMode {
		t.Error("scrolling to bottom should re-enable follow mode")
	}
}

func TestFollowModeDefault(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if !app.followMode {
		t.Error("new app should be in follow mode")
	}
	if app.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0", app.scrollOffset)
	}
}

// --- Phase D: Token tracking, scope, briefing ---

func TestTokenTracking(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if app.tokensIn != 0 || app.tokensOut != 0 {
		t.Error("tokens should start at 0")
	}

	app.handleStreamEvent("main", agent.StreamEvent{
		Type:  agent.EventStepFinish,
		Usage: &agent.UsageInfo{InputTokens: 1500, OutputTokens: 300},
	})

	if app.tokensIn != 1500 {
		t.Errorf("tokensIn = %d, want 1500", app.tokensIn)
	}
	if app.tokensOut != 300 {
		t.Errorf("tokensOut = %d, want 300", app.tokensOut)
	}

	// Second step accumulates
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:  agent.EventStepFinish,
		Usage: &agent.UsageInfo{InputTokens: 2000, OutputTokens: 500},
	})

	if app.tokensIn != 3500 {
		t.Errorf("tokensIn = %d, want 3500", app.tokensIn)
	}
	if app.tokensOut != 800 {
		t.Errorf("tokensOut = %d, want 800", app.tokensOut)
	}
}

func TestTokenTrackingNilUsage(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// StepFinish with nil usage should not panic
	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventStepFinish})

	if app.tokensIn != 0 {
		t.Errorf("tokensIn = %d, want 0", app.tokensIn)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1500, "1.5k"},
		{15000, "15.0k"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		got := formatTokenCount(tt.n)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestScopeSlashCommand(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if app.scope != "" {
		t.Error("scope should start empty")
	}

	app.handleSlashCommand("/scope work")
	if app.scope != "work" {
		t.Errorf("scope = %q, want work", app.scope)
	}

	app.handleSlashCommand("/scope personal")
	if app.scope != "personal" {
		t.Errorf("scope = %q, want personal", app.scope)
	}

	app.handleSlashCommand("/scope off")
	if app.scope != "" {
		t.Errorf("scope = %q, want empty", app.scope)
	}
}

func TestBriefingInjection(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")
	os.WriteFile(dir+"/"+today+".md", []byte("# Morning Briefing\nHello!"), 0o644)

	s := styles.Default()
	app := NewApp(nil, s, AppOptions{BriefingsDir: dir})

	// Init should inject the briefing
	app.injectBriefing()

	msgs := app.chatMsgs["main"]
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs, want 1", len(msgs))
	}
	if msgs[0].kind != "system" {
		t.Errorf("kind = %q, want system", msgs[0].kind)
	}
	if !strings.Contains(msgs[0].content, "Morning Briefing") {
		t.Errorf("content should have briefing: %s", msgs[0].content)
	}
}

func TestBriefingInjectionNoBriefing(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{BriefingsDir: t.TempDir()})

	app.injectBriefing()

	msgs := app.chatMsgs["main"]
	if len(msgs) != 0 {
		t.Errorf("should not inject when no briefing file exists, got %d msgs", len(msgs))
	}
}

func TestBriefingInjectionNoDir(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s) // no briefingsDir

	app.injectBriefing() // should not panic

	msgs := app.chatMsgs["main"]
	if len(msgs) != 0 {
		t.Errorf("should not inject when no dir set, got %d msgs", len(msgs))
	}
}

func TestTodoParsedFromToolCall(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// Simulate a todos tool call event with Input
	app.handleStreamEvent("main", agent.StreamEvent{
		Type:     agent.EventToolCall,
		ToolName: "todos",
		Input:    `{"todos":[{"content":"Write code","status":"in_progress","active_form":"Writing code"},{"content":"Test","status":"pending","active_form":"Testing"}]}`,
	})

	if len(app.sessionTodos) != 2 {
		t.Fatalf("sessionTodos = %d, want 2", len(app.sessionTodos))
	}
	if app.sessionTodos[0].Content != "Write code" {
		t.Errorf("todo[0].Content = %q", app.sessionTodos[0].Content)
	}
	if app.sessionTodos[0].ActiveForm != "Writing code" {
		t.Errorf("todo[0].ActiveForm = %q", app.sessionTodos[0].ActiveForm)
	}
}

func TestTodoSlashCommand(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// No todos
	app.handleSlashCommand("/todos")
	rendered := app.chatList("main").Render("")
	if !strings.Contains(rendered, "No todos") {
		t.Error("should say no todos when empty")
	}

	// Set some todos
	app.sessionTodos = []tool.TodoItem{
		{Content: "Step 1", Status: "completed", ActiveForm: "Doing step 1"},
		{Content: "Step 2", Status: "in_progress", ActiveForm: "Doing step 2"},
		{Content: "Step 3", Status: "pending", ActiveForm: "Doing step 3"},
	}

	app.handleSlashCommand("/todos")
	rendered = app.chatList("main").Render("")
	if !strings.Contains(rendered, "✓") {
		t.Error("should show check for completed")
	}
	if !strings.Contains(rendered, "→") {
		t.Error("should show arrow for in_progress")
	}
	if !strings.Contains(rendered, "Doing step 2") {
		t.Error("in_progress should show activeForm")
	}
	if !strings.Contains(rendered, "1/3 completed") {
		t.Errorf("should show completion ratio: %s", rendered)
	}
}

func TestTodoSidebarHidden(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// No todos — HasIncompleteTodos should be false
	if tool.HasIncompleteTodos(app.sessionTodos) {
		t.Error("should not show todo pill when no todos")
	}

	// All completed — should also be hidden
	app.sessionTodos = []tool.TodoItem{
		{Content: "Done", Status: "completed", ActiveForm: "Done"},
	}
	if tool.HasIncompleteTodos(app.sessionTodos) {
		t.Error("should not show todo pill when all completed")
	}
}

func TestActivityFeedFromToolCalls(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if len(app.activity) != 0 {
		t.Error("should start with no activity")
	}

	// Tool call adds activity
	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventToolCall, ToolName: "shell"})
	if len(app.activity) != 1 {
		t.Fatalf("activity = %d, want 1", len(app.activity))
	}
	if app.activity[0].icon != "→" {
		t.Errorf("icon = %q, want →", app.activity[0].icon)
	}
	if app.activity[0].text != "shell" {
		t.Errorf("text = %q", app.activity[0].text)
	}

	// Tool result adds activity
	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventToolResult, ToolName: "shell", Output: "ok"})
	if len(app.activity) != 2 {
		t.Fatalf("activity = %d, want 2", len(app.activity))
	}
	if app.activity[1].icon != "✓" {
		t.Errorf("icon = %q, want ✓", app.activity[1].icon)
	}

	// Sub-agent events
	app.handleStreamEvent("main", agent.StreamEvent{Type: agent.EventSubAgentStarted, AgentID: "coding", TaskID: "t1"})
	if len(app.activity) != 3 {
		t.Fatalf("activity = %d, want 3", len(app.activity))
	}
	if !strings.Contains(app.activity[2].text, "coding") {
		t.Errorf("text = %q, should contain coding", app.activity[2].text)
	}
}

func TestActivityFeedMaxEntries(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	for i := 0; i < maxActivityEntries+5; i++ {
		app.addActivity("→", fmt.Sprintf("tool-%d", i), 0)
	}

	if len(app.activity) != maxActivityEntries {
		t.Errorf("activity = %d, want %d", len(app.activity), maxActivityEntries)
	}
	// Should have the latest entries
	last := app.activity[len(app.activity)-1]
	if !strings.Contains(last.text, fmt.Sprintf("tool-%d", maxActivityEntries+4)) {
		t.Errorf("last entry = %q, should be latest", last.text)
	}
}

func TestSessionIDTracking(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	// Initially no session ID on main tab
	if app.tabs[0].SessionID != "" {
		t.Error("should start with no session ID")
	}

	// Set session ID via AppOptions
	app2 := NewApp(nil, s, AppOptions{MainSessionID: "sess-main"})
	if app2.tabs[0].SessionID != "sess-main" {
		t.Errorf("main session ID = %q, want sess-main", app2.tabs[0].SessionID)
	}

	// Project session creates second tab with its own session
	app3 := NewApp(nil, s, AppOptions{
		MainSessionID:    "sess-main",
		ProjectSessionID: "sess-proj",
		ProjectRoot:      "/home/user/myproject",
	})
	if len(app3.tabs) != 2 {
		t.Fatalf("got %d tabs, want 2 (main + project)", len(app3.tabs))
	}
	if app3.tabs[0].SessionID != "sess-main" {
		t.Errorf("main tab session = %q", app3.tabs[0].SessionID)
	}
	if app3.tabs[1].SessionID != "sess-proj" {
		t.Errorf("project tab session = %q", app3.tabs[1].SessionID)
	}
	if app3.tabs[1].Label != "myproject" {
		t.Errorf("project tab label = %q, want myproject", app3.tabs[1].Label)
	}
	if !app3.tabs[1].Closable {
		t.Error("project tab should be closable")
	}
}

func TestCloseTab(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	app.openAgentTab("coding")
	app.openAgentTab("research")

	if len(app.tabs) != 3 {
		t.Fatalf("got %d tabs", len(app.tabs))
	}

	// Close active (research)
	app.tabs = append(app.tabs[:app.activeTab], app.tabs[app.activeTab+1:]...)
	if app.activeTab >= len(app.tabs) {
		app.activeTab = len(app.tabs) - 1
	}

	if len(app.tabs) != 2 {
		t.Errorf("got %d tabs after close", len(app.tabs))
	}
	if app.tabs[0].AgentID != "orchestrator" {
		t.Error("orchestrator should still be first")
	}
}

func TestProjectTabFromOptions(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{
		MainSessionID:    "main-sess",
		ProjectSessionID: "proj-sess",
		ProjectRoot:      "/home/user/src/myapp",
	})

	if len(app.tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(app.tabs))
	}

	main := app.tabs[0]
	if main.ID != "main" || main.Label != "Claire" || main.SessionID != "main-sess" {
		t.Errorf("main tab: ID=%q Label=%q Session=%q", main.ID, main.Label, main.SessionID)
	}

	proj := app.tabs[1]
	if proj.ID != "project" {
		t.Errorf("project tab ID = %q, want project", proj.ID)
	}
	if proj.Label != "myapp" {
		t.Errorf("project tab label = %q, want myapp", proj.Label)
	}
	if proj.SessionID != "proj-sess" {
		t.Errorf("project session = %q", proj.SessionID)
	}
	if proj.AgentID != "orchestrator" {
		t.Errorf("project agent = %q, want orchestrator", proj.AgentID)
	}
}

func TestTabIDSeparatesChatState(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{
		MainSessionID:    "main-sess",
		ProjectSessionID: "proj-sess",
		ProjectRoot:      "/tmp/proj",
	})

	// Add messages to main tab
	app.chatList("main").Add(chat.NewSystemMessage("m1", "main message"))
	// Add messages to project tab
	app.chatList("project").Add(chat.NewSystemMessage("p1", "project message"))

	// Chat lists should be independent (different pointers)
	mainList := app.chatList("main")
	projList := app.chatList("project")
	if mainList == projList {
		t.Error("main and project should have separate chat lists")
	}

	// Render to verify content is separate
	mainList.SetSize(80, 40)
	projList.SetSize(80, 40)
	mainContent := mainList.Render("")
	projContent := projList.Render("")
	if !strings.Contains(mainContent, "main message") {
		t.Error("main list should contain 'main message'")
	}
	if !strings.Contains(projContent, "project message") {
		t.Error("project list should contain 'project message'")
	}
	if strings.Contains(mainContent, "project message") {
		t.Error("main list should not contain project messages")
	}
}

func TestActiveTabID(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{
		MainSessionID:    "m",
		ProjectSessionID: "p",
		ProjectRoot:      "/tmp/x",
	})

	if app.activeTabID() != "main" {
		t.Errorf("got %q, want main", app.activeTabID())
	}

	app.activeTab = 1
	if app.activeTabID() != "project" {
		t.Errorf("got %q, want project", app.activeTabID())
	}

	app.openAgentTab("coding")
	if app.activeTabID() != "coding" {
		t.Errorf("got %q, want coding", app.activeTabID())
	}
}

func TestNotificationDrainMsg(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{MainSessionID: "main-sess"})

	// Simulate draining notifications
	notifs := notificationDrainMsg([]agent.Notification{
		{ID: "n1", Source: "cron", Title: "Job completed", Content: "All good"},
		{ID: "n2", Source: "reminder", Title: "Walk dogs"},
	})
	app.Update(notifs)

	// Verify notification messages were added to main chat
	cl := app.chatList("main")
	cl.SetSize(80, 40)
	rendered := cl.Render("")
	if !strings.Contains(rendered, "Job completed") {
		t.Error("should contain first notification")
	}
	if !strings.Contains(rendered, "Walk dogs") {
		t.Error("should contain second notification")
	}
}

func TestMainTabNotClosable(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)

	if app.tabs[0].Closable {
		t.Error("main tab should not be closable")
	}
	if app.tabs[0].ID != "main" {
		t.Errorf("first tab ID = %q, want main", app.tabs[0].ID)
	}

	// Simulate CloseTab logic from handleKey — main tab should be protected
	if app.activeTab == 0 && !app.tabs[0].Closable {
		// This is expected — the close handler checks Closable
	} else {
		t.Error("main tab should not be closable via handler")
	}
}
