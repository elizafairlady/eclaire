# Claw Code Testing

Reference documentation for `rust/crates/mock-anthropic-service/src/lib.rs` and `rust/crates/rusty-claude-cli/tests/mock_parity_harness.rs`.

## MockAnthropicService (mock-anthropic-service crate)

A deterministic Anthropic-compatible mock HTTP service for CLI parity tests and local harness runs. Runs on tokio, listens on TCP.

### Constants

```rust
pub const SCENARIO_PREFIX: &str = "PARITY_SCENARIO:";
pub const DEFAULT_MODEL: &str = "claude-sonnet-4-6";
```

Scenario detection: the mock service scans the user messages in reverse for a text block containing a token prefixed with `PARITY_SCENARIO:`. The suffix identifies which scripted scenario to execute.

### CapturedRequest

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CapturedRequest {
    pub method: String,
    pub path: String,
    pub headers: HashMap<String, String>,
    pub scenario: String,
    pub stream: bool,
    pub raw_body: String,
}
```

Every request processed by the mock is captured and stored for later assertion by the test harness.

### MockAnthropicService Struct

```rust
pub struct MockAnthropicService {
    base_url: String,
    requests: Arc<Mutex<Vec<CapturedRequest>>>,
    shutdown: Option<oneshot::Sender<()>>,
    join_handle: JoinHandle<()>,
}
```

### Methods

```rust
impl MockAnthropicService {
    pub async fn spawn() -> io::Result<Self>;
    pub async fn spawn_on(bind_addr: &str) -> io::Result<Self>;
    pub fn base_url(&self) -> String;
    pub async fn captured_requests(&self) -> Vec<CapturedRequest>;
}
```

- `spawn()` -- binds to `127.0.0.1:0` (random port).
- `spawn_on(bind_addr)` -- binds to specified address.
- `base_url()` -- returns `http://{addr}`.
- `captured_requests()` -- returns all captured requests (thread-safe via Mutex).

### Drop Implementation

On drop, sends the shutdown signal and aborts the join handle, cleanly stopping the TCP listener.

### Scenario Enum (private)

```rust
enum Scenario {
    StreamingText,
    ReadFileRoundtrip,
    GrepChunkAssembly,
    WriteFileAllowed,
    WriteFileDenied,
    MultiToolTurnRoundtrip,
    BashStdoutRoundtrip,
    BashPermissionPromptApproved,
    BashPermissionPromptDenied,
    PluginToolRoundtrip,
    AutoCompactTriggered,
    TokenCostReporting,
}
```

**Parse mapping** (string to variant):

| String | Variant |
|--------|---------|
| `"streaming_text"` | `StreamingText` |
| `"read_file_roundtrip"` | `ReadFileRoundtrip` |
| `"grep_chunk_assembly"` | `GrepChunkAssembly` |
| `"write_file_allowed"` | `WriteFileAllowed` |
| `"write_file_denied"` | `WriteFileDenied` |
| `"multi_tool_turn_roundtrip"` | `MultiToolTurnRoundtrip` |
| `"bash_stdout_roundtrip"` | `BashStdoutRoundtrip` |
| `"bash_permission_prompt_approved"` | `BashPermissionPromptApproved` |
| `"bash_permission_prompt_denied"` | `BashPermissionPromptDenied` |
| `"plugin_tool_roundtrip"` | `PluginToolRoundtrip` |
| `"auto_compact_triggered"` | `AutoCompactTriggered` |
| `"token_cost_reporting"` | `TokenCostReporting` |

### Scenario Detection

```rust
fn detect_scenario(request: &MessageRequest) -> Option<Scenario>;
```

Iterates messages and content blocks in reverse, looking for a text block containing a whitespace-delimited token starting with `PARITY_SCENARIO:`. Extracts the suffix and parses it as a `Scenario`.

### Response Building

The mock service supports both streaming (SSE) and non-streaming (JSON) responses:

```rust
fn build_http_response(request: &MessageRequest, scenario: Scenario) -> String;
fn build_stream_body(request: &MessageRequest, scenario: Scenario) -> String;
fn build_message_response(request: &MessageRequest, scenario: Scenario) -> MessageResponse;
```

Each scenario implements multi-turn behavior by checking `latest_tool_result(request)`:
- **First request** (no tool results): Returns a tool_use response instructing the CLI to execute a tool.
- **Second request** (has tool result): Returns a final text response incorporating the tool result.

