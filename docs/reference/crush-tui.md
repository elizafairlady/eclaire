# Crush TUI Reference

Source: `internal/ui/model/ui.go`, `keys.go`, `filter.go`

## UI Struct

```go
type UI struct {
    com          *common.Common       // Shared styles + app reference
    session      *session.Session     // Current active session (nil when no session)
    sessionFiles []SessionFile        // Files tracked in current session

    sessionFileReads []string         // Read files while no session ID yet
    initialSessionID string           // Session to load on startup
    continueLastSession bool          // --continue flag

    lastUserMessageTime int64         // Timestamp of last user message

    width  int                        // Terminal width in cells
    height int                        // Terminal height in cells
    layout uiLayout                   // Computed layout rectangles

    isTransparent bool                // Transparent background mode

    focus uiFocusState                // Current focus: None/Editor/Main
    state uiState                     // Current state machine state

    keyMap KeyMap                     // All key bindings
    keyenh tea.KeyboardEnhancementsMsg // Terminal keyboard enhancement capabilities

    dialog *dialog.Overlay            // Stacked dialog manager
    status *Status                    // Status bar component

    isCanceling bool                  // Escape-once cancel tracking

    header *header                    // Header component

    sendProgressBar    bool           // Whether to send terminal progress bar updates
    progressBarEnabled bool           // Whether progress bar feature is enabled

    caps common.Capabilities          // Terminal capabilities (focus events, etc.)

    // Editor components
    textarea textarea.Model           // Bubble Tea textarea for prompt input

    // Attachment list
    attachments *attachments.Attachments

    readyPlaceholder   string         // Placeholder when idle
    workingPlaceholder string         // Placeholder when agent is busy

    // Completions state (@ mention popup)
    completions              *completions.Completions
    completionsOpen          bool
    completionsStartIndex    int
    completionsQuery         string
    completionsPositionStart image.Point // x,y where user typed '@'

    // Chat components
    chat *Chat                        // Chat message list

    // Onboarding state
    onboarding struct {
        yesInitializeSelected bool
    }

    // LSP states
    lspStates map[string]app.LSPClientInfo

    // MCP states
    mcpStates map[string]mcp.ClientInfo

    sidebarLogo string               // Cached sidebar logo

    // Notification state
    notifyBackend       notification.Backend
    notifyWindowFocused bool

    // Custom commands & MCP prompts
    customCommands []commands.CustomCommand
    mcpPrompts     []commands.MCPPrompt

    // Compact mode
    forceCompactMode bool             // User-toggled compact mode
    isCompact bool                    // Currently in compact layout
    detailsOpen bool                  // Details panel open in compact mode

    // Pills state (task pills bar)
    pillsExpanded      bool
    focusedPillSection pillSection
    promptQueue        int
    pillsView          string

    // Todo spinner
    todoSpinner    spinner.Model
    todoIsSpinning bool

    // Mouse highlighting
    lastClickTime time.Time

    // Prompt history (up/down navigation)
    promptHistory struct {
        messages []string
        index    int
        draft    string
    }
}
```

## uiLayout Struct

```go
type uiLayout struct {
    area           uv.Rectangle  // Overall available area
    header         uv.Rectangle  // Header (shown in some states)
    main           uv.Rectangle  // Main pane (chat, landing, configure)
    pills          uv.Rectangle  // Pills panel (task pills)
    editor         uv.Rectangle  // Editor pane (textarea + attachments)
    sidebar        uv.Rectangle  // Sidebar (session details, files, LSP)
    status         uv.Rectangle  // Status bar
    sessionDetails uv.Rectangle  // Session details overlay (compact mode)
}
```

## State Machine

Four states (`uiState`):

| State | Description |
|-------|-------------|
| `uiOnboarding` | First-run setup, model selection dialog |
| `uiInitialize` | Project initialization prompt (yes/no) |
| `uiLanding` | Configured, no active session, show logo + editor |
| `uiChat` | Active session with chat messages |

