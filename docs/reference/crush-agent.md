# Crush Agent Reference

Source: `internal/agent/`

## Coordinator Interface

```go
type Coordinator interface {
    Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
    Cancel(sessionID string)
    CancelAll()
    IsSessionBusy(sessionID string) bool
    IsBusy() bool
    QueuedPrompts(sessionID string) int
    QueuedPromptsList(sessionID string) []string
    ClearQueue(sessionID string)
    Summarize(context.Context, string) error
    Model() Model
    UpdateModels(ctx context.Context) error
}
```

## coordinator Struct

```go
type coordinator struct {
    cfg         *config.ConfigStore
    sessions    session.Service
    messages    message.Service
    permissions permission.Service
    history     history.Service
    filetracker filetracker.Service
    lspManager  *lsp.Manager
    notify      pubsub.Publisher[notify.Notification]

    currentAgent SessionAgent
    agents       map[string]SessionAgent

    readyWg errgroup.Group
}
```

### NewCoordinator

Takes all service dependencies. On construction:
1. Loads coder agent config from `cfg.Config().Agents[config.AgentCoder]`
2. Builds coder system prompt via `coderPrompt()`
3. Calls `buildAgent()` to create the `SessionAgent`
4. Stores as `currentAgent` and in `agents` map

### Run Flow

1. Waits for `readyWg` (background initialization)
2. Refreshes models via `UpdateModels(ctx)`
3. Extracts model config: max tokens, image support
4. Filters out image attachments if model doesn't support images
5. Merges provider options (catwalk defaults + provider config + model config)
6. Refreshes OAuth tokens if expired
7. Calls `currentAgent.Run()`
8. On 401 error: refreshes OAuth token or API key template, retries once

### buildAgent

1. Calls `buildAgentModels()` for large and small models
2. Creates `SessionAgent` via `NewSessionAgent()` with all options
3. Starts background tool building (`buildTools` in goroutine via `readyWg`)
4. Builds tools from: built-in tools + MCP tools
5. Sets system prompt on the agent

### Provider Options Merging

`getProviderOptions(model, providerCfg)` merges options from three layers (lowest to highest priority):
1. `model.CatwalkCfg.Options.ProviderOptions` (catwalk defaults)
2. `providerCfg.ProviderOptions` (provider config)
3. `model.ModelCfg.ProviderOptions` (user model config)

Then applies provider-specific reasoning/thinking configuration based on provider type:
- OpenAI: `reasoning_effort`, responses reasoning summary
- Anthropic: `effort` or `thinking` with budget
- OpenRouter: `reasoning.enabled` + `reasoning.effort`
- Google: `thinking_config` with budget or level
- Vercel: `reasoning.enabled` + `reasoning.effort`

### mergeCallOptions

Returns merged: `ProviderOptions`, `Temperature`, `TopP`, `TopK`, `FrequencyPenalty`, `PresencePenalty` -- each from model config with catwalk fallback.

## SessionAgent Interface

```go
type SessionAgent interface {
    Run(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)
    SetModels(large Model, small Model)
    SetTools(tools []fantasy.AgentTool)
    SetSystemPrompt(systemPrompt string)
    Cancel(sessionID string)
    CancelAll()
    IsSessionBusy(sessionID string) bool
    IsBusy() bool
    QueuedPrompts(sessionID string) int
    QueuedPromptsList(sessionID string) []string
    ClearQueue(sessionID string)
    Summarize(context.Context, string, fantasy.ProviderOptions) error
    Model() Model
}
```

## sessionAgent Struct

```go
type sessionAgent struct {
    largeModel         *csync.Value[Model]
    smallModel         *csync.Value[Model]
    systemPromptPrefix *csync.Value[string]
    systemPrompt       *csync.Value[string]
    tools              *csync.Slice[fantasy.AgentTool]

    isSubAgent           bool
    sessions             session.Service
    messages             message.Service
    disableAutoSummarize bool
    isYolo               bool
    notify               pubsub.Publisher[notify.Notification]

    messageQueue   *csync.Map[string, []SessionAgentCall]
    activeRequests *csync.Map[string, context.CancelFunc]
}
```

All mutable state wrapped in `csync` types for thread-safe access from concurrent `SetModels`/`SetTools` calls while the agent loop runs.

## Model Type

```go
type Model struct {
    Model      fantasy.LanguageModel    // Fantasy LLM interface
    CatwalkCfg catwalk.Model           // Model metadata (capabilities, defaults)
    ModelCfg   config.SelectedModel    // User config (provider, model ID, options)
}
```

## SessionAgentCall

```go
type SessionAgentCall struct {
    SessionID        string
    Prompt           string
    ProviderOptions  fantasy.ProviderOptions
    Attachments      []message.Attachment
    MaxOutputTokens  int64
    Temperature      *float64
    TopP             *float64
    TopK             *int64
    FrequencyPenalty *float64
    PresencePenalty  *float64
    NonInteractive   bool
}
```

## SessionAgentOptions

