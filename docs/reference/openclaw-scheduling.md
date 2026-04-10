# OpenClaw Scheduling System

Complete reference for the cron/scheduling subsystem.

**Source files read:** `src/cron/types.ts`, `src/cron/schedule.ts`, `src/cron/normalize.ts`, `src/cron/service/timer.ts`, `src/cron/service/ops.ts`, `src/cron/service/jobs.ts`, `src/cron/service/state.ts`, `src/cron/service/store.ts`, `src/cron/run-log.ts`, `src/cron/delivery-plan.ts`, `src/cron/active-jobs.ts`, `src/cron/service/timeout-policy.ts`

---

## Core Types (`types.ts`)

### CronSchedule (discriminated union)

Three schedule kinds:

```typescript
type CronSchedule =
  | { kind: "at"; at: string }                                  // One-shot absolute time (ISO string)
  | { kind: "every"; everyMs: number; anchorMs?: number }       // Fixed interval with optional anchor
  | { kind: "cron"; expr: string; tz?: string; staggerMs?: number }  // Cron expression with timezone and stagger
```

### CronSessionTarget

```typescript
type CronSessionTarget = "main" | "isolated" | "current" | `session:${string}`;
```

- `"main"` — delivers to the main session; requires `payload.kind="systemEvent"`
- `"isolated"` — creates an isolated agent session; requires `payload.kind="agentTurn"`
- `"current"` — resolved at creation time to `session:<sessionKey>` from context, falls back to `"isolated"`
- `session:<id>` — custom persistent session

### CronWakeMode

```typescript
type CronWakeMode = "next-heartbeat" | "now";
```

### CronDeliveryMode

```typescript
type CronDeliveryMode = "none" | "announce" | "webhook";
```

### CronDelivery

```typescript
type CronDelivery = {
  mode: CronDeliveryMode;
  channel?: CronMessageChannel;    // ChannelId ("telegram", "whatsapp", "slack", etc.)
  to?: string;                     // Target identifier (phone, chat ID, URL for webhook)
  threadId?: string | number;      // Thread/topic for threaded delivery
  accountId?: string;              // Multi-account channel support
  bestEffort?: boolean;
  failureDestination?: CronFailureDestination;
};
```

### CronFailureDestination

```typescript
type CronFailureDestination = {
  channel?: CronMessageChannel;
  to?: string;
  accountId?: string;
  mode?: "announce" | "webhook";
};
```

### CronPayload (discriminated union)

```typescript
type CronPayload =
  | { kind: "systemEvent"; text: string }
  | CronAgentTurnPayload;

type CronAgentTurnPayload = {
  kind: "agentTurn";
  message: string;
  model?: string;
  fallbacks?: string[];
  thinking?: string;
  timeoutSeconds?: number;
  allowUnsafeExternalContent?: boolean;
  externalContentSource?: HookExternalContentSource;
  lightContext?: boolean;
  toolsAllow?: string[];
};
```

### CronRunStatus / CronDeliveryStatus

```typescript
type CronRunStatus = "ok" | "error" | "skipped";
type CronDeliveryStatus = "delivered" | "not-delivered" | "unknown" | "not-requested";
```

### CronUsageSummary

```typescript
type CronUsageSummary = {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
};
```

### CronRunTelemetry

```typescript
type CronRunTelemetry = {
  model?: string;
  provider?: string;
  usage?: CronUsageSummary;
};
```

### CronRunOutcome

```typescript
type CronRunOutcome = {
  status: CronRunStatus;
  error?: string;
  errorKind?: "delivery-target";
  summary?: string;
  sessionId?: string;
  sessionKey?: string;
};
```

### CronFailureAlert

```typescript
type CronFailureAlert = {
  after?: number;           // Alert after N consecutive failures
  channel?: CronMessageChannel;
  to?: string;
  cooldownMs?: number;      // Min time between alerts
  mode?: "announce" | "webhook";
  accountId?: string;
};
```

### CronJobState

```typescript
type CronJobState = {
  nextRunAtMs?: number;
  runningAtMs?: number;
  lastRunAtMs?: number;
  lastRunStatus?: CronRunStatus;
  lastStatus?: "ok" | "error" | "skipped";  // Back-compat alias
  lastError?: string;
  lastErrorReason?: FailoverReason;
  lastDurationMs?: number;
  consecutiveErrors?: number;
  lastFailureAlertAtMs?: number;
  scheduleErrorCount?: number;
  lastDeliveryStatus?: CronDeliveryStatus;
  lastDeliveryError?: string;
  lastDelivered?: boolean;
};
```

