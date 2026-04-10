# Claw Code Conversation Runtime

Reference documentation for `rust/crates/runtime/src/conversation.rs`.

## Constants

```rust
const DEFAULT_AUTO_COMPACTION_INPUT_TOKENS_THRESHOLD: u32 = 100_000;
const AUTO_COMPACTION_THRESHOLD_ENV_VAR: &str = "CLAUDE_CODE_AUTO_COMPACT_INPUT_TOKENS";
```

## ApiRequest

Fully assembled request payload sent to the upstream model client.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ApiRequest {
    pub system_prompt: Vec<String>,
    pub messages: Vec<ConversationMessage>,
}
```

## AssistantEvent

Streamed events emitted while processing a single assistant turn.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AssistantEvent {
    TextDelta(String),
    ToolUse {
        id: String,
        name: String,
        input: String,
    },
    Usage(TokenUsage),
    PromptCache(PromptCacheEvent),
    MessageStop,
}
```

### PromptCacheEvent

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptCacheEvent {
    pub unexpected: bool,
    pub reason: String,
    pub previous_cache_read_input_tokens: u32,
    pub current_cache_read_input_tokens: u32,
    pub token_drop: u32,
}
```

## ApiClient Trait

Minimal streaming API contract required by `ConversationRuntime`.

```rust
pub trait ApiClient {
    fn stream(&mut self, request: ApiRequest) -> Result<Vec<AssistantEvent>, RuntimeError>;
}
```

Takes an `ApiRequest` (system prompt + messages) and returns a vector of `AssistantEvent` values representing the full streamed response. The runtime processes these events synchronously after the stream completes.

## ToolExecutor Trait

Trait implemented by tool dispatchers that execute model-requested tools.

```rust
pub trait ToolExecutor {
    fn execute(&mut self, tool_name: &str, input: &str) -> Result<String, ToolError>;
}
```

Takes a tool name and its JSON input string, returns either the tool's output string or a `ToolError`.

## ToolError

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolError {
    message: String,
}

impl ToolError {
    pub fn new(message: impl Into<String>) -> Self;
}

impl Display for ToolError { ... }
impl std::error::Error for ToolError {}
```

## RuntimeError

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuntimeError {
    message: String,
}

impl RuntimeError {
    pub fn new(message: impl Into<String>) -> Self;
}

impl Display for RuntimeError { ... }
impl std::error::Error for RuntimeError {}
```

## TurnSummary

Summary of one completed runtime turn, including tool results and usage.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TurnSummary {
    pub assistant_messages: Vec<ConversationMessage>,
    pub tool_results: Vec<ConversationMessage>,
    pub prompt_cache_events: Vec<PromptCacheEvent>,
    pub iterations: usize,
    pub usage: TokenUsage,
    pub auto_compaction: Option<AutoCompactionEvent>,
}
```

