# Claw Code Hooks

Reference documentation for `rust/crates/runtime/src/hooks.rs`.

## Type Alias

```rust
pub type HookPermissionDecision = PermissionOverride;
```

`HookPermissionDecision` is an alias for `PermissionOverride` (Allow/Deny/Ask).

## HookEvent

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HookEvent {
    PreToolUse,
    PostToolUse,
    PostToolUseFailure,
}

impl HookEvent {
    pub fn as_str(self) -> &'static str;
}
```

| Variant | String |
|---------|--------|
| `PreToolUse` | `"PreToolUse"` |
| `PostToolUse` | `"PostToolUse"` |
| `PostToolUseFailure` | `"PostToolUseFailure"` |

## HookProgressEvent

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HookProgressEvent {
    Started {
        event: HookEvent,
        tool_name: String,
        command: String,
    },
    Completed {
        event: HookEvent,
        tool_name: String,
        command: String,
    },
    Cancelled {
        event: HookEvent,
        tool_name: String,
        command: String,
    },
}
```

## HookProgressReporter Trait

```rust
pub trait HookProgressReporter {
    fn on_event(&mut self, event: &HookProgressEvent);
}
```

## HookAbortSignal

Thread-safe abort signal backed by `Arc<AtomicBool>`.

```rust
#[derive(Debug, Clone, Default)]
pub struct HookAbortSignal {
    aborted: Arc<AtomicBool>,
}

impl HookAbortSignal {
    pub fn new() -> Self;           // returns Self::default()
    pub fn abort(&self);            // stores true with SeqCst ordering
    pub fn is_aborted(&self) -> bool; // loads with SeqCst ordering
}
```

## HookRunResult

Result of running one or more hook commands for a single event.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HookRunResult {
    denied: bool,
    failed: bool,
    cancelled: bool,
    messages: Vec<String>,
    permission_override: Option<PermissionOverride>,
    permission_reason: Option<String>,
    updated_input: Option<String>,
}
```

### Constructor

```rust
pub fn allow(messages: Vec<String>) -> Self;
```

Creates a result with `denied=false`, `failed=false`, `cancelled=false`, no permission override, no updated input.

### Accessors

```rust
pub fn is_denied(&self) -> bool;
pub fn is_failed(&self) -> bool;
pub fn is_cancelled(&self) -> bool;
pub fn messages(&self) -> &[String];
pub fn permission_override(&self) -> Option<PermissionOverride>;
pub fn permission_decision(&self) -> Option<HookPermissionDecision>;  // alias for permission_override()
pub fn permission_reason(&self) -> Option<&str>;
pub fn updated_input(&self) -> Option<&str>;
pub fn updated_input_json(&self) -> Option<&str>;  // alias for updated_input()
```

## HookRunner

```rust
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct HookRunner {
    config: RuntimeHookConfig,
}
```

### Constructors

```rust
pub fn new(config: RuntimeHookConfig) -> Self;
pub fn from_feature_config(feature_config: &RuntimeFeatureConfig) -> Self;
```

### Public Methods

```rust
// PreToolUse
pub fn run_pre_tool_use(&self, tool_name: &str, tool_input: &str) -> HookRunResult;
pub fn run_pre_tool_use_with_signal(
    &self, tool_name: &str, tool_input: &str,
    abort_signal: Option<&HookAbortSignal>,
) -> HookRunResult;
pub fn run_pre_tool_use_with_context(
    &self, tool_name: &str, tool_input: &str,
    abort_signal: Option<&HookAbortSignal>,
    reporter: Option<&mut dyn HookProgressReporter>,
) -> HookRunResult;

