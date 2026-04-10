# Crush Overview Reference

Source: `github.com/charmbracelet/crush@v0.54.0`

## Stats

- Entry point: `main.go` (calls `internal/cmd.Execute()`)
- Optional pprof server on `CRUSH_PROFILE` env var

## Package Structure

### Top Level
- `main.go` -- entry point, optional pprof

### internal/app
- `app.go` -- `App` struct wires together all services. Fields: `Sessions`, `Messages`, `History`, `Permissions`, `FileTracker`, `AgentCoordinator`, `LSPManager`, config store, event channels, global context, cleanup funcs, `agentNotifications` broker.
- `provider.go` -- model provider resolution
- `lsp_events.go` -- LSP event types and state tracking

### internal/agent
- `coordinator.go` -- `Coordinator` interface and `coordinator` struct. Orchestrates agent runs: model selection, OAuth refresh, provider options merging, tool building, MCP integration.
- `agent.go` -- `SessionAgent` interface and `sessionAgent` struct. Core agent loop: message queuing, fantasy agent streaming, title generation, auto-summarization, tool result handling.
- `agent_tool.go` -- sub-agent tool (agent-as-a-tool)
- `agentic_fetch_tool.go` -- agentic web fetch tool
- `prompts.go` -- system prompt templates
- `event.go` -- agent event tracking
- `errors.go` -- agent error types
- `loop_detection.go` -- loop detection logic
- `notify/notify.go` -- agent notification types
- `prompt/prompt.go` -- prompt builder
- `tools/` -- 25+ built-in tools (bash, edit, view, grep, glob, ls, write, multiedit, fetch, search, web_fetch, web_search, todos, diagnostics, references, download, job_kill, job_output, lsp_restart, safe, rg, sourcegraph, mcp-tools, list_mcp_resources, read_mcp_resource)
- `tools/mcp/` -- MCP client init, tools bridge, prompts, resources
- `hyper/provider.go` -- Hyper provider adapter

### internal/session
- `session.go` -- `Session` struct (ID, ParentSessionID, Title, MessageCount, PromptTokens, CompletionTokens, SummaryMessageID, Cost, Todos, CreatedAt, UpdatedAt). `Service` interface with `Create`, `CreateTitleSession`, `CreateTaskSession`, `Get`, `GetLast`, `List`, `Save`, `UpdateTitleAndUsage`, `Rename`, `Delete`, plus agent tool session ID management. SQLite-backed via sqlc. Pubsub on CRUD events.

### internal/message
- `message.go` -- `Message` struct (ID, Role, SessionID, Parts, Model, Provider, timestamps, IsSummaryMessage). `Service` interface with CRUD. JSON serialization of typed `ContentPart` variants via discriminated union.
- `content.go` -- `ContentPart` interface and 7 types: `ReasoningContent`, `TextContent`, `ImageURLContent`, `BinaryContent`, `ToolCall`, `ToolResult`, `Finish`. Conversion to fantasy `Message` types. Incremental mutation methods (AppendContent, AddToolCall, etc.).
- `attachment.go` -- `Attachment` struct with IsText/IsImage helpers.

### internal/permission
- `permission.go` -- `Service` interface (Request/Grant/Deny/GrantPersistent/AutoApproveSession/SetSkipRequests). Blocking request flow via pending channels. Session-scoped persistent permissions. Auto-approve per session. Allowlist by tool:action.

### internal/history
- `file.go` -- `File` struct (ID, SessionID, Path, Content, Version, timestamps). `Service` interface for file versioning with auto-increment, retry on UNIQUE conflicts. SQLite-backed.

### internal/filetracker
- `service.go` -- `Service` interface (RecordRead, LastReadTime, ListReadFiles). Tracks which files have been read in each session for staleness detection.

### internal/pubsub
- `broker.go` -- Generic `Broker[T]` with lock-free fan-out, buffered channels (64), context-scoped subscriptions, non-blocking publish (drops on full).
- `events.go` -- `Event[T]` with `EventType` (created/updated/deleted), `Subscriber[T]`, `Publisher[T]` interfaces.

### internal/config
- YAML config, global+project merge, ConfigStore

### internal/db
- SQLite via sqlc, migrations

### internal/lsp
- LSP client manager, lazy initialization

### internal/ui/model
- `ui.go` -- main UI model (~2700 lines)
- `chat.go` -- Chat model wrapping list
- `keys.go` -- KeyMap
- `filter.go` -- mouse event throttling

### internal/ui/chat
- 16 files for message items: agent, assistant, bash, diagnostics, docker_mcp, fetch, file, generic, lsp_restart, mcp, messages, references, search, todos, tools, user

### internal/ui/list
- `list.go` -- lazy-rendered `List` with offset-based scrolling
- `item.go` -- `Item`, `Focusable`, `Highlightable`, `MouseClickable` interfaces
- `focus.go`, `highlight.go`, `filterable.go`

### internal/ui/dialog
- 19 dialog files: dialog.go (Overlay), common.go (RenderContext), actions, api_key_input, arguments, commands, filepicker, models, oauth, oauth_copilot, oauth_hyper, permissions, quit, reasoning, sessions

### internal/ui/styles
- `styles.go` -- massive `Styles` struct with 50+ sub-style groups
- `grad.go` -- gradient utilities

### internal/ui/common
- `common.go` -- `Common` struct (App + Styles), centering helpers, clipboard
- `markdown.go` -- `MarkdownRenderer` and `PlainMarkdownRenderer` via glamour
- `elements.go`, `highlight.go`, `button.go`, `capabilities.go`, `diff.go`, `interface.go`, `scrollbar.go`

