# OpenClaw Agents

Complete reference for agent scoping, subagent spawning, and the embedded Pi runner.

**Source files read:** `src/agents/agent-scope.ts`, `src/agents/subagent-spawn.ts`, `src/agents/pi-embedded-runner/run.ts`, `src/agents/pi-embedded-runner/runs.ts`, `src/agents/pi-embedded-runner/types.ts`, `src/agents/pi-embedded-runner/lanes.ts`

---

## Agent Scoping (`agent-scope.ts`)

### ResolvedAgentConfig

```typescript
type ResolvedAgentConfig = {
  name?: string;
  workspace?: string;
  agentDir?: string;
  systemPromptOverride?: AgentEntry["systemPromptOverride"];
  model?: AgentEntry["model"];               // string | { primary, fallbacks }
  thinkingDefault?: AgentEntry["thinkingDefault"];
  verboseDefault?: AgentDefaultsConfig["verboseDefault"];
  reasoningDefault?: AgentEntry["reasoningDefault"];
  fastModeDefault?: AgentEntry["fastModeDefault"];
  skills?: AgentEntry["skills"];             // string[]
  memorySearch?: AgentEntry["memorySearch"];
  humanDelay?: AgentEntry["humanDelay"];
  heartbeat?: AgentEntry["heartbeat"];
  identity?: AgentEntry["identity"];
  groupChat?: AgentEntry["groupChat"];
  subagents?: AgentEntry["subagents"];
  sandbox?: AgentEntry["sandbox"];
  tools?: AgentEntry["tools"];
};
```

### Agent ID Constants

- `DEFAULT_AGENT_ID` — imported from `routing/session-key.js`
- Agent IDs are normalized to lowercase via `normalizeAgentId()`

### Key Functions

#### `listAgentEntries(cfg) -> AgentEntry[]`

Returns `cfg.agents.list` filtered to valid objects. Returns empty array if no agents configured.

#### `listAgentIds(cfg) -> string[]`

Returns deduplicated, normalized agent IDs. Falls back to `[DEFAULT_AGENT_ID]` when empty.

#### `resolveDefaultAgentId(cfg) -> string`

Selects the first agent with `default: true`. Warns (once) if multiple defaults. Falls back to first entry, then `DEFAULT_AGENT_ID`.

#### `resolveSessionAgentIds(params) -> { defaultAgentId, sessionAgentId }`

Resolves agent IDs for a session by checking: explicit `agentId` > session key parsing > default agent.

#### `resolveAgentConfig(cfg, agentId) -> ResolvedAgentConfig | undefined`

Looks up agent entry by normalized ID. Merges `verboseDefault` from `agents.defaults` when not set on the agent.

#### `resolveAgentWorkspaceDir(cfg, agentId) -> string`

Resolution order:
1. Agent-specific `workspace` config (resolved via `resolveUserPath`)
2. For default agent: `agents.defaults.workspace` or system default workspace dir
3. For non-default agents: `agents.defaults.workspace/<agentId>` or `<stateDir>/workspace-<agentId>`

Null bytes are stripped from all paths to prevent ENOTDIR errors.

#### `resolveAgentIdsByWorkspacePath(cfg, workspacePath) -> string[]`

Finds agents whose workspace contains the given path. Sorted by specificity (longest workspace path first), then config order.

#### `resolveAgentDir(cfg, agentId) -> string`

Agent directory for persistent state: configured `agentDir` or `<stateDir>/agents/<agentId>/agent`.

### Model Resolution

- `resolveAgentExplicitModelPrimary(cfg, agentId)` — Agent-specific model only
- `resolveAgentEffectiveModelPrimary(cfg, agentId)` — Agent-specific or `agents.defaults.model`
- `resolveAgentModelFallbacksOverride(cfg, agentId)` — Agent-level fallback override (empty array = disable global fallbacks)
- `resolveEffectiveModelFallbacks(cfg, agentId, hasSessionModelOverride)` — Effective fallback chain

### Session Key Format

Session keys follow the pattern: `agent:<agentId>:<channel>:<chatType>:<identifier>` parsed via `parseAgentSessionKey()`.

## Subagent Spawning (`subagent-spawn.ts`)

### SpawnSubagentParams

```typescript
type SpawnSubagentParams = {
  task: string;                      // Required task description
  label?: string;                    // Display label
  agentId?: string;                  // Target agent
  model?: string;                    // Model override
  thinking?: string;                 // Thinking level override
  runTimeoutSeconds?: number;        // Per-run timeout
  thread?: boolean;                  // Thread-bound session
  mode?: SpawnSubagentMode;          // "run" | "session"
  cleanup?: "delete" | "keep";       // Post-run cleanup
  sandbox?: SpawnSubagentSandboxMode; // "inherit" | "require"
  lightContext?: boolean;            // Lightweight bootstrap
  expectsCompletionMessage?: boolean;
  attachments?: Array<{
    name: string;
    content: string;
    encoding?: "utf8" | "base64";
    mimeType?: string;
  }>;
  attachMountPath?: string;
};
```

### SpawnSubagentMode

```typescript
const SUBAGENT_SPAWN_MODES = ["run", "session"] as const;
type SpawnSubagentMode = "run" | "session";
```

- `"run"` — One-shot execution, session may be cleaned up after
- `"session"` — Persistent session that stays active for follow-ups

### SpawnSubagentSandboxMode

```typescript
const SUBAGENT_SPAWN_SANDBOX_MODES = ["inherit", "require"] as const;
type SpawnSubagentSandboxMode = "inherit" | "require";
```

### SpawnSubagentContext

