# Crush Dialogs & Styles Reference

Source: `internal/ui/dialog/`, `internal/ui/styles/`, `internal/ui/common/`, `internal/ui/completions/`, `internal/ui/anim/`

## Dialog Interface

```go
type Dialog interface {
    ID() string
    HandleMsg(msg tea.Msg) Action
    Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor
}

type LoadingDialog interface {
    StartLoading() tea.Cmd
    StopLoading()
}

type Action any  // Dialog-specific return type
```

## Overlay Manager

```go
type Overlay struct {
    dialogs []Dialog  // Stack of dialogs (last = front/active)
}
```

Methods:
- `NewOverlay(dialogs ...Dialog)` -- constructor
- `HasDialogs() bool`
- `ContainsDialog(dialogID string) bool`
- `OpenDialog(dialog Dialog)` -- push to stack
- `CloseDialog(dialogID string)` -- remove by ID
- `CloseFrontDialog()` -- pop last
- `Dialog(dialogID string) Dialog` -- get by ID
- `DialogLast() Dialog` -- get front
- `BringToFront(dialogID string)` -- move to end of stack
- `Update(msg tea.Msg) tea.Msg` -- delegates to front dialog only
- `StartLoading() tea.Cmd` / `StopLoading()` -- delegates to front if LoadingDialog
- `Draw(scr, area) *tea.Cursor` -- draws all dialogs in stack order, returns last cursor

Drawing helpers:
- `DrawCenter(scr, area, view)` -- centers content
- `DrawCenterCursor(scr, area, view, cursor)` -- centers with cursor adjustment
- `DrawOnboarding(scr, area, view)` -- positions at bottom-left
- `DrawOnboardingCursor(scr, area, view, cursor)` -- bottom-left with cursor

## Dialog Types

19 dialog files representing 15+ dialog types:

| File | Dialog ID | Description |
|------|-----------|-------------|
| `actions.go` | actions | Action selection menu |
| `api_key_input.go` | api_key_input | API key entry dialog |
| `arguments.go` | arguments | MCP prompt arguments input |
| `commands.go` | CommandsID | Command palette (slash commands, custom commands, MCP prompts) |
| `filepicker.go` | filepicker | File/image picker for attachments |
| `models.go` | models | Model selection dialog |
| `models_item.go` | -- | Model list item rendering |
| `models_list.go` | -- | Model list component |
| `oauth.go` | oauth | OAuth flow dialog (base) |
| `oauth_copilot.go` | oauth_copilot | GitHub Copilot OAuth flow |
| `oauth_hyper.go` | oauth_hyper | Hyper OAuth flow |
| `permissions.go` | permissions | Permission request dialog (grant/deny/always) |
| `quit.go` | quit | Quit confirmation |
| `reasoning.go` | reasoning | Full reasoning content viewer |
| `sessions.go` | sessions | Session list/picker |
| `sessions_item.go` | -- | Session list item rendering |

### Dialog Constants

```go
defaultDialogMaxWidth = 70
defaultDialogHeight = 20
titleContentHeight = 1
inputContentHeight = 1

var CloseKey = key.NewBinding(key.WithKeys("esc", "alt+esc"))
```

## RenderContext (Dialog Rendering)

```go
type RenderContext struct {
    Styles                 *styles.Styles
    TitleStyle             lipgloss.Style
    ViewStyle              lipgloss.Style
    TitleGradientFromColor color.Color
    TitleGradientToColor   color.Color
    Width                  int
    Gap                    int
    Title                  string
    TitleInfo              string
    Parts                  []string
    Help                   string
    IsOnboarding           bool
}
```

`Render()` assembles:
1. Title with gradient (if set)
2. Content parts joined with gap lines
3. Help view at bottom
4. Wrapped in `ViewStyle` (unless onboarding)

`InputCursor(styles, cursor)` adjusts cursor position accounting for dialog frame (border, padding, margin).

## Styles Struct

The `Styles` struct in `styles/styles.go` is the central style registry. Organized by component:

### Top-Level Styles

