# Crush Chat Reference

Source: `internal/ui/model/chat.go`, `internal/ui/chat/`, `internal/ui/list/`

## Chat Struct (model/chat.go)

```go
type Chat struct {
    com      *common.Common
    list     *list.List
    idInxMap map[string]int          // Message ID -> list index

    // Animation visibility optimization
    pausedAnimations map[string]struct{}

    // Mouse state
    mouseDown     bool
    mouseDownItem int      // Item index where mouse was pressed
    mouseDownX    int      // X position (character offset)
    mouseDownY    int      // Y position (line offset)
    mouseDragItem int      // Current drag item index
    mouseDragX    int
    mouseDragY    int

    // Click tracking for double/triple clicks
    lastClickTime time.Time
    lastClickX    int
    lastClickY    int
    clickCount    int

    // Pending single click (delayed for double-click detection)
    pendingClickID int

    // Follow mode: auto-scroll to bottom on new messages
    follow bool
}
```

### Construction
`NewChat(com)` creates a Chat with:
- Gap of 1 between list items
- Two render callbacks registered: `applyHighlightRange` and `FocusedRenderCallback`
- mouseDownItem and mouseDragItem initialized to -1

### Key Methods

**Message Management:**
- `SetMessages(msgs ...chat.MessageItem)` -- replaces all messages, rebuilds ID map, registers nested tool IDs, scrolls to bottom
- `AppendMessages(msgs ...chat.MessageItem)` -- appends messages, updates ID map
- `UpdateNestedToolIDs(containerID string)` -- re-registers nested tool IDs after modification
- `RemoveMessage(id string)` -- removes by ID, rebuilds index map for items after removed index
- `ClearMessages()` -- clears everything
- `MessageItem(id string) chat.MessageItem` -- lookup by ID

**Scrolling:**
- `ScrollToBottom()` -- scrolls to bottom, enables follow mode
- `ScrollToTop()` -- scrolls to top, disables follow mode
- `ScrollBy(lines int)` -- scrolls by delta, updates follow mode based on AtBottom()
- `ScrollToSelected()` -- scrolls to keep selected item visible
- `ScrollToIndex(index int)` -- scrolls to specific index
- All scroll methods have `*AndAnimate()` variants that restart paused animations

**Follow Mode:**
- `Follow() bool` -- returns current follow state
- Enabled when: `ScrollToBottom()` called, or `ScrollBy` reaches bottom
- Disabled when: scrolling up, `ScrollToTop()`

**Selection:**
- `SetSelected(index int)` -- sets selected, skips non-Focusable items
- `SelectPrev()` / `SelectNext()` -- skip non-Focusable items
- `SelectFirst()` / `SelectLast()` -- first/last Focusable item
- `SelectFirstInView()` / `SelectLastInView()` -- within visible range
- `ToggleExpandedSelectedItem()` -- toggles Expandable items

**Animation:**
- `Animate(msg anim.StepMsg) tea.Cmd` -- only propagates to visible items. Pauses animations for off-screen items.
- `RestartPausedVisibleAnimations() tea.Cmd` -- restarts animations for items that scrolled back into view.

**Mouse Handling:**
- `HandleMouseDown(x, y int) (bool, tea.Cmd)` -- detects single/double/triple click. Single click is delayed via `DelayedClickMsg`. Double click selects word. Triple click selects line.
- `HandleDelayedClick(msg DelayedClickMsg) bool` -- executes delayed single-click (expansion) if no double-click occurred and no text selection.
- `HandleMouseUp(x, y int) bool` -- ends mouse-down state
- `HandleMouseDrag(x, y int) bool` -- updates drag position for selection
- `HandleKeyMsg(key tea.KeyMsg) (bool, tea.Cmd)` -- delegates to selected item's `KeyEventHandler`

**Text Selection / Highlighting:**
- `HasHighlight() bool` -- whether there is a non-zero selection
- `HighlightContent() string` -- extracts selected text across items
- `ClearMouse()` -- clears all mouse state
- `applyHighlightRange()` -- render callback that sets highlight positions on each item
- `getHighlightRange()` -- computes normalized start/end across items
- `selectWord()` -- UAX#29 word boundary detection
- `selectLine()` -- selects entire line

**Click Detection Constants:**
```go
doubleClickThreshold = 400 * time.Millisecond
clickTolerance       = 2  // x,y pixel tolerance
```

## List Struct (list/list.go)

Core lazy-rendered list component. Items rendered on demand during `Render()`.

