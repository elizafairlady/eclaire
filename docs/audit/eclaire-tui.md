# eclaire TUI Audit

Audited: 2026-04-09

## What eclaire Implements

| Component | Status |
|-----------|--------|
| Bubble Tea + Ultraviolet Draw | Working |
| Message types (User, Assistant, System) | Working |
| Tool rendering (11 types) | Working |
| Nested tool trees (agent items) | Working |
| Expand/collapse (per-tool + global ctrl+o) | Working |
| Scrollback (pgup/pgdn/home/end + follow mode) | Working |
| Markdown rendering (glamour, dark theme) | Working |
| Dialog overlay stack | Working |
| Tab navigation (ctrl+n/p, ctrl+w) | Working |
| Focus management (editor ↔ chat) | Working |
| Activity feed sidebar | Working |
| Sidebar (session info, tokens, tasks, todos) | Working |

## Crush Patterns Correctly Adopted

1. Ultraviolet Draw system (uv.Screen, uv.Rectangle, NewStyledString().Draw())
2. Layout system (layout.Vertical/Horizontal for responsive splits)
3. Dialog stack (CloseAction/NoneAction pattern)
4. Markdown rendering with glamour + caching by width
5. Tool status icons (pending/running/success/error)
6. Collapsed output (first N lines + "X lines hidden")
7. Nested tool trees (ASCII tree with depth tracking)

## Missing vs Crush

| Feature | Crush | eclaire | Impact |
|---------|-------|---------|--------|
| Item-level key events | KeyEventHandler returns (consumed, cmd) | Global switch only | Can't handle Enter-to-expand per item |
| Lazy list rendering | Per-item height, offsetIdx/offsetLine | Simple string render | Slow on huge chats |
| Animation framework | anim.Anim with gradient colors | No spinner state | Tools can't animate |
| Syntax highlighting | Chroma for diffs | Not present | Code diffs not colorized |
| Tool cancellation status | ToolStatusCanceled | Not present | Can't distinguish cancelled tools |
| Custom markdown themes | glamour.WithStyles(sty.Markdown) | glamour "dark" hardcoded | Can't match palette |
| Mouse text selection | Single/double/triple click, drag | Not present | Can't copy text |
| Completions popup | Full @ mention system | Not present | No file/command completion |
| Attachments UI | Full support | Not present | No file attachment preview |
| Permission status in tools | AwaitingPermission status | Not present | Permission state invisible |

## Specific Bugs

### Digit-to-rune conversion (tool_agent.go:92-94, tool_web.go:63)
```go
string(rune('0'+len(tools)-maxVisibleNestedTools))
```
Only works for 0-9. Breaks at 10+ tools. Should use `fmt.Sprintf`.

### Scroll offset display (app.go:575)
```go
content += m.styles.Muted.Render(fmt.Sprintf(" ↑ scroll offset %d ", cl.ScrollOffset()))
```
Appended to content string instead of positioned with Ultraviolet.

### Padding inconsistency
- User messages: PaddingLeft(1) via border
- Assistant messages: PaddingLeft(2) directly
- Some tools add `"  "` prefix manually (tool_file.go:71)

### Markdown width caching (markdown.go:28)
```go
if width < 20 { width = 20 }
```
Caches by original width, then clamps. Multiple renderers created for widths 1-20.

### Streaming text concatenation (app.go:561-568)
AssistantBody style applies padding, then `"\n"` separator breaks it. Should build as list items.

## Not Tested on Real TTY

The TUI has only been tested with in-memory screen buffers (`app_test.go`). No verification on actual terminal. Key handling, scrollback, dialog rendering all untested in real environment.

## What Needs to Happen

1. Add `KeyEventHandler` interface to items (return consumed bool)
2. Fix digit-formatting bugs (use fmt.Sprintf)
3. Test on real terminal with isatty check
4. Implement lazy list rendering with item-aware scrolling
5. Add animation state to tool rendering
6. Integrate custom markdown styles
7. Pass CWD to gateway on connect
8. Drain notifications on connect
9. Show main session as permanent tab
10. Wire approval dialog to live permission prompts

## Reference

- Crush TUI: `docs/reference/crush-tui.md`
- Crush chat: `docs/reference/crush-chat.md`
- Crush dialogs/styles: `docs/reference/crush-dialogs-styles.md`
