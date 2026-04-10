# eclaire Codebase Overview

Audited: 2026-04-09

## Stats

- **LOC**: ~27,100
- **Go files**: 138 (74 source + 64 test)
- **Tests**: 379
- **Packages**: 12 internal packages + cmd/ecl
- **Binary**: `ecl`

## Package Inventory

```
cmd/ecl/              Entry point (8 lines — calls cli.Execute())
internal/
  agent/              25 files, ~4,500 LOC
                      Agent interface, registry, runner, runtime, context engine,
                      workspace loader, scheduler, job store, job executor,
                      approval gate, task registry, flow executor, coordinator,
                      skills, compaction, loop detection, notifications, run logs
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
                      22+ tools, registry, permissions, approval wrapper
  ui/                 20 files, ~1,500+ LOC
                      Bubble Tea v2 + Ultraviolet Draw, approval dialog,
                      chat rendering, markdown, scrollback
```

## Wiring Diagram — What's Actually Connected

```
Gateway.Start()
  ├── AgentRegistry (5 built-in + disk-loaded)          ← WORKS
  ├── ToolRegistry (30 tools, permission checking)       ← WORKS
  ├── SessionStore (per-session JSONL)                   ← WORKS
  ├── MessageBus (~15 topics)                            ← WORKS
  ├── ProviderRouter (ollama/openrouter)                 ← WORKS
  ├── Runner
  │   ├── ConversationRuntime (agentic loop)             ← WORKS
  │   ├── HookRunner (if configured)                     ← WORKS
  │   └── PermissionChecker                              ← EXISTS but mode=Allow (never prompts)
  ├── WorkspaceLoader (layers 1-3, NOT layer 4)          ← PARTIAL (no project layer)
  ├── ContextEngine (priority sections, git, skills)     ← WORKS
  ├── Scheduler (LEGACY)
  │   ├── Heartbeat loop (30min ticks)                   ← WORKS
  │   └── Cron loop (1min ticks, 5-field only)           ← WORKS
  ├── JobStore (persistent JSON)                         ← EXISTS, wired
  ├── JobExecutor (timer loop, at/every/cron)            ← EXISTS, wired, COMPETING with Scheduler
  ├── NotificationStore (JSONL persistence)              ← EXISTS but NOT subscribed to bus
  ├── ApprovalGate (bus-based blocking)                  ← EXISTS but NEVER triggered
  └── RunLog (per-job JSONL)                             ← EXISTS, used by JobExecutor only

TUI ←→ Gateway (Unix socket, NDJSON)
  TUI does NOT pass CWD to gateway
  TUI does NOT drain notifications on connect
  TUI does NOT show main session as permanent tab
  Approval dialog exists but is NEVER shown (mode=Allow)
```

## Dead Code

| Item | Location | Why Dead |
|------|----------|----------|
| `HeartbeatTask.Once` | scheduler.go:48 | Defined but never checked in isTaskDue() |
| `BackgroundResult.OneShot` | topics.go:59 | Defined but never set to true |
| `builtinAgent.Handle/Stream()` | builtin.go:41-48 | Returns dummy "use Runner" string, never called |
| `Coordinator.Spawn/Kill/Delegate` | coordinator.go | Defined but not called by Gateway |
| Project workspace layer | workspace.go | projectDir is never set |

## Subsystem Status

**"Code exists" means the code is present and compiles. It does NOT mean it works correctly. Only user validation determines if something works.**

| Subsystem | Status | Validated? |
|-----------|--------|------------|
| Agent execution (streaming, tools, compaction) | Code exists, tests pass with mocks | **NO** — not validated with real LLM by user |
| 30 tools with hook integration | Code exists, unit tests pass | **NO** — individual tools not validated by user |
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
| NotificationStore | Code exists but never subscribed to bus | **NO** — dead path |
| ApprovalGate | Code exists but mode always Allow | **NO** — dead path |
| Permission prompting | Code exists but never triggered | **NO** — dead path |
| Main session | Concept exists, never created | **NO** — not implemented |
| Project root detection | Not implemented | **NO** |
| Project workspace layer | Code exists but projectDir never set | **NO** — dead path |
| Coordinator agent management | Code exists but methods never called | **NO** — dead path |
