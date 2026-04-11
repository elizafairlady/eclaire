package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/ultraviolet/screen"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/elizafairlady/eclaire/internal/ui/chat"
	"github.com/elizafairlady/eclaire/internal/ui/dialog"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
)

const (
	compactWidthBreakpoint = 120
	sidebarWidth           = 30
)

// --- Key bindings ---

type keyMap struct {
	Quit       key.Binding
	NextTab    key.Binding
	PrevTab    key.Binding
	CloseTab   key.Binding
	Send       key.Binding
	FocusSwap  key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	ScrollTop  key.Binding
	ScrollEnd  key.Binding
	ExpandAll  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		NextTab:    key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next tab")),
		PrevTab:    key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev tab")),
		CloseTab:   key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "close tab")),
		Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		FocusSwap:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		ScrollUp:   key.NewBinding(key.WithKeys("pgup", "shift+up"), key.WithHelp("pgup", "scroll up")),
		ScrollDown: key.NewBinding(key.WithKeys("pgdown", "shift+down"), key.WithHelp("pgdn", "scroll down")),
		ScrollTop:  key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		ScrollEnd:  key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
		ExpandAll:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "expand/collapse all")),
	}
}

// --- Tab ---

type Tab struct {
	ID       string
	Label    string
	AgentID  string
	Closable bool
}

// --- Chat entry ---

type chatEntry struct {
	kind    string // "user", "assistant", "tool_call", "tool_result", "system"
	content string
	agentID string // which agent produced this entry
	taskID  string // sub-agent task ID (for grouping)
	depth   int    // 0 = top-level, 1 = sub-agent
}

// --- Active tasks ---

type activeTask struct {
	agentID string
	taskID  string
	prompt  string
	status  string // "running", "completed", "error"
}

// --- Activity feed ---

type activityEntry struct {
	time    string // "21:04"
	icon    string // "→", "✓", "✗"
	text    string // "shell (ls -la)"
	depth   int    // 0 = top-level, 1 = sub-agent
}

const maxActivityEntries = 12

// --- Layout ---

type uiLayout struct {
	area    uv.Rectangle
	header  uv.Rectangle
	main    uv.Rectangle
	editor  uv.Rectangle
	sidebar uv.Rectangle
	status  uv.Rectangle
}

// --- Focus ---

type uiFocus uint8

const (
	focusEditor uiFocus = iota
	focusMain
)

// --- Bubble Tea messages ---

type agentListMsg []agent.Info
type streamEventMsg agent.StreamEvent
type streamDoneMsg struct{ AgentID string }
type gatewayEventMsg gateway.Envelope
type errorMsg struct{ Err error }
type spinnerTickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// --- App ---

type App struct {
	gw     *gateway.Client
	styles styles.Styles
	keys   keyMap
	help   help.Model

	width, height int
	layout        uiLayout
	isCompact     bool
	focus         uiFocus

	// Tabs — first tab is always orchestrator
	tabs      []Tab
	activeTab int

	// Agent state
	agents   []agent.Info
	activity []activityEntry // parsed activity feed for sidebar

	// Chat state per agent
	chatLists   map[string]*chat.MessageList
	chatMsgs    map[string][]chatEntry         // legacy, kept for non-tool messages during transition
	streaming   map[string]string
	busy        map[string]bool
	busyStatus  map[string]string // what the agent is doing right now
	spinnerIdx  int               // braille spinner frame index
	activeTasks map[string]*activeTask // taskID -> activeTask
	sessionIDs  map[string]string      // agentID -> session ID for conversation continuity

	// Streaming channel
	streamCh chan tea.Msg

	// Input
	textarea textarea.Model

	// Dialog overlay
	dialog *dialog.Overlay

	// Markdown rendering
	markdown *markdownRenderer

	// Scrollback
	scrollOffset int  // line offset from bottom (0 = follow mode)
	followMode   bool // auto-scroll to bottom on new content

	// Token tracking
	tokensIn       int64
	tokensOut      int64
	activeProvider string // "ollama", "openrouter", etc. — set from stream events

	// Session todos (updated when todos tool is called)
	sessionTodos []tool.TodoItem

	// Scope indicator
	scope string // "work", "personal", "other", or ""

	// Briefings directory for startup injection
	briefingsDir string

	// Reminders store for sidebar display
	reminders *tool.ReminderStore

	// Client CWD — sent with each agent run for project context
	cwd string

	// Approval flow
	approvalCh chan ApprovalResponseMsg
}

// AppOptions configures optional App behavior.
type AppOptions struct {
	BriefingsDir     string
	RemindersPath    string
	MainSessionID    string
	ProjectSessionID string
	ProjectRoot      string
}

