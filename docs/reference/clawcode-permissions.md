# Claw Code Permissions

Reference documentation for `rust/crates/runtime/src/permission_enforcer.rs` and `rust/crates/runtime/src/permissions.rs`.

## PermissionMode (permissions.rs)

Permission level assigned to a tool invocation or runtime session. Implements `PartialOrd`/`Ord` for mode comparison.

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum PermissionMode {
    ReadOnly,
    WorkspaceWrite,
    DangerFullAccess,
    Prompt,
    Allow,
}
```

**Ordering** (lowest to highest): `ReadOnly < WorkspaceWrite < DangerFullAccess < Prompt < Allow`

### as_str() mapping

| Variant | String |
|---------|--------|
| `ReadOnly` | `"read-only"` |
| `WorkspaceWrite` | `"workspace-write"` |
| `DangerFullAccess` | `"danger-full-access"` |
| `Prompt` | `"prompt"` |
| `Allow` | `"allow"` |

### 5 Permission Modes Explained

1. **ReadOnly** -- Only read-only tools (read_file, grep_search, glob_search) are allowed. Bash commands are checked against a read-only heuristic whitelist.

2. **WorkspaceWrite** -- Read-only tools plus file-writing tools within the workspace boundary. Bash is allowed. Tools requiring `DangerFullAccess` trigger a prompt if a prompter is provided, or are denied.

3. **DangerFullAccess** -- All tools are allowed without prompting. No restrictions.

4. **Prompt** -- Every tool invocation that would otherwise be denied requires interactive approval. The `PermissionEnforcer.check()` method returns `Allowed` when mode is `Prompt` (deferring to caller's interactive flow). However, `check_bash()` and `check_file_write()` return `Denied` in Prompt mode, requiring the caller to handle prompting.

5. **Allow** -- Everything is permitted. No restrictions, no prompts.

## PermissionOverride (permissions.rs)

Hook-provided override applied before standard permission evaluation.

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PermissionOverride {
    Allow,
    Deny,
    Ask,
}
```

## PermissionContext (permissions.rs)

Additional permission context supplied by hooks or higher-level orchestration.

```rust
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct PermissionContext {
    override_decision: Option<PermissionOverride>,
    override_reason: Option<String>,
}

impl PermissionContext {
    pub fn new(
        override_decision: Option<PermissionOverride>,
        override_reason: Option<String>,
    ) -> Self;
    pub fn override_decision(&self) -> Option<PermissionOverride>;
    pub fn override_reason(&self) -> Option<&str>;
}
```

## PermissionRequest (permissions.rs)

Full authorization request presented to a permission prompt.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PermissionRequest {
    pub tool_name: String,
    pub input: String,
    pub current_mode: PermissionMode,
    pub required_mode: PermissionMode,
    pub reason: Option<String>,
}
```

## PermissionPromptDecision (permissions.rs)

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PermissionPromptDecision {
    Allow,
    Deny { reason: String },
}
```

## PermissionPrompter Trait (permissions.rs)

Prompting interface used when policy requires interactive approval.

```rust
pub trait PermissionPrompter {
    fn decide(&mut self, request: &PermissionRequest) -> PermissionPromptDecision;
}
```

## PermissionOutcome (permissions.rs)

Final authorization result after evaluating static rules and prompts.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PermissionOutcome {
    Allow,
    Deny { reason: String },
}
```

## PermissionPolicy (permissions.rs)

Evaluates permission mode requirements plus allow/deny/ask rules.

```rust
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PermissionPolicy {
    active_mode: PermissionMode,
    tool_requirements: BTreeMap<String, PermissionMode>,
    allow_rules: Vec<PermissionRule>,
    deny_rules: Vec<PermissionRule>,
    ask_rules: Vec<PermissionRule>,
}
```

### Constructor and Builder Methods

```rust
impl PermissionPolicy {
    pub fn new(active_mode: PermissionMode) -> Self;
    pub fn with_tool_requirement(mut self, tool_name: impl Into<String>, required_mode: PermissionMode) -> Self;
    pub fn with_permission_rules(mut self, config: &RuntimePermissionRuleConfig) -> Self;
    pub fn active_mode(&self) -> PermissionMode;
    pub fn required_mode_for(&self, tool_name: &str) -> PermissionMode;
}
```

`required_mode_for()` returns the registered requirement for a tool, defaulting to `DangerFullAccess` if none is registered.

### authorize() Method

```rust
pub fn authorize(
    &self,
    tool_name: &str,
    input: &str,
    prompter: Option<&mut dyn PermissionPrompter>,
) -> PermissionOutcome;
```

Delegates to `authorize_with_context()` with `PermissionContext::default()`.

### authorize_with_context() -- Full Authorization Flow

```rust
pub fn authorize_with_context(
    &self,
    tool_name: &str,
    input: &str,
    context: &PermissionContext,
    prompter: Option<&mut dyn PermissionPrompter>,
) -> PermissionOutcome;
```

**Evaluation order:**

1. **Deny rules checked first**: If any deny rule matches `(tool_name, input)`, immediately return `Deny` with message referencing the rule.

2. **Collect ask and allow rule matches** for this tool/input.

3. **Process hook override** from `context.override_decision()`:
   - `Some(Deny)`: immediately return `Deny` with hook reason.
   - `Some(Ask)`: prompt user (or deny if no prompter), with hook reason.
   - `Some(Allow)`: if an ask rule also matches, prompt anyway. If an allow rule matches, or `current_mode == Allow`, or `current_mode >= required_mode`, return `Allow`. Otherwise fall through.
   - `None`: fall through to standard evaluation.

4. **Ask rules**: If an ask rule matches, prompt user (or deny if no prompter).

5. **Allow rules / mode check**: If an allow rule matches, or `current_mode == Allow`, or `current_mode >= required_mode`, return `Allow`.

6. **Escalation prompt**: If `current_mode == Prompt`, or if `current_mode == WorkspaceWrite` and `required_mode == DangerFullAccess`, prompt user for escalation.

7. **Final deny**: Return `Deny` with mode mismatch reason.

### Permission Rules (private)

```rust
struct PermissionRule {
    raw: String,
    tool_name: String,
    matcher: PermissionRuleMatcher,
}