### CronJob

```typescript
type CronJob = CronJobBase<CronSchedule, CronSessionTarget, CronWakeMode, CronPayload, CronDelivery, CronFailureAlert | false> & {
  state: CronJobState;
};
// CronJobBase adds: id, agentId?, sessionKey?, name, description?, enabled, deleteAfterRun?, createdAtMs, updatedAtMs, schedule, sessionTarget, wakeMode, payload, delivery?, failureAlert?
```

### CronStoreFile

```typescript
type CronStoreFile = { version: 1; jobs: CronJob[] };
```

### CronJobCreate / CronJobPatch

```typescript
type CronJobCreate = Omit<CronJob, "id" | "createdAtMs" | "updatedAtMs" | "state"> & { state?: Partial<CronJobState> };
type CronJobPatch = Partial<Omit<CronJob, "id" | "createdAtMs" | "state" | "payload">> & { payload?: CronPayloadPatch; delivery?: CronDeliveryPatch; state?: Partial<CronJobState> };
```

## Schedule Computation (`schedule.ts`)

### `computeNextRunAtMs(schedule, nowMs) -> number | undefined`

For each schedule kind:

- **`at`**: Parses `at` string (or legacy `atMs` number) via `parseAbsoluteTimeMs()`. Returns the timestamp if in the future, else `undefined`.
- **`every`**: Computes next fire from anchor + interval. `anchorMs` defaults to `nowMs`. Calculates `steps = ceil((nowMs - anchor) / everyMs)` and returns `anchor + steps * everyMs`.
- **`cron`**: Uses `croner` library with timezone (defaults to `Intl.DateTimeFormat().resolvedOptions().timeZone`). Includes workaround for croner year-rollback bug: retries from next second and tomorrow if result is in the past.

Cron expression evaluation is cached (LRU, max 512 entries keyed by `timezone\0expr`).

### `computePreviousRunAtMs(schedule, nowMs) -> number | undefined`

Only for `kind: "cron"`. Uses `croner.previousRuns(1)`.

## Stagger Algorithm (`jobs.ts`)

For cron-kind jobs, a per-job deterministic offset is computed:

```
offset = sha256(jobId).readUInt32BE(0) % staggerMs
```

The offset is cached (max 4096 entries). `computeStaggeredCronNextRunAtMs` shifts the schedule cursor backwards by the offset, finds the base next-run, then adds the offset. Retries up to 4 times to find a future slot.

Default stagger values are resolved from cron expression patterns (e.g., top-of-hour expressions get a default stagger window).

## Normalization (`normalize.ts`)

### `normalizeCronJobInput(raw, options) -> UnknownRecord | null`

Comprehensive input normalization:

1. **Unwrapping**: Extracts from `raw.data` or `raw.job` wrapper if present.
2. **Agent ID**: Sanitized via `sanitizeAgentId()`.
3. **Schedule coercion**: Auto-infers `kind` from fields (`atMs`/`at` -> `"at"`, `everyMs` -> `"every"`, `expr`/`cron` -> `"cron"`). Converts legacy `atMs` number to ISO `at` string. Normalizes stagger. Strips irrelevant fields per kind.
4. **Payload coercion**: Auto-infers kind from `message` (-> `agentTurn`) or `text` (-> `systemEvent`). Normalizes model, thinking, timeout, fallbacks, toolsAllow fields. Strips delivery fields from payload.
5. **Session target**: Normalizes to `"main"` | `"isolated"` | `"current"` | `"session:<id>"`. Custom IDs validated via `assertSafeCronSessionTargetId()`.
6. **Wake mode**: `"now"` | `"next-heartbeat"`.
7. **Delivery**: Normalizes mode, channel, to, threadId, accountId.

When `applyDefaults: true` (used by `normalizeCronJobCreate`):
- `wakeMode` defaults to `"now"`
- `enabled` defaults to `true`
- `name` auto-generated from schedule/payload if missing
- `sessionTarget` defaults: systemEvent -> `"main"`, agentTurn -> `"isolated"`
- `"current"` resolved to `session:<sessionKey>` from context, falls back to `"isolated"`
- `at` kind gets `deleteAfterRun: true`
- Cron kind gets default stagger
- Isolated agentTurn gets `delivery: { mode: "announce" }`

