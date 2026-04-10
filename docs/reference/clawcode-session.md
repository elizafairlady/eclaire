# Claw Code Session

Reference documentation for `rust/crates/runtime/src/session.rs`.

## Constants

```rust
const SESSION_VERSION: u32 = 1;
const ROTATE_AFTER_BYTES: u64 = 256 * 1024;    // 256 KiB
const MAX_ROTATED_FILES: usize = 3;
static SESSION_ID_COUNTER: AtomicU64 = AtomicU64::new(0);
```

## MessageRole

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MessageRole {
    System,
    User,
    Assistant,
    Tool,
}
```

Serialized as: `"system"`, `"user"`, `"assistant"`, `"tool"`.

## ContentBlock

Structured message content stored inside a Session.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ContentBlock {
    Text {
        text: String,
    },
    ToolUse {
        id: String,
        name: String,
        input: String,
    },
    ToolResult {
        tool_use_id: String,
        tool_name: String,
        output: String,
        is_error: bool,
    },
}
```

### ContentBlock JSON Serialization

**Text:**
```json
{"type": "text", "text": "..."}
```

**ToolUse:**
```json
{"type": "tool_use", "id": "...", "name": "...", "input": "..."}
```

**ToolResult:**
```json
{"type": "tool_result", "tool_use_id": "...", "tool_name": "...", "output": "...", "is_error": true}
```

### ContentBlock Methods

```rust
impl ContentBlock {
    pub fn to_json(&self) -> JsonValue;
    fn from_json(value: &JsonValue) -> Result<Self, SessionError>;
}
```

## ConversationMessage

One conversation message with optional token-usage metadata.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConversationMessage {
    pub role: MessageRole,
    pub blocks: Vec<ContentBlock>,
    pub usage: Option<TokenUsage>,
}
```

### Factory Methods

```rust
impl ConversationMessage {
    pub fn user_text(text: impl Into<String>) -> Self;
    pub fn assistant(blocks: Vec<ContentBlock>) -> Self;
    pub fn assistant_with_usage(blocks: Vec<ContentBlock>, usage: Option<TokenUsage>) -> Self;
    pub fn tool_result(
        tool_use_id: impl Into<String>,
        tool_name: impl Into<String>,
        output: impl Into<String>,
        is_error: bool,
    ) -> Self;
}
```

### Serialization Methods

```rust
impl ConversationMessage {
    pub fn to_json(&self) -> JsonValue;
    fn from_json(value: &JsonValue) -> Result<Self, SessionError>;
}
```

**JSON format:**
```json
{
    "role": "assistant",
    "blocks": [ ... ],
    "usage": { "input_tokens": 100, "output_tokens": 50, ... }
}
```

The `usage` field is only present when `self.usage.is_some()`.

## SessionCompaction

Metadata describing the latest compaction that summarized a session.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionCompaction {
    pub count: u32,
    pub removed_message_count: usize,
    pub summary: String,
}
```

### Methods

```rust
impl SessionCompaction {
    pub fn to_json(&self) -> Result<JsonValue, SessionError>;
    pub fn to_jsonl_record(&self) -> Result<JsonValue, SessionError>;
    fn from_json(value: &JsonValue) -> Result<Self, SessionError>;
}
```

**JSONL record format:**
```json
{"type": "compaction", "count": 2, "removed_message_count": 10, "summary": "..."}
```

## SessionFork

Provenance recorded when a session is forked from another session.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionFork {
    pub parent_session_id: String,
    pub branch_name: Option<String>,
}
```

### Methods

```rust
impl SessionFork {
    pub fn to_json(&self) -> JsonValue;
    fn from_json(value: &JsonValue) -> Result<Self, SessionError>;
}
```

**JSON format:**
```json
{"parent_session_id": "abc123", "branch_name": "feature-x"}
```

The `branch_name` field is omitted when `None`.

## SessionPromptEntry

A single user prompt recorded with a timestamp for history tracking.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionPromptEntry {
    pub timestamp_ms: u64,
    pub text: String,
}
```

### Methods