// NewApp creates a new App. Opens directly to orchestrator chat.
func NewApp(gw *gateway.Client, s styles.Styles, opts ...AppOptions) *App {
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.Placeholder = "Send a message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetHeight(1)
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 15
	ta.SetStyles(textarea.Styles{
		Focused: textarea.StyleState{
			Base:        lipgloss.NewStyle(),
			Text:        lipgloss.NewStyle().Foreground(styles.FgBase),
			Placeholder: lipgloss.NewStyle().Foreground(styles.FgMuted),
			Prompt:      lipgloss.NewStyle().Foreground(styles.GreenDark),
		},
		Blurred: textarea.StyleState{
			Base:        lipgloss.NewStyle(),
			Text:        lipgloss.NewStyle().Foreground(styles.FgMuted),
			Placeholder: lipgloss.NewStyle().Foreground(styles.FgMuted),
			Prompt:      lipgloss.NewStyle().Foreground(styles.FgMuted),
		},
		Cursor: textarea.CursorStyle{
			Color: styles.Secondary,
			Shape: tea.CursorBlock,
			Blink: true,
		},
	})

	var opt AppOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	var reminderStore *tool.ReminderStore
	if opt.RemindersPath != "" {
		reminderStore = tool.NewReminderStore(opt.RemindersPath)
	}

	app := &App{
		gw:     gw,
		styles: s,
		keys:   defaultKeyMap(),
		help:   help.New(),
		focus:  focusEditor,
		tabs: []Tab{{
			ID:      "orchestrator",
			Label:   "eclaire",
			AgentID: "orchestrator",
		}},
		chatLists:    make(map[string]*chat.MessageList),
		chatMsgs:     make(map[string][]chatEntry),
		streaming:    make(map[string]string),
		busy:         make(map[string]bool),
		busyStatus:   make(map[string]string),
		activeTasks:  make(map[string]*activeTask),
		sessionIDs:   make(map[string]string),
		streamCh:     make(chan tea.Msg, 64),
		textarea:     ta,
		dialog:       dialog.NewOverlay(),
		markdown:     newMarkdownRenderer(),
		followMode:   true,
		briefingsDir: opt.BriefingsDir,
		reminders:    reminderStore,
		approvalCh:   make(chan ApprovalResponseMsg, 4),
	}

	// Set initial session IDs from gateway connect response
	if opt.MainSessionID != "" {
		app.sessionIDs["orchestrator"] = opt.MainSessionID
	}

	// Store CWD for agent runs. Prefer project root from connect response.
	if opt.ProjectRoot != "" {
		app.cwd = opt.ProjectRoot
	} else {
		app.cwd, _ = os.Getwd()
	}

	return app
}

func (m *App) Init() tea.Cmd {
	// Inject today's briefing on startup if available
	m.injectBriefing()

	return tea.Batch(
		m.textarea.Focus(),
		m.fetchAgents(),
		m.listenEvents(),
		m.listenApprovals(),
	)
}

func (m *App) injectBriefing() {
	if m.briefingsDir == "" {
		return
	}
	today := time.Now().Format("2006-01-02")
	path := m.briefingsDir + "/" + today + ".md"
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	m.chatMsgs["orchestrator"] = append(m.chatMsgs["orchestrator"],
		chatEntry{kind: "system", content: string(data)})
}

func (m *App) listenApprovals() tea.Cmd {
	return func() tea.Msg {
		resp := <-m.approvalCh
		return resp
	}
}

// --- Update ---