## Service State (`state.ts`)

### CronServiceState

```typescript
type CronServiceState = {
  deps: CronServiceDepsInternal;
  store: CronStoreFile | null;
  timer: NodeJS.Timeout | null;
  running: boolean;
  op: Promise<unknown>;
  warnedDisabled: boolean;
  storeLoadedAtMs: number | null;
  storeFileMtimeMs: number | null;
};
```

### CronServiceDeps

```typescript
type CronServiceDeps = {
  nowMs?: () => number;
  log: Logger;
  storePath: string;
  cronEnabled: boolean;
  cronConfig?: CronConfig;
  defaultAgentId?: string;
  resolveSessionStorePath?: (agentId?: string) => string;
  sessionStorePath?: string;
  missedJobStaggerMs?: number;          // Default: 5000
  maxMissedJobsPerRestart?: number;     // Default: 5
  enqueueSystemEvent: (text, opts?) => void;
  requestHeartbeatNow: (opts?) => void;
  runHeartbeatOnce?: (opts?) => Promise<HeartbeatRunResult>;
  wakeNowHeartbeatBusyMaxWaitMs?: number;
  wakeNowHeartbeatBusyRetryDelayMs?: number;
  runIsolatedAgentJob: (params) => Promise<CronRunOutcome & CronRunTelemetry & {...}>;
  sendCronFailureAlert?: (params) => Promise<void>;
  onEvent?: (evt: CronEvent) => void;
};
```

### CronEvent

```typescript
type CronEvent = {
  jobId: string;
  action: "added" | "updated" | "removed" | "started" | "finished";
  runAtMs?: number;
  durationMs?: number;
  status?: CronRunStatus;
  error?: string;
  summary?: string;
  delivered?: boolean;
  deliveryStatus?: CronDeliveryStatus;
  deliveryError?: string;
  sessionId?: string;
  sessionKey?: string;
  nextRunAtMs?: number;
} & CronRunTelemetry;
```

## Timer Loop (`timer.ts`)

### Constants

- `MAX_TIMER_DELAY_MS = 60_000` — Timer never sleeps longer than 60s
- `MIN_REFIRE_GAP_MS = 2_000` — Safety net to prevent spin-loops
- `DEFAULT_MISSED_JOB_STAGGER_MS = 5_000` — Delay between missed job catchups
- `DEFAULT_MAX_MISSED_JOBS_PER_RESTART = 5` — Max missed jobs run on startup
- `DEFAULT_FAILURE_ALERT_AFTER = 2` — Alert after 2 consecutive failures
- `DEFAULT_FAILURE_ALERT_COOLDOWN_MS = 3_600_000` — 1 hour cooldown

### Error Backoff Schedule

```typescript
const DEFAULT_BACKOFF_SCHEDULE_MS = [
  30_000,      // 1st error  → 30s
  60_000,      // 2nd error  → 1 min
  5 * 60_000,  // 3rd error  → 5 min
  15 * 60_000, // 4th error  → 15 min
  60 * 60_000, // 5th+ error → 60 min (stays constant)
];
```

### Transient Error Detection

```typescript
const TRANSIENT_PATTERNS = {
  rate_limit: /(rate[_ ]limit|too many requests|429|...)/i,
  overloaded: /\b529\b|\boverloaded\b|high demand|.../i,
  network: /(network|econnreset|econnrefused|...)/i,
  timeout: /(timeout|etimedout)/i,
  server_error: /\b5\d{2}\b/,
};
```

One-shot jobs get up to `DEFAULT_MAX_TRANSIENT_RETRIES = 3` retries on transient errors.

### `armTimer(state)`

Computes the next wake time from all enabled jobs' `nextRunAtMs`. Sets a `setTimeout` capped at `MAX_TIMER_DELAY_MS`. On fire, acquires lock, reloads store if stale, walks due jobs, executes them, persists results, and re-arms.

### `runMissedJobs(state, opts?)`

