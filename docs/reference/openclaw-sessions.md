# OpenClaw Sessions

Complete reference for the session model, session store, and session lifecycle.

**Source files read:** `src/config/sessions/types.ts`, `src/config/sessions/reset.ts`, `src/config/sessions/store.ts`, `src/config/sessions/main-session.ts`, `src/config/sessions/targets.ts`, `src/config/sessions/metadata.ts`, `src/sessions/session-chat-type.ts`, `src/sessions/send-policy.ts`, `src/sessions/session-lifecycle-events.ts`, `src/sessions/transcript-events.ts`, `src/sessions/model-overrides.ts`, `src/sessions/session-id-resolution.ts`

---

## SessionEntry (`config/sessions/types.ts`)

The central session record, stored in the session store JSON:

```typescript
type SessionEntry = {
  // Identity
  sessionId: string;
  updatedAt: number;
  sessionFile?: string;
  label?: string;
  displayName?: string;

  // Hierarchy
  spawnedBy?: string;              // Parent session key
  spawnedWorkspaceDir?: string;    // Inherited workspace
  parentSessionKey?: string;       // Dashboard-created parent linkage
  forkedFromParent?: boolean;      // True after thread fork
  spawnDepth?: number;             // 0=main, 1=sub, 2=sub-sub
  subagentRole?: "orchestrator" | "leaf";
  subagentControlScope?: "children" | "none";

  // Lifecycle
  systemSent?: boolean;
  abortedLastRun?: boolean;
  startedAt?: number;
  endedAt?: number;
  runtimeMs?: number;
  status?: "running" | "done" | "failed" | "killed" | "timeout";
  abortCutoffMessageSid?: string;
  abortCutoffTimestamp?: number;

  // Chat context
  chatType?: SessionChatType;       // "direct" | "group" | "channel"
  channel?: string;
  groupId?: string;
  subject?: string;
  groupChannel?: string;
  space?: string;
  origin?: SessionOrigin;
  deliveryContext?: DeliveryContext;
  lastChannel?: SessionChannelId;
  lastTo?: string;
  lastAccountId?: string;
  lastThreadId?: string | number;

  // User preferences (session-scoped)
  thinkingLevel?: string;
  fastMode?: boolean;
  verboseLevel?: string;
  reasoningLevel?: string;
  elevatedLevel?: string;
  ttsAuto?: TtsAutoMode;

  // Execution security
  execHost?: string;
  execSecurity?: string;
  execAsk?: string;
  execNode?: string;

  // Model state
  providerOverride?: string;
  modelOverride?: string;
  modelOverrideSource?: "auto" | "user";
  authProfileOverride?: string;
  authProfileOverrideSource?: "auto" | "user";
  authProfileOverrideCompactionCount?: number;
  liveModelSwitchPending?: boolean;
  modelProvider?: string;
  model?: string;
  contextTokens?: number;

  // Token accounting
  inputTokens?: number;
  outputTokens?: number;
  totalTokens?: number;
  totalTokensFresh?: boolean;
  estimatedCostUsd?: number;
  cacheRead?: number;
  cacheWrite?: number;

  // Compaction
  compactionCount?: number;
  compactionCheckpoints?: SessionCompactionCheckpoint[];
  memoryFlushAt?: number;
  memoryFlushCompactionCount?: number;
  memoryFlushContextHash?: string;

  // Heartbeat state
  lastHeartbeatText?: string;
  lastHeartbeatSentAt?: number;
  heartbeatTaskState?: Record<string, number>;

  // Queue behavior
  groupActivation?: "mention" | "always";
  groupActivationNeedsSystemIntro?: boolean;
  sendPolicy?: "allow" | "deny";
  queueMode?: "steer" | "followup" | "collect" | "steer-backlog" | "steer+backlog" | "queue" | "interrupt";
  queueDebounceMs?: number;
  queueCap?: number;
  queueDrop?: "old" | "new" | "summarize";
  responseUsage?: "on" | "off" | "tokens" | "full";

  // Fallback notice
  fallbackNoticeSelectedModel?: string;
  fallbackNoticeActiveModel?: string;
  fallbackNoticeReason?: string;

  // CLI session bindings
  cliSessionIds?: Record<string, string>;
  cliSessionBindings?: Record<string, CliSessionBinding>;
  claudeCliSessionId?: string;

  // Skills & prompt report
  skillsSnapshot?: SessionSkillSnapshot;
  systemPromptReport?: SessionSystemPromptReport;

  // ACP (Agent Communication Protocol)
  acp?: SessionAcpMeta;
};
```

### SessionOrigin

```typescript
type SessionOrigin = {
  label?: string;
  provider?: string;
  surface?: string;
  chatType?: SessionChatType;
  from?: string;
  to?: string;
  nativeChannelId?: string;
  nativeDirectUserId?: string;
  accountId?: string;
  threadId?: string | number;
};
```

### SessionChatType

```typescript
type SessionChatType = "direct" | "group" | "channel";
```

Derived from session key patterns:
- Keys containing `"group"` or matching WhatsApp group patterns -> `"group"`
- Keys containing `"channel"` or Discord guild patterns -> `"channel"`
- Keys containing `"direct"` or `"dm"` -> `"direct"`

### SessionScope

```typescript
type SessionScope = "per-sender" | "global";
```

### CliSessionBinding

```typescript
type CliSessionBinding = {
  sessionId: string;
  authProfileId?: string;
  authEpoch?: string;
  extraSystemPromptHash?: string;
  mcpConfigHash?: string;
};
```

### SessionCompactionCheckpoint

