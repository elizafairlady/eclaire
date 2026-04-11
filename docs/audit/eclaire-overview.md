# eclaire Codebase Overview

Audited: 2026-04-09, updated 2026-04-10

## Stats

- **LOC**: ~34,400
- **Go files**: 162 (103 source + 59 test)
- **Tests**: 497
- **Packages**: 12 internal packages + cmd/ecl
- **Binary**: `ecl`

## Package Inventory

```
cmd/ecl/              Entry point (8 lines — calls cli.Execute())
internal/
  agent/              30+ files, ~6,000+ LOC
                      Agent interface, registry, runner, runtime, context engine,
                      workspace loader, scheduler, job store, job executor,
                      approval gate, task registry, flow executor, coordinator,
                      skills, compaction, loop detection, notifications, run logs,
                      dreaming, memory flush, system events queue, session reaper
  bus/                2 files, ~160 LOC
                      In-memory typed pub/sub, topic constants
  channel/            1 file, ~154 LOC
                      Channel plugin interface (no implementations yet)
  cli/                12 files, ~1,400 LOC
                      Cobra subcommands (run, agent, session, job, cron, etc.)
  config/             2 files, ~139 LOC
                      YAML config, env var expansion
  gateway/            3 files, ~500+ LOC
                      Daemon, Unix socket, NDJSON, RPC handlers
  hook/               2 files, ~220 LOC
                      Pre/post tool hooks, shell execution
  persist/            2 files, ~200+ LOC
                      JSONL sessions, message conversion
  provider/           5 files, ~400+ LOC
                      Ollama + OpenRouter via fantasy, routing, fallback
  testutil/           3 files, ~50 LOC
                      MockModel, MockRouter, TestEnv
  tool/               30 files, ~2,500 LOC
                      26 tools, registry, permissions, approval wrapper
  ui/                 20 files, ~1,500+ LOC
                      Bubble Tea v2 + Ultraviolet Draw, approval dialog,
                      chat rendering, markdown, scrollback
```

## Wiring Diagram — What's Actually Connected

```
Gateway.Start()
  ├── AgentRegistry (5 built-in + disk-loaded)          ← WORKS
  ├── ToolRegistry (26 tools, permission checking)       ← WORKS
  ├── SessionStore (per-session JSONL)                   ← WORKS
  │   └── GetOrCreateMain("orchestrator")                ← WORKS (created on startup)
  ├── MessageBus (~15 topics)                            ← WORKS
  ├── ProviderRouter (ollama/openrouter)                 ← WORKS
  ├── Runner
  │   ├── ConversationRuntime (agentic loop)             ← WORKS
  │   ├── HookRunner (if configured)                     ← WORKS
  │   ├── PermissionChecker (PermissionWriteOnly)        ← WORKS (prompts on Dangerous tools)
  │   ├── SystemEventQueue                               ← WORKS (drains into prompt)
  │   └── MemoryFlush (before compaction)                ← WORKS
  ├── WorkspaceLoader (layers 1-3 + layer 4 from daemon CWD) ← PARTIAL (not per-connection)
  ├── ContextEngine (priority sections, git, skills)     ← WORKS
  ├── Scheduler (LEGACY — should be removed)
  │   ├── Heartbeat loop (30min ticks)                   ← WORKS but COMPETING
  │   └── Cron loop (1min ticks, 5-field only)           ← WORKS but COMPETING
  ├── JobStore (persistent JSON, at/every/cron)          ← WORKS
  ├── JobExecutor (timer loop, at/every/cron)            ← WORKS, COMPETING with Scheduler
  ├── NotificationStore (JSONL persistence)              ← WORKS, subscribed to bus
  ├── ApprovalGate (bus-based blocking)                  ← WORKS, wired end-to-end
  ├── RunLog (per-job JSONL)                             ← WORKS
  ├── DreamingService (3-phase memory consolidation)     ← WORKS (via JobStore)
  └── SessionReaper (stale session cleanup)              ← WORKS

TUI ←→ Gateway (Unix socket, NDJSON)
  CLI sends CWD on connect via ConnectWithCWD()          ← WORKS
  Gateway detects project root (detectProjectRoot)       ← WORKS
  Approval dialog wired via TypeEvent broadcast          ← WORKS
  TUI does NOT drain notifications on connect            ← MISSING
  TUI does NOT show main session as permanent tab        ← MISSING
```

## Dead Code

| Item | Location | Why Dead |
|------|----------|----------|
| `HeartbeatTask.Once` | scheduler.go:49 | Defined but never checked in isTaskDue() |
| `BackgroundResult.OneShot` | topics.go:60 | Defined but never set to true |
| `builtinAgent.Handle/Stream()` | builtin.go:41-49 | Returns dummy "use Runner" string, never called |
| `Coordinator.Spawn/Kill/Delegate` | coordinator.go | Defined but not called by Gateway |

## Subsystem Status

**"Code exists" means the code is present and compiles. It does NOT mean it works correctly. Only user validation determines if something works.**

| Subsystem | Status | Validated? |
|-----------|--------|------------|
| Agent execution (streaming, tools, compaction) | Code exists, tests pass with mocks | **NO** — not validated with real LLM by user |
| 26 tools with hook integration | Code exists, unit tests pass | **NO** — individual tools not validated by user |
| Session JSONL persistence | Code exists, unit tests pass | **NO** �� not validated across restarts by user |
| Heartbeat scheduling (30min) | Code exists, runs on timer | **NO** — user has not confirmed results arrive |
| Cron scheduling (5-field, 1min ticks) | Code exists, runs on timer | **NO** — user has not confirmed results arrive |
| BOOT.md once-per-day | Code exists | **NO** |
| Workspace loading (layers 1-3) | Code exists | **NO** |
| Context engine (prompt assembly) | Code exists, unit tests pass | **NO** — prompt quality not validated by user |
| Skills discovery | Code exists | **NO** |
| TUI (markdown, tools, scrollback) | Code exists, not tested on real TTY | **NO** |
| JobExecutor (unified scheduling) | Code exists, competing with Scheduler | **NO** |
| JobStore (persistent jobs) | Code exists, unit tests pass | **NO** |
| NotificationStore | Code exists, subscribed to bus, CLI works | **NO** — not validated by user |
| ApprovalGate | Code exists, wired end-to-end, PermissionWriteOnly | **NO** — not validated by user |
| Permission prompting | Code exists, PermissionWriteOnly default | **NO** — not validated by user |
| Main session | Created on gateway startup, persists across restarts | **NO** — not validated by user |
| Project root detection | detectProjectRoot() in gateway, CWD sent on connect | **NO** — not validated by user |
| Project workspace layer | Loads from daemon CWD only, not per-connection | **NO** — partially working |
| Memory dreaming (3-phase) | Via JobStore, light/deep/REM schedules | **NO** — not validated by user |
| System events queue | Drains into prompt, background awareness | **NO** — not validated by user |
| Coordinator agent management | Code exists but methods never called | **NO** — dead path |
