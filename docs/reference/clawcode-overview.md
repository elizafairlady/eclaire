# Claw Code Overview

Reference documentation for the Claw Code Rust workspace at `/tmp/claw-code/rust/`.

## Repository Identity

- **Repository**: `ultraworkers/claw-code`
- **Canonical implementation**: `rust/` directory
- **Binary name**: `claw`
- **Default model**: `claude-opus-4-6`
- **Default permissions**: `danger-full-access`
- **Stats**: ~20K lines of Rust, 9 crates in workspace

## Workspace Cargo.toml

```toml
[workspace]
members = ["crates/*"]
resolver = "2"

[workspace.package]
version = "0.1.0"
edition = "2021"
license = "MIT"
publish = false

[workspace.dependencies]
serde_json = "1"

[workspace.lints.rust]
unsafe_code = "forbid"

[workspace.lints.clippy]
all = { level = "warn", priority = -1 }
pedantic = { level = "warn", priority = -1 }
module_name_repetitions = "allow"
missing_panics_doc = "allow"
missing_errors_doc = "allow"
```

## Crate Map (9 crates)

### 1. `runtime`

**Path**: `crates/runtime/`

**Purpose**: Core runtime primitives. Session persistence, permission evaluation, prompt assembly, MCP plumbing, tool-facing file operations, and the core conversation loop.

**Dependencies**: `sha2`, `glob`, `plugins`, `regex`, `serde`, `serde_json`, `telemetry`, `tokio`, `walkdir`

**Cargo.toml**:
```toml
[package]
name = "runtime"

[dependencies]
sha2 = "0.10"
glob = "0.3"
plugins = { path = "../plugins" }
regex = "1"
serde = { version = "1", features = ["derive"] }
serde_json.workspace = true
telemetry = { path = "../telemetry" }
tokio = { version = "1", features = ["io-std", "io-util", "macros", "process", "rt", "rt-multi-thread", "time"] }
walkdir = "2"
```

### 2. `api`

**Path**: `crates/api/`

**Purpose**: Provider clients, SSE streaming, request/response types, auth (API key + OAuth bearer), request-size/context-window preflight.

**Dependencies**: `reqwest`, `runtime`, `serde`, `serde_json`, `telemetry`, `tokio`

**Cargo.toml**:
```toml
[package]
name = "api"

[dependencies]
reqwest = { version = "0.12", default-features = false, features = ["json", "rustls-tls"] }
runtime = { path = "../runtime" }
serde = { version = "1", features = ["derive"] }
serde_json.workspace = true
telemetry = { path = "../telemetry" }
tokio = { version = "1", features = ["io-util", "macros", "net", "rt-multi-thread", "time"] }
```

### 3. `tools`

**Path**: `crates/tools/`

**Purpose**: Tool specs and execution: Bash, ReadFile, WriteFile, EditFile, GlobSearch, GrepSearch, WebSearch, WebFetch, Agent, TodoWrite, NotebookEdit, Skill, ToolSearch, and runtime-facing tool discovery.

**Dependencies**: `api`, `commands`, `flate2`, `plugins`, `runtime`, `reqwest`, `serde`, `serde_json`, `tokio`

**Cargo.toml**:
```toml
[package]
name = "tools"

[dependencies]
api = { path = "../api" }
commands = { path = "../commands" }
flate2 = "1"
plugins = { path = "../plugins" }
runtime = { path = "../runtime" }
reqwest = { version = "0.12", default-features = false, features = ["blocking", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json.workspace = true
tokio = { version = "1", features = ["rt-multi-thread"] }
```

### 4. `commands`

**Path**: `crates/commands/`

**Purpose**: Slash command definitions, parsing, help text generation, JSON/text command rendering.

**Dependencies**: `plugins`, `runtime`, `serde_json`

### 5. `rusty-claude-cli`

**Path**: `crates/rusty-claude-cli/`

**Purpose**: Main CLI binary (`claw`). REPL, one-shot prompt, direct CLI subcommands, streaming display, tool call rendering, CLI argument parsing.

**Binary**: `claw` (defined in `src/main.rs`)

**Dependencies**: `api`, `commands`, `compat-harness`, `crossterm`, `pulldown-cmark`, `rustyline`, `runtime`, `plugins`, `serde`, `serde_json`, `syntect`, `tokio`, `tools`

**Dev Dependencies**: `mock-anthropic-service`, `serde_json`, `tokio`

### 6. `plugins`

**Path**: `crates/plugins/`

**Purpose**: Plugin metadata, install/enable/disable/update flows, plugin tool definitions, hook integration surfaces.

**Dependencies**: `serde`, `serde_json`

### 7. `telemetry`

**Path**: `crates/telemetry/`

**Purpose**: Session trace events and supporting telemetry payloads.

**Dependencies**: `serde`, `serde_json`

### 8. `mock-anthropic-service`

**Path**: `crates/mock-anthropic-service/`

**Purpose**: Deterministic `/v1/messages` mock for CLI parity tests and local harness runs.

**Binary**: `mock-anthropic-service` (defined in `src/main.rs`)

