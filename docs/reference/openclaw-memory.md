# OpenClaw Memory & Dreaming

Complete reference for the memory dreaming system (3-phase consolidation).

**Source files read:** `src/memory-host-sdk/dreaming.ts`, `src/memory-host-sdk/host/`

---

## Overview

The memory dreaming system runs periodic background memory consolidation in three phases modeled after human sleep stages: light, deep, and REM. Each phase has its own cron schedule, data sources, and processing parameters.

Memory is a special plugin slot where only one memory plugin can be active at a time. The default plugin ID is `"memory-core"`.

## Configuration Types

### MemoryDreamingConfig (top-level)

```typescript
type MemoryDreamingConfig = {
  enabled: boolean;
  frequency: string;            // Legacy cron expression (default: "0 3 * * *")
  timezone?: string;
  verboseLogging: boolean;
  storage: MemoryDreamingStorageConfig;
  execution: {
    defaults: MemoryDreamingExecutionConfig;
  };
  phases: {
    light: MemoryLightDreamingConfig;
    deep: MemoryDeepDreamingConfig;
    rem: MemoryRemDreamingConfig;
  };
};
```

### Phase Names

```typescript
type MemoryDreamingPhaseName = "light" | "deep" | "rem";
```

## Light Dreaming Phase

Recent memory consolidation. Runs frequently, processes recent conversations.

```typescript
type MemoryLightDreamingConfig = {
  enabled: boolean;
  cron: string;                    // Default: "0 */6 * * *" (every 6 hours)
  lookbackDays: number;            // Default: 2
  limit: number;                   // Default: 100 entries to process
  dedupeSimilarity: number;        // Default: 0.9 (90% similarity threshold)
  sources: MemoryLightDreamingSource[];
  execution: MemoryDreamingExecutionConfig;
};

type MemoryLightDreamingSource = "daily" | "sessions" | "recall";
```

**Default sources:** `["daily", "sessions", "recall"]`

### Light Dreaming Defaults

| Parameter | Default |
|-----------|---------|
| `cron` | `"0 */6 * * *"` (every 6 hours) |
| `lookbackDays` | `2` |
| `limit` | `100` |
| `dedupeSimilarity` | `0.9` |

## Deep Dreaming Phase

Knowledge strengthening. Identifies high-value memories for reinforcement.

```typescript
type MemoryDeepDreamingConfig = {
  enabled: boolean;
  cron: string;                    // Default: "0 3 * * *" (daily at 3 AM)
  limit: number;                   // Default: 10
  minScore: number;                // Default: 0.8
  minRecallCount: number;          // Default: 3
  minUniqueQueries: number;        // Default: 3
  recencyHalfLifeDays: number;     // Default: 14
  maxAgeDays?: number;             // Default: 30
  sources: MemoryDeepDreamingSource[];
  recovery: MemoryDeepDreamingRecoveryConfig;
  execution: MemoryDreamingExecutionConfig;
};

type MemoryDeepDreamingSource = "daily" | "memory" | "sessions" | "logs" | "recall";
```

**Default sources:** `["daily", "memory", "sessions", "logs", "recall"]`

### Deep Dreaming Defaults

| Parameter | Default |
|-----------|---------|
| `cron` | `"0 3 * * *"` (daily at 3 AM) |
| `limit` | `10` |
| `minScore` | `0.8` |
| `minRecallCount` | `3` |
| `minUniqueQueries` | `3` |
| `recencyHalfLifeDays` | `14` |
| `maxAgeDays` | `30` |

### Health Recovery

Automatic memory health recovery when quality degrades:

```typescript
type MemoryDeepDreamingRecoveryConfig = {
  enabled: boolean;                          // Default: true
  triggerBelowHealth: number;                // Default: 0.35 (35%)
  lookbackDays: number;                      // Default: 30
  maxRecoveredCandidates: number;            // Default: 20
  maxCandidates: number;                     // Alias for maxRecoveredCandidates
  minRecoveryConfidence: number;             // Default: 0.9 (90%)
  autoWriteMinConfidence: number;            // Default: 0.97 (97%)
};
```

Recovery triggers when memory health score drops below `triggerBelowHealth` (default 0.35). Looks back `lookbackDays` (default 30) for candidates. Auto-writes only when confidence exceeds 0.97.

