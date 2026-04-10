# OpenClaw Agent Defaults Config

Complete reference for the `AgentDefaultsConfig` schema and all related config types.

**Source files read:** `src/config/types.agent-defaults.ts`, `src/config/types.agents-shared.ts`, `src/config/types.base.ts`, `src/config/types.tools.ts`

---

## AgentDefaultsConfig

The top-level agent defaults configuration, applied at `agents.defaults` in the config file. Per-agent overrides are set at `agents.list[].{field}`.

```typescript
type AgentDefaultsConfig = {
  // --- Model Configuration ---
  params?: Record<string, unknown>;              // Global provider params for all models
  model?: AgentModelConfig;                       // Primary model + fallbacks
  imageModel?: AgentModelConfig;                  // Image-capable model
  imageGenerationModel?: AgentModelConfig;        // Image generation model
  videoGenerationModel?: AgentModelConfig;        // Video generation model
  musicGenerationModel?: AgentModelConfig;        // Music generation model
  mediaGenerationAutoProviderFallback?: boolean;  // Auto-append auth-backed providers (default: true)
  pdfModel?: AgentModelConfig;                    // PDF-capable model
  pdfMaxBytesMb?: number;                         // Max PDF size in MB (default: 10)
  pdfMaxPages?: number;                           // Max PDF pages (default: 20)
  models?: Record<string, AgentModelEntryConfig>; // Model catalog with aliases

  // --- Workspace ---
  workspace?: string;                              // Agent working directory
  repoRoot?: string;                               // Override auto-detected repo root

  // --- Skills ---
  skills?: string[];                               // Default skill allowlist

  // --- System Prompt ---
  systemPromptOverride?: string;                   // Full system prompt replacement
  skipBootstrap?: boolean;                         // Skip BOOTSTRAP.md creation
  contextInjection?: AgentContextInjection;        // "always" | "continuation-skip"
  bootstrapMaxChars?: number;                      // Per-file limit (default: 20000)
  bootstrapTotalMaxChars?: number;                 // Total limit (default: 150000)
  bootstrapPromptTruncationWarning?: "off" | "once" | "always";

  // --- Time & Locale ---
  userTimezone?: string;                           // IANA timezone for system prompt
  timeFormat?: "auto" | "12" | "24";              // Time format preference
  envelopeTimezone?: string;                       // "utc" | "local" | "user" | IANA
  envelopeTimestamp?: "on" | "off";               // Absolute timestamps in envelopes
  envelopeElapsed?: "on" | "off";                 // Elapsed time in envelopes

  // --- Context ---
  contextTokens?: number;                          // Context window cap
  contextPruning?: AgentContextPruningConfig;      // Tool result pruning

  // --- LLM ---
  llm?: AgentLlmConfig;                           // LLM timeout config

  // --- Compaction ---
  compaction?: AgentCompactionConfig;              // Compaction tuning

  // --- Embedded Pi ---
  embeddedPi?: {
    projectSettingsPolicy?: "trusted" | "sanitize" | "ignore";
  };

  // --- Memory ---
  memorySearch?: MemorySearchConfig;

  // --- Behavioral Defaults ---
  thinkingDefault?: "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "adaptive";
  verboseDefault?: "off" | "on" | "full";
  elevatedDefault?: "off" | "on" | "ask" | "full";

  // --- Block Streaming ---
  blockStreamingDefault?: "off" | "on";
  blockStreamingBreak?: "text_end" | "message_end";
  blockStreamingChunk?: BlockStreamingChunkConfig;
  blockStreamingCoalesce?: BlockStreamingCoalesceConfig;

  // --- Human Delay ---
  humanDelay?: HumanDelayConfig;

  // --- Timeouts ---
  timeoutSeconds?: number;

  // --- Media ---
  mediaMaxMb?: number;                             // Max inbound media size in MB
  imageMaxDimensionPx?: number;                    // Max image side (default: 1200)

  // --- Typing ---
  typingIntervalSeconds?: number;
  typingMode?: TypingMode;                         // "never" | "instant" | "thinking" | "message"

  // --- Heartbeat ---
  heartbeat?: AgentHeartbeatConfig;

  // --- Concurrency ---
  maxConcurrent?: number;                          // Max concurrent agent runs (default: 1)

  // --- Subagents ---
  subagents?: AgentSubagentsConfig;

  // --- Sandbox ---
  sandbox?: AgentSandboxConfig;

  // --- CLI Backends ---
  cliBackends?: Record<string, CliBackendConfig>;
};
```