Transitions:
- `uiOnboarding` -> `uiLanding` (after model configured)
- `uiInitialize` -> `uiLanding` (after init decision)
- `uiLanding` -> `uiChat` (on first message or session load)
- `uiChat` -> `uiLanding` (on new session / ctrl+n)

`setState(state, focus)` changes state, updates layout. Going to `uiLanding` forces `isCompact = false`.

## Focus States

```go
type uiFocusState uint8
const (
    uiFocusNone   uiFocusState = iota  // No focus
    uiFocusEditor                       // Textarea focused
    uiFocusMain                         // Chat list focused
)
```

Tab cycles between `uiFocusEditor` and `uiFocusMain`.

## generateLayout()

Computes all `uiLayout` rectangles from terminal `width` and `height`.

Layout constants:
- `compactModeWidthBreakpoint = 120` -- auto-compact below this width
- `compactModeHeightBreakpoint = 30` -- auto-compact below this height
- `TextareaMaxHeight = 15`
- `TextareaMinHeight = 3`
- `editorHeightMargin = 2` -- for attachments row + bottom
- `sessionDetailsMaxHeight = 20`
- Sidebar width: 30

Layout varies by state:

**uiOnboarding / uiInitialize:**
```
header (4 lines)
------
main (remaining)
------
help (1 line)
```

**uiLanding:**
```
header (4 lines)
------
main (remaining)
------
editor
------
help (1 line)
```

**uiChat (compact mode):**
```
header (1 line)
------
main
------
pills (if expanded)
------
editor
------
help (1 line)
```

**uiChat (full mode):**
```
main              | sidebar
------            |
pills (if expanded)|
------            |
editor            |
------            |
help              |
```

Sidebar placed on the right via horizontal split. Main pane gets the left portion.

## updateLayoutAndSize() / updateSize()

Called on:
- `tea.WindowSizeMsg` -- terminal resize
- `setState()` -- state change
- Session load
- Todo state changes
- Queue size changes

Recomputes layout, resizes textarea and chat components.

## Draw() Method

`Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor`

Uses Ultraviolet Draw API (not Bubble Tea View). Renders by state:

1. **uiOnboarding** -- header + main area for onboarding content
2. **uiInitialize** -- header + init prompt (yes/no buttons)
3. **uiLanding** -- logo in main area + editor at bottom + status
4. **uiChat** -- chat in main, editor below, optional sidebar, optional pills, optional session details overlay, optional progress bar

The dialog overlay draws on top of everything via `m.dialog.Draw(scr, area)`.

Cursor returned from textarea or dialog (whichever has focus).

## KeyMap Struct

```go
type KeyMap struct {
    Editor struct {
        AddFile             key.Binding  // "/"
        SendMessage         key.Binding  // "enter"
        OpenEditor          key.Binding  // "ctrl+o"
        Newline             key.Binding  // "shift+enter", "ctrl+j"
        AddImage            key.Binding  // "ctrl+f"
        PasteImage          key.Binding  // "ctrl+v"
        MentionFile         key.Binding  // "@"
        Commands            key.Binding  // "/"
        AttachmentDeleteMode key.Binding // "ctrl+r"
        Escape              key.Binding  // "esc"
        DeleteAllAttachments key.Binding // "r" (in delete mode: ctrl+r+r)
        HistoryPrev         key.Binding  // "up"
        HistoryNext         key.Binding  // "down"
    }

    Chat struct {
        NewSession     key.Binding  // "ctrl+n"
        AddAttachment  key.Binding  // "ctrl+f"
        Cancel         key.Binding  // "esc"
        Tab            key.Binding  // "tab"
        Details        key.Binding  // "ctrl+d"
        TogglePills    key.Binding  // "ctrl+t", "ctrl+space"
        PillLeft       key.Binding  // "left"
        PillRight      key.Binding  // "right"
        Down           key.Binding  // "down", "ctrl+j", "j"
        Up             key.Binding  // "up", "ctrl+k", "k"
        UpDown         key.Binding  // "up", "down"
        DownOneItem    key.Binding  // "shift+down", "J"
        UpOneItem      key.Binding  // "shift+up", "K"
        UpDownOneItem  key.Binding  // "shift+up", "shift+down"
        PageDown       key.Binding  // "pgdown", " ", "f"
        PageUp         key.Binding  // "pgup", "b"
        HalfPageDown   key.Binding  // "d"
        HalfPageUp     key.Binding  // "u"
        Home           key.Binding  // "g", "home"
        End            key.Binding  // "G", "end"
        Copy           key.Binding  // "c", "y", "C", "Y"
        ClearHighlight key.Binding  // "esc"
        Expand         key.Binding  // "space"
    }

    Initialize struct {
        Yes    key.Binding  // "y", "Y"
        No     key.Binding  // "n", "N", "esc"
        Enter  key.Binding  // "enter"
        Switch key.Binding  // "left", "right", "tab"
    }

    // Global
    Quit     key.Binding  // "ctrl+c"
    Help     key.Binding  // "ctrl+g"
    Commands key.Binding  // "ctrl+p"
    Models   key.Binding  // "ctrl+m", "ctrl+l"
    Suspend  key.Binding  // "ctrl+z"
    Sessions key.Binding  // "ctrl+s"
    Tab      key.Binding  // "tab"
}
```