func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if m.dialog.HasDialogs() {
		if _, ok := msg.(tea.KeyMsg); ok {
			action := m.dialog.Update(msg)
			if _, ok := action.(dialog.CloseAction); ok {
				m.dialog.CloseFrontDialog()
			}
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayoutAndSize()

	case tea.KeyMsg:
		if cmd, handled := m.handleKey(msg); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

	case agentListMsg:
		m.agents = []agent.Info(msg)

	case streamEventMsg:
		cmds = append(cmds, m.handleStreamEvent(agent.StreamEvent(msg)))

	case streamDoneMsg:
		agentID := msg.AgentID
		if text, ok := m.streaming[agentID]; ok && text != "" {
			cl := m.chatList(agentID)
			cl.Add(chat.NewAssistantMessage(
				"asst_"+fmt.Sprintf("%d", time.Now().UnixNano()),
				text,
				func(content string, width int) string {
					return m.markdown.Render(content, width)
				},
			))
			delete(m.streaming, agentID)
		}
		m.busy[agentID] = false
		delete(m.busyStatus, agentID)

	case gatewayEventMsg:
		cmd := m.handleGatewayEvent(gateway.Envelope(msg))
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case ApprovalRequestMsg:
		dlg := newApprovalDialog(msg, m.approvalCh)
		m.dialog.OpenDialog(dlg)

	case ApprovalResponseMsg:
		// Send approval response to gateway
		if m.gw != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				m.gw.Call(ctx, gateway.MethodApprovalRespond, map[string]any{
					"request_id": msg.RequestID,
					"approved":   msg.Approved,
				})
			}()
		}
		cmds = append(cmds, m.listenApprovals())

	case errorMsg:
		agentID := m.activeAgentID()
		cl := m.chatList(agentID)
		cl.Add(chat.NewSystemMessage("err_"+fmt.Sprintf("%d", time.Now().UnixNano()), "Error: "+msg.Err.Error()))
		m.addActivity("✗", msg.Err.Error(), 0)
		m.busy[agentID] = false
		delete(m.busyStatus, agentID)

	case spinnerTickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		// Keep ticking while any agent is busy
		for _, busy := range m.busy {
			if busy {
				cmds = append(cmds, spinnerTick())
				break
			}
		}
	}

	// Update textarea
	if m.focus == focusEditor {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// --- View ---

func (m *App) View() tea.View {
	var v tea.View
	v.AltScreen = true

	if m.width == 0 || m.height == 0 {
		v.SetContent("Loading...")
		return v
	}

	canvas := uv.NewScreenBuffer(m.width, m.height)
	m.Draw(canvas, canvas.Bounds())

	content := canvas.Render()
	content = strings.ReplaceAll(content, "\r\n", "\n")
	v.SetContent(content)

	if m.focus == focusEditor && m.textarea.Focused() {
		cur := m.textarea.Cursor()
		if cur != nil {
			cur.X++
			cur.Y += m.layout.editor.Min.Y
			v.Cursor = cur
		}
	}

	return v
}

// Draw implements Ultraviolet Drawable.
func (m *App) Draw(scr uv.Screen, area uv.Rectangle) {
	m.layout = m.generateLayout(area.Dx(), area.Dy())
	screen.Clear(scr)

	m.drawHeader(scr, m.layout.header)
	m.drawChat(scr, m.layout.main)

	if !m.isCompact && m.layout.sidebar.Dx() > 0 {
		m.drawSidebar(scr, m.layout.sidebar)
	}

	editorView := m.textarea.View()
	uv.NewStyledString(editorView).Draw(scr, m.layout.editor)

	m.drawStatus(scr, m.layout.status)

	if m.dialog.HasDialogs() {
		m.dialog.Draw(scr, scr.Bounds())
	}
}

// --- Layout ---

func (m *App) generateLayout(w, h int) uiLayout {
	area := image.Rect(0, 0, w, h)
	m.isCompact = w < compactWidthBreakpoint

	headerHeight := 2
	statusHeight := 1
	editorHeight := m.textarea.Height()

	regions := layout.Vertical(
		layout.Len(headerHeight),
		layout.Fill(1),
		layout.Len(editorHeight),
		layout.Len(statusHeight),
	).Split(area)

	lo := uiLayout{
		area:   area,
		header: regions[0],
		main:   regions[1],
		editor: regions[2],
		status: regions[3],
	}

	// Sidebar in wide mode
	if !m.isCompact {
		lr := layout.Horizontal(
			layout.Fill(1),
			layout.Len(sidebarWidth),
		).Split(lo.main)
		lo.main = lr[0]
		lo.sidebar = lr[1]
	}

	return lo
}

func (m *App) updateLayoutAndSize() {
	m.layout = m.generateLayout(m.width, m.height)
	w := m.layout.editor.Dx() - 4
	if w < 10 {
		w = 10
	}
	m.textarea.SetWidth(w)
}

// --- Drawing ---

func (m *App) drawHeader(scr uv.Screen, area uv.Rectangle) {
	w := area.Dx()

	logo := m.styles.Logo.Render("eclaire")

	// Scope badge
	scopeBadge := ""
	if m.scope != "" {
		scopeBadge = " " + m.styles.SystemMsg.Render("["+m.scope+"]")
	}

	var tabs []string
	for i, tab := range m.tabs {
		if i == m.activeTab {
			tabs = append(tabs, m.styles.TabActive.Render("["+tab.Label+"]"))
		} else {
			tabs = append(tabs, m.styles.TabInactive.Render(" "+tab.Label+" "))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	logoW := lipgloss.Width(logo) + lipgloss.Width(scopeBadge)
	tabW := lipgloss.Width(tabBar)
	diagCount := w - logoW - tabW - 4
	if diagCount < 3 {
		diagCount = 3
	}
	diags := m.styles.LogoDiag.Render(strings.Repeat(styles.Diag, diagCount))

	line1 := " " + logo + scopeBadge + " " + diags + " " + tabBar
	line2 := m.styles.HeaderSep.Render(strings.Repeat("─", w))
	uv.NewStyledString(line1 + "\n" + line2).Draw(scr, area)
}

func formatTokenCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func (m *App) drawChat(scr uv.Screen, area uv.Rectangle) {
	agentID := m.tabs[m.activeTab].AgentID
	chatWidth := area.Dx()
	chatHeight := area.Dy()

	cl := m.chatList(agentID)
	cl.SetSize(chatWidth, chatHeight)

	// Build streaming text with busy indicator
	streamText := ""
	if text, ok := m.streaming[agentID]; ok && text != "" {
		streamText = m.styles.AssistantBody.Render(text)
	}
	if m.busy[agentID] {
		if streamText != "" {
			streamText += "\n"
		}
		frame := spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
		status := m.busyStatus[agentID]
		if status == "" {
			status = "thinking..."
		}
		streamText += m.styles.AgentWaiting.Render("  " + frame + " " + status)
	}

	content := cl.Render(streamText)

	// Scroll indicator
	if cl.ScrollOffset() > 0 {
		content += m.styles.Muted.Render(fmt.Sprintf(" ↑ scroll offset %d ", cl.ScrollOffset()))
	}

	uv.NewStyledString(content).Draw(scr, area)
}

// wrapLine hard-wraps a single line to maxWidth characters.
// Uses ANSI-aware wrapping that preserves escape sequences.
func wrapLine(line string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{line}
	}
	wrapped := ansi.Hardwrap(line, maxWidth, false)
	return strings.Split(wrapped, "\n")
}

func (m *App) drawSidebar(scr uv.Screen, area uv.Rectangle) {
	var b strings.Builder
	maxW := area.Dx() - 2

	// --- SESSION ---
	agentID := m.activeAgentID()
	b.WriteString(m.styles.SectionTitle.Render(" SESSION") + "\n")
	sessionID := m.sessionIDs[agentID]
	if sessionID != "" {
		short := sessionID
		if len(short) > 8 {
			short = short[:8]
		}
		b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.ToolName.Render(agentID), m.styles.Muted.Render(short)))
	} else {
		b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.ToolName.Render(agentID), m.styles.Muted.Render("new")))
	}
	b.WriteByte('\n')

	// --- CONTEXT (tokens + cost) ---
	if m.tokensIn > 0 || m.tokensOut > 0 {
		b.WriteString(m.styles.SectionTitle.Render(" CONTEXT") + "\n")
		b.WriteString(fmt.Sprintf(" %s↓ %s↑\n",
			m.styles.ToolName.Render(formatTokenCount(m.tokensIn)),
			m.styles.Muted.Render(formatTokenCount(m.tokensOut))))
		// Only show cost estimate for remote providers (OpenRouter etc.)
		// Local models (Ollama) have no cost. The provider type comes from
		// the active route — if not available, skip the cost line.
		if m.activeProvider == "openrouter" {
			costIn := float64(m.tokensIn) * 3.0 / 1_000_000
			costOut := float64(m.tokensOut) * 15.0 / 1_000_000
			cost := costIn + costOut
			if cost >= 0.01 {
				b.WriteString(fmt.Sprintf(" %s\n", m.styles.Muted.Render(fmt.Sprintf("~$%.2f", cost))))
			}
		}
		b.WriteByte('\n')
	}

	// --- TASKS (running sub-agents) ---
	var running []*activeTask
	for _, t := range m.activeTasks {
		if t.status == "running" {
			running = append(running, t)
		}
	}
	if len(running) > 0 {
		b.WriteString(m.styles.SectionTitle.Render(" TASKS") + "\n")
		for _, t := range running {
			icon := m.styles.AgentRunning.Render(styles.SpinnerIcon)
			name := t.agentID
			if len(name) > maxW-4 {
				name = name[:maxW-4]
			}
			b.WriteString(fmt.Sprintf(" %s %s\n", icon, m.styles.ToolName.Render(name)))
			if t.prompt != "" {
				preview := t.prompt
				if len(preview) > maxW-4 {
					preview = preview[:maxW-4] + "…"
				}
				b.WriteString(fmt.Sprintf("   %s\n", m.styles.Muted.Render(preview)))
			}
		}
		b.WriteByte('\n')
	}

	// --- TO-DO ---
	if tool.HasIncompleteTodos(m.sessionTodos) {
		completed := 0
		for _, td := range m.sessionTodos {
			if td.Status == "completed" {
				completed++
			}
		}
		total := len(m.sessionTodos)
		ratio := m.styles.ToolName.Render(fmt.Sprintf("%d/%d", completed, total))
		current := tool.CurrentTodoActiveForm(m.sessionTodos)
		if current != "" {
			if len(current) > maxW-10 {
				current = current[:maxW-10] + "…"
			}
			b.WriteString(fmt.Sprintf(" %s %s %s\n", m.styles.SectionTitle.Render("TO-DO"), ratio, m.styles.Muted.Render(current)))
		} else {
			b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.SectionTitle.Render("TO-DO"), ratio))
		}
		b.WriteByte('\n')
	}

	// --- REMINDERS ---
	if m.reminders != nil {
		overdue, _ := m.reminders.Overdue()
		pending, _ := m.reminders.Pending()
		if len(overdue) > 0 || len(pending) > 0 {
			b.WriteString(m.styles.SectionTitle.Render(" REMINDERS") + "\n")
			if len(overdue) > 0 {
				b.WriteString(fmt.Sprintf(" %s %s\n",
					m.styles.SystemMsg.Render("⚠"),
					m.styles.SystemMsg.Render(fmt.Sprintf("%d overdue", len(overdue)))))
			}
			// Show next 2 upcoming
			shown := 0
			for _, r := range pending {
				if r.DueAt.After(time.Now()) && shown < 2 {
					text := r.Text
					if len(text) > maxW-8 {
						text = text[:maxW-8] + "…"
					}
					due := r.DueAt.Format("15:04")
					if !sameDay(r.DueAt, time.Now()) {
						due = r.DueAt.Format("Jan 2")
					}
					b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.Muted.Render(due), text))
					shown++
				}
			}
			b.WriteByte('\n')
		}
	}

	// --- ACTIVITY ---
	b.WriteString(m.styles.SectionTitle.Render(" ACTIVITY") + "\n")

	// Calculate how many activity lines we can fit
	usedLines := strings.Count(b.String(), "\n") + 1
	maxFeed := area.Dy() - usedLines
	if maxFeed < 2 {
		maxFeed = 2
	}

	if len(m.activity) == 0 {
		b.WriteString(m.styles.Muted.Render("  no activity") + "\n")
	} else {
		start := 0
		if len(m.activity) > maxFeed {
			start = len(m.activity) - maxFeed
		}
		for _, entry := range m.activity[start:] {
			indent := " "
			if entry.depth > 0 {
				indent = "  "
			}
			text := entry.text
			// Truncate text to fit sidebar (leave room for time + icon + spaces)
			textMax := maxW - 8
			if entry.depth > 0 {
				textMax -= 1
			}
			if textMax > 0 && len(text) > textMax {
				text = text[:textMax-1] + "…"
			}
			line := fmt.Sprintf("%s%s %s %s",
				indent,
				m.styles.Muted.Render(entry.time),
				entry.icon,
				text)
			b.WriteString(line + "\n")
		}
	}

	uv.NewStyledString(b.String()).Draw(scr, area)
}

