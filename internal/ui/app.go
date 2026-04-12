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
	"github.com/elizafairlady/eclaire/internal/persist"
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
	NewTab     key.Binding
	Send       key.Binding
	FocusSwap  key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	ScrollTop  key.Binding
	ScrollEnd  key.Binding
	ExpandAll  key.Binding
	NextNotif  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		NextTab:    key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next tab")),
		PrevTab:    key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev tab")),
		CloseTab:   key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "close tab")),
		NewTab:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "sessions")),
		Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		FocusSwap:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		ScrollUp:   key.NewBinding(key.WithKeys("pgup", "shift+up"), key.WithHelp("pgup", "scroll up")),
		ScrollDown: key.NewBinding(key.WithKeys("pgdown", "shift+down"), key.WithHelp("pgdn", "scroll down")),
		ScrollTop:  key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
		ScrollEnd:  key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
		ExpandAll:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "expand/collapse all")),
		NextNotif:  key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "next notification")),
	}
}

// --- Tab ---

type Tab struct {
	ID        string
	Label     string
	AgentID   string
	SessionID string // explicit session ID for this tab (empty = new session on first message)
	Closable  bool
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
	focusNotification // viewing a notification's details + actions in the editor area
	focusSessionPicker // selecting an existing session to resume as a tab
)

// --- Bubble Tea messages ---

type agentListMsg []agent.Info