### Scenario Behaviors

**StreamingText**: Returns streaming text "Mock streaming says hello from the parity harness." No tool use.

**ReadFileRoundtrip**: First turn requests `read_file` with path `"fixture.txt"`. Second turn confirms the content was read.

**GrepChunkAssembly**: First turn requests `grep_search` with pattern `"parity"` on `"fixture.txt"` with `output_mode: "count"`. The tool input is sent as multiple JSON chunks to test chunk assembly. Second turn reports match count.

**WriteFileAllowed**: First turn requests `write_file` to `"generated/output.txt"` with content `"created by mock service\n"`. Second turn confirms success.

**WriteFileDenied**: Same write_file request but the harness runs in `read-only` mode, so the tool returns a permission denial. Second turn confirms the denial.

**MultiToolTurnRoundtrip**: First turn requests BOTH `read_file` AND `grep_search` in a single response (parallel tool use). Uses `tool_results_by_name()` to match results. Second turn confirms both results.

**BashStdoutRoundtrip**: First turn requests `bash` with command `printf 'alpha from bash'`. Second turn echoes the stdout.

**BashPermissionPromptApproved**: First turn requests `bash` in `workspace-write` mode (requiring escalation). Harness provides `"y\n"` on stdin. Tool result should succeed.

**BashPermissionPromptDenied**: Same as above but with `"n\n"` on stdin. Tool result should be an error with denial message.

**PluginToolRoundtrip**: First turn requests `plugin_echo` (a plugin tool). Second turn confirms the plugin output.

**AutoCompactTriggered**: Returns text with `input_tokens: 50_000` and `output_tokens: 200`. Tests auto_compaction field presence in JSON output.

**TokenCostReporting**: Returns text with `input_tokens: 1_000` and `output_tokens: 500`. Tests cost reporting fields in JSON output.

### Request ID Assignment

Each scenario has a deterministic `x-request-id` header (e.g., `"req_streaming_text"`, `"req_read_file_roundtrip"`).

### Helper Functions

```rust
fn latest_tool_result(request: &MessageRequest) -> Option<(String, bool)>;
fn tool_results_by_name(request: &MessageRequest) -> HashMap<String, (String, bool)>;
fn flatten_tool_result_content(content: &[ToolResultContentBlock]) -> String;
```

- `latest_tool_result()` -- finds the most recent tool result in the message history.
- `tool_results_by_name()` -- maps tool names to their results by cross-referencing tool_use IDs.
- `flatten_tool_result_content()` -- joins text/JSON content blocks into a single string.

## Mock Parity Harness (mock_parity_harness.rs)

End-to-end CLI test harness that exercises the `claw` binary against the mock service.

### Test Function

```rust
#[test]
fn clean_env_cli_reaches_mock_anthropic_service_across_scripted_parity_scenarios();
```

Single test function that runs all 12 scenarios sequentially.

### ScenarioCase Struct

```rust
#[derive(Clone, Copy)]
struct ScenarioCase {
    name: &'static str,
    permission_mode: &'static str,
    allowed_tools: Option<&'static str>,
    stdin: Option<&'static str>,
    prepare: fn(&HarnessWorkspace),
    assert: fn(&HarnessWorkspace, &ScenarioRun),
    extra_env: Option<(&'static str, &'static str)>,
    resume_session: Option<&'static str>,
}
```

### HarnessWorkspace

```rust
struct HarnessWorkspace {
    root: PathBuf,
    config_home: PathBuf,
    home: PathBuf,
}

impl HarnessWorkspace {
    fn new(root: PathBuf) -> Self;
    fn create(&self) -> std::io::Result<()>;
}
```

Creates a clean-environment workspace with:
- `root/` -- working directory for the CLI
- `root/config-home/` -- for `CLAW_CONFIG_HOME`
- `root/home/` -- for `HOME`

### ScenarioRun

```rust
struct ScenarioRun {
    response: Value,     // parsed JSON output from stdout
    stdout: String,      // raw stdout
}
```

### ScenarioManifestEntry

```rust
struct ScenarioManifestEntry {
    name: String,
    category: String,
    description: String,
    parity_refs: Vec<String>,
}
```

Loaded from `mock_parity_scenarios.json` at the workspace root.

### ScenarioReport