## AutoCompactionEvent

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AutoCompactionEvent {
    pub removed_message_count: usize,
}
```

## ConversationRuntime\<C, T\>

Coordinates the model loop, tool execution, hooks, and session updates. Generic over `C: ApiClient` and `T: ToolExecutor`.

### Struct Fields

```rust
pub struct ConversationRuntime<C, T> {
    session: Session,
    api_client: C,
    tool_executor: T,
    permission_policy: PermissionPolicy,
    system_prompt: Vec<String>,
    max_iterations: usize,                          // default: usize::MAX
    usage_tracker: UsageTracker,
    hook_runner: HookRunner,
    auto_compaction_input_tokens_threshold: u32,     // default: 100_000
    hook_abort_signal: HookAbortSignal,
    hook_progress_reporter: Option<Box<dyn HookProgressReporter>>,
    session_tracer: Option<SessionTracer>,
}
```

### Constructors

```rust
impl<C, T> ConversationRuntime<C, T>
where
    C: ApiClient,
    T: ToolExecutor,
{
    pub fn new(
        session: Session,
        api_client: C,
        tool_executor: T,
        permission_policy: PermissionPolicy,
        system_prompt: Vec<String>,
    ) -> Self;

    pub fn new_with_features(
        session: Session,
        api_client: C,
        tool_executor: T,
        permission_policy: PermissionPolicy,
        system_prompt: Vec<String>,
        feature_config: &RuntimeFeatureConfig,
    ) -> Self;
}
```

`new()` delegates to `new_with_features()` with `RuntimeFeatureConfig::default()`. The constructor initializes `usage_tracker` from the session, creates a `HookRunner` from the feature config, reads the auto-compaction threshold from the `CLAUDE_CODE_AUTO_COMPACT_INPUT_TOKENS` environment variable, and sets `max_iterations` to `usize::MAX`.

### Builder Methods

```rust
pub fn with_max_iterations(mut self, max_iterations: usize) -> Self;
pub fn with_auto_compaction_input_tokens_threshold(mut self, threshold: u32) -> Self;
pub fn with_hook_abort_signal(mut self, hook_abort_signal: HookAbortSignal) -> Self;
pub fn with_hook_progress_reporter(mut self, reporter: Box<dyn HookProgressReporter>) -> Self;
pub fn with_session_tracer(mut self, session_tracer: SessionTracer) -> Self;
```

### Hook Integration Methods (private)

```rust
fn run_pre_tool_use_hook(&mut self, tool_name: &str, input: &str) -> HookRunResult;
fn run_post_tool_use_hook(&mut self, tool_name: &str, input: &str, output: &str, is_error: bool) -> HookRunResult;
fn run_post_tool_use_failure_hook(&mut self, tool_name: &str, input: &str, output: &str) -> HookRunResult;
```

Each hook method passes the `hook_abort_signal` and (if set) the `hook_progress_reporter` to the `HookRunner`. The reporter is passed as `Some(reporter.as_mut())` when present, `None` otherwise.

### run_turn() -- The Agentic Loop

```rust
pub fn run_turn(
    &mut self,
    user_input: impl Into<String>,
    mut prompter: Option<&mut dyn PermissionPrompter>,
) -> Result<TurnSummary, RuntimeError>;
```

**Step-by-step execution flow:**

1. **Record turn started** via session tracer (if configured).
2. **Push user message** to session via `session.push_user_text(user_input)`.
3. **Enter the agentic loop** (repeats until no tool uses remain or max iterations exceeded):
   1. **Increment iteration counter**. If `iterations > max_iterations`, return `RuntimeError`.
   2. **Build ApiRequest** from `system_prompt` and `session.messages`.
   3. **Stream API call** via `api_client.stream(request)`. On error, record failure and return error.
   4. **Build assistant message** via `build_assistant_message(events)`. Returns `(ConversationMessage, Option<TokenUsage>, Vec<PromptCacheEvent>)`.
   5. **Record usage** in `usage_tracker` if present.
   6. **Collect prompt cache events**.
   7. **Extract pending tool uses** from assistant message blocks (filter for `ContentBlock::ToolUse`).
   8. **Record assistant iteration** via session tracer.
   9. **Push assistant message** to session.
   10. **If no pending tool uses**: break out of loop.
   11. **For each pending tool use** `(tool_use_id, tool_name, input)`:
       1. **Run PreToolUse hook** via `run_pre_tool_use_hook()`.
       2. **Compute effective input**: use `pre_hook_result.updated_input()` if present, otherwise original input.
       3. **Build PermissionContext** from hook's `permission_override()` and `permission_reason()`.
       4. **Determine permission outcome**:
          - If hook was cancelled: `PermissionOutcome::Deny` with cancellation message.
          - If hook failed: `PermissionOutcome::Deny` with failure message.
          - If hook denied: `PermissionOutcome::Deny` with denial message.
          - Otherwise: call `permission_policy.authorize_with_context(tool_name, effective_input, permission_context, prompter)`.
       5. **On PermissionOutcome::Allow**:
          - Record tool started via session tracer.
          - Execute tool via `tool_executor.execute(tool_name, effective_input)`.
          - Merge pre-hook feedback messages into output.
          - Run PostToolUse hook (or PostToolUseFailure if tool errored).
          - If post-hook denied/failed/cancelled: mark output as error.
          - Merge post-hook feedback messages into output.
          - Create `ConversationMessage::tool_result(tool_use_id, tool_name, output, is_error)`.
       6. **On PermissionOutcome::Deny { reason }**:
          - Create `ConversationMessage::tool_result(tool_use_id, tool_name, reason, is_error=true)`.
          - Merge pre-hook feedback messages into the denial reason.
       7. **Push tool result message** to session.
       8. **Record tool finished** via session tracer.
       9. **Append to tool_results vector**.
4. **Run auto-compaction** via `maybe_auto_compact()`.
5. **Build TurnSummary** with all collected data.
6. **Record turn completed** via session tracer.
7. **Return** `Ok(summary)`.

### Auto-Compaction

```rust
fn maybe_auto_compact(&mut self) -> Option<AutoCompactionEvent>;
```

Triggered when `usage_tracker.cumulative_usage().input_tokens >= auto_compaction_input_tokens_threshold`. Calls `compact_session()` with `max_estimated_tokens: 0` (compact everything possible) and `preserve_recent_messages: 4` (default). If compaction removed messages, replaces `self.session` with the compacted session and returns an `AutoCompactionEvent`.

### Session Access Methods

```rust
pub fn compact(&self, config: CompactionConfig) -> CompactionResult;
pub fn estimated_tokens(&self) -> usize;
pub fn usage(&self) -> &UsageTracker;
pub fn session(&self) -> &Session;
pub fn session_mut(&mut self) -> &mut Session;
pub fn fork_session(&self, branch_name: Option<String>) -> Session;
pub fn into_session(self) -> Session;
```

### Session Tracer Integration

The runtime records trace events at key points if a `SessionTracer` is configured:

- `record_turn_started(user_input)` -- attributes: `user_input`
- `record_assistant_iteration(iteration, assistant_message, pending_tool_use_count)` -- attributes: `iteration`, `assistant_blocks`, `pending_tool_use_count`
- `record_tool_started(iteration, tool_name)` -- attributes: `iteration`, `tool_name`
- `record_tool_finished(iteration, result_message)` -- attributes: `iteration`, `tool_name`, `is_error`
- `record_turn_completed(summary)` -- attributes: `iterations`, `assistant_messages`, `tool_results`, `prompt_cache_events`
- `record_turn_failed(iteration, error)` -- attributes: `iteration`, `error`

## build_assistant_message()

Private function that processes a `Vec<AssistantEvent>` into a structured assistant message.

```rust
fn build_assistant_message(
    events: Vec<AssistantEvent>,
) -> Result<(ConversationMessage, Option<TokenUsage>, Vec<PromptCacheEvent>), RuntimeError>;
```

**Processing logic:**

1. Iterates through events:
   - `TextDelta(delta)`: appends to accumulating text buffer.
   - `ToolUse { id, name, input }`: flushes any accumulated text as a `ContentBlock::Text`, then pushes `ContentBlock::ToolUse`.
   - `Usage(value)`: captures as the turn's usage.
   - `PromptCache(event)`: accumulates prompt cache events.
   - `MessageStop`: marks the stream as finished.
2. Flushes any remaining text buffer.
3. **Error conditions**:
   - Returns `RuntimeError` if stream ended without `MessageStop`.
   - Returns `RuntimeError` if no content blocks were produced.
4. Returns `ConversationMessage::assistant_with_usage(blocks, usage)`, the optional usage, and prompt cache events.

## auto_compaction_threshold_from_env()

```rust
pub fn auto_compaction_threshold_from_env() -> u32;
```

Reads `CLAUDE_CODE_AUTO_COMPACT_INPUT_TOKENS` from the environment. Parses as `u32`, filters for `> 0`, falls back to `DEFAULT_AUTO_COMPACTION_INPUT_TOKENS_THRESHOLD` (100,000).

## Hook Feedback Merging

```rust
fn merge_hook_feedback(messages: &[String], output: String, is_error: bool) -> String;
```

If hook messages are empty, returns the output unchanged. Otherwise, appends a `"Hook feedback:"` (or `"Hook feedback (error):"`) section with the hook messages joined by newlines.

```rust
fn format_hook_message(result: &HookRunResult, fallback: &str) -> String;
```

Returns hook messages joined by newlines, or the fallback string if no messages exist.

## StaticToolExecutor

Simple in-memory tool executor for tests and lightweight integrations.

```rust
#[derive(Default)]
pub struct StaticToolExecutor {
    handlers: BTreeMap<String, ToolHandler>,
}

type ToolHandler = Box<dyn FnMut(&str) -> Result<String, ToolError>>;

impl StaticToolExecutor {
    pub fn new() -> Self;
    pub fn register(
        mut self,
        tool_name: impl Into<String>,
        handler: impl FnMut(&str) -> Result<String, ToolError> + 'static,
    ) -> Self;
}

impl ToolExecutor for StaticToolExecutor {
    fn execute(&mut self, tool_name: &str, input: &str) -> Result<String, ToolError>;
}
```

`register()` is a builder method that inserts a handler closure for a named tool. `execute()` looks up the handler by name and calls it, returning `ToolError("unknown tool: {name}")` if not found.