// cleanToolOutput cleans tool output for display in chat.
// Strips HTML tags from web results for readability.
func cleanToolOutput(output, toolName string) string {
	if toolName == "fetch" || toolName == "web_search" || toolName == "download" {
		output = stripHTMLTags(output)
	}
	return output
}

// stripHTMLTags removes HTML tags from a string (rough, for display only).
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	// Collapse whitespace
	result := b.String()
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (m *App) renderChatEntry(e chatEntry) string {
	indent := "  "
	if e.depth > 0 {
		indent = "    │ "
	}

	switch e.kind {
	case "user":
		content := m.styles.UserLabel.Render("you") + "\n" + e.content
		return m.styles.UserBorder.Render(content)
	case "assistant":
		width := 80
		if m.layout.main.Dx() > 4 {
			width = m.layout.main.Dx() - 4
		}
		return m.markdown.Render(e.content, width)
	case "tool_call":
		icon := m.styles.ToolPendingIcon.Render(styles.ToolPending)
		name := m.styles.ToolName.Render(e.content)
		if e.depth > 0 {
			agent := m.styles.Muted.Render("[" + e.agentID + "]")
			return indent + icon + " " + name + " " + agent
		}
		return indent + icon + " " + name
	case "tool_result":
		lines := strings.Split(e.content, "\n")
		if e.depth > 0 && len(lines) > 5 {
			hidden := len(lines) - 5
			lines = append(lines[:5], m.styles.Muted.Render(fmt.Sprintf("… (%d lines hidden)", hidden)))
		} else if len(lines) > 10 {
			hidden := len(lines) - 10
			lines = append(lines[:10], m.styles.Muted.Render(fmt.Sprintf("… (%d lines hidden)", hidden)))
		}
		body := strings.Join(lines, "\n")
		if e.depth > 0 {
			return indent + m.styles.ToolBody.Render(body)
		}
		return m.styles.ToolBody.Render(body)
	case "system":
		return m.styles.SystemMsg.Render(e.content)
	default:
		return e.content
	}
}

