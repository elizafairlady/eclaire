# Crush Services Reference

Source: `internal/session/`, `internal/message/`, `internal/permission/`, `internal/history/`, `internal/filetracker/`

## Session Service

### Session Struct

```go
type Session struct {
    ID               string
    ParentSessionID  string
    Title            string
    MessageCount     int64
    PromptTokens     int64
    CompletionTokens int64
    SummaryMessageID string
    Cost             float64
    Todos            []Todo
    CreatedAt        int64
    UpdatedAt        int64
}
```

### Todo Struct

```go
type TodoStatus string
const (
    TodoStatusPending    TodoStatus = "pending"
    TodoStatusInProgress TodoStatus = "in_progress"
    TodoStatusCompleted  TodoStatus = "completed"
)

type Todo struct {
    Content    string     `json:"content"`
    Status     TodoStatus `json:"status"`
    ActiveForm string     `json:"active_form"`
}
```

`HasIncompleteTodos(todos)` returns true if any todo is not completed.

### Service Interface

```go
type Service interface {
    pubsub.Subscriber[Session]
    Create(ctx context.Context, title string) (Session, error)
    CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error)
    CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error)
    Get(ctx context.Context, id string) (Session, error)
    GetLast(ctx context.Context) (Session, error)
    List(ctx context.Context) ([]Session, error)
    Save(ctx context.Context, session Session) (Session, error)
    UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens int64, cost float64) error
    Rename(ctx context.Context, id string, title string) error
    Delete(ctx context.Context, id string) error

    // Agent tool session management
    CreateAgentToolSessionID(messageID, toolCallID string) string
    ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool)
    IsAgentToolSession(sessionID string) bool
}
```

### Implementation Details

- SQLite-backed via sqlc `db.Queries`
- All mutations publish pubsub events (`CreatedEvent`, `UpdatedEvent`, `DeletedEvent`)
- `Create` generates UUID for ID
- `CreateTitleSession` creates with ID `"title-" + parentSessionID`
- `CreateTaskSession` uses `toolCallID` as session ID, sets parent
- `Delete` runs in transaction: deletes messages, files, then session
- `Save` marshals Todos as JSON, updates all fields
- `UpdateTitleAndUsage` is atomic partial update (safer than fetch-modify-save)
- `Rename` updates only title without touching timestamps
- Agent tool session IDs use format `"messageID$$toolCallID"`, parsed by splitting on `$$`
- `HashID` uses XXH3 hash of UUID for filesystem-safe short IDs

## Message Service

### Message Struct

```go
type Message struct {
    ID               string
    Role             MessageRole  // "assistant", "user", "system", "tool"
    SessionID        string
    Parts            []ContentPart
    Model            string
    Provider         string
    CreatedAt        int64
    UpdatedAt        int64
    IsSummaryMessage bool
}
```

### MessageRole

```go
const (
    Assistant MessageRole = "assistant"
    User      MessageRole = "user"
    System    MessageRole = "system"
    Tool      MessageRole = "tool"
)
```

### ContentPart Types

`ContentPart` is an interface with a marker method `isPart()`. Seven implementations:

**ReasoningContent:**
```go
type ReasoningContent struct {
    Thinking         string
    Signature        string                             // Anthropic signature
    ThoughtSignature string                             // Google signature
    ToolID           string                             // OpenRouter Google models
    ResponsesData    *openai.ResponsesReasoningMetadata // OpenAI responses
    StartedAt        int64
    FinishedAt       int64
}
```

**TextContent:**
```go
type TextContent struct {
    Text string
}
```

**ImageURLContent:**
```go
type ImageURLContent struct {
    URL    string
    Detail string  // optional
}
```

**BinaryContent:**
```go
type BinaryContent struct {
    Path     string
    MIMEType string
    Data     []byte
}
```

**ToolCall:**
```go
type ToolCall struct {
    ID               string
    Name             string
    Input            string  // JSON string
    ProviderExecuted bool
    Finished         bool
}
```

**ToolResult:**
```go
type ToolResult struct {
    ToolCallID string
    Name       string
    Content    string
    Data       string   // base64 for media
    MIMEType   string
    Metadata   string
    IsError    bool
}
```