```rust
impl SessionPromptEntry {
    pub fn to_jsonl_record(&self) -> JsonValue;
    fn from_json_opt(value: &JsonValue) -> Option<Self>;
}
```

**JSONL record format:**
```json
{"type": "prompt_history", "timestamp_ms": 1700000000000, "text": "explain this code"}
```

## SessionError

```rust
#[derive(Debug)]
pub enum SessionError {
    Io(std::io::Error),
    Json(JsonError),
    Format(String),
}

impl Display for SessionError { ... }
impl std::error::Error for SessionError {}
impl From<std::io::Error> for SessionError { ... }
impl From<JsonError> for SessionError { ... }
```

## Session

Persisted conversational state for the runtime and CLI session manager.

```rust
#[derive(Debug, Clone)]
pub struct Session {
    pub version: u32,                           // SESSION_VERSION (1)
    pub session_id: String,                     // auto-generated unique ID
    pub created_at_ms: u64,                     // wall-clock creation time
    pub updated_at_ms: u64,                     // wall-clock last update time
    pub messages: Vec<ConversationMessage>,      // conversation transcript
    pub compaction: Option<SessionCompaction>,   // latest compaction metadata
    pub fork: Option<SessionFork>,               // parent session provenance
    pub workspace_root: Option<PathBuf>,         // bound worktree root
    pub prompt_history: Vec<SessionPromptEntry>, // user prompt history
    persistence: Option<SessionPersistence>,     // private: file path for auto-save
}
```

`SessionPersistence` is private:
```rust
struct SessionPersistence {
    path: PathBuf,
}
```

### PartialEq Implementation

Custom `PartialEq` that compares all public fields but excludes `persistence` (file path is not part of logical equality).

### Constructor

```rust
impl Session {
    pub fn new() -> Self;
}

impl Default for Session {
    fn default() -> Self { Self::new() }
}
```

Creates a session with `version=1`, auto-generated `session_id`, current timestamp for both `created_at_ms` and `updated_at_ms`, empty messages/compaction/fork/workspace_root/prompt_history/persistence.

### Builder Methods

```rust
pub fn with_persistence_path(mut self, path: impl Into<PathBuf>) -> Self;
pub fn with_workspace_root(mut self, workspace_root: impl Into<PathBuf>) -> Self;
```

### Accessor Methods

```rust
pub fn workspace_root(&self) -> Option<&Path>;
pub fn persistence_path(&self) -> Option<&Path>;
```

### Persistence Methods

#### save_to_path()

```rust
pub fn save_to_path(&self, path: impl AsRef<Path>) -> Result<(), SessionError>;
```

1. Renders the full JSONL snapshot via `render_jsonl_snapshot()`.
2. Calls `rotate_session_file_if_needed(path)` (rotates if file exceeds `ROTATE_AFTER_BYTES`).
3. Writes atomically via `write_atomic(path, snapshot)`.
4. Cleans up old rotated logs via `cleanup_rotated_logs(path)` (keeps `MAX_ROTATED_FILES`).

#### load_from_path()

```rust
pub fn load_from_path(path: impl AsRef<Path>) -> Result<Self, SessionError>;
```

1. Reads file contents as string.
2. Attempts to parse as a single JSON object: if it has a `"messages"` key, loads via `from_json()`.
3. Otherwise falls back to JSONL parsing via `from_jsonl()`.
4. Sets the persistence path to the loaded file.

#### push_message()

```rust
pub fn push_message(&mut self, message: ConversationMessage) -> Result<(), SessionError>;
```

1. Calls `touch()` to update `updated_at_ms`.
2. Pushes message to `self.messages`.
3. Calls `append_persisted_message()` to incrementally write to the session file.
4. If persistence fails, **pops the message back off** and returns the error.

#### push_user_text()

```rust
pub fn push_user_text(&mut self, text: impl Into<String>) -> Result<(), SessionError>;
```

Convenience wrapper that creates a `ConversationMessage::user_text(text)` and calls `push_message()`.

#### push_prompt_entry()