func (m *App) drawStatus(scr uv.Screen, area uv.Rectangle) {
	left := m.styles.HelpKey.Render("enter") + m.styles.HelpDesc.Render(" send  ") +
		m.styles.HelpKey.Render("tab") + m.styles.HelpDesc.Render(" focus  ") +
		m.styles.HelpKey.Render("ctrl+o") + m.styles.HelpDesc.Render(" expand  ") +
		m.styles.HelpKey.Render("ctrl+n/p") + m.styles.HelpDesc.Render(" tabs  ") +
		m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpDesc.Render(" quit")

	right := ""
	if len(m.agents) > 0 {
		right = m.styles.Muted.Render(fmt.Sprintf("%d agents", len(m.agents)))
	}
	gap := area.Dx() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	uv.NewStyledString(left + strings.Repeat(" ", gap) + right).Draw(scr, area)
}

// --- Key handling ---

func (m *App) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit, true

	case key.Matches(msg, m.keys.NextTab):
		m.activeTab = (m.activeTab + 1) % len(m.tabs)
		return nil, true

	case key.Matches(msg, m.keys.PrevTab):
		m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
		return nil, true

	case key.Matches(msg, m.keys.CloseTab):
		if m.activeTab > 0 && m.tabs[m.activeTab].Closable {
			m.tabs = append(m.tabs[:m.activeTab], m.tabs[m.activeTab+1:]...)
			if m.activeTab >= len(m.tabs) {
				m.activeTab = len(m.tabs) - 1
			}
		}
		return nil, true

	case key.Matches(msg, m.keys.FocusSwap):
		if m.focus == focusEditor {
			m.focus = focusMain
			m.textarea.Blur()
		} else {
			m.focus = focusEditor
			m.textarea.Focus()
		}
		return nil, true

	case key.Matches(msg, m.keys.Send):
		if m.focus == focusEditor {
			return m.sendMessage(), true
		}
		return nil, true

	case key.Matches(msg, m.keys.ScrollUp):
		halfPage := m.layout.main.Dy() / 2
		if halfPage < 1 {
			halfPage = 1
		}
		m.chatList(m.activeAgentID()).ScrollBy(halfPage)
		return nil, true

	case key.Matches(msg, m.keys.ScrollDown):
		halfPage := m.layout.main.Dy() / 2
		if halfPage < 1 {
			halfPage = 1
		}
		m.chatList(m.activeAgentID()).ScrollBy(-halfPage)
		return nil, true

	case key.Matches(msg, m.keys.ScrollTop):
		m.chatList(m.activeAgentID()).ScrollToTop()
		return nil, true

	case key.Matches(msg, m.keys.ScrollEnd):
		m.chatList(m.activeAgentID()).ScrollToBottom()
		return nil, true

	case key.Matches(msg, m.keys.ExpandAll):
		m.chatList(m.activeAgentID()).ToggleExpandAll()
		return nil, true
	}

	return nil, false
}