```go
type Styles struct {
    WindowTooSmall lipgloss.Style

    // Text styles
    Base      lipgloss.Style
    Muted     lipgloss.Style
    HalfMuted lipgloss.Style
    Subtle    lipgloss.Style

    // Tags
    TagBase  lipgloss.Style
    TagError lipgloss.Style
    TagInfo  lipgloss.Style

    // Panels
    PanelMuted lipgloss.Style
    PanelBase  lipgloss.Style

    LineNumber lipgloss.Style
    FocusedMessageBorder lipgloss.Border

    // Tool call status icons
    ToolCallPending   lipgloss.Style
    ToolCallError     lipgloss.Style
    ToolCallSuccess   lipgloss.Style
    ToolCallCancelled lipgloss.Style
    EarlyStateMessage lipgloss.Style

    TextSelection lipgloss.Style

    // Markdown
    Markdown      ansi.StyleConfig
    PlainMarkdown ansi.StyleConfig

    // Inputs
    TextInput textinput.Styles
    TextArea  textarea.Styles

    Help       help.Styles
    Diff       diffview.Style
    FilePicker filepicker.Styles

    // Buttons
    ButtonFocus lipgloss.Style
    ButtonBlur  lipgloss.Style

    // Borders
    BorderFocus lipgloss.Style
    BorderBlur  lipgloss.Style

    // Editor prompt styles (6 variants: normal/yolo x focused/blurred)
    EditorPromptNormalFocused   lipgloss.Style
    EditorPromptNormalBlurred   lipgloss.Style
    EditorPromptYoloIconFocused lipgloss.Style
    EditorPromptYoloIconBlurred lipgloss.Style
    EditorPromptYoloDotsFocused lipgloss.Style
    EditorPromptYoloDotsBlurred lipgloss.Style

    RadioOn  lipgloss.Style
    RadioOff lipgloss.Style

    Background color.Color
```

### Color Palette

```go
    Primary       color.Color
    Secondary     color.Color
    Tertiary      color.Color
    BgBase        color.Color
    BgBaseLighter color.Color
    BgSubtle      color.Color
    BgOverlay     color.Color
    FgBase        color.Color
    FgMuted       color.Color
    FgHalfMuted   color.Color
    FgSubtle      color.Color
    Border        color.Color
    BorderColor   color.Color
    Error         color.Color
    Warning       color.Color
    Info          color.Color
    White         color.Color
    BlueLight     color.Color
    Blue          color.Color
    BlueDark      color.Color
    GreenLight    color.Color
    Green         color.Color
    GreenDark     color.Color
    Red           color.Color
    RedDark       color.Color
    Yellow        color.Color

    // Logo colors
    LogoFieldColor   color.Color
    LogoTitleColorA  color.Color
    LogoTitleColorB  color.Color
    LogoCharmColor   color.Color
    LogoVersionColor color.Color
```

### Sub-Style Groups

**Header:**
```go
    Header struct {
        Charm, Diagonals, Percentage, Keystroke,
        KeystrokeTip, WorkingDir, Separator lipgloss.Style
    }
```

**CompactDetails:**
```go
    CompactDetails struct { View, Version, Title lipgloss.Style }
```

**Section:**
```go
    Section struct { Title, Line lipgloss.Style }
```

**Initialize:**
```go
    Initialize struct { Header, Content, Accent lipgloss.Style }
```

**LSP:**
```go
    LSP struct {
        ErrorDiagnostic, WarningDiagnostic,
        HintDiagnostic, InfoDiagnostic lipgloss.Style
    }
```

**Files:**
```go
    Files struct { Path, Additions, Deletions lipgloss.Style }
```

**Chat.Message (18 styles):**
```go
    Chat struct {
        Message struct {
            UserBlurred, UserFocused,
            AssistantBlurred, AssistantFocused,
            NoContent, Thinking,
            ErrorTag, ErrorTitle, ErrorDetails,
            ToolCallFocused, ToolCallCompact, ToolCallBlurred,
            SectionHeader,
            ThinkingBox, ThinkingTruncationHint,
            ThinkingFooterTitle, ThinkingFooterDuration,
            AssistantInfoIcon, AssistantInfoModel,
            AssistantInfoProvider, AssistantInfoDuration lipgloss.Style
        }
    }
```

