# Claw Code Compaction

Reference documentation for `rust/crates/runtime/src/compact.rs`.

## Constants

```rust
const COMPACT_CONTINUATION_PREAMBLE: &str =
    "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n";
const COMPACT_RECENT_MESSAGES_NOTE: &str = "Recent messages are preserved verbatim.";
const COMPACT_DIRECT_RESUME_INSTRUCTION: &str = "Continue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, and do not preface with continuation text.";
```

## CompactionConfig

Thresholds controlling when and how a session is compacted.

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CompactionConfig {
    pub preserve_recent_messages: usize,
    pub max_estimated_tokens: usize,
}

impl Default for CompactionConfig {
    fn default() -> Self {
        Self {
            preserve_recent_messages: 4,
            max_estimated_tokens: 10_000,
        }
    }
}
```

## CompactionResult

Result of compacting a session into a summary plus preserved tail messages.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CompactionResult {
    pub summary: String,
    pub formatted_summary: String,
    pub compacted_session: Session,
    pub removed_message_count: usize,
}
```

## estimate_session_tokens()

```rust
pub fn estimate_session_tokens(session: &Session) -> usize;
```

Roughly estimates the token footprint of the current session transcript. Sums `estimate_message_tokens()` across all messages.

### estimate_message_tokens() (private)

```rust
fn estimate_message_tokens(message: &ConversationMessage) -> usize;
```

Per-block estimation:
- `Text { text }`: `text.len() / 4 + 1`
- `ToolUse { name, input, .. }`: `(name.len() + input.len()) / 4 + 1`
- `ToolResult { tool_name, output, .. }`: `(tool_name.len() + output.len()) / 4 + 1`

Simple character-based heuristic (4 chars per token).

## should_compact()

```rust
pub fn should_compact(session: &Session, config: CompactionConfig) -> bool;
```

Returns `true` when the session exceeds the configured compaction budget.

**Logic:**
1. Determine the `start` index: skip any existing compacted summary message at position 0 (via `compacted_summary_prefix_len()`).
2. Slice `compactable = &session.messages[start..]`.
3. Return `true` if BOTH:
   - `compactable.len() > config.preserve_recent_messages`
   - Sum of estimated tokens for compactable messages `>= config.max_estimated_tokens`

This means an existing compacted summary message is never counted toward the compaction threshold.

## format_compact_summary()

```rust
pub fn format_compact_summary(summary: &str) -> String;
```

Normalizes a compaction summary into user-facing continuation text.

1. Strip `<analysis>...</analysis>` tag blocks.
2. Replace `<summary>...</summary>` with `"Summary:\n{content}"` (trimmed).
3. Collapse consecutive blank lines.
4. Trim leading/trailing whitespace.

## get_compact_continuation_message()

```rust
pub fn get_compact_continuation_message(
    summary: &str,
    suppress_follow_up_questions: bool,
    recent_messages_preserved: bool,
) -> String;
```

Builds the synthetic system message used after session compaction.

**Structure:**
1. `COMPACT_CONTINUATION_PREAMBLE` ("This session is being continued...")
2. `format_compact_summary(summary)`
3. If `recent_messages_preserved`: append `"\n\n"` + `COMPACT_RECENT_MESSAGES_NOTE`
4. If `suppress_follow_up_questions`: append `"\n"` + `COMPACT_DIRECT_RESUME_INSTRUCTION`

## compact_session()

```rust
pub fn compact_session(session: &Session, config: CompactionConfig) -> CompactionResult;
```

Compacts a session by summarizing older messages and preserving the recent tail.

**Algorithm:**

1. **Check threshold**: If `!should_compact(session, config)`, return unchanged session with empty summary and `removed_message_count: 0`.

2. **Extract existing summary**: Check if the first message is a system message with a compacted summary (by looking for `COMPACT_CONTINUATION_PREAMBLE` prefix).

3. **Compute compacted prefix length**: 1 if an existing summary exists, 0 otherwise.

4. **Determine preservation boundary**: `keep_from = session.messages.len().saturating_sub(config.preserve_recent_messages)`.

5. **Slice messages**:
   - `removed = &session.messages[compacted_prefix_len..keep_from]` -- messages to summarize.
   - `preserved = session.messages[keep_from..]` -- messages to keep verbatim.

6. **Generate summary**: `merge_compact_summaries(existing_summary, summarize_messages(removed))`.

7. **Format summary** for display: `format_compact_summary(summary)`.

8. **Build continuation message**: `get_compact_continuation_message(summary, true, !preserved.is_empty())`.

9. **Construct compacted messages**:
   - First: a System message containing the continuation text.
   - Then: all preserved messages.

10. **Clone session and replace messages**. Call `session.record_compaction(summary, removed.len())`.

11. **Return** `CompactionResult { summary, formatted_summary, compacted_session, removed_message_count }`.

## summarize_messages() (private)

```rust
fn summarize_messages(messages: &[ConversationMessage]) -> String;
```

Generates a structured bullet-point summary of the removed messages.

**Format:**
```
<summary>
Conversation summary:
- Scope: {N} earlier messages compacted (user={U}, assistant={A}, tool={T}).
- Tools mentioned: {tool1, tool2, ...}.
- Recent user requests:
  - {request1}
  - {request2}
  - {request3}
- Pending work:
  - {item1}
  - {item2}
- Key files referenced: {file1, file2, ...}.
- Current work: {last non-empty text content}
- Key timeline:
  - user: {summarized content}
  - assistant: {summarized content}
  - tool: {summarized content}
  ...
</summary>
```