```go
type List struct {
    width, height int      // Viewport size
    items         []Item   // All items
    gap           int      // Gap between items (0 = no gap)
    reverse       bool     // Reverse rendering order
    focused       bool
    selectedIdx   int      // -1 = no selection

    // Lazy scrolling state
    offsetIdx  int         // Index of first visible item
    offsetLine int         // Lines of offsetIdx item hidden above viewport (>= 0)

    renderCallbacks []func(idx, selectedIdx int, item Item) Item
}
```

### Lazy Rendering

The list does not render all items. `Render()` walks from `offsetIdx` forward, rendering items until the viewport height is filled. Each item is rendered by calling `item.Render(width)`.

`getItem(idx)` renders an item by:
1. Running all registered `renderCallbacks` (for highlight, focus)
2. Calling `item.Render(l.width)`
3. Trimming trailing newlines
4. Computing height from newline count

### Scrolling Implementation

`ScrollBy(lines int)`:
- Positive lines: scroll down. Adds to `offsetLine`. When `offsetLine >= currentItem.height`, advances `offsetIdx`. Clamps to `lastOffsetItem()`.
- Negative lines: scroll up. Subtracts from `offsetLine`. When `offsetLine < 0`, decrements `offsetIdx` and adds previous item's height.

`ScrollToBottom()`: computes the last offset position via `lastOffsetItem()` which walks backwards from the end, accumulating heights until exceeding viewport.

`VisibleItemIndices() (startIdx, endIdx int)`: walks from `offsetIdx` forward, accumulating heights until viewport is filled.

`ScrollToSelected()`: if selected is above visible range, sets `offsetIdx = selectedIdx`. If below, calculates offset so selected item is at bottom.

### Item Interfaces

```go
// Core interface - every item must implement
type Item interface {
    Render(width int) string
}

// Optional: raw rendering without styling (for highlight extraction)
type RawRenderable interface {
    RawRender(width int) string
}

// Optional: focus awareness
type Focusable interface {
    SetFocused(focused bool)
}

// Optional: text selection
type Highlightable interface {
    SetHighlight(startLine, startCol, endLine, endCol int)
    Highlight() (startLine, startCol, endLine, endCol int)
}

// Optional: mouse click handling
type MouseClickable interface {
    HandleMouseClick(btn ansi.MouseButton, x, y int) bool
}

// Built-in spacer
type SpacerItem struct { Height int }
```

`ItemIndexAtPosition(x, y int)` walks visible items to find which contains the given y coordinate, returning item index and offset within item.

## NestedToolContainer Interface

```go
type NestedToolContainer interface {
    NestedTools() []ToolMessageItem
    SetNestedTools(tools []ToolMessageItem)
    AddNestedTool(tool ToolMessageItem)
}
```

Implemented by `AgentToolMessageItem` and `AgenticFetchToolMessageItem`. When a nested tool is added, it is marked as compact mode. The container renders nested tools as a tree using `lipgloss/v2/tree`.

## AgentToolMessageItem

```go
type AgentToolMessageItem struct {
    *baseToolMessageItem
    nestedTools []ToolMessageItem
}
```

- Implements both `ToolMessageItem` and `NestedToolContainer`
- Animation dispatches to self or to nested tools by matching `msg.ID`
- Renders with `tree.Root(header)` and `childTools.Child(childView)` for tree structure
- Shows "Task" tag with prompt text, then nested tool tree

## Chat Message Interfaces

```go
// Core message item: renderable + identifiable
type MessageItem interface {
    list.Item
    list.RawRenderable
    Identifiable  // ID() string
}

// Animatable items
type Animatable interface {
    StartAnimation() tea.Cmd
    Animate(msg anim.StepMsg) tea.Cmd
}

// Expandable items (tool results, reasoning)
type Expandable interface {
    ToggleExpanded() bool  // Returns whether now expanded
}

// Key event handling (for focused items)
type KeyEventHandler interface {
    HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd)
}

// Tool message items
type ToolMessageItem interface {
    MessageItem
    ToolCall() message.ToolCall
    SetToolCall(tc message.ToolCall)
    SetResult(res *message.ToolResult)
    MessageID() string
    SetMessageID(id string)
    SetStatus(status ToolStatus)
    Status() ToolStatus
}

// Compact mode for nested tools
type Compactable interface {
    SetCompact(compact bool)
}
```

## Animation Framework (anim/anim.go)