**Tool (40+ styles):**
```go
    Tool struct {
        // Status icons
        IconPending, IconSuccess, IconError, IconCancelled lipgloss.Style
        // Names
        NameNormal, NameNested lipgloss.Style
        // Parameters
        ParamMain, ParamKey lipgloss.Style
        // Content rendering
        ContentLine, ContentTruncation,
        ContentCodeLine, ContentCodeTruncation lipgloss.Style
        ContentCodeBg color.Color
        Body lipgloss.Style
        // Deprecated
        ContentBg, ContentText, ContentLineNumber lipgloss.Style
        // States
        StateWaiting, StateCancelled lipgloss.Style
        // Errors
        ErrorTag, ErrorMessage lipgloss.Style
        // Diffs
        DiffTruncation lipgloss.Style
        // Notes
        NoteTag, NoteMessage lipgloss.Style
        // Jobs (bash)
        JobIconPending, JobIconError, JobIconSuccess,
        JobToolName, JobAction, JobPID, JobDescription lipgloss.Style
        // Agent
        AgentTaskTag, AgentPrompt lipgloss.Style
        // Agentic fetch
        AgenticFetchPromptTag lipgloss.Style
        // Todos
        TodoRatio, TodoCompletedIcon, TodoInProgressIcon, TodoPendingIcon lipgloss.Style
        // MCP
        MCPName, MCPToolName, MCPArrow lipgloss.Style
        // Resources
        ResourceLoadedText, ResourceLoadedIndicator,
        ResourceName, ResourceSize, MediaType lipgloss.Style
        // Docker MCP
        DockerMCPActionAdd, DockerMCPActionDel lipgloss.Style
    }
```

**Dialog (25+ styles):**
```go
    Dialog struct {
        Title, TitleText, TitleError, TitleAccent lipgloss.Style
        View lipgloss.Style
        PrimaryText, SecondaryText lipgloss.Style
        HelpView lipgloss.Style
        Help struct {
            Ellipsis, ShortKey, ShortDesc, ShortSeparator,
            FullKey, FullDesc, FullSeparator lipgloss.Style
        }
        NormalItem, SelectedItem lipgloss.Style
        InputPrompt lipgloss.Style
        List lipgloss.Style
        Spinner lipgloss.Style
        ContentPanel lipgloss.Style
        ScrollbarThumb, ScrollbarTrack lipgloss.Style
        Arguments struct {
            Content, Description,
            InputLabelBlurred, InputLabelFocused,
            InputRequiredMarkBlurred, InputRequiredMarkFocused lipgloss.Style
        }
        Commands struct{}
        ImagePreview lipgloss.Style
        Sessions struct {
            DeletingView, DeletingItemFocused, DeletingItemBlurred,
            DeletingTitle, DeletingMessage lipgloss.Style
            DeletingTitleGradientFromColor, DeletingTitleGradientToColor color.Color
            RenamingView lipgloss.Style
            // ... more session styles
        }
    }
```

**Attachments, Completions, Pills, Scrollbar** sub-groups also present.

### Icon Constants

```go
CheckIcon   = "✓"    SpinnerIcon = "⋯"    LoadingIcon = "⟳"
ModelIcon   = "◇"    ArrowRightIcon = "→"
ToolPending = "●"    ToolSuccess = "✓"     ToolError   = "×"
RadioOn     = "◉"    RadioOff    = "○"
BorderThin  = "│"    BorderThick = "▌"
SectionSeparator = "─"
TodoCompletedIcon  = "✓"  TodoPendingIcon = "•"  TodoInProgressIcon = "→"
ImageIcon = "■"    TextIcon = "≡"
ScrollbarThumb = "┃"  ScrollbarTrack = "│"
LSPErrorIcon = "E"  LSPWarningIcon = "W"  LSPInfoIcon = "I"  LSPHintIcon = "H"
```

## Common Package

### Common Struct