When terminal supports key disambiguation (`tea.KeyboardEnhancementsMsg`), the Models help text changes to "ctrl+m" and Newline changes to "shift+enter".

## Mouse Event Throttling

```go
// filter.go
var lastMouseEvent time.Time

func MouseEventFilter(m tea.Model, msg tea.Msg) tea.Msg {
    switch msg.(type) {
    case tea.MouseWheelMsg, tea.MouseMotionMsg:
        now := time.Now()
        if now.Sub(lastMouseEvent) < 15*time.Millisecond {
            return nil  // Drop event
        }
        lastMouseEvent = now
    }
    return msg
}
```

Registered as a Bubble Tea filter. Drops mouse wheel and motion events that arrive within 15ms of each other to prevent trackpad flooding.

## Constants

```go
MouseScrollThreshold = 5           // Lines per mouse wheel event
compactModeWidthBreakpoint  = 120  // Auto-compact below this width
compactModeHeightBreakpoint = 30   // Auto-compact below this height
pasteLinesThreshold = 10           // Pasted text with >10 newlines -> attachment
pasteColsThreshold = 1000          // Pasted text with >1000 cols -> attachment
sessionDetailsMaxHeight = 20
TextareaMaxHeight = 15
TextareaMinHeight = 3
editorHeightMargin = 2
```

## Internal Message Types

```go
openEditorMsg struct{ Text string }
cancelTimerExpiredMsg struct{}
userCommandsLoadedMsg struct{ Commands []commands.CustomCommand }
mcpPromptsLoadedMsg struct{ Prompts []commands.MCPPrompt }
mcpStateChangedMsg struct{ states map[string]mcp.ClientInfo }
sendMessageMsg struct{ Content string; Attachments []message.Attachment }
closeDialogMsg struct{}
copyChatHighlightMsg struct{}
sessionFilesUpdatesMsg struct{ sessionFiles []SessionFile }
```

## Update() Flow

The Update method handles (in order of the switch):
1. Queue size polling for prompt queue display
2. Terminal capabilities updates
3. Environment queries (`tea.EnvMsg`)
4. Focus/blur events for notifications
5. Agent notifications
6. Session load events
7. Session file updates
8. Send message events
9. Custom command / MCP prompt loading
10. Prompt history loading
11. Dialog close events
12. Session pubsub events (create/update/delete)
13. Message pubsub events (create/update/delete, including child session handling)
14. History file events
15. LSP events
16. MCP events (state changed, prompts list, tools list, resources list)
17. Permission request events (open dialog)
18. Permission notification events
19. Cancel timer expiry
20. Terminal version detection (for progress bar)
21. Window resize
22. Keyboard enhancements
23. Copy highlight
24. Delayed click (double-click detection)
25. Mouse click/motion/release/wheel
26. Animation step messages