```go
type StepMsg struct{ ID string }

type Settings struct {
    ID          string
    Size        int          // Number of cycling chars (default 10)
    Label       string       // Text label after animation
    LabelColor  color.Color
    GradColorA  color.Color  // Gradient start
    GradColorB  color.Color  // Gradient end
    CycleColors bool         // Whether gradient cycles
}

type Anim struct {
    width            int
    cyclingCharWidth int
    label            *csync.Slice[string]
    labelWidth       int
    labelColor       color.Color
    startTime        time.Time
    birthOffsets     []time.Duration   // Staggered entrance per char
    initialFrames    [][]string        // Pre-rendered initial frames
    initialized      atomic.Bool
    cyclingFrames    [][]string        // Pre-rendered cycling frames
    step             atomic.Int64      // Current frame
    ellipsisStep     atomic.Int64      // Ellipsis frame
    ellipsisFrames   *csync.Slice[string]
    id               string
}
```

Key constants:
- `fps = 20` (50ms per frame)
- `maxBirthOffset = 1 second` -- staggered entrance effect
- `ellipsisAnimSpeed = 8` frames per ellipsis change (400ms)
- `prerenderedFrames = 10` for non-cycling mode

Flow:
1. `New(opts)` pre-renders all frames (initial dots + cycling random chars with gradient colors). Results cached by settings hash.
2. `Start()` returns `Step()` command (a `tea.Tick` at fps rate)
3. `Animate(msg)` advances step counter, manages ellipsis, returns next `Step()` command
4. `Render()` builds string from pre-rendered frames based on current step and birth offsets

Available runes: `0123456789abcdefABCDEF~!@#$%^&*()+=_`
Ellipsis frames: `.`, `..`, `...`, `` (empty)

Animation uses `csync` atomics for thread-safe step tracking.

## Tool Message Item Types

All tool items share `baseToolMessageItem` which provides:
- `highlightableMessageItem` (text selection)
- `cachedMessageItem` (render caching)
- `focusableMessageItem` (focus state)
- `toolRenderer` (delegated rendering)
- `toolCall`, `result`, `messageID`, `status`
- `anim *anim.Anim` for spinning animation
- `spinningFunc SpinningFunc` for custom spinning logic

### Tool types (from chat/ files):

| File | Tool Type | Description |
|------|-----------|-------------|
| `agent.go` | `AgentToolMessageItem` | Sub-agent with nested tool tree |
| `agent.go` | `AgenticFetchToolMessageItem` | Agentic fetch with nested tools |
| `bash.go` | Bash tool | Command execution with output |
| `file.go` | File tools | Edit, Write, MultiEdit with diffs |
| `search.go` | Search tools | Grep, Glob, Read results |
| `fetch.go` | Fetch tool | URL fetch results |
| `diagnostics.go` | Diagnostics | LSP diagnostics display |
| `references.go` | References | Code references |
| `todos.go` | Todos | Todo list management |
| `mcp.go` | MCP tools | MCP tool calls |
| `docker_mcp.go` | Docker MCP | Docker MCP operations |
| `lsp_restart.go` | LSP Restart | LSP server restart |
| `generic.go` | Generic tool | Fallback for unknown tools |
| `user.go` | User message | User input display |
| `assistant.go` | Assistant message | AI response with markdown |

### ToolStatus enum:
```go
const (
    ToolStatusAwaitingPermission ToolStatus = iota
    ToolStatusRunning
    ToolStatusSuccess
    ToolStatusError
    ToolStatusCanceled
)
```

## Message Extraction

`ExtractMessageItems(sty, msg, toolResults)` converts a `message.Message` into display items:
- User messages -> `NewUserMessageItem`
- Assistant messages -> `NewAssistantMessageItem` (if has text/thinking/error) + one `NewToolMessageItem` per tool call
- Tool result messages linked by `BuildToolResultMap`

`ShouldRenderAssistantMessage(msg)` returns false only when the message has tool calls and no text/thinking/error content.

## Render Caching

```go
type cachedMessageItem struct {
    rendered string
    width    int
    height   int
}
```

- `getCachedRender(width)` returns `(string, height, ok)` if cached width matches
- `setCachedRender(rendered, width, height)` stores cache
- `clearCache()` resets (called when content changes)

## Constants

```go
MessageLeftPaddingTotal = 2   // Border + padding width consumed
maxTextWidth = 120            // Max text width for readability
responseContextHeight = 10    // Lines shown in tool output
toolBodyLeftPaddingTotal = 2  // Tool body padding
```