**Finish:**
```go
type Finish struct {
    Reason  FinishReason
    Time    int64
    Message string  // optional
    Details string  // optional
}

type FinishReason string
const (
    FinishReasonEndTurn          FinishReason = "end_turn"
    FinishReasonMaxTokens        FinishReason = "max_tokens"
    FinishReasonToolUse          FinishReason = "tool_use"
    FinishReasonCanceled         FinishReason = "canceled"
    FinishReasonError            FinishReason = "error"
    FinishReasonPermissionDenied FinishReason = "permission_denied"
    FinishReasonUnknown          FinishReason = "unknown"
)
```

### Service Interface

```go
type Service interface {
    pubsub.Subscriber[Message]
    Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error)
    Update(ctx context.Context, message Message) error
    Get(ctx context.Context, id string) (Message, error)
    List(ctx context.Context, sessionID string) ([]Message, error)
    ListUserMessages(ctx context.Context, sessionID string) ([]Message, error)
    ListAllUserMessages(ctx context.Context) ([]Message, error)
    Delete(ctx context.Context, id string) error
    DeleteSessionMessages(ctx context.Context, sessionID string) error
}
```

### Message Mutation Methods

The `Message` struct has extensive mutation methods for streaming updates:

- `AppendContent(delta string)` -- appends to existing TextContent or creates new
- `AppendReasoningContent(delta string)` -- appends to ReasoningContent, sets StartedAt on first call
- `AppendReasoningSignature(signature string)` -- Anthropic signature
- `AppendThoughtSignature(signature, toolCallID string)` -- Google signature
- `SetReasoningResponsesData(data)` -- OpenAI responses metadata
- `FinishThinking()` -- sets FinishedAt timestamp
- `ThinkingDuration() time.Duration` -- calculates thinking time
- `AddToolCall(tc ToolCall)` -- adds or replaces by ID
- `SetToolCalls(tc []ToolCall)` -- replaces all
- `AppendToolCallInput(toolCallID, inputDelta string)` -- streaming input append
- `FinishToolCall(toolCallID string)` -- marks finished
- `AddToolResult(tr ToolResult)` -- appends result
- `SetToolResults(tr []ToolResult)` -- appends all
- `AddFinish(reason, message, details string)` -- removes existing finish, adds new with timestamp
- `AddImageURL(url, detail string)`
- `AddBinary(mimeType string, data []byte)`
- `Clone() Message` -- deep copy with independent Parts slice (prevents race conditions)

### Serialization

Parts serialized as JSON array of `{type: string, data: ...}` wrappers. Type discriminator values:
- `"reasoning"`, `"text"`, `"image_url"`, `"binary"`, `"tool_call"`, `"tool_result"`, `"finish"`

### Fantasy Conversion

`Message.ToAIMessage()` converts to `[]fantasy.Message`:
- User messages: text + binary files (text attachments inlined in prompt XML, images as FilePart)
- Assistant messages: text + reasoning (with provider-specific signatures) + tool calls
- Tool messages: tool results with text/error/media content

### Attachment

```go
type Attachment struct {
    FilePath string
    FileName string
    MimeType string
    Content  []byte
}

func (a Attachment) IsText() bool   // strings.HasPrefix(MimeType, "text/")
func (a Attachment) IsImage() bool  // strings.HasPrefix(MimeType, "image/")
```

`PromptWithTextAttachments(prompt, attachments)` wraps text attachments in XML: `<system_info>` notice + `<file path='...'>` blocks.

## Permission Service

### Types

```go
type CreatePermissionRequest struct {
    SessionID   string
    ToolCallID  string
    ToolName    string
    Description string
    Action      string
    Params      any
    Path        string
}

type PermissionRequest struct {
    ID          string  // UUID
    SessionID   string
    ToolCallID  string
    ToolName    string
    Description string
    Action      string
    Params      any
    Path        string  // Resolved to directory
}

type PermissionNotification struct {
    ToolCallID string
    Granted    bool
    Denied     bool
}
```

### Service Interface

```go
type Service interface {
    pubsub.Subscriber[PermissionRequest]
    GrantPersistent(permission PermissionRequest)
    Grant(permission PermissionRequest)
    Deny(permission PermissionRequest)
    Request(ctx context.Context, opts CreatePermissionRequest) (bool, error)
    AutoApproveSession(sessionID string)
    SetSkipRequests(skip bool)
    SkipRequests() bool
    SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification]
}
```

### Request Flow