### internal/ui/completions
- `completions.go` -- `Completions` struct for file/MCP resource mention popup
- `item.go`, `keys.go`

### internal/ui/anim
- `anim.go` -- `Anim` struct for gradient cycling character animation

### Other UI packages
- `internal/ui/attachments` -- attachment list rendering
- `internal/ui/diffview` -- split/unified diff viewer
- `internal/ui/image` -- terminal image rendering
- `internal/ui/logo` -- logo rendering
- `internal/ui/notification` -- native notification backend
- `internal/ui/util` -- error/info reporting

### Other internal packages
- `internal/cmd` -- Cobra CLI commands
- `internal/commands` -- custom command loading
- `internal/csync` -- thread-safe primitives (`Value[T]`, `Slice[T]`, `Map[K,V]`)
- `internal/diff` -- diff computation
- `internal/env` -- environment detection
- `internal/event` -- telemetry event tracking
- `internal/filepathext` -- path utilities
- `internal/format` -- formatting helpers
- `internal/fsext` -- filesystem extensions
- `internal/home` -- home directory resolution
- `internal/log` -- structured logging
- `internal/oauth` -- OAuth flows (copilot, hyper)
- `internal/projects` -- project detection
- `internal/shell` -- shell execution
- `internal/skills` -- skill definitions
- `internal/stringext` -- string utilities
- `internal/update` -- version update checking
- `internal/version` -- version info

## Layered Architecture

```
TUI (internal/ui/model) -- Ultraviolet Draw, Bubble Tea v2
  |
App (internal/app) -- wires services, manages lifecycle
  |
Agent (internal/agent) -- coordinator, session agent, fantasy integration
  |
Services -- session, message, history, permission, filetracker (all SQLite-backed)
  |
DB (internal/db) -- sqlc-generated queries, SQLite
```

## Data Flow

### Message Sending
1. User types in textarea, presses Enter
2. `UI.sendMessage()` creates/resumes session
3. `AgentCoordinator.Run()` is called with sessionID + prompt
4. `sessionAgent.Run()` queues if busy, otherwise begins agent loop
5. Creates user message via `message.Service.Create()`
6. Builds fantasy `Agent` with tools and system prompt
7. `agent.Stream()` calls LLM via fantasy, fires callbacks:
   - `OnTextDelta` -> `message.Update()` -> pubsub event -> UI updates chat item
   - `OnToolCall` -> `message.Update()` -> UI adds tool item
   - `OnToolResult` -> `message.Create()` (tool role) -> UI updates tool item
8. Agent loop continues until no more tool calls
9. Title generated async in background

### Tool Execution
1. LLM emits tool call in streaming response
2. Fantasy dispatches to matching `AgentTool.Run()`
3. Tool executes (bash, file edit, etc.)
4. If permission needed: `permission.Service.Request()` blocks on channel
5. UI receives permission pubsub event, opens dialog
6. User grants/denies, response sent back via channel
7. Tool result returned to fantasy, which feeds it back to LLM

### LSP Integration
- Lazy LSP: `lsp.Manager` starts LSP servers on-demand when files are opened
- Diagnostics tool reads LSP diagnostics for the session's files
- LSP state changes published via pubsub, UI updates status indicators

## Key Design Decisions

### Split Models
- `coordinator` maintains `largeModel` and `smallModel` (both `csync.Value[Model]`)
- Large model used for main agent loop
- Small model used for title generation and summarization
- Models refreshed before each run via `UpdateModels()`

### Session Queuing
- `sessionAgent.messageQueue` is `csync.Map[string, []SessionAgentCall]`
- If `IsSessionBusy()`, new prompts are queued instead of rejected
- Queued prompts drained in `PrepareStep` callback, injected as additional user messages
- UI shows queue count in pills

### Permission as Service
- `permission.Service` is a standalone service with pubsub
- Request blocks the tool goroutine on a channel
- UI subscribes to permission events, opens dialog
- Grant/Deny sends response through the channel
- `GrantPersistent` remembers tool+action+session+path for auto-approval
- `AutoApproveSession` blanket-approves all tools for a session
- `SetSkipRequests(true)` enables YOLO mode

### Pubsub Decoupling
- Generic `Broker[T]` with typed events
- Services publish CRUD events (created/updated/deleted)
- UI subscribes to service events via `pubsub.Subscribe(ctx)`
- Bubble Tea converts channel events to `tea.Msg` via `tea.ListenFor`
- Non-blocking publish: if subscriber channel is full, event is dropped

### Thread-Safe Primitives
- `csync.Value[T]` -- atomic get/set for any type (mutex-based)
- `csync.Slice[T]` -- thread-safe slice with Copy, Append, Get, Seq2
- `csync.Map[K,V]` -- thread-safe map with Get/Set/Del
- Used throughout agent to allow concurrent model/tool updates while agent loop runs

### Lazy LSP
- LSP servers started only when files are first opened in a session
- `lsp.Manager` tracks active servers
- State changes published via pubsub

### File Versioning
- `history.Service` tracks file versions per session
- Auto-incrementing version numbers
- Transaction with retry on UNIQUE constraint conflicts (up to 3 retries)
- Used for undo/diff in file editing tools

### Render Caching
- `cachedMessageItem` struct caches rendered output by width
- Avoids re-rendering unchanged messages on every frame
- Cache cleared when message content changes (tool results arrive, etc.)
- `getCachedRender(width)` returns cached string if width matches