type streamEventMsg struct {
	tabID string
	event agent.StreamEvent
}
type streamDoneMsg struct{ tabID string }
type sessionIDMsg struct {
	tabID     string
	sessionID string
}
type notificationListMsg []agent.Notification
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

	// Chat state per tab (keyed by Tab.ID)
	chatLists   map[string]*chat.MessageList
	streaming   map[string]string
	busy        map[string]bool
	busyStatus  map[string]string // what the tab's agent is doing right now
	spinnerIdx  int               // braille spinner frame index
	activeTasks map[string]*activeTask // taskID -> activeTask

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

	// Pending notifications (fetched on connect, updated via events)
	pendingNotifs      []agent.Notification
	activeNotifIdx     int    // index into pendingNotifs when focusNotification is active
	activeNotifID      string // ID of the notification being viewed
	notifActionCursor  int    // which action is highlighted in notification focus mode

	// Session picker state
	sessionPickerItems  []persist.SessionMeta
	sessionPickerCursor int
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

	// Build initial tabs: main session is always tab 0
	tabs := []Tab{{
		ID:        "main",
		Label:     "Claire",
		AgentID:   "orchestrator",
		SessionID: opt.MainSessionID,
	}}
	// Project session tab when connecting from a project directory
	if opt.ProjectSessionID != "" {
		label := opt.ProjectRoot
		if i := strings.LastIndex(label, "/"); i >= 0 {
			label = label[i+1:]
		}
		if label == "" {
			label = "project"
		}
		tabs = append(tabs, Tab{
			ID:        "project",
			Label:     label,
			AgentID:   "orchestrator",
			SessionID: opt.ProjectSessionID,
			Closable:  true,
		})
	}

	app := &App{
		gw:          gw,
		styles:      s,
		keys:        defaultKeyMap(),
		help:        help.New(),
		focus:       focusEditor,
		tabs:        tabs,
		chatLists:   make(map[string]*chat.MessageList),
		streaming:   make(map[string]string),
		busy:        make(map[string]bool),
		busyStatus:  make(map[string]string),
		activeTasks: make(map[string]*activeTask),
		streamCh:    make(chan tea.Msg, 64),
		textarea:    ta,
		dialog:      dialog.NewOverlay(),
		markdown:    newMarkdownRenderer(),
		followMode:  true,
		briefingsDir: opt.BriefingsDir,
		reminders:   reminderStore,
		approvalCh:  make(chan ApprovalResponseMsg, 4),
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
		m.fetchNotifications(),
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
	m.chatList("main").Add(chat.NewSystemMessage("briefing-"+today, string(data)))
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
		cmds = append(cmds, m.handleStreamEvent(msg.tabID, msg.event))

	case streamDoneMsg:
		tabID := msg.tabID
		if text, ok := m.streaming[tabID]; ok && text != "" {
			cl := m.chatList(tabID)
			cl.Add(chat.NewAssistantMessage(
				"asst_"+fmt.Sprintf("%d", time.Now().UnixNano()),
				text,
				func(content string, width int) string {
					return m.markdown.Render(content, width)
				},
			))
			delete(m.streaming, tabID)
		}
		m.busy[tabID] = false
		delete(m.busyStatus, tabID)

	case sessionIDMsg:
		for i := range m.tabs {
			if m.tabs[i].ID == msg.tabID {
				m.tabs[i].SessionID = msg.sessionID
				break
			}
		}

	case notificationListMsg:
		// Store pending notifications for sidebar display.
		// User navigates to them when ready — never forced.
		m.pendingNotifs = []agent.Notification(msg)

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
					"persist":    msg.Persist,
				})
			}()
		}
		cmds = append(cmds, m.listenApprovals())

	case sessionPickerMsg:
		// Filter to resumable sessions (not main, not archived)
		var items []persist.SessionMeta
		for _, s := range msg {
			if s.Kind == "main" || s.Status == "archived" {
				continue
			}
			items = append(items, s)
		}
		m.sessionPickerItems = items
		m.sessionPickerCursor = 0
		m.focus = focusSessionPicker
		m.textarea.Blur()

	case notifResolvedMsg:
		// Mark notification as resolved locally and exit notification focus
		for i := range m.pendingNotifs {
			if m.pendingNotifs[i].ID == msg.ID {
				m.pendingNotifs[i].Resolved = true
				break
			}
		}
		if m.focus == focusNotification {
			m.focus = focusEditor
		}
		tabID := m.activeTabID()
		m.addSystemMsg(tabID, fmt.Sprintf("Notification %s: %s", msg.ID, msg.Action))

	case errorMsg:
		tabID := m.activeTabID()
		cl := m.chatList(tabID)
		cl.Add(chat.NewSystemMessage("err_"+fmt.Sprintf("%d", time.Now().UnixNano()), "Error: "+msg.Err.Error()))
		m.addActivity("✗", msg.Err.Error(), 0)
		m.busy[tabID] = false
		delete(m.busyStatus, tabID)

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

	switch m.focus {
	case focusNotification:
		uv.NewStyledString(m.renderNotificationPrompt()).Draw(scr, m.layout.editor)
	case focusSessionPicker:
		uv.NewStyledString(m.renderSessionPicker()).Draw(scr, m.layout.editor)
	default:
		uv.NewStyledString(m.textarea.View()).Draw(scr, m.layout.editor)
	}

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

	// When viewing a notification, expand the editor area to fit the notification
	// content + action menu + hint line.
	if m.focus == focusNotification && m.activeNotifIdx < len(m.pendingNotifs) {
		n := m.pendingNotifs[m.activeNotifIdx]
		// title + content + blank + actions + hint = at least 4 + len(actions)
		needed := 4 + len(n.Actions)
		if n.Content != "" {
			needed++
		}
		if needed > editorHeight {
			editorHeight = needed
		}
		// Cap at half the terminal height
		maxEditor := h / 2
		if editorHeight > maxEditor {
			editorHeight = maxEditor
		}
	}

	// Session picker also needs expanded editor area
	if m.focus == focusSessionPicker {
		needed := 4 + len(m.sessionPickerItems) // title + blank + "New session" + sessions
		if needed > editorHeight {
			editorHeight = needed
		}
		maxEditor := h / 2
		if editorHeight > maxEditor {
			editorHeight = maxEditor
		}
	}

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
	tabID := m.activeTabID()
	chatWidth := area.Dx()
	chatHeight := area.Dy()

	cl := m.chatList(tabID)
	cl.SetSize(chatWidth, chatHeight)

	// Build streaming text with busy indicator.
	// Spinner only shows when waiting (no tokens yet). Once text is streaming, hide it.
	streamText := ""
	if text, ok := m.streaming[tabID]; ok && text != "" {
		streamText = m.markdown.Render(text, chatWidth-4)
	} else if m.busy[tabID] {
		frame := spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
		status := m.busyStatus[tabID]
		if status == "" {
			status = "thinking..."
		}
		streamText = m.styles.AgentWaiting.Render("  " + frame + " " + status)
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
	tab := m.tabs[m.activeTab]
	b.WriteString(m.styles.SectionTitle.Render(" SESSION") + "\n")
	if tab.SessionID != "" {
		short := tab.SessionID
		if len(short) > 8 {
			short = short[:8]
		}
		b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.ToolName.Render(tab.AgentID), m.styles.Muted.Render(short)))
	} else {
		b.WriteString(fmt.Sprintf(" %s %s\n", m.styles.ToolName.Render(tab.AgentID), m.styles.Muted.Render("new")))
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

	// --- NOTIFICATIONS ---
	if len(m.pendingNotifs) > 0 {
		unresolved := 0
		for _, n := range m.pendingNotifs {
			if !n.Resolved {
				unresolved++
			}
		}
		if unresolved > 0 {
			b.WriteString(m.styles.SectionTitle.Render(fmt.Sprintf(" NOTIFS (%d)", unresolved)) + "\n")
			shown := 0
			for _, n := range m.pendingNotifs {
				if n.Resolved || shown >= 5 {
					continue
				}
				icon := " "
				switch n.Source {
				case "approval":
					icon = m.styles.SystemMsg.Render("!")
				case "reminder":
					icon = m.styles.Muted.Render("R")
				}
				title := n.Title
				if len(title) > maxW-6 {
					title = title[:maxW-6] + "…"
				}
				b.WriteString(fmt.Sprintf(" %s %s %s\n", icon, m.styles.Muted.Render(n.ID), title))
				if len(n.Actions) > 0 {
					b.WriteString(fmt.Sprintf("   [%s]\n", strings.Join(n.Actions, "/")))
				}
				shown++
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

func (m *App) drawStatus(scr uv.Screen, area uv.Rectangle) {
	var left string

	switch m.focus {
	case focusNotification:
		left = m.styles.HelpKey.Render("↑↓") + m.styles.HelpDesc.Render(" select  ") +
			m.styles.HelpKey.Render("enter") + m.styles.HelpDesc.Render(" confirm  ") +
			m.styles.HelpKey.Render("ctrl+j") + m.styles.HelpDesc.Render(" next  ") +
			m.styles.HelpKey.Render("esc") + m.styles.HelpDesc.Render(" cancel")
	case focusSessionPicker:
		left = m.styles.HelpKey.Render("↑↓") + m.styles.HelpDesc.Render(" select  ") +
			m.styles.HelpKey.Render("enter") + m.styles.HelpDesc.Render(" resume  ") +
			m.styles.HelpKey.Render("esc") + m.styles.HelpDesc.Render(" cancel")
	default:
		left = m.styles.HelpKey.Render("enter") + m.styles.HelpDesc.Render(" send  ") +
			m.styles.HelpKey.Render("tab") + m.styles.HelpDesc.Render(" focus  ") +
			m.styles.HelpKey.Render("ctrl+j") + m.styles.HelpDesc.Render(" notif  ") +
			m.styles.HelpKey.Render("ctrl+n/p") + m.styles.HelpDesc.Render(" tabs  ") +
			m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpDesc.Render(" quit")
	}

	right := ""
	unresolved := 0
	for _, n := range m.pendingNotifs {
		if !n.Resolved {
			unresolved++
		}
	}
	if unresolved > 0 {
		right = m.styles.SystemMsg.Render(fmt.Sprintf(" %d notif ", unresolved))
	} else if len(m.agents) > 0 {
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
	// Notification focus mode: arrow keys navigate actions, enter selects, esc cancels, ctrl+j cycles
	if m.focus == focusNotification {
		switch {
		case msg.String() == "esc":
			m.focus = focusEditor
			m.textarea.Focus()
			return nil, true
		case key.Matches(msg, m.keys.NextNotif):
			m.cycleToNextNotification()
			m.notifActionCursor = 0
			return nil, true
		case msg.String() == "up" || msg.String() == "k":
			if m.activeNotifIdx < len(m.pendingNotifs) {
				n := m.pendingNotifs[m.activeNotifIdx]
				if m.notifActionCursor > 0 {
					m.notifActionCursor--
				} else {
					m.notifActionCursor = len(n.Actions) - 1
				}
			}
			return nil, true
		case msg.String() == "down" || msg.String() == "j":
			if m.activeNotifIdx < len(m.pendingNotifs) {
				n := m.pendingNotifs[m.activeNotifIdx]
				if m.notifActionCursor < len(n.Actions)-1 {
					m.notifActionCursor++
				} else {
					m.notifActionCursor = 0
				}
			}
			return nil, true
		case msg.String() == "enter":
			if m.activeNotifIdx < len(m.pendingNotifs) {
				n := m.pendingNotifs[m.activeNotifIdx]
				if m.notifActionCursor < len(n.Actions) {
					action := n.Actions[m.notifActionCursor]
					m.focus = focusEditor
					m.textarea.Focus()
					return m.actOnNotification(n.ID, action), true
				}
			}
			return nil, true
		}
		return nil, true // consume all other keys in notification mode
	}

	// Session picker mode: arrow keys navigate, enter resumes/creates session, esc cancels
	// Item 0 = "New session", items 1..N = existing sessions
	if m.focus == focusSessionPicker {
		totalItems := 1 + len(m.sessionPickerItems)
		switch {
		case msg.String() == "esc":
			m.focus = focusEditor
			m.textarea.Focus()
			return nil, true
		case msg.String() == "up" || msg.String() == "k":
			if m.sessionPickerCursor > 0 {
				m.sessionPickerCursor--
			} else {
				m.sessionPickerCursor = totalItems - 1
			}
			return nil, true
		case msg.String() == "down" || msg.String() == "j":
			if m.sessionPickerCursor < totalItems-1 {
				m.sessionPickerCursor++
			} else {
				m.sessionPickerCursor = 0
			}
			return nil, true
		case msg.String() == "enter":
			m.focus = focusEditor
			m.textarea.Focus()
			if m.sessionPickerCursor == 0 {
				// New session — create tab with empty SessionID
				m.tabs = append(m.tabs, Tab{
					ID:       fmt.Sprintf("new-%d", time.Now().UnixNano()),
					Label:    "new",
					AgentID:  "orchestrator",
					Closable: true,
				})
				m.activeTab = len(m.tabs) - 1
				return nil, true
			}
			sessIdx := m.sessionPickerCursor - 1
			if sessIdx < len(m.sessionPickerItems) {
				return m.openSessionTab(m.sessionPickerItems[sessIdx]), true
			}
			return nil, true
		}
		return nil, true
	}

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

	case key.Matches(msg, m.keys.NewTab):
		return m.openSessionPicker(), true

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
		m.chatList(m.activeTabID()).ScrollBy(halfPage)
		return nil, true

	case key.Matches(msg, m.keys.ScrollDown):
		halfPage := m.layout.main.Dy() / 2
		if halfPage < 1 {
			halfPage = 1
		}
		m.chatList(m.activeTabID()).ScrollBy(-halfPage)
		return nil, true

	case key.Matches(msg, m.keys.ScrollTop):
		m.chatList(m.activeTabID()).ScrollToTop()
		return nil, true

	case key.Matches(msg, m.keys.ScrollEnd):
		m.chatList(m.activeTabID()).ScrollToBottom()
		return nil, true

	case key.Matches(msg, m.keys.ExpandAll):
		m.chatList(m.activeTabID()).ToggleExpandAll()
		return nil, true

	case key.Matches(msg, m.keys.NextNotif):
		if m.openNextNotification() {
			return nil, true
		}
	}

	return nil, false
}

// openNextNotification finds the first unresolved notification and enters notification focus.
// Returns true if a notification was found.
func (m *App) openNextNotification() bool {
	for i, n := range m.pendingNotifs {
		if !n.Resolved {
			m.activeNotifIdx = i
			m.activeNotifID = n.ID
			m.notifActionCursor = 0
			m.focus = focusNotification
			m.textarea.Blur()
			return true
		}
	}
	return false
}

// cycleToNextNotification moves to the next unresolved notification after the current one.
func (m *App) cycleToNextNotification() {
	start := m.activeNotifIdx + 1
	for i := 0; i < len(m.pendingNotifs); i++ {
		idx := (start + i) % len(m.pendingNotifs)
		if !m.pendingNotifs[idx].Resolved {
			m.activeNotifIdx = idx
			m.activeNotifID = m.pendingNotifs[idx].ID
			return
		}
	}
	// No more unresolved — exit notification mode
	m.focus = focusEditor
	m.textarea.Focus()
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

	tab := m.tabs[m.activeTab]
	tabID := tab.ID
	cl := m.chatList(tabID)
	cl.Add(chat.NewUserMessage("user_"+fmt.Sprintf("%d", time.Now().UnixNano()), text))
	m.busy[tabID] = true
	m.busyStatus[tabID] = "thinking..."

	go m.runStream(tabID, tab, text)
	return tea.Batch(m.waitForStream(), spinnerTick())
}

func (m *App) runStream(tabID string, tab Tab, prompt string) {
	req := gateway.AgentRunRequest{
		AgentID:   tab.AgentID,
		Prompt:    prompt,
		SessionID: tab.SessionID,
		CWD:       m.cwd,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := m.gw.Stream(ctx, gateway.MethodAgentRun, req)
	if err != nil {
		m.streamCh <- errorMsg{Err: err}
		m.streamCh <- streamDoneMsg{tabID: tabID}
		return
	}

	for env := range ch {
		switch env.Type {
		case gateway.TypeStream:
			var ev agent.StreamEvent
			if json.Unmarshal(env.Data, &ev) == nil {
				m.streamCh <- streamEventMsg{tabID: tabID, event: ev}
			}
		case gateway.TypeResponse:
			if env.Error != nil {
				m.streamCh <- errorMsg{Err: fmt.Errorf("%s", env.Error.Message)}
			} else if env.Data != nil {
				var resp struct {
					SessionID string `json:"session_id"`
				}
				if json.Unmarshal(env.Data, &resp) == nil && resp.SessionID != "" {
					m.streamCh <- sessionIDMsg{tabID: tabID, sessionID: resp.SessionID}
				}
			}
		}
	}
	m.streamCh <- streamDoneMsg{tabID: tabID}
}

func (m *App) handleStreamEvent(tabID string, ev agent.StreamEvent) tea.Cmd {
	cl := m.chatList(tabID)

	switch ev.Type {
	case agent.EventTextDelta:
		m.streaming[tabID] += ev.Delta
		m.busyStatus[tabID] = "responding..."

	case agent.EventToolCall:
		item := chat.NewToolItem(ev.ToolCallID, ev.ToolName, ev.Input)
		cl.AddTool(ev.ToolCallID, item)
		m.addActivity("→", ev.ToolName, 0)
		m.busyStatus[tabID] = "running " + ev.ToolName + "..."

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
		m.busyStatus[tabID] = "thinking..."

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
		m.busy[tabID] = false
		delete(m.busyStatus, tabID)

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

func (m *App) activeTabID() string {
	if m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].ID
	}
	return "main"
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
	tabID := m.activeTabID()

	switch cmd {
	case "/clear":
		m.chatList(tabID).Clear()
		delete(m.streaming, tabID)
	case "/status":
		return m.showStatusInline(tabID)
	case "/agents":
		return m.showAgentsInline(tabID)
	case "/sessions":
		return m.openSessionPicker()
	case "/tools":
		return m.fetchToolsIntoChat(tabID)
	case "/notif":
		if len(args) == 0 {
			// List notifications inline
			if len(m.pendingNotifs) == 0 {
				m.addSystemMsg(tabID, "No pending notifications.")
			} else {
				var sb strings.Builder
				for i, n := range m.pendingNotifs {
					if n.Resolved {
						continue
					}
					sb.WriteString(fmt.Sprintf("  %d. [%s] %s (%s)", i, n.ID, n.Title, n.Source))
					if len(n.Actions) > 0 {
						sb.WriteString(fmt.Sprintf(" [%s]", strings.Join(n.Actions, "/")))
					}
					sb.WriteByte('\n')
				}
				m.addSystemMsg(tabID, sb.String())
			}
		} else if len(args) >= 1 {
			// Open notification for action: /notif <id> [action]
			notifID := args[0]
			if len(args) >= 2 {
				// Direct action: /notif <id> <action>
				return m.actOnNotification(notifID, args[1])
			}
			// Show notification details and enter notification focus mode
			for i, n := range m.pendingNotifs {
				if n.ID == notifID {
					m.activeNotifIdx = i
					m.activeNotifID = n.ID
					m.notifActionCursor = 0
					m.focus = focusNotification
					m.textarea.Blur()
					break
				}
			}
		}
	case "/scope":
		if len(args) > 0 {
			m.scope = args[0]
			m.addSystemMsg(tabID, fmt.Sprintf("Scope set to: %s", m.scope))
		} else if m.scope != "" {
			m.addSystemMsg(tabID, fmt.Sprintf("Current scope: %s (use /scope <name> to change, /scope off to clear)", m.scope))
		} else {
			m.addSystemMsg(tabID, "No scope set. Use /scope work, /scope personal, or /scope <name>")
		}
		if len(args) > 0 && args[0] == "off" {
			m.scope = ""
			m.addSystemMsg(tabID, "Scope cleared.")
		}
	case "/todos":
		if len(m.sessionTodos) == 0 {
			m.addSystemMsg(tabID, "No todos in this session.")
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
			m.addSystemMsg(tabID, sb.String())
		}
	case "/model":
		m.addSystemMsg(tabID, "Current agent: "+m.activeAgentID())
	default:
		m.addSystemMsg(tabID, "Commands: /clear /status /agents /open <agent> /tools /scope <name> /todos /model")
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

func (m *App) addSystemMsg(tabID, text string) {
	m.chatList(tabID).Add(chat.NewSystemMessage("sys_"+fmt.Sprintf("%d", time.Now().UnixNano()), text))
}

// actOnNotification sends an action to the gateway for a notification.
func (m *App) actOnNotification(notifID, action string) tea.Cmd {
	if m.gw == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req := gateway.NotificationActRequest{
			ID:     notifID,
			Action: action,
		}
		_, err := m.gw.Call(ctx, gateway.MethodNotificationAct, req)
		if err != nil {
			return errorMsg{Err: err}
		}
		// Mark as resolved locally
		return notifResolvedMsg{ID: notifID, Action: action}
	}
}

// notifResolvedMsg is sent when a notification action completes.
type notifResolvedMsg struct {
	ID     string
	Action string
}

// renderNotificationPrompt renders the notification detail view that replaces the editor.
// Shows a selectable menu of actions — arrow keys navigate, enter selects.
func (m *App) renderNotificationPrompt() string {
	if m.activeNotifIdx >= len(m.pendingNotifs) {
		m.focus = focusEditor
		return ""
	}
	n := m.pendingNotifs[m.activeNotifIdx]
	if n.ID != m.activeNotifID {
		m.focus = focusEditor
		return ""
	}

	var sb strings.Builder
	sb.WriteString(m.styles.SectionTitle.Render(fmt.Sprintf(" [%s] %s ", n.Source, n.Title)))
	sb.WriteByte('\n')
	if n.Content != "" {
		sb.WriteString(" " + n.Content)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
	for i, action := range n.Actions {
		if i == m.notifActionCursor {
			sb.WriteString(m.styles.ToolName.Render(fmt.Sprintf(" > %s", action)))
		} else {
			sb.WriteString(m.styles.Muted.Render(fmt.Sprintf("   %s", action)))
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(m.styles.Muted.Render(" ↑↓ navigate  enter select  esc cancel  ctrl+j next"))
	return sb.String()
}

// openSessionPicker fetches sessions from the gateway and enters session picker mode.
func (m *App) openSessionPicker() tea.Cmd {
	if m.gw == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, err := m.gw.Call(ctx, gateway.MethodSessionList, nil)
		if err != nil {
			return nil
		}
		var sessions []persist.SessionMeta
		json.Unmarshal(data, &sessions)
		return sessionPickerMsg(sessions)
	}
}

// sessionPickerMsg carries fetched sessions into the Update loop.
type sessionPickerMsg []persist.SessionMeta

// renderSessionPicker renders the session list that replaces the editor.
// Item 0 is always "New session". Existing sessions follow.
func (m *App) renderSessionPicker() string {
	var sb strings.Builder
	sb.WriteString(m.styles.SectionTitle.Render(" Sessions"))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// First item: new session
	if m.sessionPickerCursor == 0 {
		sb.WriteString(m.styles.ToolName.Render(" > + New session"))
	} else {
		sb.WriteString(m.styles.Muted.Render("   + New session"))
	}
	sb.WriteByte('\n')

	for i, s := range m.sessionPickerItems {
		short := s.ID
		if len(short) > 8 {
			short = short[:8]
		}
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		if len(title) > 40 {
			title = title[:40] + "…"
		}
		age := time.Since(s.UpdatedAt).Truncate(time.Minute)
		label := fmt.Sprintf("%-8s %-12s %-42s %s ago", short, s.AgentID, title, age)
		if s.ProjectRoot != "" {
			proj := s.ProjectRoot
			if idx := strings.LastIndex(proj, "/"); idx >= 0 {
				proj = proj[idx+1:]
			}
			label += " [" + proj + "]"
		}
		if i+1 == m.sessionPickerCursor { // +1 because "New session" is item 0
			sb.WriteString(m.styles.ToolName.Render(" > " + label))
		} else {
			sb.WriteString(m.styles.Muted.Render("   " + label))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// openSessionTab creates or switches to a tab for the given session.
func (m *App) openSessionTab(sess persist.SessionMeta) tea.Cmd {
	// Check if tab already exists for this session
	for i, tab := range m.tabs {
		if tab.SessionID == sess.ID {
			m.activeTab = i
			return nil
		}
	}

	label := sess.Title
	if label == "" {
		label = sess.ID
	}
	if len(label) > 20 {
		label = label[:20]
	}

	m.tabs = append(m.tabs, Tab{
		ID:        sess.ID,
		Label:     label,
		AgentID:   sess.AgentID,
		SessionID: sess.ID,
		Closable:  true,
	})
	m.activeTab = len(m.tabs) - 1
	return nil
}

func (m *App) showStatusInline(tabID string) tea.Cmd {
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
		m.chatList(tabID).Add(chat.NewSystemMessage("sys-"+tabID, sb.String()))
		return nil
	}
}

func (m *App) showAgentsInline(tabID string) tea.Cmd {
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
		m.chatList(tabID).Add(chat.NewSystemMessage("sys-"+tabID, sb.String()))
		return nil
	}
}

func (m *App) fetchToolsIntoChat(tabID string) tea.Cmd {
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
		m.chatList(tabID).Add(chat.NewSystemMessage("sys-"+tabID, sb.String()))
		return nil
	}
}

// --- Tab management ---

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

func (m *App) fetchNotifications() tea.Cmd {
	if m.gw == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// List pending (unread) notifications — don't drain, because approval
		// notifications must stay available until the user actually responds.
		data, err := m.gw.Call(ctx, gateway.MethodNotificationList, nil)
		if err != nil {
			return nil
		}
		var notifs []agent.Notification
		if json.Unmarshal(data, &notifs) != nil || len(notifs) == 0 {
			return nil
		}
		return notificationListMsg(notifs)
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
