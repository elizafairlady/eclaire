# eclaire Scheduling Audit

Audited: 2026-04-09, updated 2026-04-10

## Two Competing Systems

eclaire has TWO scheduling systems running in parallel. Both are started by the Gateway.

### System A: Scheduler (Legacy)

**File**: `internal/agent/scheduler.go` (~700 lines)

**What it does**:
- **Heartbeat loop**: Ticks every 30 minutes. Parses HEARTBEAT.md for YAML task blocks. Runs each due task as an agent turn.
- **Cron loop**: Ticks every 1 minute. Loads entries from `cron.yaml`. 5-field cron expressions ONLY.
- **BOOT.md**: Runs once per day on first scheduler start.

**Key types**:
- `CronEntry`: id, schedule (5-field string), agentID, prompt, enabled, lastRun, nextRun
- `HeartbeatTask`: name, interval, agent, prompt, once (**DEAD FIELD**)
- `HeartbeatConfig`: freeform markdown + structured YAML tasks

**Code exists for** (not validated by user):
- Heartbeat task parsing from YAML blocks
- Interval-based due checking
- Cron entry loading from disk
- BOOT.md once-per-day
- Per-task state tracking (lastRun, status, error)
- Bus publishing for lifecycle events

**What's broken**:
- `HeartbeatTask.Once` field: defined at line 48, NEVER read in `isTaskDue()` at line 319. Dead code.
- `runHeartbeatLegacy()` runs entire HEARTBEAT.md as single prompt — sends ALL tasks as one string to orchestrator
- Heartbeat tasks use `PromptModeMinimal` (wrong for heartbeat — should be Full for date/memory context)
- CronEntry.LastRun not persisted across restarts
- No exponential backoff on failure
- No task history/run logs
- No `at` (one-shot) schedule kind
- No `every` (interval) schedule kind
- CLI `cron_add` hardcodes 5-field validation

### System B: JobExecutor (New)

**Files**: `internal/agent/jobstore.go` (~300 lines), `internal/agent/jobexec.go` (~350 lines)

**What it does**:
- `JobStore`: Persistent JSON storage of Job definitions
- `JobExecutor`: Timer-based loop that executes due jobs

**Key types**:
- `Job`: unified type with `Schedule.Kind` of "at", "every", or "cron"
- `JobSchedule`: `Kind` + `At` (time or duration) + `Every` (duration) + `Cron` (5-field) + `Timezone`
- `JobState`: NextRunAt, LastRunAt, LastStatus, ConsecutiveErrors, RunningAt

**Code exists for** (not validated by user):
- Three schedule kinds: at (one-shot), every (interval), cron (5-field)
- Persistent job store (atomic JSON writes)
- Schedule computation for all three kinds
- Timer-based execution (sub-minute precision)
- Transient error retry with exponential backoff (30s, 60s, 5min)
- One-shot auto-deletion after success
- Job state persistence
- Bus publishing
- Notification creation on completion
- Run log integration

**What works** (added since initial audit):
- `eclaire_manage job_add/remove/list/runs/run` — all 5 tool operations exist (25 total manage operations)
- `ecl job add/remove/list/runs/run` CLI — fully implemented
- Notification creation on job completion — wired via bus subscription
- Run logs per-job JSONL — implemented
- Memory dreaming uses 3 jobs (light/deep/REM phases) via this system

**What's broken**:
- `Job.ContextMessages` field: designed to embed session history, never set by any tool
- `Job.SessionTarget` supports "isolated" vs "main" but executor ignores it — always isolated
- Expired `at` jobs return nil NextRunAt — ambiguous (error vs expected?)
- No max retry limit for recurring jobs
- No startup catchup for missed jobs

## The Conflict

Both systems are started by Gateway:
```
gateway.go: sched.Start(ctx)      // Legacy scheduler
gateway.go: jobExecutor.Start(ctx) // New job executor
```

They publish to the SAME bus topics (`TopicCronStarted`, `TopicCronCompleted`). They can both try to run the same underlying work. The Scheduler reads from HEARTBEAT.md and cron.yaml. The JobExecutor reads from jobs.json. They don't know about each other.

## What Needs to Happen

1. Remove Scheduler entirely
2. Migrate heartbeat tasks to Jobs (kind: "every")
3. Migrate cron entries to Jobs (kind: "cron")
4. Keep BOOT.md as a special one-shot job (kind: "at", run on startup if not run today)
5. Update `eclaire_manage` to use `job_add/remove/list` with all three kinds
6. Update CLI `ecl job add` to accept all three kinds
7. Delete `HeartbeatTask.Once`, `BackgroundResult.OneShot` dead code
8. Implement startup catchup for missed recurring jobs
9. Implement session target routing (main vs isolated)

## Reference

- OpenClaw scheduling: `docs/reference/openclaw-scheduling.md`
- Claw Code task registry: `docs/reference/clawcode-tools.md`