## AgentModelConfig

```typescript
type AgentModelConfig = string | { primary?: string; fallbacks?: string[] };
```

## AgentModelEntryConfig

```typescript
type AgentModelEntryConfig = {
  alias?: string;
  params?: Record<string, unknown>;    // Provider-specific API params
  streaming?: boolean;                  // Default: true, false for Ollama
};
```

## AgentContextPruningConfig

```typescript
type AgentContextPruningConfig = {
  mode?: "off" | "cache-ttl";
  ttl?: string;                         // Duration string, default unit: minutes
  keepLastAssistants?: number;
  softTrimRatio?: number;
  hardClearRatio?: number;
  minPrunableToolChars?: number;
  tools?: {
    allow?: string[];
    deny?: string[];
  };
  softTrim?: {
    maxChars?: number;
    headChars?: number;
    tailChars?: number;
  };
  hardClear?: {
    enabled?: boolean;
    placeholder?: string;
  };
};
```

## AgentLlmConfig

```typescript
type AgentLlmConfig = {
  idleTimeoutSeconds?: number;    // Streaming idle timeout (default: 60, 0 = disable)
};
```

## AgentCompactionConfig

```typescript
type AgentCompactionConfig = {
  mode?: AgentCompactionMode;                    // "default" | "safeguard"
  reserveTokens?: number;                         // Pi reserve tokens target
  keepRecentTokens?: number;                      // Budget for cut-point selection
  reserveTokensFloor?: number;                    // Minimum reserve (0 = no floor)
  maxHistoryShare?: number;                       // 0.1-0.9, default 0.5
  customInstructions?: string;                    // Extra compaction instructions
  recentTurnsPreserve?: number;                   // Verbatim recent turns
  identifierPolicy?: AgentCompactionIdentifierPolicy;  // "strict" | "off" | "custom"
  identifierInstructions?: string;                // Custom identifier instructions
  qualityGuard?: AgentCompactionQualityGuardConfig;
  postIndexSync?: AgentCompactionPostIndexSyncMode;    // "off" | "async" | "await"
  memoryFlush?: AgentCompactionMemoryFlushConfig;
  postCompactionSections?: string[];              // Default: ["Session Startup", "Red Lines"]
  model?: string;                                  // Override compaction model
  timeoutSeconds?: number;                         // Default: 900
  provider?: string;                               // Plugin compaction provider
  truncateAfterCompaction?: boolean;               // Default: false
  notifyUser?: boolean;                            // Default: false
};
```

### AgentCompactionMemoryFlushConfig

```typescript
type AgentCompactionMemoryFlushConfig = {
  enabled?: boolean;                     // Default: true
  softThresholdTokens?: number;          // Flush when within N tokens of threshold
  forceFlushTranscriptBytes?: number | string;  // Force flush at size (0 = disable)
  prompt?: string;                       // User prompt for flush turn
  systemPrompt?: string;                 // System prompt for flush turn
};
```

### AgentCompactionQualityGuardConfig

```typescript
type AgentCompactionQualityGuardConfig = {
  enabled?: boolean;     // Default: false
  maxRetries?: number;   // Default: 1 when enabled
};
```

## Heartbeat Config