```typescript
type SpawnSubagentContext = {
  agentSessionKey?: string;
  agentChannel?: string;
  agentAccountId?: string;
  agentTo?: string;
  agentThreadId?: string | number;
  agentGroupId?: string | null;
  agentGroupChannel?: string | null;
  agentGroupSpace?: string | null;
  requesterAgentIdOverride?: string;
  workspaceDir?: string;
};
```

### SpawnSubagentResult

```typescript
type SpawnSubagentResult = {
  status: "accepted" | "forbidden" | "error";
  childSessionKey?: string;
  runId?: string;
  mode?: SpawnSubagentMode;
  note?: string;
  modelApplied?: boolean;
  error?: string;
  attachments?: {
    count: number;
    totalBytes: number;
    files: Array<{ name: string; bytes: number; sha256: string }>;
    relDir: string;
  };
};
```

### Spawn Limits

- `DEFAULT_SUBAGENT_MAX_SPAWN_DEPTH` — Maximum nesting depth
- Max concurrent sub-agents configurable via `agents.defaults.subagents.maxConcurrent`
- Max children per requester: `agents.defaults.subagents.maxChildrenPerAgent` (default 5)
- Active runs tracked via `countActiveRunsForSession()` and `registerSubagentRun()`

### Spawn Flow

1. Load config via `loadSubagentConfig()`
2. Resolve agent config, model, and thinking plan
3. Check spawn depth via `getSubagentDepthFromSessionStore()`
4. Check active children limit
5. Resolve child session key (internal key format)
6. Build subagent system prompt via `buildSubagentSystemPrompt()`
7. Persist initial child session model override
8. Materialize attachments to disk (if any)
9. Call gateway `agent` method with the task
10. Emit session lifecycle event

### Accepted Notes

When spawn succeeds, two standardized notes:

- **Run mode:** `"Auto-announce is push-based. After spawning children, do NOT call sessions_list, sessions_history, exec sleep, or any polling tool. Wait for completion events to arrive as user messages..."`
- **Session mode:** `"thread-bound session stays active after this task; continue in-thread for follow-ups."`

### Gateway Communication

Subagent spawn uses `callSubagentGateway()` which pins admin-only methods (e.g., `sessions.patch`, `sessions.delete`) to `ADMIN_SCOPE` while keeping other methods at least-privilege scope.

## Embedded Pi Runner (`pi-embedded-runner/run.ts`)

### RunEmbeddedPiAgentParams

The runner accepts a params object including:

- `config` — OpenClaw config
- `sessionId` — Target session
- `sessionKey` — Session key (backfilled from sessionId if missing)
- `agentId` — Agent identifier
- `message` — User message
- `model` / `provider` — Model selection
- `thinking` — Thinking level
- Various runtime options

### Run Loop Architecture

The embedded runner (`runEmbeddedPiAgent`) implements a retry/failover loop:

1. **Session key backfill** — Resolves sessionKey from sessionId when not provided
2. **Context engine initialization** — Ensures context engines are ready
3. **Runtime plugins** — Ensures runtime plugins are loaded
4. **Auth profile resolution** — Resolves API key / OAuth credentials via auth profile store
5. **Model resolution** — Async model resolution including hook-based model selection
6. **Payload construction** — Builds embedded run payloads
7. **Attempt execution** — Runs via `runEmbeddedAttempt()`
8. **Error classification** — On failure, classifies error type:
   - Auth errors (billing, credentials)
   - Rate limit errors
   - Context overflow (with observed token count extraction)
   - Image dimension/size errors
   - Compaction failures
   - Failover-eligible errors
9. **Failover decision** — `resolveRunFailoverDecision()` determines retry strategy:
   - Model fallback
   - Auth profile rotation (up to configurable limits for rate-limit and overload)
   - Thinking level downgrade
   - Compaction retry
10. **Usage accumulation** — Token usage tracked across retries
11. **Retry limit exhaustion** — `handleRetryLimitExhaustion()` on max iterations

### Key Constants/Helpers

- `resolveMaxRunRetryIterations()` — Max retry loop iterations
- `resolveOverloadFailoverBackoffMs()` — Backoff delay for overload retries
- `resolveOverloadProfileRotationLimit()` — Max auth profile rotations for overload
- `resolveRateLimitProfileRotationLimit()` — Max auth profile rotations for rate limits
- `scrubAnthropicRefusalMagic()` — Removes Anthropic refusal strings from output

### Auth Controller

`createEmbeddedRunAuthController()` manages:
- Auth profile ordering and rotation
- Cooldown tracking for failed profiles
- Profile usage marking (good/used/failure)

### Session Lane

Jobs execute on a per-session lane (`resolveSessionLane()`) or global lane (`resolveGlobalLane()`), with subagent work on `AGENT_LANE_SUBAGENT`.

### Result Types

```typescript
type EmbeddedPiRunResult = {
  status: "ok" | "error" | "skipped";
  error?: string;
  summary?: string;
  sessionId?: string;
  sessionKey?: string;
  model?: string;
  provider?: string;
  usage?: CronUsageSummary;
  delivered?: boolean;
  deliveryAttempted?: boolean;
};

type EmbeddedPiAgentMeta = {
  // Model, provider, usage, timing, failover info
};
```

### Compaction

Post-compaction side effects handled by `runPostCompactionSideEffects()`. Context engine maintenance via `runContextEngineMaintenance()`. Oversized tool results detected and truncated via `truncateOversizedToolResultsInSession()`.

### Live Model Switch

During an active run, if the user changes the session model (via `/model` or `sessions.patch`), the runner detects `liveModelSwitchPending` and throws `LiveSessionModelSwitchError`, causing the turn to restart with the new model.