On startup, collects jobs that are past due. Sorts by `nextRunAtMs`. Runs up to `maxMissedJobsPerRestart` with `missedJobStaggerMs` delay between them. One-shot jobs interrupted mid-run (detected by stale `runningAtMs` marker) are excluded via `skipJobIds`.

### `executeJobCoreWithTimeout(state, job)`

Wraps `executeJobCore()` with per-job timeout via `AbortController` and `Promise.race`.

### `applyJobResult(state, job, result, opts?)`

Handles post-execution state updates:
- Sets `lastRunAtMs`, `lastDurationMs`, `lastRunStatus`, `lastStatus`
- On success: clears `consecutiveErrors`, clears `lastError`, recomputes `nextRunAtMs`
- On error: increments `consecutiveErrors`, applies backoff delay to `nextRunAtMs`, checks failure alert thresholds
- For one-shot (`at`): disables job after success, returns `true` if `deleteAfterRun` is set
- Respects `MIN_REFIRE_GAP_MS` to prevent spin-loops

### Failure Alerts

When `consecutiveErrors >= failureAlert.after` (default 2) and cooldown has elapsed:
- Emits alert via `sendCronFailureAlert()` or falls back to `enqueueSystemEvent()` + `requestHeartbeatNow()`
- Records `lastFailureAlertAtMs` for cooldown gating

### Schedule Error Auto-Disable

`MAX_SCHEDULE_ERRORS = 3`. After 3 consecutive `computeJobNextRunAtMs()` failures, the job is auto-disabled with a system event notification.

## Operations (`ops.ts`)

### `start(state)`

1. Clear stale `runningAtMs` markers from all jobs
2. Run missed jobs (with stagger, skipping interrupted one-shots)
3. Recompute all next-run times
4. Arm timer

### `stop(state)`

Clears the timer.

### `add(state, input: CronJobCreate) -> CronJob`

Creates job with UUID, computes initial `nextRunAtMs`, persists, arms timer, emits "added" event.

### `update(state, id, patch: CronJobPatch) -> CronJob`

Applies patch, recomputes `nextRunAtMs` if schedule/enabled changed, persists, arms timer, emits "updated" event. For `every` kind, ensures `anchorMs` exists.

### `remove(state, id)`

Filters job from store, persists, arms timer, emits "removed" event.

### `run(state, id, mode?: "due" | "force")`

Manual run with two-phase execution:
1. **Preflight** (under lock): validates job, checks `runningAtMs`, checks due/forced
2. **Prepare** (under lock): sets `runningAtMs` marker, persists, creates task ledger record, snapshots job for execution
3. **Execute** (outside lock): runs `executeJobCoreWithTimeout()` so read ops stay responsive
4. **Finish** (under lock): reloads store, applies result, handles one-shot deletion, persists, re-arms timer

### `enqueueRun(state, id, mode?)`

Queues manual run via `CommandLane.Cron` for background execution.

### `list(state, opts?) / listPage(state, opts?)`

Paginated listing with filters (enabled/disabled/all), sorting (nextRunAtMs/updatedAtMs/name), and text search across name/description/agentId.

## Job Logic (`jobs.ts`)

### `createJob(state, input) -> CronJob`

- UUID via `crypto.randomUUID()`
- Normalizes agent ID, name, description
- Resolves `every` anchor, cron stagger
- `deleteAfterRun` defaults to `true` for `at` kind
- Validates: `assertSupportedJobSpec`, `assertMainSessionAgentId`, `assertDeliverySupport`, `assertFailureDestinationSupport`
- Computes initial `nextRunAtMs`

### `assertSupportedJobSpec(job)`

- `main` requires `payload.kind="systemEvent"`
- `isolated`/`current`/`session:*` requires `payload.kind="agentTurn"`

### `isJobDue(job, nowMs, { forced })`

Returns `false` if `runningAtMs` is set (already running). If forced, always `true`. Otherwise, `enabled && nextRunAtMs <= nowMs`.

### `recomputeNextRuns(state)`

Walks all schedulable jobs. Only recomputes if `nextRunAtMs` is missing or past-due. Preserves still-future values to avoid advancing unexecuted jobs.

### `recomputeNextRunsForMaintenance(state, opts?)`