## REM Dreaming Phase

Pattern discovery and cross-reference. Runs weekly.

```typescript
type MemoryRemDreamingConfig = {
  enabled: boolean;
  cron: string;                    // Default: "0 5 * * 0" (Sunday 5 AM)
  lookbackDays: number;            // Default: 7
  limit: number;                   // Default: 10
  minPatternStrength: number;      // Default: 0.75
  sources: MemoryRemDreamingSource[];
  execution: MemoryDreamingExecutionConfig;
};

type MemoryRemDreamingSource = "memory" | "daily" | "deep";
```

**Default sources:** `["memory", "daily", "deep"]`

### REM Dreaming Defaults

| Parameter | Default |
|-----------|---------|
| `cron` | `"0 5 * * 0"` (Sunday 5 AM) |
| `lookbackDays` | `7` |
| `limit` | `10` |
| `minPatternStrength` | `0.75` |

## Execution Config

Shared execution parameters for all phases, with per-phase overrides:

```typescript
type MemoryDreamingExecutionConfig = {
  speed: MemoryDreamingSpeed;              // "fast" | "balanced" | "slow"
  thinking: MemoryDreamingThinking;        // "low" | "medium" | "high"
  budget: MemoryDreamingBudget;            // "cheap" | "medium" | "expensive"
  model?: string;                           // Optional model override
  maxOutputTokens?: number;
  temperature?: number;
  timeoutMs?: number;
};
```

### Default Execution Values

| Parameter | Default |
|-----------|---------|
| `speed` | `"balanced"` |
| `thinking` | `"medium"` |
| `budget` | `"medium"` |

## Storage Config

```typescript
type MemoryDreamingStorageConfig = {
  mode: MemoryDreamingStorageMode;
  separateReports: boolean;
};

type MemoryDreamingStorageMode = "inline" | "separate" | "both";
```

| Parameter | Default |
|-----------|---------|
| `mode` | `"inline"` |
| `separateReports` | `false` |

- `"inline"` — Dream results stored in the main memory store
- `"separate"` — Dream results stored in separate report files
- `"both"` — Both inline and separate storage

## Workspace Resolution

```typescript
type MemoryDreamingWorkspace = {
  workspaceDir: string;
  agentIds: string[];
};
```

The dreaming system resolves workspace directories from agent configuration. Each agent's workspace is resolved via `resolveAgentWorkspaceDir()`.

## Global Defaults

```typescript
const DEFAULT_MEMORY_DREAMING_ENABLED = false;
const DEFAULT_MEMORY_DREAMING_TIMEZONE = undefined;
const DEFAULT_MEMORY_DREAMING_VERBOSE_LOGGING = false;
const DEFAULT_MEMORY_DREAMING_STORAGE_MODE = "inline";
const DEFAULT_MEMORY_DREAMING_SEPARATE_REPORTS = false;
const DEFAULT_MEMORY_DREAMING_FREQUENCY = "0 3 * * *";
const DEFAULT_MEMORY_DREAMING_PLUGIN_ID = "memory-core";
```

## Deduplication

Light dreaming uses embedding similarity for deduplication. When a new memory candidate has cosine similarity >= `dedupeSimilarity` (default 0.9) with an existing memory, it is considered a duplicate and skipped.

## Schedule Summary

| Phase | Default Cron | Frequency | Purpose |
|-------|-------------|-----------|---------|
| Light | `0 */6 * * *` | Every 6 hours | Recent memory consolidation |
| Deep | `0 3 * * *` | Daily at 3 AM | Knowledge strengthening |
| REM | `0 5 * * 0` | Weekly (Sunday 5 AM) | Pattern discovery |

## Config Resolution

The dreaming config is resolved from the OpenClaw config at `memory.dreaming.*` with extensive normalization:

- Boolean fields: `normalizeBoolean(value, fallback)`
- Numeric fields: `normalizeNonNegativeInt(value, fallback)`, `normalizeOptionalPositiveInt(value)`
- String fields: `normalizeTrimmedString(value)`
- Source arrays: validated against allowed values per phase

Each phase's execution config inherits from `execution.defaults` with per-phase overrides.