// --- Sending messages ---

func (m *App) sendMessage() tea.Cmd {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return nil
	}
	m.textarea.Reset()

	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	agentID := m.tabs[m.activeTab].AgentID
	cl := m.chatList(agentID)
	cl.Add(chat.NewUserMessage("user_"+fmt.Sprintf("%d", time.Now().UnixNano()), text))
	m.busy[agentID] = true
	m.busyStatus[agentID] = "thinking..."

	go m.runStream(agentID, text)
	return tea.Batch(m.waitForStream(), spinnerTick())
}

func (m *App) runStream(agentID, prompt string) {
	req := gateway.AgentRunRequest{
		AgentID:   agentID,
		Prompt:    prompt,
		SessionID: m.sessionIDs[agentID], // empty on first message, populated after
		CWD:       m.cwd,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := m.gw.Stream(ctx, gateway.MethodAgentRun, req)
	if err != nil {
		m.streamCh <- errorMsg{Err: err}
		m.streamCh <- streamDoneMsg{AgentID: agentID}
		return
	}

	for env := range ch {
		switch env.Type {
		case gateway.TypeStream:
			var ev agent.StreamEvent
			if json.Unmarshal(env.Data, &ev) == nil {
				m.streamCh <- streamEventMsg(ev)
			}
		case gateway.TypeResponse:
			// Capture session ID from final response for conversation continuity
			if env.Error == nil && env.Data != nil {
				var resp struct {
					SessionID string `json:"session_id"`
				}
				if json.Unmarshal(env.Data, &resp) == nil && resp.SessionID != "" {
					m.sessionIDs[agentID] = resp.SessionID
				}
			}
		}
	}
	m.streamCh <- streamDoneMsg{AgentID: agentID}
}

func (m *App) handleStreamEvent(ev agent.StreamEvent) tea.Cmd {
	agentID := m.activeAgentID()
	if ev.AgentID != "" && !ev.Nested && !isSubAgentEvent(ev.Type) {
		agentID = ev.AgentID
	}
	cl := m.chatList(agentID)

	switch ev.Type {
	case agent.EventTextDelta:
		m.streaming[agentID] += ev.Delta
		m.busyStatus[agentID] = "responding..."

	case agent.EventToolCall:
		item := chat.NewToolItem(ev.ToolCallID, ev.ToolName, ev.Input)
		cl.AddTool(ev.ToolCallID, item)
		m.addActivity("→", ev.ToolName, 0)
		m.busyStatus[agentID] = "running " + ev.ToolName + "..."

		// Capture todo updates
		if ev.ToolName == "todos" && ev.Input != "" {
			var todosInput struct {
				Todos []tool.TodoItem `json:"todos"`
			}
			if json.Unmarshal([]byte(ev.Input), &todosInput) == nil && len(todosInput.Todos) > 0 {
				m.sessionTodos = todosInput.Todos
			}
		}

	case agent.EventToolResult:
		output := cleanToolOutput(ev.Output, ev.ToolName)
		isError := strings.HasPrefix(output, "Error:")
		cl.SetToolResult(ev.ToolCallID, output, isError)
		m.addActivity("✓", ev.ToolName, 0)
		m.busyStatus[agentID] = "thinking..."

	case agent.EventStepFinish:
		if ev.Usage != nil {
			m.tokensIn += ev.Usage.InputTokens
			m.tokensOut += ev.Usage.OutputTokens
		}
		if ev.Provider != "" {
			m.activeProvider = ev.Provider
		}

	case agent.EventError:
		cl.Add(chat.NewSystemMessage("err_"+ev.AgentID, "Error: "+ev.Error))
		m.busy[agentID] = false
		delete(m.busyStatus, agentID)

	// Sub-agent lifecycle
	case agent.EventSubAgentStarted:
		m.activeTasks[ev.TaskID] = &activeTask{
			agentID: ev.AgentID,
			taskID:  ev.TaskID,
			prompt:  ev.Output,
			status:  "running",
		}
		// Create agent tool item — use TaskID as the tool call ID since
		// we don't have the parent's tool call ID here. The agent tool's
		// real call ID will come in via EventToolCall before this.
		// Find the most recent agent tool call or create one.
		if _, ok := cl.GetAgent(ev.TaskID); !ok {
			agentItem := chat.NewAgentToolItem(ev.TaskID, `{"agent":"`+ev.AgentID+`","prompt":"`+ev.Output+`"}`)
			cl.AddAgentTool(ev.TaskID, ev.TaskID, agentItem)
		}
		m.addActivity("→", "agent "+ev.AgentID, 0)

	case agent.EventSubAgentToolCall:
		nested := chat.NewToolItem(ev.ToolCallID, ev.ToolName, ev.Input)
		cl.AddNestedTool(ev.TaskID, ev.ToolCallID, nested)
		m.addActivity("→", ev.ToolName, 1)

	case agent.EventSubAgentToolResult:
		output := cleanToolOutput(ev.Output, ev.ToolName)
		isError := strings.HasPrefix(output, "Error:")
		cl.SetToolResult(ev.ToolCallID, output, isError)

	case agent.EventSubAgentCompleted:
		if task, ok := m.activeTasks[ev.TaskID]; ok {
			task.status = "completed"
		}
		// Mark the agent tool as complete
		if agentItem, ok := cl.GetAgent(ev.TaskID); ok {
			agentItem.SetResult(ev.Output, false)
		}
		m.addActivity("✓", "agent "+ev.AgentID, 0)
	}

	return m.waitForStream()
}

func (m *App) waitForStream() tea.Cmd {
	return func() tea.Msg { return <-m.streamCh }
}

func (m *App) activeAgentID() string {
	if m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].AgentID
	}
	return "orchestrator"
}

