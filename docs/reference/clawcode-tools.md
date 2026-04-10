# Claw Code Tools

Reference documentation for `rust/crates/tools/src/lib.rs`.

## Global Singleton Registries

The tools crate maintains several global singleton registries via `OnceLock`:

```rust
fn global_lsp_registry() -> &'static LspRegistry;
fn global_mcp_registry() -> &'static McpToolRegistry;
fn global_team_registry() -> &'static TeamRegistry;
fn global_cron_registry() -> &'static CronRegistry;
fn global_task_registry() -> &'static TaskRegistry;
fn global_worker_registry() -> &'static WorkerRegistry;
```

Each is initialized once on first access and shared across all tool invocations within the process.

## ToolManifestEntry

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolManifestEntry {
    pub name: String,
    pub source: ToolSource,
}
```

## ToolSource

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolSource {
    Base,
    Conditional,
}
```

## ToolRegistry

```rust
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ToolRegistry {
    entries: Vec<ToolManifestEntry>,
}

impl ToolRegistry {
    pub fn new(entries: Vec<ToolManifestEntry>) -> Self;
    pub fn entries(&self) -> &[ToolManifestEntry];
}
```

## ToolSpec

Static tool specification with name, description, input schema, and required permission level.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolSpec {
    pub name: &'static str,
    pub description: &'static str,
    pub input_schema: Value,
    pub required_permission: PermissionMode,
}
```

## RuntimeToolDefinition

Dynamic tool definition registered at runtime (e.g., from MCP servers).

```rust
#[derive(Debug, Clone, PartialEq)]
pub struct RuntimeToolDefinition {
    pub name: String,
    pub description: Option<String>,
    pub input_schema: Value,
    pub required_permission: PermissionMode,
}
```

## GlobalToolRegistry

```rust
#[derive(Debug, Clone)]
pub struct GlobalToolRegistry {
    plugin_tools: Vec<PluginTool>,
    runtime_tools: Vec<RuntimeToolDefinition>,
    enforcer: Option<PermissionEnforcer>,
}
```

### Constructors

```rust
pub fn builtin() -> Self;
```

Creates a registry with no plugin tools, no runtime tools, and no enforcer.

```rust
pub fn with_plugin_tools(plugin_tools: Vec<PluginTool>) -> Result<Self, String>;
```

Creates a registry with plugin tools. Validates that no plugin tool name conflicts with a built-in tool name, and that no two plugin tools share a name.

### Builder Methods

```rust
pub fn with_runtime_tools(mut self, runtime_tools: Vec<RuntimeToolDefinition>) -> Result<Self, String>;
pub fn with_enforcer(mut self, enforcer: PermissionEnforcer) -> Self;
pub fn set_enforcer(&mut self, enforcer: PermissionEnforcer);
pub fn has_runtime_tool(&self, name: &str) -> bool;
```

`with_runtime_tools()` validates that no runtime tool name conflicts with any existing built-in or plugin tool name.

### normalize_allowed_tools()

```rust
pub fn normalize_allowed_tools(
    &self,
    values: &[String],
) -> Result<Option<BTreeSet<String>>, String>;
```

Parses `--allowedTools` CLI values into a canonical set of tool names. Empty input returns `None` (no filtering). Supports comma/whitespace-separated tool names.

**Tool aliases:**

| Alias | Canonical |
|-------|-----------|
| `read` | `read_file` |
| `write` | `write_file` |
| `edit` | `edit_file` |
| `glob` | `glob_search` |
| `grep` | `grep_search` |

Tool names are normalized by trimming, replacing hyphens with underscores, and lowercasing. Returns an error if any token is not a recognized tool name.

### definitions()

```rust
pub fn definitions(&self, allowed_tools: Option<&BTreeSet<String>>) -> Vec<ToolDefinition>;
```

Returns API `ToolDefinition` objects for all tools (built-in + runtime + plugin), filtered by the optional allowed set. Used to build the tool list sent in API requests.

### permission_specs()

```rust
pub fn permission_specs(
    &self,
    allowed_tools: Option<&BTreeSet<String>>,
) -> Result<Vec<(String, PermissionMode)>, String>;
```

Returns `(tool_name, required_permission)` pairs for all tools, filtered by the optional allowed set.

### search()

```rust
pub fn search(
    &self,
    query: &str,
    max_results: usize,
    pending_mcp_servers: Option<Vec<String>>,
    mcp_degraded: Option<McpDegradedReport>,
) -> ToolSearchOutput;
```

Searches deferred tool specs (all tools except the 6 base tools: bash, read_file, write_file, edit_file, glob_search, grep_search) plus runtime and plugin tools. Supports `"select:ToolName1,ToolName2"` for exact selection or keyword matching.

### execute()

```rust
pub fn execute(&self, name: &str, input: &Value) -> Result<String, String>;
```

Executes a tool by name. If the tool is a built-in (in `mvp_tool_specs()`), delegates to `execute_tool_with_enforcer()`. Otherwise, searches plugin tools and calls the plugin's execute method.

## deferred_tool_specs() (private)

```rust
fn deferred_tool_specs() -> Vec<ToolSpec>;
```

Returns all tool specs EXCEPT the 6 base tools (`bash`, `read_file`, `write_file`, `edit_file`, `glob_search`, `grep_search`). These are the tools available via the `ToolSearch` tool.

## mvp_tool_specs() -- All Tool Specifications

Returns all built-in tool specs. Listed below with complete schema information.

### 1. bash

- **Description**: Execute a shell command in the current workspace.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `command` (string, **required**)
  - `timeout` (integer, min 1)
  - `description` (string)
  - `run_in_background` (boolean)
  - `dangerouslyDisableSandbox` (boolean)
  - `namespaceRestrictions` (boolean)
  - `isolateNetwork` (boolean)
  - `filesystemMode` (string, enum: `"off"`, `"workspace-only"`, `"allow-list"`)
  - `allowedMounts` (array of strings)

### 2. read_file

- **Description**: Read a text file from the workspace.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `path` (string, **required**)
  - `offset` (integer, min 0)
  - `limit` (integer, min 1)

### 3. write_file

- **Description**: Write a text file in the workspace.
- **Required permission**: `WorkspaceWrite`
- **Input schema**:
  - `path` (string, **required**)
  - `content` (string, **required**)

### 4. edit_file

- **Description**: Replace text in a workspace file.
- **Required permission**: `WorkspaceWrite`
- **Input schema**:
  - `path` (string, **required**)
  - `old_string` (string, **required**)
  - `new_string` (string, **required**)
  - `replace_all` (boolean)

### 5. glob_search

- **Description**: Find files by glob pattern.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `pattern` (string, **required**)
  - `path` (string)

### 6. grep_search

- **Description**: Search file contents with a regex pattern.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `pattern` (string, **required**)
  - `path` (string)
  - `glob` (string)
  - `output_mode` (string)
  - `-B` (integer, min 0)
  - `-A` (integer, min 0)
  - `-C` (integer, min 0)
  - `context` (integer, min 0)
  - `-n` (boolean)
  - `-i` (boolean)
  - `type` (string)
  - `head_limit` (integer, min 1)
  - `offset` (integer, min 0)
  - `multiline` (boolean)

### 7. WebFetch

- **Description**: Fetch a URL, convert it into readable text, and answer a prompt about it.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `url` (string, format: uri, **required**)
  - `prompt` (string, **required**)

### 8. WebSearch

- **Description**: Search the web for current information and return cited results.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `query` (string, minLength 2, **required**)
  - `allowed_domains` (array of strings)
  - `blocked_domains` (array of strings)

### 9. TodoWrite

- **Description**: Update the structured task list for the current session.
- **Required permission**: `WorkspaceWrite`
- **Input schema**:
  - `todos` (array, **required**) -- items with:
    - `content` (string, **required**)
    - `activeForm` (string, **required**)
    - `status` (string, enum: `"pending"`, `"in_progress"`, `"completed"`, **required**)

### 10. Skill

- **Description**: Load a local skill definition and its instructions.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `skill` (string, **required**)
  - `args` (string)

### 11. Agent

- **Description**: Launch a specialized agent task and persist its handoff metadata.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `description` (string, **required**)
  - `prompt` (string, **required**)
  - `subagent_type` (string)
  - `name` (string)
  - `model` (string)

### 12. ToolSearch

- **Description**: Search for deferred or specialized tools by exact name or keywords.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `query` (string, **required**)
  - `max_results` (integer, min 1)

### 13. NotebookEdit

- **Description**: Replace, insert, or delete a cell in a Jupyter notebook.
- **Required permission**: `WorkspaceWrite`
- **Input schema**:
  - `notebook_path` (string, **required**)
  - `cell_id` (string)
  - `new_source` (string)
  - `cell_type` (string, enum: `"code"`, `"markdown"`)
  - `edit_mode` (string, enum: `"replace"`, `"insert"`, `"delete"`)

### 14. Sleep

- **Description**: Wait for a specified duration without holding a shell process.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `duration_ms` (integer, min 0, **required**)

### 15. SendUserMessage

- **Description**: Send a message to the user.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `message` (string, **required**)
  - `attachments` (array of strings)
  - `status` (string, enum: `"normal"`, `"proactive"`, **required**)

### 16. Config

- **Description**: Get or set Claude Code settings.
- **Required permission**: `WorkspaceWrite`
- **Input schema**:
  - `setting` (string, **required**)
  - `value` (string | boolean | number)

### 17. EnterPlanMode

- **Description**: Enable a worktree-local planning mode override and remember the previous local setting for ExitPlanMode.
- **Required permission**: `WorkspaceWrite`
- **Input schema**: empty object

### 18. ExitPlanMode

- **Description**: Restore or clear the worktree-local planning mode override created by EnterPlanMode.
- **Required permission**: `WorkspaceWrite`
- **Input schema**: empty object

### 19. StructuredOutput

- **Description**: Return structured output in the requested format.
- **Required permission**: `ReadOnly`
- **Input schema**: `{ "type": "object", "additionalProperties": true }`

### 20. REPL

- **Description**: Execute code in a REPL-like subprocess.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `code` (string, **required**)
  - `language` (string, **required**)
  - `timeout_ms` (integer, min 1)

### 21. PowerShell

- **Description**: Execute a PowerShell command with optional timeout.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `command` (string, **required**)
  - `timeout` (integer, min 1)
  - `description` (string)
  - `run_in_background` (boolean)

### 22. AskUserQuestion

- **Description**: Ask the user a question and wait for their response.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `question` (string, **required**)
  - `options` (array of strings)

### 23. TaskCreate

- **Description**: Create a background task that runs in a separate subprocess.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `prompt` (string, **required**)
  - `description` (string)

### 24. RunTaskPacket

- **Description**: Create a background task from a structured task packet.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `objective` (string, **required**)
  - `scope` (string, **required**)
  - `repo` (string, **required**)
  - `branch_policy` (string, **required**)
  - `acceptance_tests` (array of strings, **required**)
  - `commit_policy` (string, **required**)
  - `reporting_contract` (string, **required**)
  - `escalation_policy` (string, **required**)

### 25. TaskGet

- **Description**: Get the status and details of a background task by ID.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `task_id` (string, **required**)

### 26. TaskList

- **Description**: List all background tasks and their current status.
- **Required permission**: `ReadOnly`
- **Input schema**: empty object

### 27. TaskStop

- **Description**: Stop a running background task by ID.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `task_id` (string, **required**)

### 28. TaskUpdate

- **Description**: Send a message or update to a running background task.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `task_id` (string, **required**)
  - `message` (string, **required**)

### 29. TaskOutput

- **Description**: Retrieve the output produced by a background task.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `task_id` (string, **required**)

### 30. WorkerCreate

- **Description**: Create a coding worker boot session with trust-gate and prompt-delivery guards.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `cwd` (string, **required**)
  - `trusted_roots` (array of strings)
  - `auto_recover_prompt_misdelivery` (boolean)

### 31. WorkerGet

- **Description**: Fetch the current worker boot state, last error, and event history.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `worker_id` (string, **required**)

### 32. WorkerObserve

- **Description**: Feed a terminal snapshot into worker boot detection to resolve trust gates, ready handshakes, and prompt misdelivery.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `worker_id` (string, **required**)
  - `screen_text` (string, **required**)

### 33. WorkerResolveTrust

- **Description**: Resolve a detected trust prompt so worker boot can continue.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `worker_id` (string, **required**)

### 34. WorkerAwaitReady

- **Description**: Return the current ready-handshake verdict for a coding worker.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `worker_id` (string, **required**)

### 35. WorkerSendPrompt

- **Description**: Send a task prompt only after the worker reaches ready_for_prompt; can replay a recovered prompt.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `worker_id` (string, **required**)
  - `prompt` (string)

### 36. WorkerRestart

- **Description**: Restart worker boot state after a failed or stale startup.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `worker_id` (string, **required**)

### 37. WorkerTerminate

- **Description**: Terminate a worker and mark the lane finished from the control plane.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `worker_id` (string, **required**)

### 38. WorkerObserveCompletion

- **Description**: Report session completion to the worker, classifying finish_reason into Finished or Failed (provider-degraded).
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `worker_id` (string, **required**)
  - `finish_reason` (string, **required**)
  - `tokens_output` (integer, min 0, **required**)

### 39. TeamCreate

- **Description**: Create a team of sub-agents for parallel task execution.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `name` (string, **required**)
  - `tasks` (array, **required**) -- items with:
    - `prompt` (string, **required**)
    - `description` (string)

### 40. TeamDelete

- **Description**: Delete a team and stop all its running tasks.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `team_id` (string, **required**)

### 41. CronCreate

- **Description**: Create a scheduled recurring task.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `schedule` (string, **required**)
  - `prompt` (string, **required**)
  - `description` (string)

### 42. CronDelete

- **Description**: Delete a scheduled recurring task by ID.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `cron_id` (string, **required**)

### 43. CronList

- **Description**: List all scheduled recurring tasks.
- **Required permission**: `ReadOnly`
- **Input schema**: empty object

### 44. LSP

- **Description**: Query Language Server Protocol for code intelligence (symbols, references, diagnostics).
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `action` (string, enum: `"symbols"`, `"references"`, `"diagnostics"`, `"definition"`, `"hover"`, **required**)
  - `path` (string)
  - `line` (integer, min 0)
  - `character` (integer, min 0)
  - `query` (string)

### 45. ListMcpResources

- **Description**: List available resources from connected MCP servers.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `server` (string)

### 46. ReadMcpResource

- **Description**: Read a specific resource from an MCP server by URI.
- **Required permission**: `ReadOnly`
- **Input schema**:
  - `uri` (string, **required**)
  - `server` (string)

### 47. McpAuth

- **Description**: Authenticate with an MCP server that requires OAuth or credentials.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `server` (string, **required**)

### 48. RemoteTrigger

- **Description**: Trigger a remote action or webhook endpoint.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `url` (string, **required**)
  - `method` (string, enum: `"GET"`, `"POST"`, `"PUT"`, `"DELETE"`)
  - `headers` (object)
  - `body` (string)

### 49. MCP

- **Description**: Execute a tool provided by a connected MCP server.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `server` (string, **required**)
  - `tool` (string, **required**)
  - `arguments` (object)

### 50. TestingPermission

- **Description**: Test-only tool for verifying permission enforcement behavior.
- **Required permission**: `DangerFullAccess`
- **Input schema**:
  - `action` (string, **required**)

## Tool Permission Summary

| Permission Level | Tools |
|-----------------|-------|
| **ReadOnly** | `read_file`, `glob_search`, `grep_search`, `WebFetch`, `WebSearch`, `Skill`, `ToolSearch`, `Sleep`, `SendUserMessage`, `AskUserQuestion`, `StructuredOutput`, `TaskGet`, `TaskList`, `TaskOutput`, `WorkerGet`, `WorkerObserve`, `WorkerAwaitReady`, `CronList`, `LSP`, `ListMcpResources`, `ReadMcpResource` |
| **WorkspaceWrite** | `write_file`, `edit_file`, `TodoWrite`, `NotebookEdit`, `Config`, `EnterPlanMode`, `ExitPlanMode` |
| **DangerFullAccess** | `bash`, `Agent`, `REPL`, `PowerShell`, `TaskCreate`, `RunTaskPacket`, `TaskStop`, `TaskUpdate`, `WorkerCreate`, `WorkerResolveTrust`, `WorkerSendPrompt`, `WorkerRestart`, `WorkerTerminate`, `WorkerObserveCompletion`, `TeamCreate`, `TeamDelete`, `CronCreate`, `CronDelete`, `McpAuth`, `RemoteTrigger`, `MCP`, `TestingPermission` |

## Tool Execution Dispatch

```rust
pub fn execute_tool(name: &str, input: &Value) -> Result<String, String>;
fn execute_tool_with_enforcer(enforcer: Option<&PermissionEnforcer>, name: &str, input: &Value) -> Result<String, String>;
pub fn enforce_permission_check(enforcer: &PermissionEnforcer, tool_name: &str, input: &Value) -> Result<(), String>;
```

`execute_tool_with_enforcer()` dispatches to specific handlers by tool name match:
- `"bash"` -> `run_bash()`
- `"read_file"` -> `run_read_file()`
- `"write_file"` -> `run_write_file()`
- `"edit_file"` -> `run_edit_file()`
- `"glob_search"` -> `run_glob_search()`
- `"grep_search"` -> `run_grep_search()`
- And so on for all built-in tools.

Each handler optionally checks permissions first via `maybe_enforce_permission_check()`, then deserializes the input JSON and calls the appropriate runtime function.