```typescript
heartbeat?: {
  every?: string;                        // Interval (duration, default: 30m)
  activeHours?: {
    start?: string;                      // HH:MM, inclusive
    end?: string;                        // HH:MM, exclusive ("24:00" for EOD)
    timezone?: string;                   // "user" | "local" | IANA
  };
  model?: string;                        // Heartbeat model override
  session?: string;                      // "main" or explicit session key
  target?: ChannelId;                    // Delivery target ("last", "none", channel)
  directPolicy?: "allow" | "block";     // DM delivery policy (default: "allow")
  to?: string;                           // Delivery override (E.164, chat ID, etc.)
  accountId?: string;                    // Multi-account channel
  prompt?: string;                       // Override heartbeat prompt body
  includeSystemPromptSection?: boolean;  // Default: true
  ackMaxChars?: number;                  // Max chars after HEARTBEAT_OK (default: 30)
  suppressToolErrorWarnings?: boolean;
  lightContext?: boolean;                // Only HEARTBEAT.md from bootstrap
  isolatedSession?: boolean;             // No prior conversation history
  includeReasoning?: boolean;            // Deliver reasoning payload (default: false)
};
```

## Subagents Config

```typescript
subagents?: {
  allowAgents?: string[];          // Allowlist of target agent IDs ("*" = any)
  maxConcurrent?: number;          // Global sub-agent concurrency (default: 1)
  maxSpawnDepth?: number;          // Max nesting depth (default: 1)
  maxChildrenPerAgent?: number;    // Per-requester limit (default: 5)
  archiveAfterMinutes?: number;    // Auto-archive timeout (default: 60, 0 = disable)
  model?: AgentModelConfig;        // Default sub-agent model
  thinking?: string;               // Default thinking level
  runTimeoutSeconds?: number;      // Default run timeout (0 = none)
  announceTimeoutMs?: number;      // Gateway timeout for announce (default: 90000)
  requireAgentId?: boolean;        // Require explicit agentId (default: false)
};
```

## Sandbox Config

```typescript
type AgentSandboxConfig = {
  mode?: "off" | "require" | "prefer";
  image?: string;
  mounts?: Array<{ host: string; container: string; readonly?: boolean }>;
  // Additional Docker/Podman configuration
};
```

## CLI Backend Config

```typescript
type CliBackendConfig = {
  command: string;                    // CLI command (absolute path or on PATH)
  args?: string[];                    // Base args for every invocation
  output?: "json" | "text" | "jsonl";
  resumeOutput?: "json" | "text" | "jsonl";
  input?: "arg" | "stdin";
  maxPromptArgChars?: number;
  env?: Record<string, string>;
  clearEnv?: string[];
  modelArg?: string;                  // Flag to pass model (e.g. --model)
  modelAliases?: Record<string, string>;
  sessionArg?: string;
  sessionArgs?: string[];
  resumeArgs?: string[];
  sessionMode?: "always" | "existing" | "none";
  sessionIdFields?: string[];
  systemPromptArg?: string;
  systemPromptFileConfigArg?: string;
  systemPromptFileConfigKey?: string;
  systemPromptMode?: "append" | "replace";
  systemPromptWhen?: "first" | "always" | "never";
  imageArg?: string;
  imageMode?: "repeat" | "list";
  imagePathScope?: "temp" | "workspace";
  serialize?: boolean;
  reliability?: {
    watchdog?: {
      fresh?: {
        noOutputTimeoutMs?: number;
        noOutputTimeoutRatio?: number;
        minMs?: number;
        maxMs?: number;
      };
      resume?: {
        noOutputTimeoutMs?: number;
        noOutputTimeoutRatio?: number;
        minMs?: number;
        maxMs?: number;
      };
    };
  };
};
```

## AgentContextInjection

```typescript
type AgentContextInjection = "always" | "continuation-skip";
```

- `"always"` — Inject workspace bootstrap files on every turn (default)
- `"continuation-skip"` — Skip injection on safe continuation turns once a completed assistant turn exists in transcript