```rust
struct ScenarioReport {
    name: String,
    category: String,
    description: String,
    parity_refs: Vec<String>,
    iterations: u64,
    request_count: usize,
    tool_uses: Vec<String>,
    tool_error_count: usize,
    final_message: String,
}
```

### Test Execution Pattern

1. **Load scenario manifest** from `mock_parity_scenarios.json`.
2. **Spawn MockAnthropicService** via tokio runtime.
3. **Define all 12 ScenarioCases** as a static array.
4. **Assert manifest and cases are aligned** (same names, same order).
5. **For each case**:
   1. Create a unique temporary `HarnessWorkspace`.
   2. Call `prepare` function to set up fixtures.
   3. Run the `claw` binary via `Command` with clean environment:
      - `ANTHROPIC_API_KEY=test-parity-key`
      - `ANTHROPIC_BASE_URL={mock_url}`
      - `CLAW_CONFIG_HOME={workspace.config_home}`
      - `HOME={workspace.home}`
      - `NO_COLOR=1`
      - `PATH=/usr/bin:/bin`
      - `--model sonnet --permission-mode {mode} --output-format=json`
      - `--allowedTools {tools}` (if specified)
      - Prompt: `PARITY_SCENARIO:{scenario_name}`
      - Stdin piped if `case.stdin` is set.
   4. Assert process exited successfully.
   5. Parse JSON output from stdout.
   6. Call `assert` function to validate the scenario.
   7. Build scenario report.
   8. Clean up workspace.
6. **Assert captured requests**: After all scenarios, verify that exactly 21 `/v1/messages` requests were captured (some scenarios do 2 rounds, some do 1). All must be streaming.
7. **Verify request ordering** matches the expected sequence.
8. **Optionally write a report** to `MOCK_PARITY_REPORT_PATH` if that environment variable is set.

### All 12 Scenarios with Assertions

#### 1. streaming_text

- **Permission mode**: `read-only`
- **Allowed tools**: None
- **Prepare**: noop
- **Asserts**:
  - `response["message"]` == `"Mock streaming says hello from the parity harness."`
  - `response["iterations"]` == 1
  - `response["tool_uses"]` == empty array
  - `response["tool_results"]` == empty array

#### 2. read_file_roundtrip

- **Permission mode**: `read-only`
- **Allowed tools**: `read_file`
- **Prepare**: writes `fixture.txt` with `"alpha parity line\n"`
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"read_file"`
  - First tool use input == `{"path":"fixture.txt"}`
  - `response["message"]` contains `"alpha parity line"`
  - Tool output contains the absolute path to fixture.txt and its content

#### 3. grep_chunk_assembly

- **Permission mode**: `read-only`
- **Allowed tools**: `grep_search`
- **Prepare**: writes `fixture.txt` with 3 lines (2 containing "parity")
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"grep_search"`
  - First tool use input includes `"pattern":"parity"` and `"output_mode":"count"`
  - `response["message"]` contains `"2 occurrences"`
  - First tool result `is_error` == false

#### 4. write_file_allowed

- **Permission mode**: `workspace-write`
- **Allowed tools**: `write_file`
- **Prepare**: noop
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"write_file"`
  - `response["message"]` contains `"generated/output.txt"`
  - File `generated/output.txt` exists with content `"created by mock service\n"`
  - First tool result `is_error` == false

#### 5. write_file_denied

- **Permission mode**: `read-only`
- **Allowed tools**: `write_file`
- **Prepare**: noop
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"write_file"`
  - Tool output contains `"requires workspace-write permission"`
  - First tool result `is_error` == true
  - `response["message"]` contains `"denied as expected"`
  - File `generated/denied.txt` does NOT exist

#### 6. multi_tool_turn_roundtrip

- **Permission mode**: `read-only`
- **Allowed tools**: `read_file,grep_search`
- **Prepare**: writes `fixture.txt` with 3 lines
- **Asserts**:
  - `response["iterations"]` == 2
  - Two tool uses: `read_file` and `grep_search`
  - Two tool results
  - `response["message"]` contains `"alpha parity line"` and `"2 occurrences"`

#### 7. bash_stdout_roundtrip