// chatList returns or creates the MessageList for an agent.
func (m *App) chatList(agentID string) *chat.MessageList {
	if l, ok := m.chatLists[agentID]; ok {
		return l
	}
	l := chat.NewMessageList()
	m.chatLists[agentID] = l
	return l
}

// --- Slash commands ---

// ParseSlashCommand extracts command and args. Exported for testing.
func ParseSlashCommand(text string) (cmd string, args []string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

func (m *App) handleSlashCommand(text string) tea.Cmd {
	cmd, args := ParseSlashCommand(text)
	agentID := m.activeAgentID()

	switch cmd {
	case "/clear":
		m.chatList(agentID).Clear()
		m.chatMsgs[agentID] = nil
		delete(m.streaming, agentID)
	case "/status":
		return m.showStatusInline(agentID)
	case "/agents":
		return m.showAgentsInline(agentID)
	case "/open":
		if len(args) > 0 {
			return m.openAgentTab(args[0])
		}
		m.addSystemMsg(agentID, "Usage: /open <agent-id>")
	case "/tools":
		return m.fetchToolsIntoChat(agentID)
	case "/scope":
		if len(args) > 0 {
			m.scope = args[0]
			m.addSystemMsg(agentID, fmt.Sprintf("Scope set to: %s", m.scope))
		} else if m.scope != "" {
			m.addSystemMsg(agentID, fmt.Sprintf("Current scope: %s (use /scope <name> to change, /scope off to clear)", m.scope))
		} else {
			m.addSystemMsg(agentID, "No scope set. Use /scope work, /scope personal, or /scope <name>")
		}
		if len(args) > 0 && args[0] == "off" {
			m.scope = ""
			m.addSystemMsg(agentID, "Scope cleared.")
		}
	case "/todos":
		if len(m.sessionTodos) == 0 {
			m.addSystemMsg(agentID, "No todos in this session.")
		} else {
			var sb strings.Builder
			sb.WriteString("Session Todos:\n")
			for _, td := range m.sessionTodos {
				icon := "○"
				text := td.Content
				switch td.Status {
				case "completed":
					icon = "✓"
				case "in_progress":
					icon = "→"
					if td.ActiveForm != "" {
						text = td.ActiveForm
					}
				}
				sb.WriteString(fmt.Sprintf("  %s %s\n", icon, text))
			}
			completed := 0
			for _, td := range m.sessionTodos {
				if td.Status == "completed" {
					completed++
				}
			}
			sb.WriteString(fmt.Sprintf("\n%d/%d completed", completed, len(m.sessionTodos)))
			m.addSystemMsg(agentID, sb.String())
		}
	case "/model":
		m.addSystemMsg(agentID, "Current agent: "+agentID)
	default:
		m.addSystemMsg(agentID, "Commands: /clear /status /agents /open <agent> /tools /scope <name> /todos /model")
	}
	return nil
}

func isSubAgentEvent(evType string) bool {
	switch evType {
	case agent.EventSubAgentStarted, agent.EventSubAgentToolCall,
		agent.EventSubAgentToolResult, agent.EventSubAgentCompleted:
		return true
	}
	return false
}

func (m *App) addActivity(icon, text string, depth int) {
	entry := activityEntry{
		time:  time.Now().Format("15:04"),
		icon:  icon,
		text:  text,
		depth: depth,
	}
	m.activity = append(m.activity, entry)
	if len(m.activity) > maxActivityEntries {
		m.activity = m.activity[len(m.activity)-maxActivityEntries:]
	}
}

func (m *App) addSystemMsg(agentID, text string) {
	m.chatList(agentID).Add(chat.NewSystemMessage("sys_"+fmt.Sprintf("%d", time.Now().UnixNano()), text))
}

func (m *App) showStatusInline(agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := m.gw.Call(ctx, gateway.MethodAgentList, nil)
		if err != nil {
			return errorMsg{Err: err}
		}
		var agents []agent.Info
		json.Unmarshal(data, &agents)

		var sb strings.Builder
		sb.WriteString("Agent Status:\n")
		for _, a := range agents {
			icon := "○"
			if a.Status == agent.StatusRunning {
				icon = "●"
			}
			bi := ""
			if a.BuiltIn {
				bi = " (built-in)"
			}
			sb.WriteString(fmt.Sprintf("  %s %-14s %-8s %s%s\n", icon, a.ID, a.Role, string(a.Status), bi))
		}
		m.chatMsgs[agentID] = append(m.chatMsgs[agentID],
			chatEntry{kind: "system", content: sb.String()})
		return nil
	}
}