enum PermissionRuleMatcher {
    Any,
    Exact(String),
    Prefix(String),
}
```

**Rule parsing** (`PermissionRule::parse()`):

- `"bash"` -- matches tool `bash` with any input (`PermissionRuleMatcher::Any`)
- `"bash(git:*)"` -- matches tool `bash` where the permission subject starts with `"git"` (`PermissionRuleMatcher::Prefix`)
- `"bash(git status)"` -- matches tool `bash` where the permission subject is exactly `"git status"` (`PermissionRuleMatcher::Exact`)
- Escaped parentheses `\(` `\)` are unescaped during parsing.

**Permission subject extraction** (`extract_permission_subject()`):

Parses the tool input as JSON and checks these keys in order: `command`, `path`, `file_path`, `filePath`, `notebook_path`, `notebookPath`, `url`, `pattern`, `code`, `message`. Returns the first string value found. If input is not valid JSON, returns the raw input string.

## PermissionEnforcer (permission_enforcer.rs)

```rust
#[derive(Debug, Clone, PartialEq)]
pub struct PermissionEnforcer {
    policy: PermissionPolicy,
}
```

### EnforcementResult

```rust
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "outcome")]
pub enum EnforcementResult {
    Allowed,
    Denied {
        tool: String,
        active_mode: String,
        required_mode: String,
        reason: String,
    },
}
```

### Methods

```rust
impl PermissionEnforcer {
    pub fn new(policy: PermissionPolicy) -> Self;
    pub fn check(&self, tool_name: &str, input: &str) -> EnforcementResult;
    pub fn is_allowed(&self, tool_name: &str, input: &str) -> bool;
    pub fn active_mode(&self) -> PermissionMode;
    pub fn check_file_write(&self, path: &str, workspace_root: &str) -> EnforcementResult;
    pub fn check_bash(&self, command: &str) -> EnforcementResult;
}
```

### check() Method

When `active_mode == Prompt`, returns `Allowed` (defers to caller's interactive prompt flow). Otherwise, calls `policy.authorize(tool_name, input, None)` and maps the outcome.

### check_file_write() Method

Classifies a file write operation against workspace boundaries:

| Mode | Inside workspace | Outside workspace |
|------|-----------------|-------------------|
| `ReadOnly` | **Denied** (requires workspace-write) | **Denied** |
| `WorkspaceWrite` | **Allowed** | **Denied** (requires danger-full-access) |
| `Allow` | **Allowed** | **Allowed** |
| `DangerFullAccess` | **Allowed** | **Allowed** |
| `Prompt` | **Denied** (requires confirmation) | **Denied** |

### check_bash() Method

Gates bash command execution based on current mode:

| Mode | Read-only command | Non-read-only command |
|------|------------------|----------------------|
| `ReadOnly` | **Allowed** | **Denied** |
| `Prompt` | **Denied** (requires confirmation) | **Denied** |
| `WorkspaceWrite` | **Allowed** | **Allowed** |
| `Allow` | **Allowed** | **Allowed** |
| `DangerFullAccess` | **Allowed** | **Allowed** |

### is_within_workspace() (private)

```rust
fn is_within_workspace(path: &str, workspace_root: &str) -> bool;
```

Simple string-prefix boundary check. Relative paths are resolved by prepending `workspace_root/`. Ensures the root has a trailing `/` for prefix matching. Returns `true` if the normalized path starts with the normalized root, or equals the root exactly.

### is_read_only_command() Heuristic (private)

```rust
fn is_read_only_command(command: &str) -> bool;
```

Extracts the first token from the command, strips any path prefix (e.g. `/usr/bin/cat` becomes `cat`), then checks against this whitelist:

**Full whitelist (55 commands):**

```
cat, head, tail, less, more, wc, ls, find, grep, rg, awk, sed, echo, printf,
which, where, whoami, pwd, env, printenv, date, cal, df, du, free, uptime,
uname, file, stat, diff, sort, uniq, tr, cut, paste, tee, xargs, test, true,
false, type, readlink, realpath, basename, dirname, sha256sum, md5sum, b3sum,
xxd, hexdump, od, strings, tree, jq, yq, python3, python, node, ruby, cargo,
rustc, git, gh
```

**Additionally blocked** even if the command starts with a whitelisted token:
- Contains `-i ` (interactive flag)
- Contains `--in-place` (in-place editing)
- Contains ` > ` (stdout redirect overwrite)
- Contains ` >> ` (stdout redirect append)