- **Permission mode**: `danger-full-access`
- **Allowed tools**: `bash`
- **Prepare**: noop
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"bash"`
  - Tool output (JSON) has `stdout` == `"alpha from bash"`
  - First tool result `is_error` == false
  - `response["message"]` contains `"alpha from bash"`

#### 8. bash_permission_prompt_approved

- **Permission mode**: `workspace-write`
- **Allowed tools**: `bash`
- **Stdin**: `"y\n"`
- **Prepare**: noop
- **Asserts**:
  - Raw stdout contains `"Permission approval required"`
  - Raw stdout contains `"Approve this tool call? [y/N]:"`
  - `response["iterations"]` == 2
  - First tool result `is_error` == false
  - Tool output (JSON) has `stdout` == `"approved via prompt"`
  - `response["message"]` contains `"approved and executed"`

#### 9. bash_permission_prompt_denied

- **Permission mode**: `workspace-write`
- **Allowed tools**: `bash`
- **Stdin**: `"n\n"`
- **Prepare**: noop
- **Asserts**:
  - Raw stdout contains `"Permission approval required"`
  - Raw stdout contains `"Approve this tool call? [y/N]:"`
  - `response["iterations"]` == 2
  - Tool output contains `"denied by user approval prompt"`
  - First tool result `is_error` == true
  - `response["message"]` contains `"denied as expected"`

#### 10. plugin_tool_roundtrip

- **Permission mode**: `workspace-write`
- **Allowed tools**: None (plugin tools are auto-included)
- **Prepare**: Creates plugin fixture:
  - `external-plugins/parity-plugin/.claude-plugin/plugin.json` with tool `plugin_echo`
  - `external-plugins/parity-plugin/tools/echo-json.sh` (executable shell script that echoes JSON with plugin/tool/input)
  - `settings.json` in config_home enabling the plugin
- **Asserts**:
  - `response["iterations"]` == 2
  - First tool use name == `"plugin_echo"`
  - Tool output (JSON) has `plugin` == `"parity-plugin@external"`, `tool` == `"plugin_echo"`, `input.message` == `"hello from plugin parity"`
  - `response["message"]` contains `"hello from plugin parity"`

#### 11. auto_compact_triggered

- **Permission mode**: `read-only`
- **Allowed tools**: None
- **Prepare**: noop
- **Asserts**:
  - `response["iterations"]` == 1
  - `response["tool_uses"]` == empty array
  - `response["message"]` contains `"auto compact parity complete."`
  - `response` object contains `"auto_compaction"` key
  - `response["usage"]["input_tokens"]` >= 50,000

#### 12. token_cost_reporting

- **Permission mode**: `read-only`
- **Allowed tools**: None
- **Prepare**: noop
- **Asserts**:
  - `response["iterations"]` == 1
  - `response["message"]` contains `"token cost reporting parity complete."`
  - `response["usage"]["input_tokens"]` > 0
  - `response["usage"]["output_tokens"]` > 0
  - `response["estimated_cost"]` is a string starting with `"$"`

### Global Request Count Assertions

After all 12 scenarios run, the harness verifies:

- Exactly **21** `/v1/messages` requests were captured total (after filtering out `/v1/messages/count_tokens` preflight requests).
- All 21 requests used streaming mode.
- The request scenarios in order are:
  ```
  streaming_text,
  read_file_roundtrip, read_file_roundtrip,
  grep_chunk_assembly, grep_chunk_assembly,
  write_file_allowed, write_file_allowed,
  write_file_denied, write_file_denied,
  multi_tool_turn_roundtrip, multi_tool_turn_roundtrip,
  bash_stdout_roundtrip, bash_stdout_roundtrip,
  bash_permission_prompt_approved, bash_permission_prompt_approved,
  bash_permission_prompt_denied, bash_permission_prompt_denied,
  plugin_tool_roundtrip, plugin_tool_roundtrip,
  auto_compact_triggered,
  token_cost_reporting
  ```

Single-turn scenarios (streaming_text, auto_compact_triggered, token_cost_reporting) produce 1 request each. Two-turn scenarios (the rest) produce 2 requests each: first for the tool_use, second for the final response.

### Report Generation

If `MOCK_PARITY_REPORT_PATH` is set, writes a JSON report with:
```json
{
    "scenario_count": 12,
    "request_count": 21,
    "scenarios": [
        {
            "name": "streaming_text",
            "category": "...",
            "description": "...",
            "parity_refs": ["..."],
            "iterations": 1,
            "request_count": 1,
            "tool_uses": [],
            "tool_error_count": 0,
            "final_message": "Mock streaming says hello from the parity harness."
        },
        ...
    ]
}
```