func (m *App) showAgentsInline(agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := m.gw.Call(ctx, gateway.MethodAgentList, nil)
		if err != nil {
			return errorMsg{Err: err}
		}
		var agents []agent.Info
		json.Unmarshal(data, &agents)

		var sb strings.Builder
		sb.WriteString("Available Agents:\n")
		for _, a := range agents {
			sb.WriteString(fmt.Sprintf("  %-14s %s\n", a.ID, a.Description))
		}
		sb.WriteString("\nUse /open <agent-id> to open a direct chat tab.")
		m.chatMsgs[agentID] = append(m.chatMsgs[agentID],
			chatEntry{kind: "system", content: sb.String()})
		return nil
	}
}

func (m *App) fetchToolsIntoChat(agentID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := m.gw.Call(ctx, gateway.MethodToolList, nil)
		if err != nil {
			return errorMsg{Err: err}
		}
		var tools []gateway.ToolInfo
		json.Unmarshal(data, &tools)
		var sb strings.Builder
		sb.WriteString("Registered tools:\n")
		for _, t := range tools {
			tier := "read"
			if t.Tier == 1 {
				tier = "modify"
			} else if t.Tier >= 2 {
				tier = "danger"
			}
			sb.WriteString(fmt.Sprintf("  %-15s %-8s %s\n", t.Name, t.Category, tier))
		}
		m.chatMsgs[agentID] = append(m.chatMsgs[agentID],
			chatEntry{kind: "system", content: sb.String()})
		return nil
	}
}

// --- Tab management ---

func (m *App) openAgentTab(agentID string) tea.Cmd {
	for i, tab := range m.tabs {
		if tab.AgentID == agentID {
			m.activeTab = i
			return nil
		}
	}
	m.tabs = append(m.tabs, Tab{
		ID:       agentID,
		Label:    agentID,
		AgentID:  agentID,
		Closable: agentID != "orchestrator",
	})
	m.activeTab = len(m.tabs) - 1
	return nil
}

// --- Gateway commands ---

func (m *App) fetchAgents() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := m.gw.Call(ctx, gateway.MethodAgentList, nil)
		if err != nil {
			return errorMsg{Err: err}
		}
		var agents []agent.Info
		json.Unmarshal(data, &agents)
		return agentListMsg(agents)
	}
}

func (m *App) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.gw.Events()
		if !ok {
			return errorMsg{Err: fmt.Errorf("gateway connection closed")}
		}
		return gatewayEventMsg(ev)
	}
}

func (m *App) handleGatewayEvent(env gateway.Envelope) tea.Cmd {
	if env.Type == gateway.TypeEvent {
		// Check if this is an approval request event
		var eventData struct {
			EventType   string `json:"event_type"`
			ID          string `json:"id"`
			AgentID     string `json:"agent_id"`
			Action      string `json:"action"`
			Description string `json:"description"`
		}
		if json.Unmarshal(env.Data, &eventData) == nil && eventData.EventType == "approval_request" {
			return func() tea.Msg {
				return ApprovalRequestMsg{
					RequestID:   eventData.ID,
					AgentID:     eventData.AgentID,
					Action:      eventData.Action,
					Description: eventData.Description,
				}
			}
		}
		m.addActivity("·", string(env.Data), 0)
	}
	return m.listenEvents()
}
