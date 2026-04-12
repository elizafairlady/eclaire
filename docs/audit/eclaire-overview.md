# eclaire Codebase Overview

Audited: 2026-04-11

## Stats

- **LOC**: ~40,000
- **Go files**: 171 (103 source + 68 test)
- **Tests**: 591
- **Packages**: 16 (13 internal + 3 ui sub-packages + cmd/ecl)
- **Binary**: `ecl`

## Package Inventory

```
cmd/ecl/              Entry point (1 file, 7 lines — calls cli.Execute())
internal/
  agent/              58 files, ~16,500 LOC
                      Agent interface, registry, runner, runtime, context engine,
                      workspace loader, job store, job executor, flowstore,
                      approval gate, task registry, flow executor, coordinator,
                      skills, compaction, loop detection, notifications, run logs,
                      dreaming, memory flush, system events queue, session reaper,
                      loader, builtin_prompts, integration tests
  bus/                3 files, ~415 LOC
                      In-memory typed pub/sub, topic constants, 100ms retry window
  channel/            2 files, ~269 LOC
                      Channel plugin interface (no implementations yet)
  cli/                14 files, ~2,175 LOC
                      Cobra subcommands (run, agent, session, job, cron, init,
                      flow, remind, briefing, tasks, notifications, system-prompt)
  config/             4 files, ~589 LOC
                      YAML config, env var expansion, global+project merge
  gateway/            5 files, ~2,734 LOC
                      Daemon, Unix socket, NDJSON, RPC handlers, project root detection
  hook/               3 files, ~504 LOC
                      Pre/post tool hooks, shell execution
  persist/            6 files, ~1,194 LOC
                      JSONL sessions, message conversion, approval pattern persistence
  provider/           5 files, ~520 LOC
                      Ollama + OpenRouter via fantasy, routing, fallback
  testutil/           3 files, ~578 LOC
                      MockModel (thread-safe), TestEnv, MockRouter
  tool/               47 files, ~9,454 LOC
                      26 tools, registry, permissions, shell executor chokepoint,
                      sandbox (bwrap), input validation (validate.go)
  ui/                 5 files + 3 sub-packages, ~5,022 LOC total
    (root)            5 files, ~3,492 LOC — Bubble Tea v2 + Ultraviolet Draw,
                      notification focus, session picker, markdown, scrollback
    chat/             11 files, ~1,132 LOC — chat rendering, message list, items
    dialog/           2 files, ~166 LOC — modal dialogs
    styles/           2 files, ~232 LOC — color palette, layout constants
```

## Wiring Diagram — What's Actually Connected

```
Gateway.Start()
  ├── AgentRegistry (5 built-in + disk-loaded)          ← WORKS
  ├── ToolRegistry (26 tools, permission checking)       ← WORKS
  ├── SessionStore (per-session JSONL)                   ← WORKS
  │   └── GetOrCreateMain("orchestrator")                ← WORKS (created on startup)
  ├── MessageBus (~15 topics, 100ms retry window)        ← WORKS
  ├── ProviderRouter (ollama/openrouter)                 ← WORKS
  ├── Runner
  │   ├── ConversationRuntime (agentic loop)             ← WORKS
  │   ├── HookRunner (if configured)                     ← WORKS
  │   ├── PermissionChecker (PermissionWriteOnly)        ← WORKS (granular command-pattern approvals)
  │   ├── Sandbox (bwrap filesystem isolation)            ← WORKS (write roots from workspace)
  │   ├── SystemEventQueue                               ← WORKS (drains into prompt)
  │   └── MemoryFlush (before compaction)                ← WORKS
  ├── WorkspaceLoader (layers 1-4 via LoadWithProject)   ← WORKS (per-run via cfg.ProjectDir)
  ├── ContextEngine (priority sections, git, skills)     ← WORKS
  ├── JobStore (persistent JSON, at/every/cron)          ← WORKS (unified, Scheduler removed)
  ├── JobExecutor (timer loop, at/every/cron)            ← WORKS
  ├── FlowStore (persistent flow runs, 30-day cleanup)   ← WORKS
  ├── NotificationStore (JSONL persistence)              ← WORKS, subscribed to bus
  ├── ApprovalGate (bus-based blocking)                  ← WORKS, wired end-to-end
  ├── RunLog (per-job JSONL)                             ← WORKS
  ├── DreamingService (3-phase memory consolidation)     ← WORKS (via JobStore)
  └── SessionReaper (stale session cleanup)              ← WORKS

TUI ←→ Gateway (Unix socket, NDJSON)
  CLI sends CWD on connect via ConnectWithCWD()          ← WORKS
  Gateway detects project root (detectProjectRoot)       ← WORKS
  Approval dialog integrated into app.go (TypeEvent)     ← WORKS
  Notifications fetched on connect (sidebar + count)     ← WORKS
  ctrl+j notification focus mode                         ← WORKS
  Cost tracking in status bar (totalCost from provider)  ← WORKS
  TUI does NOT show main session as permanent tab        ← MISSING
```