```go
type SessionAgentOptions struct {
    LargeModel           Model
    SmallModel           Model
    SystemPromptPrefix   string
    SystemPrompt         string
    IsSubAgent           bool
    DisableAutoSummarize bool
    IsYolo               bool
    Sessions             session.Service
    Messages             message.Service
    Tools                []fantasy.AgentTool
    Notify               pubsub.Publisher[notify.Notification]
}
```

## Run Flow (sessionAgent)

1. Validates prompt and sessionID (returns `ErrEmptyPrompt` / `ErrSessionMissing`)
2. If busy for this session: **queues the call** in `messageQueue` and returns nil
3. Copies mutable fields under lock: tools, model, system prompt, prefix
4. Appends MCP instructions from connected servers
5. Adds Anthropic cache control to last tool
6. Creates `fantasy.Agent` with model, system prompt, tools
7. Gets current session and existing messages
8. If first message: spawns background goroutine for title generation
9. Creates user message in DB
10. Sets up cancellable context, registers in `activeRequests`
11. Prepares prompt history and file attachments
12. Calls `agent.Stream()` with extensive callbacks:

### Streaming Callbacks

**PrepareStep:**
- Clears provider options from messages
- Uses latest tools (allows hot-reload of MCP tools)
- Drains queued prompts from `messageQueue`, creates user messages for each
- Applies provider media limitation workarounds
- Adds Anthropic cache control to system message and last 2 messages
- Prepends system prompt prefix
- Creates assistant message in DB (empty, will be filled by streaming)

**OnReasoningStart/Delta/End:**
- Appends reasoning content to current assistant message
- On end: stores provider-specific signatures (Anthropic, Google, OpenAI)
- Calls `FinishThinking()`

**OnTextDelta:**
- Strips leading newline from initial text
- Appends to assistant message via `AppendContent()`

**OnToolInputStart:**
- Creates `ToolCall` with name, adds to assistant message

**OnToolCall:**
- Updates tool call with full input, marks finished

**OnToolResult:**
- Converts to `message.ToolResult`, creates tool message in DB

**OnStepFinish:**
- Adds finish part to assistant message (finish reason, usage)
- Checks if summarization needed (auto-summarize threshold)

**OnRetry:**
- Currently TODO (not implemented)

13. After stream completes: handles errors (cancellation, context too large with auto-summarize)
14. Processes queued messages (runs again if queue non-empty)

## Auto-Summarization

Constants:
```go
largeContextWindowThreshold = 200_000  // tokens
largeContextWindowBuffer    = 20_000   // tokens
smallContextWindowRatio     = 0.2      // 20% of context window
```

Triggered when:
- `shouldSummarize` flag set during `OnStepFinish`
- Or `ContextTooLarge` error from provider

`Summarize()` method:
1. Gets all messages for session
2. Calls small model with summary prompt template
3. Creates summary message in DB with `IsSummaryMessage = true`
4. Updates session with `SummaryMessageID`

When summary exists, `getSessionMessages()` returns only messages after the summary point.

## Title Generation

Async in background goroutine:
1. Creates title session via `sessions.CreateTitleSession()`
2. Calls small model with title prompt template + user's prompt
3. Cleans result: strips think tags, takes first line, truncates to 80 chars
4. Updates session title via `sessions.UpdateTitleAndUsage()`

## Message Queue

- `messageQueue` is `csync.Map[string, []SessionAgentCall]` keyed by sessionID
- If `IsSessionBusy()` returns true, new calls are appended to the queue
- During `PrepareStep`, queued calls are drained and injected as user messages
- After agent loop completes, if queue has more entries, `Run()` is called again
- `QueuedPrompts()` returns count, `QueuedPromptsList()` returns prompt text
- `ClearQueue()` deletes the queue entry

## Cancellation

- `Cancel(sessionID)` calls stored `context.CancelFunc` from `activeRequests`
- `CancelAll()` cancels all active requests
- Cancelled context propagates through fantasy agent loop
- On cancellation: assistant message gets `FinishReasonCanceled`

## Event Tracking

`event.go` contains event emission methods:
- `eventPromptSent(sessionID)` -- emitted when prompt is sent to model

## MCP Integration

During agent construction:
- `mcp.GetStates()` lists connected MCP servers
- Connected server instructions appended to system prompt in `<mcp-instructions>` XML
- MCP tools added alongside built-in tools
- Tools refreshed in `PrepareStep` (allows hot-reload when MCP servers change)

## Tool Building

`buildTools()` in coordinator:
1. Builds built-in tools from `tools.BuildTools()` with context (permissions, history, filetracker, lsp, sessions, messages)
2. Adds MCP tools from connected servers
3. Calls `agent.SetTools()` to update the agent's tool list

## Errors

```go
var (
    ErrEmptyPrompt   = errors.New("prompt is empty")
    ErrSessionMissing = errors.New("session ID is required")
    errCoderAgentNotConfigured = errors.New("coder agent not configured")
    errModelProviderNotConfigured = errors.New("model provider not configured")
    // ... and more model/provider errors
)
```

## Loop Detection

`loop_detection.go` detects when the agent is stuck in a tool call loop. Checks for repeated identical tool calls across steps.