**Sections:**

1. **Scope**: Count of all messages, broken down by role.
2. **Tools mentioned**: Deduplicated, sorted list of tool names from ToolUse and ToolResult blocks.
3. **Recent user requests**: Last 3 user messages (text content, truncated to 160 chars). Collected via `collect_recent_role_summaries()`.
4. **Pending work**: Last 3 messages whose text contains "todo", "next", "pending", "follow up", or "remaining" (case-insensitive). Collected via `infer_pending_work()`.
5. **Key files referenced**: Up to 8 deduplicated file paths extracted from all message content. A file path must contain `/` and have an interesting extension (rs, ts, tsx, js, json, md). Collected via `collect_key_files()`.
6. **Current work**: The last non-empty text block from any message, truncated to 200 chars. Via `infer_current_work()`.
7. **Key timeline**: Every message summarized as `{role}: {block_summary | block_summary}`. Each block is truncated to 160 chars.

### summarize_block() (private)

```rust
fn summarize_block(block: &ContentBlock) -> String;
```

- `Text { text }` -- the text itself.
- `ToolUse { name, input, .. }` -- `"tool_use {name}({input})"`.
- `ToolResult { tool_name, output, is_error, .. }` -- `"tool_result {tool_name}: [error ]{output}"`.

All truncated to 160 characters with `...` suffix.

## merge_compact_summaries()

```rust
fn merge_compact_summaries(existing_summary: Option<&str>, new_summary: &str) -> String;
```

Merges a previous compacted summary with a new one to preserve context across multiple compactions.

**When no existing summary**: Returns `new_summary` unchanged.

**When existing summary present**:

1. Extract highlights (non-timeline bullet points) from both the existing and new summaries.
2. Extract timeline entries from the new summary only.
3. Build merged output:
```
<summary>
Conversation summary:
- Previously compacted context:
  {existing highlight lines}
- Newly compacted context:
  {new highlight lines}
- Key timeline:
  {new timeline lines}
</summary>
```

### Helper Functions

#### extract_summary_highlights() (private)

Extracts all non-empty, non-header lines from a formatted summary, stopping when "- Key timeline:" is reached.

#### extract_summary_timeline() (private)

Extracts lines after "- Key timeline:" from a formatted summary, stopping at the first empty line.

#### extract_existing_compacted_summary() (private)

Checks if a message is a System message whose text starts with `COMPACT_CONTINUATION_PREAMBLE`. If so, strips the preamble, the `COMPACT_RECENT_MESSAGES_NOTE` suffix, and the `COMPACT_DIRECT_RESUME_INSTRUCTION` suffix, returning the raw summary.

## Helper Functions

### collect_recent_role_summaries() (private)

```rust
fn collect_recent_role_summaries(
    messages: &[ConversationMessage],
    role: MessageRole,
    limit: usize,
) -> Vec<String>;
```

Returns the last `limit` text blocks from messages with the given role, each truncated to 160 chars. Results are in chronological order (reversed after collecting from the end).

### infer_pending_work() (private)

```rust
fn infer_pending_work(messages: &[ConversationMessage]) -> Vec<String>;
```

Scans messages in reverse for text blocks containing keywords: "todo", "next", "pending", "follow up", "remaining" (case-insensitive). Returns up to 3 matches, each truncated to 160 chars, in chronological order.

### collect_key_files() (private)

```rust
fn collect_key_files(messages: &[ConversationMessage]) -> Vec<String>;
```

Extracts file path candidates from all block content. A candidate must:
- Be a whitespace-delimited token
- Contain `/`
- Have an extension matching: `rs`, `ts`, `tsx`, `js`, `json`, `md`

Returns up to 8 deduplicated, sorted file paths.

### extract_file_candidates() (private)

```rust
fn extract_file_candidates(content: &str) -> Vec<String>;
```

Splits content on whitespace, strips punctuation (`,`, `.`, `:`, `;`, `)`, `(`, `"`, `'`, `` ` ``), filters for candidates with `/` and interesting extensions.

### has_interesting_extension() (private)

```rust
fn has_interesting_extension(candidate: &str) -> bool;
```

Returns true if the path extension (case-insensitive) matches: `rs`, `ts`, `tsx`, `js`, `json`, `md`.

### truncate_summary() (private)

```rust
fn truncate_summary(content: &str, max_chars: usize) -> String;
```

If content exceeds `max_chars` characters, truncates and appends `...` (Unicode ellipsis character).

### XML Tag Helpers (private)

```rust
fn extract_tag_block(content: &str, tag: &str) -> Option<String>;
fn strip_tag_block(content: &str, tag: &str) -> String;
fn collapse_blank_lines(content: &str) -> String;
```

- `extract_tag_block()`: Extracts content between `<tag>` and `</tag>`.
- `strip_tag_block()`: Removes the entire `<tag>...</tag>` block from content.
- `collapse_blank_lines()`: Replaces consecutive blank lines with a single blank line.

### compacted_summary_prefix_len() (private)

```rust
fn compacted_summary_prefix_len(session: &Session) -> usize;
```

Returns 1 if the first message is an existing compacted summary (system message starting with continuation preamble), 0 otherwise. Used by `should_compact()` to skip the summary message when measuring session size.