```rust
pub fn push_prompt_entry(&mut self, text: impl Into<String>) -> Result<(), SessionError>;
```

Records a user prompt with current wall-clock timestamp. Appends to in-memory `prompt_history` and, when persistence path is configured, incrementally writes to the JSONL session file.

### Compaction

```rust
pub fn record_compaction(&mut self, summary: impl Into<String>, removed_message_count: usize);
```

Updates `updated_at_ms`, increments the compaction counter (or starts at 1 if first compaction), and stores the `SessionCompaction`.

### Fork

```rust
pub fn fork(&self, branch_name: Option<String>) -> Self;
```

Creates a new session with:
- New `session_id` and timestamps
- Cloned messages, compaction, workspace_root, prompt_history
- `fork` set to `SessionFork { parent_session_id: self.session_id, branch_name }`
- `persistence` set to `None` (not bound to any file yet)

Empty or whitespace-only branch names are normalized to `None`.

### JSON Serialization

```rust
pub fn to_json(&self) -> Result<JsonValue, SessionError>;
pub fn from_json(value: &JsonValue) -> Result<Self, SessionError>;
```

**JSON format (single object):**
```json
{
    "version": 1,
    "session_id": "sess-abc123",
    "created_at_ms": 1700000000000,
    "updated_at_ms": 1700000001000,
    "messages": [ ... ],
    "compaction": { "count": 1, "removed_message_count": 5, "summary": "..." },
    "fork": { "parent_session_id": "sess-parent", "branch_name": "feature" },
    "workspace_root": "/home/user/project",
    "prompt_history": [ ... ]
}
```

Optional fields (`compaction`, `fork`, `workspace_root`, `prompt_history`) are omitted when empty/None.

### JSONL Format

#### render_jsonl_snapshot() (private)

Renders the full session as a JSONL string with one record per line:

1. **Session metadata record**: `{"type": "session_meta", "version": 1, "session_id": "...", "created_at_ms": ..., "updated_at_ms": ..., "fork": {...}, "workspace_root": "..."}`
2. **Compaction record** (if present): `{"type": "compaction", "count": ..., "removed_message_count": ..., "summary": "..."}`
3. **Prompt history records**: `{"type": "prompt_history", "timestamp_ms": ..., "text": "..."}`
4. **Message records**: `{"type": "message", "message": {...}}`

Each line is a complete JSON object. Terminated with a final newline.

#### from_jsonl() (private)

Parses JSONL contents line by line. Each line must be a JSON object with a `"type"` field:

| Type | Action |
|------|--------|
| `"session_meta"` | Extracts `version`, `session_id`, `created_at_ms`, `updated_at_ms`, `fork`, `workspace_root` |
| `"message"` | Parses the `"message"` sub-object as a `ConversationMessage` |
| `"compaction"` | Parses as `SessionCompaction` |
| `"prompt_history"` | Parses as `SessionPromptEntry` |
| other | Returns `SessionError::Format` |

Empty lines are skipped. Missing session metadata fields get defaults (current time, auto-generated ID).

### Incremental Persistence (private)

#### append_persisted_message()

If no persistence path is set, returns Ok immediately. If the file doesn't exist or is empty, calls `save_to_path()` for a full bootstrap. Otherwise, opens the file in append mode and writes the message record as a single JSONL line.

#### append_persisted_prompt_entry()

Same pattern as `append_persisted_message()` but for prompt history entries.

### Rotation

Session files are rotated when they exceed `ROTATE_AFTER_BYTES` (256 KiB). Rotated files are named `{original}.1`, `{original}.2`, etc. A maximum of `MAX_ROTATED_FILES` (3) rotated copies are kept; older ones are deleted during cleanup.

### Private Helpers

- `touch()` -- updates `updated_at_ms` to current wall-clock time.
- `meta_record()` -- builds the `session_meta` JSONL record.
- `message_record(message)` -- builds a `{"type": "message", "message": {...}}` record.
- `generate_session_id()` -- produces a unique session ID using timestamp + process ID + atomic counter.
- `current_time_millis()` -- returns milliseconds since Unix epoch.