Read-only-safe version. Only repairs missing/invalid `nextRunAtMs`. Does NOT advance past-due values (prevents silent skip of unexecuted jobs, see #13992). Optionally recomputes expired slots that have already been executed.

### Stuck Run Detection

`STUCK_RUN_MS = 2 * 60 * 60 * 1000` (2 hours). Jobs with `runningAtMs` older than 2 hours have their marker cleared.

## Active Jobs Tracking (`active-jobs.ts`)

Global singleton `Set<string>` of currently executing job IDs. Used for concurrency control.

```typescript
function markCronJobActive(jobId: string): void
function clearCronJobActive(jobId: string): void
function isCronJobActive(jobId: string): boolean
```

## Concurrency Model

- **Store locking:** All mutations go through `locked(state, fn)` which serializes operations via a Promise chain (`state.op`).
- **Timer rearm:** After every mutation that changes next-run times.
- **Run concurrency:** Configurable via `cronConfig.maxConcurrentRuns` (default 1). Execution happens outside the lock so reads stay responsive.
- **Store reload:** Checks file mtime on disk to detect external modifications.

## Run Log (`run-log.ts`)

### CronRunLogEntry

```typescript
type CronRunLogEntry = {
  ts: number;
  jobId: string;
  action: "finished";
  status?: CronRunStatus;
  error?: string;
  summary?: string;
  delivered?: boolean;
  deliveryStatus?: CronDeliveryStatus;
  deliveryError?: string;
  sessionId?: string;
  sessionKey?: string;
  runAtMs?: number;
  durationMs?: number;
  nextRunAtMs?: number;
} & CronRunTelemetry;
```

### Storage

- Path: `<storeDir>/runs/<jobId>.jsonl`
- Format: JSONL, one entry per line
- Permissions: `0o600` (file), `0o700` (directory)
- Pruning: when file exceeds `maxBytes` (default 2MB), keeps last `keepLines` (default 2000)
- Serialized writes: per-path Promise chain prevents concurrent append corruption

### Pagination

```typescript
type CronRunLogPageResult = {
  entries: CronRunLogEntry[];
  total: number;
  offset: number;
  limit: number;          // Max 200
  hasMore: boolean;
  nextOffset: number | null;
};
```

Filters: jobId, status (all/ok/error/skipped), deliveryStatus, text query. Sort: asc/desc by timestamp.

### Cross-Job View

`readCronRunLogEntriesPageAll()` reads all `.jsonl` files in the runs directory, merges, filters, sorts, and paginates. Optionally enriches entries with `jobName`.

## Delivery Planning (`delivery-plan.ts`)

### CronDeliveryPlan

```typescript
type CronDeliveryPlan = {
  mode: CronDeliveryMode;
  channel?: CronMessageChannel;
  to?: string;
  threadId?: string | number;
  accountId?: string;
  source: "delivery";
  requested: boolean;
};
```

### `resolveCronDeliveryPlan(job) -> CronDeliveryPlan`

1. If job has explicit delivery config: uses that mode (normalizes `"deliver"` -> `"announce"`)
2. If no delivery: isolated agentTurn jobs default to `{ mode: "announce", channel: "last" }`, others to `{ mode: "none" }`

### CronFailureDeliveryPlan

```typescript
type CronFailureDeliveryPlan = {
  mode: "announce" | "webhook";
  channel?: CronMessageChannel;
  to?: string;
  accountId?: string;
};
```

### `resolveFailureDestination(job, globalConfig?) -> CronFailureDeliveryPlan | null`

Merges job-level `failureDestination` over global `cron.failureAlert` config. Returns `null` if the failure destination would be identical to the primary delivery target (no point delivering twice to the same place).

## One-Shot Lifecycle

1. Created with `schedule: { kind: "at", at: "2025-12-25T00:00:00Z" }`
2. `deleteAfterRun` defaults to `true`
3. Job stays due until successfully executed (even if past the scheduled time)
4. On `status: "ok"`: if `deleteAfterRun`, removed from store and "removed" event emitted
5. On transient error: up to 3 retries with backoff
6. Interrupted one-shots (stale `runningAtMs` on startup) are NOT retried

## Startup Catchup

1. Clears stale `runningAtMs` markers
2. Collects past-due jobs (excluding interrupted one-shots)
3. Runs up to `maxMissedJobsPerRestart` (default 5) with `missedJobStaggerMs` (default 5s) delay between them
4. Deferred jobs reschedule to their next window
