package styles

import (
	"charm.land/lipgloss/v2"
)

// Icons.
const (
	ToolPending  = "●"
	ToolSuccess  = "✓"
	ToolError    = "×"
	SpinnerIcon  = "⋯"
	ArrowRight   = "→"
	RadioOn      = "◉"
	RadioOff     = "○"
	Diag         = "╱"
	BorderThick  = "▌"
	TodoPending  = "•"
	TodoActive   = "→"
	TodoDone     = "✓"
)

// Color palette — warm dark theme inspired by Crush's charmtone.
var (
	// Backgrounds
	BgBase    = lipgloss.Color("#1e1e2e")
	BgSurface = lipgloss.Color("#313244")
	BgOverlay = lipgloss.Color("#45475a")

	// Foregrounds
	FgBase    = lipgloss.Color("#cdd6f4")
	FgMuted   = lipgloss.Color("#6c7086")
	FgSubtle  = lipgloss.Color("#a6adc8")

	// Accents
	Primary   = lipgloss.Color("#cba6f7") // purple
	Secondary = lipgloss.Color("#f9e2af") // yellow
	Blue      = lipgloss.Color("#89b4fa")
	Green     = lipgloss.Color("#a6e3a1")
	GreenDark = lipgloss.Color("#74c7ab")
	Red       = lipgloss.Color("#f38ba8")
	RedDark   = lipgloss.Color("#e06c75")
	Yellow    = lipgloss.Color("#f9e2af")

	// Borders
	BorderColor = lipgloss.Color("#45475a")
	BorderFocus = lipgloss.Color("#cba6f7")
)

// Styles holds all application styles.
type Styles struct {
	// Header
	Logo      lipgloss.Style
	LogoDiag  lipgloss.Style
	HeaderSep lipgloss.Style

	// Tabs
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	// Dashboard sections
	SectionTitle lipgloss.Style
	SectionHint  lipgloss.Style
	SelectorOn   lipgloss.Style
	SelectorOff  lipgloss.Style
	ConfigKey    lipgloss.Style
	ConfigVal    lipgloss.Style

	// Agent status
	AgentRunning lipgloss.Style
	AgentIdle    lipgloss.Style
	AgentWaiting lipgloss.Style
	AgentError   lipgloss.Style

	// Chat messages
	UserBorder    lipgloss.Style
	UserLabel     lipgloss.Style
	AssistantBody lipgloss.Style

	// Tool calls
	ToolPendingIcon lipgloss.Style
	ToolSuccessIcon lipgloss.Style
	ToolErrorIcon   lipgloss.Style
	ToolName        lipgloss.Style
	ToolBody        lipgloss.Style
	ToolParam       lipgloss.Style

	// System messages
	SystemMsg lipgloss.Style

	// Editor
	EditorBorder  lipgloss.Style
	EditorPrompt  lipgloss.Style
	EditorBlurred lipgloss.Style

	// Status bar
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
	StatusOK lipgloss.Style

	// Activity feed
	FeedTime  lipgloss.Style
	FeedAgent lipgloss.Style
	FeedMsg   lipgloss.Style

	// Scrollbar
	Muted lipgloss.Style
}

// Default returns the default style set.
func Default() Styles {
	return Styles{
		// Header
		Logo:      lipgloss.NewStyle().Foreground(Primary).Bold(true),
		LogoDiag:  lipgloss.NewStyle().Foreground(BorderColor),
		HeaderSep: lipgloss.NewStyle().Foreground(BorderColor),

		// Tabs
		TabActive: lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true).
			Padding(0, 1),
		TabInactive: lipgloss.NewStyle().
			Foreground(FgMuted).
			Padding(0, 1),

		// Dashboard sections
		SectionTitle: lipgloss.NewStyle().
			Foreground(FgBase).
			Bold(true).
			MarginBottom(1),
		SectionHint: lipgloss.NewStyle().
			Foreground(FgMuted).
			Italic(true),
		SelectorOn: lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true),
		SelectorOff: lipgloss.NewStyle().
			Foreground(FgMuted),
		ConfigKey: lipgloss.NewStyle().
			Foreground(FgMuted).
			Width(12),
		ConfigVal: lipgloss.NewStyle().
			Foreground(Blue),

		// Agent status
		AgentRunning: lipgloss.NewStyle().Foreground(Green),
		AgentIdle:    lipgloss.NewStyle().Foreground(FgMuted),
		AgentWaiting: lipgloss.NewStyle().Foreground(Yellow),
		AgentError:   lipgloss.NewStyle().Foreground(Red),

		// Chat messages — user gets left border like Crush
		UserBorder: lipgloss.NewStyle().
			BorderStyle(lipgloss.Border{Left: BorderThick}).
			BorderLeft(true).
			BorderForeground(Primary).
			PaddingLeft(1),
		UserLabel: lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true),
		AssistantBody: lipgloss.NewStyle().
			PaddingLeft(2),

		// Tool calls
		ToolPendingIcon: lipgloss.NewStyle().Foreground(GreenDark),
		ToolSuccessIcon: lipgloss.NewStyle().Foreground(Green),
		ToolErrorIcon:   lipgloss.NewStyle().Foreground(RedDark),
		ToolName:        lipgloss.NewStyle().Foreground(Blue),
		ToolBody: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(FgSubtle),
		ToolParam: lipgloss.NewStyle().
			Foreground(FgMuted),

		// System
		SystemMsg: lipgloss.NewStyle().
			Foreground(FgMuted).
			Italic(true).
			PaddingLeft(2),

		// Editor
		EditorBorder: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderFocus).
			Padding(0, 1),
		EditorPrompt: lipgloss.NewStyle().
			Foreground(GreenDark).
			Bold(true),
		EditorBlurred: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(BorderColor).
			Padding(0, 1),

		// Status bar
		HelpKey: lipgloss.NewStyle().
			Foreground(FgSubtle).
			Bold(true),
		HelpDesc: lipgloss.NewStyle().
			Foreground(FgMuted),
		StatusOK: lipgloss.NewStyle().
			Foreground(Green),

		// Activity feed
		FeedTime:  lipgloss.NewStyle().Foreground(FgMuted),
		FeedAgent: lipgloss.NewStyle().Foreground(Blue),
		FeedMsg:   lipgloss.NewStyle().Foreground(FgSubtle),

		// Scrollbar
		Muted: lipgloss.NewStyle().Foreground(FgMuted),
	}
}