```typescript
type SessionCompactionCheckpoint = {
  checkpointId: string;
  sessionKey: string;
  sessionId: string;
  createdAt: number;
  reason: "manual" | "auto-threshold" | "overflow-retry" | "timeout-retry";
  tokensBefore?: number;
  tokensAfter?: number;
  summary?: string;
  firstKeptEntryId?: string;
  preCompaction: SessionCompactionTranscriptReference;
  postCompaction: SessionCompactionTranscriptReference;
};
```

### ACP Session Meta

```typescript
type SessionAcpMeta = {
  backend: string;
  agent: string;
  runtimeSessionName: string;
  identity?: SessionAcpIdentity;
  mode: "persistent" | "oneshot";
  runtimeOptions?: AcpSessionRuntimeOptions;
  cwd?: string;
  state: "idle" | "running" | "error";
  lastActivityAt: number;
  lastError?: string;
};
```

## Session Entry Operations

### `mergeSessionEntry(existing, patch) -> SessionEntry`

Merges patch over existing entry. Generates `sessionId` via `crypto.randomUUID()` if missing. Updates `updatedAt` to max of existing/patch/now. Guards against stale provider carry-over when model is patched without provider.

### `mergeSessionEntryPreserveActivity(existing, patch) -> SessionEntry`

Same as merge but preserves existing `updatedAt` (used for background state updates that should not change activity timestamp).

### `normalizeSessionRuntimeModelFields(entry) -> SessionEntry`

Cleans up empty/whitespace model and provider fields.

## Reset Policies (`config/sessions/reset.ts`)

Default reset triggers: `"/new"`, `"/reset"`

```typescript
const DEFAULT_RESET_TRIGGER = "/new";
const DEFAULT_RESET_TRIGGERS = ["/new", "/reset"];
const DEFAULT_IDLE_MINUTES = 0;  // No idle reset by default
```

## Send Policy (`sessions/send-policy.ts`)

### `resolveSendPolicy(params) -> "allow" | "deny"`

Resolution order:
1. Session-level override (`entry.sendPolicy`)
2. Config rules (`session.sendPolicy.rules[]`) — matched against channel, chatType, keyPrefix, rawKeyPrefix
3. Config default (`session.sendPolicy.default`)
4. Global default: `"allow"`

Rule matching supports:
- `match.channel` — Channel ID matching
- `match.chatType` — Chat type matching
- `match.keyPrefix` — Prefix match on stripped session key
- `match.rawKeyPrefix` — Prefix match on raw session key (including agent prefix)

First matching `deny` rule wins immediately. First matching `allow` sets `allowedMatch` flag.

## Session Lifecycle Events (`sessions/session-lifecycle-events.ts`)

```typescript
type SessionLifecycleEvent = {
  sessionKey: string;
  reason: string;
  parentSessionKey?: string;
  label?: string;
  displayName?: string;
};
```

Global listener set. `emitSessionLifecycleEvent()` fires to all registered listeners (best-effort, errors swallowed). Register via `onSessionLifecycleEvent(listener)` which returns an unsubscribe function.

## Model Overrides (`sessions/model-overrides.ts`)

Session-level model overrides track both the override value and its source:
- `modelOverrideSource: "user"` — Set by explicit user action (`/model`, `sessions.patch`)
- `modelOverrideSource: "auto"` — Set by runtime fallback

Session resets only preserve user-driven overrides. The `liveModelSwitchPending` flag signals the embedded runner to restart with the new model.

## Session ID Resolution (`sessions/session-id-resolution.ts`)

Resolves session IDs from various formats:
- Direct session ID strings
- Session key to session ID mapping
- Channel-specific session key derivation

## Session Key Utilities

### Key Format

Session keys encode channel, agent, and conversation identity:

```
agent:<agentId>:<channel>:<chatType>:<identifier>
```

Examples:
- `agent:default:telegram:direct:123456789`
- `agent:default:discord:guild-123:channel-456`
- `agent:default:whatsapp:group:123456789@g.us`

### Chat Type Derivation

`deriveSessionChatType(sessionKey)` inspects key tokens to determine chat type, delegating to channel plugins for legacy key patterns.

## Transcript Events (`sessions/transcript-events.ts`)

Events emitted during conversation transcript processing, used for hooks and logging.

## System Prompt Report

```typescript
type SessionSystemPromptReport = {
  source: "run" | "estimate";
  generatedAt: number;
  sessionId?: string;
  sessionKey?: string;
  provider?: string;
  model?: string;
  workspaceDir?: string;
  bootstrapMaxChars?: number;
  bootstrapTotalMaxChars?: number;
  bootstrapTruncation?: {
    warningMode?: "off" | "once" | "always";
    warningShown?: boolean;
    promptWarningSignature?: string;
    warningSignaturesSeen?: string[];
    truncatedFiles?: number;
    nearLimitFiles?: number;
    totalNearLimit?: boolean;
  };
  sandbox?: { mode?: string; sandboxed?: boolean };
  systemPrompt: {
    chars: number;
    projectContextChars: number;
    nonProjectContextChars: number;
  };
  injectedWorkspaceFiles: Array<{
    name: string;
    path: string;
    missing: boolean;
    rawChars: number;
    injectedChars: number;
    truncated: boolean;
  }>;
  skills: {
    promptChars: number;
    entries: Array<{ name: string; blockChars: number }>;
  };
  tools: {
    listChars: number;
    schemaChars: number;
    entries: Array<{
      name: string;
      summaryChars: number;
      schemaChars: number;
      propertiesCount?: number | null;
    }>;
  };
};
```

## Queue Modes

Sessions support configurable message queue behavior:

- `"steer"` — New messages steer the current conversation
- `"followup"` — Queue as follow-up after current turn completes
- `"collect"` — Collect messages and batch-process
- `"steer-backlog"` / `"steer+backlog"` — Steer with backlog processing
- `"queue"` — Strict FIFO queue
- `"interrupt"` — Interrupt current turn