**Dependencies**: `api`, `serde_json`, `tokio`

### 9. `compat-harness`

**Path**: `crates/compat-harness/`

**Purpose**: Extracts tool/prompt manifests from upstream TypeScript source.

**Dependencies**: `commands`, `tools`, `runtime`

## Crate Dependency Graph

```
telemetry (leaf)
plugins (leaf)
    |
runtime (depends on: plugins, telemetry)
    |
api (depends on: runtime, telemetry)
commands (depends on: plugins, runtime)
    |
tools (depends on: api, commands, plugins, runtime)
compat-harness (depends on: commands, tools, runtime)
    |
rusty-claude-cli (depends on: api, commands, compat-harness, runtime, plugins, tools)
mock-anthropic-service (depends on: api)
```

## runtime/src/lib.rs Module Map

All modules declared in the runtime crate's `lib.rs`:

```rust
mod bash;                          // Shell command execution
pub mod bash_validation;           // Bash command validation
mod bootstrap;                     // Bootstrap phase/plan
pub mod branch_lock;               // Branch lock collision detection
mod compact;                       // Session compaction/summarization
mod config;                        // YAML config, loader, merged config
pub mod config_validate;           // Config file validation/diagnostics
mod conversation;                  // ConversationRuntime, ApiClient, ToolExecutor
mod file_ops;                      // edit_file, glob_search, grep_search, read_file, write_file
mod git_context;                   // Git context (commits, branch info)
pub mod green_contract;            // Green contract enforcement
mod hooks;                         // Pre/post tool-use shell hooks
mod json;                          // JSON value types (custom lightweight)
mod lane_events;                   // Lane event tracking (commit provenance, blockers)
pub mod lsp_client;                // LSP client integration
mod mcp;                           // MCP naming/prefix utilities
mod mcp_client;                    // MCP client transports (stdio, remote, SDK, proxy)
pub mod mcp_lifecycle_hardened;    // MCP lifecycle validation/degraded reporting
pub mod mcp_server;                // MCP server hosting (ToolCallHandler)
mod mcp_stdio;                     // MCP stdio process management, tool discovery
mod oauth;                         // OAuth PKCE flow, token persistence
pub mod permission_enforcer;       // PermissionEnforcer, EnforcementResult
mod permissions;                   // PermissionMode, PermissionPolicy, PermissionPrompter
pub mod plugin_lifecycle;          // Plugin state machine, healthcheck
mod policy_engine;                 // Policy rules, evaluation, lane blockers
mod prompt;                        // System prompt assembly (SystemPromptBuilder)
pub mod recovery_recipes;          // Failure recovery (scenarios, recipes, escalation)
mod remote;                        // Remote session context, upstream proxy
pub mod sandbox;                   // Linux sandbox, container detection, filesystem isolation
mod session;                       // Session persistence (JSONL), messages, fork
pub mod session_control;           // SessionStore
mod sse;                           // Incremental SSE parser
pub mod stale_base;                // Base commit staleness detection
pub mod stale_branch;              // Branch freshness policy
pub mod summary_compression;       // Summary text compression
pub mod task_packet;               // Structured task packets with validation
pub mod task_registry;             // In-memory task registry
pub mod team_cron_registry;        // Team and cron registries
mod trust_resolver;                // Trust config/decision/policy (test-only)
mod usage;                         // Token usage tracking, cost estimation
pub mod worker_boot;               // Worker boot lifecycle, events, registry
```

## Build Commands

```bash
cd rust/

# Build the workspace
cargo build --workspace

# Run the interactive REPL
cargo run -p rusty-claude-cli -- --model claude-opus-4-6

# One-shot prompt
cargo run -p rusty-claude-cli -- prompt "explain this codebase"

# JSON output for automation
cargo run -p rusty-claude-cli -- --output-format json prompt "summarize src/main.rs"

# Run workspace tests
cargo test --workspace

# Run verification
cargo fmt
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace

# Run mock parity harness
./scripts/run_mock_parity_harness.sh

# Start mock service manually
cargo run -p mock-anthropic-service -- --bind 127.0.0.1:0
```

## CLI Surface

```
claw [OPTIONS] [COMMAND]

Flags:
  --model MODEL
  --output-format text|json
  --permission-mode MODE
  --dangerously-skip-permissions
  --allowedTools TOOLS
  --resume [SESSION.jsonl|session-id|latest]
  --version, -V

Top-level commands:
  prompt <text>
  help
  version
  status
  sandbox
  dump-manifests
  bootstrap-plan
  agents
  mcp
  skills
  system-prompt
  login
  logout
  init
```

## Model Aliases

| Alias | Resolves To |
|-------|------------|
| `opus` | `claude-opus-4-6` |
| `sonnet` | `claude-sonnet-4-6` |
| `haiku` | `claude-haiku-4-5-20251213` |

## Authentication

```bash
# API key
export ANTHROPIC_API_KEY="sk-ant-..."

# Or proxy
export ANTHROPIC_BASE_URL="https://your-proxy.com"

# Or OAuth login
cargo run -p rusty-claude-cli -- login
```