## Dead Code

Most dead code identified in the April 9 audit has been removed:
- `HeartbeatTask.Once` — removed
- `BackgroundResult.OneShot` — removed
- `builtinAgent.Handle/Stream()` — removed
- `Coordinator.Spawn/Kill/Delegate` — removed (coordinator simplified)
- `pipeline.go` (PipelineRunner) — removed
- `cronexpr.go` — removed (cron parsing consolidated)
- `dreaming_prompts.go` — removed (moved to builtin_prompts.go)
- `instructions.go` — removed (consolidated into context_engine.go/loader.go)
- `approval_dialog.go` — removed (integrated into app.go)
- `PermissionWrapper` — removed from production code (only in test code)
- `Scheduler` struct — removed (replaced by unified JobExecutor)

| Item | Location | Why Dead |
|------|----------|----------|
| `PermissionWrapper` | permission_test.go only | Test-only remnant, not used in production |

## Subsystem Status

**"Code exists" means the code is present and compiles. It does NOT mean it works correctly. Only user validation determines if something works.**

| Subsystem | Status | Validated? |
|-----------|--------|------------|
| Agent execution (streaming, tools, compaction) | Code exists, tests pass with mocks + integration tests | **NO** — not validated with real LLM by user |
| 26 tools with hook integration | Code exists, unit tests pass | **NO** — individual tools not validated by user |
| Session JSONL persistence | Code exists, unit tests pass | **NO** — not validated across restarts by user |
| Unified JobExecutor (at/every/cron) | Code exists, Scheduler removed, unit tests pass | **NO** — user has not confirmed results arrive |
| JobStore (persistent jobs) | Code exists, unit tests pass | **NO** — not validated by user |
| BOOT.md once-per-day | Code exists, via JobExecutor.RunBootIfNeeded | **NO** |
| Workspace loading (layers 1-4, per-run) | Code exists, LoadWithProject per-run via cfg.ProjectDir | **NO** |
| Context engine (prompt assembly) | Code exists, unit tests pass | **NO** — prompt quality not validated by user |
| Skills discovery | Code exists | **NO** |
| TUI (markdown, tools, scrollback, approvals) | Code exists, approval integrated into app.go | **NO** — not tested on real TTY |
| NotificationStore | Code exists, bus-subscribed, sidebar + focus mode | **NO** — not validated by user |
| ApprovalGate | Code exists, wired end-to-end, PermissionWriteOnly | **NO** — not validated by user |
| Permission system (granular command-pattern) | ApprovePattern, ExtractCommandPattern, AST-based, persists to session | **NO** — not validated by user |
| Sandbox (bwrap) | SandboxConfig, write root isolation, unit tests pass | **NO** — not validated by user |
| FlowStore (persistent flow runs) | 30-day cleanup, unit tests pass | **NO** — not validated by user |
| Main session | Created on gateway startup, persists across restarts | **NO** — not validated by user |
| Project root detection | detectProjectRoot() in gateway, CWD sent on connect | **NO** — not validated by user |
| Cost tracking | totalCost in TUI from provider, displayed in status bar | **NO** — not validated by user |
| Tool input validation | validate.go: JSON size, control chars, null bytes | **NO** — not validated by user |
| Memory dreaming (3-phase) | Via JobStore, light/deep/REM schedules | **NO** — not validated by user |
| System events queue | Drains into prompt, background awareness | **NO** — not validated by user |
| `ecl init` | Scaffolds .eclaire/ in project directory | **NO** — not validated by user |