```go
type Common struct {
    App    *app.App
    Styles *styles.Styles
}
```

- `Config()` returns `*config.Config`
- `Store()` returns `*config.ConfigStore`
- `DefaultCommon(app)` creates with `DefaultStyles()`

### Geometry Helpers

```go
func CenterRect(area uv.Rectangle, width, height int) uv.Rectangle
func BottomLeftRect(area uv.Rectangle, width, height int) uv.Rectangle
```

### Clipboard

```go
func CopyToClipboard(text, successMessage string) tea.Cmd
func CopyToClipboardWithCallback(text, successMessage string, callback tea.Cmd) tea.Cmd
```

Uses both OSC 52 (`tea.SetClipboard`) and native clipboard (`clipboard.WriteAll`) for maximum compatibility.

### Markdown Renderers

```go
func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer
func PlainMarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer
```

`MarkdownRenderer` uses `sty.Markdown` style config (colors, formatting).
`PlainMarkdownRenderer` uses `sty.PlainMarkdown` (no colors, structure only).

Both configured with word wrap at the given width.

### Other Common Files

- `elements.go` -- reusable UI elements (section headers, dialog titles with gradients)
- `highlight.go` -- syntax highlighting helpers
- `button.go` -- button rendering
- `capabilities.go` -- `Capabilities` struct tracking terminal features (focus events)
- `diff.go` -- diff display helpers
- `interface.go` -- shared interfaces
- `scrollbar.go` -- scrollbar rendering

Constants:
```go
MaxAttachmentSize = 5 * 1024 * 1024  // 5 MB
AllowedImageTypes = []string{".jpg", ".jpeg", ".png"}
```

## Completions System

### Completions Struct

```go
type Completions struct {
    width, height int
    open          bool
    query         string
    keyMap        KeyMap
    list          *list.FilterableList
    normalStyle   lipgloss.Style
    focusedStyle  lipgloss.Style
    matchStyle    lipgloss.Style
    allItems      []list.FilterableItem
    filtered      []list.FilterableItem
}
```

### Sizing

```go
minHeight = 1     maxHeight = 10
minWidth  = 10    maxWidth  = 100
```

### Priority Tiers

File completions are sorted by name match quality:
```go
tierExactName    = 0  // Exact filename or stem match
tierPrefixName   = 1  // Filename starts with query
tierPathSegment  = 2  // Path segment matches query
tierFallback     = 3  // Fuzzy match only
```

### Methods

- `Open(depth, limit int) tea.Cmd` -- async loads files + MCP resources
- `SetItems(files, resources)` -- sets items, opens popup, focuses list
- `Close()`
- `Filter(query string)` -- applies priority filter
- `Update(msg tea.KeyPressMsg) (tea.Msg, bool)` -- handles up/down/select/cancel
- `Render() string` -- renders the list
- `HasItems() bool`
- `IsOpen() bool`
- `Size() (width, height int)` -- visible dimensions

### Selection Messages

```go
type SelectionMsg[T any] struct {
    Value    T
    KeepOpen bool  // Insert without closing (shift+up/down)
}
type ClosedMsg struct{}
```

Value types: `FileCompletionValue` (Path) or `ResourceCompletionValue` (MCPName, URI, Title, MIMEType).

### Item Loading

`loadFiles(depth, limit)` uses `fsext.ListDirectory` to list project files.
`loadMCPResources()` iterates `mcp.Resources()` for MCP server resources.
Both run in parallel via `sync.WaitGroup`.

## Anim Struct

See crush-chat.md for full Anim documentation. Summary:

```go
type Anim struct {
    width, cyclingCharWidth int
    label        *csync.Slice[string]
    startTime    time.Time
    birthOffsets []time.Duration
    initialFrames, cyclingFrames [][]string
    step, ellipsisStep atomic.Int64
    ellipsisFrames *csync.Slice[string]
    id string
}
```

- Pre-rendered gradient frames cached by settings hash
- 20 FPS tick rate
- Staggered character entrance over 1 second
- Ellipsis animation (`.`, `..`, `...`, empty) at 400ms intervals
- Thread-safe via atomics