1. `Request()` checks skip mode (YOLO) -> auto-approve
2. Checks allowlist by `"toolName:action"` or `"toolName"` -> auto-approve
3. Serializes requests (one at a time via `requestMu`)
4. Checks auto-approve sessions -> auto-approve
5. Resolves path to directory
6. Checks session persistent permissions (matching tool+action+session+path) -> auto-approve
7. Creates pending channel, publishes request via pubsub
8. Blocks on channel or context cancellation
9. UI receives pubsub event, opens permission dialog
10. User decision calls `Grant`/`GrantPersistent`/`Deny` which sends response through channel

### Permission Storage

- `GrantPersistent` adds to `sessionPermissions` slice -> future requests auto-approved
- `Grant` is one-time (just unblocks the channel)
- `Deny` unblocks with false
- `AutoApproveSession(sessionID)` blanket-approves all tools for a session
- `SetSkipRequests(true)` enables YOLO mode (skip all permission checks)

### Implementation

```go
type permissionService struct {
    *pubsub.Broker[PermissionRequest]
    notificationBroker    *pubsub.Broker[PermissionNotification]
    workingDir            string
    sessionPermissions    []PermissionRequest
    sessionPermissionsMu  sync.RWMutex
    pendingRequests       *csync.Map[string, chan bool]
    autoApproveSessions   map[string]bool
    autoApproveSessionsMu sync.RWMutex
    skip                  bool
    allowedTools          []string
    requestMu             sync.Mutex      // Serialize requests
    activeRequest         *PermissionRequest
    activeRequestMu       sync.Mutex
}
```

## History Service (File Versioning)

### File Struct

```go
type File struct {
    ID        string
    SessionID string
    Path      string
    Content   string
    Version   int64
    CreatedAt int64
    UpdatedAt int64
}
```

### Service Interface

```go
type Service interface {
    pubsub.Subscriber[File]
    Create(ctx context.Context, sessionID, path, content string) (File, error)
    CreateVersion(ctx context.Context, sessionID, path, content string) (File, error)
    Get(ctx context.Context, id string) (File, error)
    GetByPathAndSession(ctx context.Context, path, sessionID string) (File, error)
    ListBySession(ctx context.Context, sessionID string) ([]File, error)
    ListLatestSessionFiles(ctx context.Context, sessionID string) ([]File, error)
    Delete(ctx context.Context, id string) error
    DeleteSessionFiles(ctx context.Context, sessionID string) error
}
```

### Versioning Logic

- `Create` creates with `InitialVersion = 0`
- `CreateVersion` auto-increments: queries latest version for path, adds 1. If no previous versions, creates initial.
- Uses transactions with up to 3 retries on `UNIQUE constraint failed` (increments version on each retry)
- Files ordered by `version DESC, created_at DESC`
- SQLite-backed via sqlc

## FileTracker Service

### Service Interface

```go
type Service interface {
    RecordRead(ctx context.Context, sessionID, path string)
    LastReadTime(ctx context.Context, sessionID, path string) time.Time
    ListReadFiles(ctx context.Context, sessionID string) ([]string, error)
}
```

### Implementation

- Tracks file reads per session in SQLite
- Paths stored as relative (via `filepath.Rel` from CWD)
- `LastReadTime` returns zero time if never read
- `ListReadFiles` returns absolute paths (joins with CWD)
- Used by tools to detect stale file reads

## Pubsub System

### Broker

```go
type Broker[T any] struct {
    subs      map[chan Event[T]]struct{}
    mu        sync.RWMutex
    done      chan struct{}
    subCount  int
    maxEvents int
}
```

- `NewBroker[T]()` creates with buffer size 64, max events 1000
- `Subscribe(ctx)` returns buffered channel, auto-unsubscribes on context cancellation
- `Publish(type, payload)` non-blocking fan-out (drops events if channel full)
- `Shutdown()` closes all channels, prevents further publishes

### Events

```go
type EventType string
const (
    CreatedEvent EventType = "created"
    UpdatedEvent EventType = "updated"
    DeletedEvent EventType = "deleted"
)

type Event[T any] struct {
    Type    EventType
    Payload T
}

type Subscriber[T any] interface {
    Subscribe(context.Context) <-chan Event[T]
}

type Publisher[T any] interface {
    Publish(EventType, T)
}
```

All services embed `*pubsub.Broker[T]` to get both `Subscriber` and `Publisher` capabilities.