// PostToolUse
pub fn run_post_tool_use(
    &self, tool_name: &str, tool_input: &str,
    tool_output: &str, is_error: bool,
) -> HookRunResult;
pub fn run_post_tool_use_with_signal(
    &self, tool_name: &str, tool_input: &str,
    tool_output: &str, is_error: bool,
    abort_signal: Option<&HookAbortSignal>,
) -> HookRunResult;
pub fn run_post_tool_use_with_context(
    &self, tool_name: &str, tool_input: &str,
    tool_output: &str, is_error: bool,
    abort_signal: Option<&HookAbortSignal>,
    reporter: Option<&mut dyn HookProgressReporter>,
) -> HookRunResult;

// PostToolUseFailure
pub fn run_post_tool_use_failure(
    &self, tool_name: &str, tool_input: &str,
    tool_error: &str,
) -> HookRunResult;
pub fn run_post_tool_use_failure_with_signal(
    &self, tool_name: &str, tool_input: &str,
    tool_error: &str, abort_signal: Option<&HookAbortSignal>,
) -> HookRunResult;
pub fn run_post_tool_use_failure_with_context(
    &self, tool_name: &str, tool_input: &str,
    tool_error: &str,
    abort_signal: Option<&HookAbortSignal>,
    reporter: Option<&mut dyn HookProgressReporter>,
) -> HookRunResult;
```

Each variant delegates to the `_with_context` form, passing `None` for missing optional parameters.

## run_commands() -- Core Execution Flow (private)

```rust
fn run_commands(
    event: HookEvent,
    commands: &[String],
    tool_name: &str,
    tool_input: &str,
    tool_output: Option<&str>,
    is_error: bool,
    abort_signal: Option<&HookAbortSignal>,
    mut reporter: Option<&mut dyn HookProgressReporter>,
) -> HookRunResult;
```

**Step-by-step execution:**

1. **Empty commands**: Return `HookRunResult::allow(Vec::new())` immediately.

2. **Check abort signal**: If already aborted, return a cancelled result with message `"{event} hook cancelled before execution"`.

3. **Build JSON payload** via `hook_payload()` for stdin.

4. **Initialize result** as `HookRunResult::allow(Vec::new())`.

5. **Iterate through each command** sequentially:
   1. Report `HookProgressEvent::Started`.
   2. Call `run_command()` for the individual command.
   3. Handle outcome:
      - **`HookCommandOutcome::Allow { parsed }`**: Report `Completed`, merge parsed output into result, continue to next command.
      - **`HookCommandOutcome::Deny { parsed }`**: Report `Completed`, merge parsed output, set `result.denied = true`, **return immediately** (stops further commands).
      - **`HookCommandOutcome::Failed { parsed }`**: Report `Completed`, merge parsed output, set `result.failed = true`, **return immediately** (stops further commands).
      - **`HookCommandOutcome::Cancelled { message }`**: Report `Cancelled`, set `result.cancelled = true`, add message, **return immediately**.

6. **Return accumulated result** (if all commands allowed).

**Key behavior**: Commands execute in order. A deny, failure, or cancellation stops the chain.

## run_command() -- Single Command Execution (private)

```rust
fn run_command(
    command: &str,
    event: HookEvent,
    tool_name: &str,
    tool_input: &str,
    tool_output: Option<&str>,
    is_error: bool,
    payload: &str,
    abort_signal: Option<&HookAbortSignal>,
) -> HookCommandOutcome;
```

**Execution:**

1. **Spawn shell process** via `sh -lc <command>` (Unix) or `cmd /C <command>` (Windows).
2. **Set environment variables**:
   - `HOOK_EVENT` = event.as_str()
   - `HOOK_TOOL_NAME` = tool_name
   - `HOOK_TOOL_INPUT` = tool_input
   - `HOOK_TOOL_IS_ERROR` = `"1"` or `"0"`
   - `HOOK_TOOL_OUTPUT` = tool_output (if present)
3. **Write JSON payload to stdin** of the child process.
4. **Poll for completion** in a loop (20ms sleep between checks), checking abort signal on each iteration. If aborted, kills the child and returns `Cancelled`.
5. **Process exit status**:
   - **Exit code 0**: Parse stdout as hook output. If parsed output has `deny=true`, return `Deny`; otherwise return `Allow`.
   - **Exit code 2**: Return `Deny` with message `"{event} hook denied tool `{tool_name}`"`.
   - **Other non-zero exit code**: Return `Failed` with formatted failure message.
   - **Killed by signal (no exit code)**: Return `Failed` with signal termination message.
6. **Process spawn failure**: Return `Failed` with error message.

## HookCommandOutcome (private enum)

```rust
enum HookCommandOutcome {
    Allow { parsed: ParsedHookOutput },
    Deny { parsed: ParsedHookOutput },
    Failed { parsed: ParsedHookOutput },
    Cancelled { message: String },
}
```

## Hook Payload JSON Format

Built by `hook_payload()`:

**For PreToolUse and PostToolUse:**
```json
{
    "hook_event_name": "PreToolUse",
    "tool_name": "bash",
    "tool_input": { ... },
    "tool_input_json": "{...}",
    "tool_output": "...",
    "tool_result_is_error": false
}
```

**For PostToolUseFailure:**
```json
{
    "hook_event_name": "PostToolUseFailure",
    "tool_name": "bash",
    "tool_input": { ... },
    "tool_input_json": "{...}",
    "tool_error": "...",
    "tool_result_is_error": true
}
```

`tool_input` is the parsed JSON object (via `parse_tool_input()`). If parsing fails, it becomes `{ "raw": "<original string>" }`. `tool_input_json` is always the raw string form.

## parse_hook_output() -- Stdout Parsing (private)

```rust
fn parse_hook_output(stdout: &str) -> ParsedHookOutput;
```

**ParsedHookOutput struct:**
```rust
struct ParsedHookOutput {
    messages: Vec<String>,
    deny: bool,
    permission_override: Option<PermissionOverride>,
    permission_reason: Option<String>,
    updated_input: Option<String>,
}
```

**Parsing logic:**

1. **Empty stdout**: Returns default (empty) output.
2. **Non-JSON stdout**: Returns the raw stdout string as the sole message.
3. **JSON object stdout**: Parses the following fields:
   - `"systemMessage"` (string) -- added to messages.
   - `"reason"` (string) -- added to messages.
   - `"continue"` (bool) -- if `false`, sets `deny = true`.
   - `"decision"` (string) -- if `"block"`, sets `deny = true`.
   - `"hookSpecificOutput"` (object):
     - `"additionalContext"` (string) -- added to messages.
     - `"permissionDecision"` (string) -- maps to `PermissionOverride`:
       - `"allow"` -> `Some(PermissionOverride::Allow)`
       - `"deny"` -> `Some(PermissionOverride::Deny)`
       - `"ask"` -> `Some(PermissionOverride::Ask)`
       - anything else -> `None`
     - `"permissionDecisionReason"` (string) -- stored as `permission_reason`.
     - `"updatedInput"` (any JSON value) -- serialized back to JSON string and stored as `updated_input`.
4. **Fallback**: If no messages were extracted, adds the raw stdout as a message.

## merge_parsed_hook_output() (private)

```rust
fn merge_parsed_hook_output(target: &mut HookRunResult, parsed: ParsedHookOutput);
```

Extends target messages, overwrites permission_override/permission_reason/updated_input only if the parsed output has `Some` values.

## Environment Variables Available to Hook Scripts

| Variable | Description |
|----------|-------------|
| `HOOK_EVENT` | Event type: `PreToolUse`, `PostToolUse`, or `PostToolUseFailure` |
| `HOOK_TOOL_NAME` | Name of the tool being invoked |
| `HOOK_TOOL_INPUT` | Raw JSON string of the tool input |
| `HOOK_TOOL_IS_ERROR` | `"1"` if the tool result is an error, `"0"` otherwise |
| `HOOK_TOOL_OUTPUT` | Tool output string (PostToolUse/PostToolUseFailure only) |

Additionally, the full JSON payload is written to the hook script's stdin.
