# eclaire Gateway Architecture — Research Synthesis

Audited: 2026-04-11

---

## What the Gateway IS (Documented Intent)

### Conceptual Role

The gateway is the **central nervous system** of eclaire. It is not merely a router or proxy — it is the persistent daemon that owns every subsystem and exposes them all through a single, clean Unix socket interface to any client (TUI, CLI, future channels).

The design philosophy, stated explicitly in PROJECT_SPEC.md, is:

> "I'm building this for me and me alone [...] I need you to fucking understand what I am fucking communicating to you about this being an AI gateway to a personal assistant orchestrator agent."

> "I don't want a fucking facade. Everything should be composable, from the orchestrator and agents down."

The gateway is explicitly modeled on **OpenClaw's gateway architecture** — a daemon with a wire protocol, session management, scheduling, channels, and composable multi-agent orchestration. eclaire's difference is that it targets a single user on a single local machine instead of multi-tenant cloud deployment, but the internal composability model is the same.

### The Gateway's Seven Responsibilities

According to the documentation (README.md, PROJECT_SPEC.md, gateway/CLAUDE.md), the gateway is responsible for:

1. **Agent registry** — 5 built-in + disk-loaded agents with dynamic discovery
2. **Tool execution** — 26 tools with permission checking, hooks, workspace boundaries
3. **Session management** — persistent main session, project sessions scoped to CWD, isolated ephemeral sessions for background work
4. **Job scheduling** — unified at/every/cron scheduling with persistent store and run logs
5. **Notification delivery** — persistent store, severity levels, approval gate integration, channel plugins
6. **Permission and approval system** — PermissionWriteOnly default, dangerous tool approval gate, TUI/CLI wired delivery
7. **Provider routing** — Ollama, OpenRouter, role-based model routing with fallback chains

All of these are owned by the Gateway struct and initialized in `Gateway.New()`.

---

## High-Level Goals for the Gateway (Extracted from Docs)

### Goal 1: Single Trusted Entry Point
All AI agent execution flows through the gateway. No direct model calls from TUI or CLI. The Unix socket is the only interface. `chmod 0600` enforces single-user access.

### Goal 2: Persistent Daemon
The gateway starts once, stays running, and resumes state across restarts:
- Main session (Claire's permanent conversation) loaded from disk on startup
- Job store persisted as JSON
- Notification store persisted as JSONL
- Sessions stored as JSONL per-session in `~/.eclaire/sessions/`

### Goal 3: Composable Multi-Agent Orchestration
Claire (orchestrator) delegates to specialist agents. Agents are defined as YAML + workspace files on disk. New agents can be created at runtime via `eclaire_manage`. The agent tool dynamically lists all registered agents — no hardcoded agent names anywhere.

Multi-model orchestration is validated: DeepSeek orchestrator → Claude coding agent → Grok adversarial QA.

### Goal 4: Background Work and Scheduling
Three schedule kinds (at/every/cron) unified into one job system. Background work runs in isolated sessions, publishes results back to the main session via the system events queue, and creates notifications on failure.

### Goal 5: Approval Gate
Dangerous tool calls block until the user responds. Approval requests are broadcast to all connected clients as TypeEvent envelopes. CLI falls back to notification-based approval when no TTY is available. Background work creates approval notifications that persist via `ecl notifications`.

### Goal 6: Channel Extensibility (Planned, Not Implemented)
The `internal/channel/channel.go` interface and `channel.Manager` exist as a plugin substrate. In OpenClaw, channels are the primary inbound path (Telegram, Signal, WhatsApp, etc.). In eclaire, this is intentionally deferred but architecturally present.

### Goal 7: Self-Improvement
Claire can use `eclaire_manage` to create agents, skills, cron jobs, and modify her own workspace files. The gateway's `reloadFn` closure reloads agents and re-syncs heartbeat jobs without a restart.

---

## Planned But Unimplemented Features

### 1. Session Target Routing (Medium Priority)
`Job.SessionTarget` field exists ("isolated" / "main" / named), but the job executor **always uses isolated**. The documented intent (PROJECT_SPEC.md §Scheduling) is that cron/heartbeat results route to the main session as system events, while truly isolated runs get their own ephemeral session. The wiring partially exists (SystemEventQueue + bus subscription) but the executor never chooses the "main" path.

### 2. Per-Connection Project Workspace Loading (High Priority)
The workspace loader's `projectDir` is set **once at daemon startup** based on the daemon's CWD. Clients connecting from a different project directory correctly get a `ProjectSessionID` from `handleConnect()`, but the workspace loader used for that session is still the daemon's global one. The documented intent is that each project session loads `.eclaire/workspace/` from its project root.

### 3. TUI Notification Drain on Connect (Medium Priority)
The gateway has `MethodNotificationDrain` in its protocol. The TUI never calls it on connect. Notifications accumulate but are invisible to TUI users. Only `ecl notifications` CLI exposes them.

### 4. TUI Main Session Tab (Medium Priority)
The gateway creates a main session on startup and returns `MainSessionID` in every `ConnectResponse`. The TUI never opens this as a persistent, always-visible tab. The documented intent (PROJECT_TASKS.md §Milestone 2) is that the main session is always accessible.

### 5. Agent Cancel (Low Priority)
`MethodAgentCancel` is defined in the protocol and dispatched in `handleRequest`, but `handleAgentCancel` is a stub (empty function body with `// TODO: wire cancel map`). There is no way to cancel a running agent turn remotely.

### 6. Channel Plugins (Long-Term)
`internal/channel/channel.go` defines `Channel`, `Manager`, `InboundMessage`, `OutboundMessage` interfaces. The `ChannelManager` is initialized in `Gateway.New()` but no channels are registered, and the inbound handler just logs. OpenClaw's reference implementation has 25+ channels. eclaire's `channel` package is a structural placeholder.

### 7. Background Mode for Agent Runs (Planned)
`AgentRunRequest.Background bool` field exists in protocol.go with a comment: "run without interactive approval; timeouts create notifications". This field is parsed but never used in `handleAgentRun`. Background runs currently behave identically to foreground runs.

### 8. Context Message Embedding in Jobs (Low Priority)
`Job.ContextMessages` field exists, designed to embed recent session history at job creation time. No tool ever sets this field.

### 9. Startup Catchup for Missed Jobs (Low Priority)
If the daemon restarts, recurring jobs that were due during the downtime are not caught up. The job executor starts fresh without running missed intervals.

### 10. Approval Timeout (Low Priority)
Approval requests block indefinitely until the user responds (or gateway shuts down). The THREAT_MODEL.md explicitly notes: "Approval timeout with configurable duration (currently blocks until gateway shutdown)."

### 11. Command-Pattern Approval Storage — RESOLVED
~~Approvals are currently stored as `agentID:toolName` booleans in `PermissionChecker`.~~ Now uses granular command-pattern approvals via `ApprovePattern()` and `ExtractCommandPattern()` with AST-based pattern extraction. `SessionMeta.ApprovalPatterns` is wired: written by runtime on `Persist=true` approvals, loaded in runner.go on session resume.

### 12. Agent Hot-Reload on Filesystem Change (Low Priority)
Agents are reloaded on `gateway.reload` RPC call (and via `eclaire_manage reload`), but not on inotify/fsnotify filesystem events. Adding a YAML agent on disk requires a manual reload.

---

## Comparison: Documented Intent vs. Current File Structure

### What Matches

| Documented Feature | Implementation Status |
|---|---|
| Unix socket daemon with NDJSON wire protocol | ✅ `internal/gateway/` — full implementation |
| 5 built-in agents + disk-loaded YAML agents | ✅ `internal/agent/builtin.go`, `loader.go`, `registry.go` |
| 26 tools with trust tier enforcement | ✅ `internal/tool/` — 30 files |
| Persistent main session | ✅ `sessionStore.GetOrCreateMain()` on startup |
| Project root detection from CWD | ✅ `detectProjectRoot()` in gateway.go, `ConnectWithCWD()` in client.go |
| Unified job system (at/every/cron) | ✅ `internal/agent/jobstore.go` + `jobexec.go` |
| Notification store with bus subscription | ✅ `internal/agent/notifications.go`, subscribed in `Gateway.New()` |
| Approval gate with bus-based blocking | ✅ `internal/agent/approval.go`, `broadcastApprovalRequests()` |
| Permission system (PermissionWriteOnly default) | ✅ Hardcoded in `handleAgentRun` and `handleSessionContinue` |
| Channel manager interface | ✅ `internal/channel/channel.go` — struct and types exist |
| Provider routing (Ollama + OpenRouter) | ✅ `internal/provider/` |
| Memory dreaming (3-phase) | ✅ `internal/agent/dreaming.go` via JobStore |
| Self-improvement tools (`eclaire_manage`) | ✅ 25 operations in `internal/tool/manage.go` |
| System events queue | ✅ `internal/agent/sysevents.go`, wired in gateway.go |
| Auto-compaction | ✅ `internal/agent/loop.go` + runner |

### What Is Documented But Not Yet Implemented

| Documented Feature | Gap |
|---|---|
| Per-connection project workspace loading | `projectDir` set once at daemon CWD, never per-connection |
| TUI notification drain on connect | `MethodNotificationDrain` exists in protocol; TUI never calls it |
| TUI main session tab | `MainSessionID` returned in `ConnectResponse`; TUI ignores it for tab management |
| Session target routing in jobs | `Job.SessionTarget` field exists; executor always uses "isolated" |
| Agent cancel | `handleAgentCancel` is a stub `// TODO` |
| Channel plugin implementations | `channel.Manager` initialized; no channels registered |
| Background mode for runs | `AgentRunRequest.Background` field parsed; never used |
| Command-pattern approvals | RESOLVED — `ApprovePattern()` + `ExtractCommandPattern()` wired, `ApprovalPatterns` persisted in session meta |
| Startup job catchup | Documented as missing in PROJECT_TASKS.md §Milestone 4 |

### What Is In Code But NOT In Docs

| Code Item | Notes |
|---|---|
| `handleCLIApprovals()` TTY detection in CLI | Undocumented but important: CLI detects `term.IsTerminal()` and either prompts inline or defers to notification store |
| Idle timeout auto-shutdown | Gateway shuts down after configurable idle period (defaults 10min) unless agents are running or jobs exist |
| Stale socket cleanup on startup | `os.Remove(socketPath)` before `net.Listen()` |
| `EnsureGateway()` auto-start | Client auto-launches daemon if not running, with PID file staleness detection |
| `reminderAdapter` in gateway | Adapts ReminderStore to JobExecutor's `ReminderFirer` interface; reminder firing integrated into job executor's tick loop |

---

## The Gateway's Role in the Overall Ecosystem

The gateway is the **process boundary** between user intent and AI execution. Everything important happens inside or is owned by the gateway. The TUI and CLI are thin clients that speak NDJSON over a Unix socket.

```
User (human)
  │
  ├── TUI (ecl with no args)
  │     └── Unix socket → Gateway
  │                         ├── agent.run (streaming)
  │                         ├── session.continue
  │                         ├── notification.list / notification.drain
  │                         ├── approval.respond
  │                         └── TypeEvent ← gateway pushes approval requests, stream events
  │
  └── CLI (ecl run / ecl job / ecl notifications)
        └── Unix socket → Gateway
                           └── same RPC methods, no streaming display
                           
Gateway Internal
  ├── Runner (agentic loop)
  │     ├── fantasy LLM abstraction → Ollama / OpenRouter
  │     ├── Tool execution (with permission checking + hooks)
  │     └── Session events (JSONL persistence)
  ├── JobExecutor (background work scheduler)
  │     ├── at / every / cron jobs
  │     └── Reminder firing
  ├── NotificationStore (persistent approval/event queue)
  ├── ApprovalGate (blocking human-in-the-loop)
  ├── SystemEventQueue (background→foreground awareness)
  ├── ChannelManager (plugin substrate, no implementations yet)
  └── MessageBus (~15 topics for internal pub/sub)
```

The key architectural insight is that the gateway is not just a server — it runs **continuously** (via the job executor) even when no client is connected. Background work, scheduled jobs, reminders, and memory dreaming all execute through the gateway's job executor. Results surface as notifications in the notification store, which clients drain on connect. This is the "gateway" model: the daemon is always the authority on state; clients are stateless consumers.

---

## Summary of Critical Gaps (Priority Order)

1. **Per-connection project workspace loading** — Project sessions are created correctly, but the workspace loaded for them is always the daemon's global workspace. Any project-specific SOUL.md, AGENTS.md, etc. are silently ignored. This breaks the documented 4-layer workspace model.

2. **TUI notification drain on connect** — The most user-visible gap. Background work results, approval requests, and reminders accumulate in the notification store but the TUI never surfaces them automatically. Users must know to run `ecl notifications`.

3. **Session target routing in jobs** — Heartbeat and cron jobs always run in isolated sessions. The documented intent is that results route to the main session as system events. The wiring partially exists (SystemEventQueue + bus subscription for `cron`/`heartbeat` sources) but the job executor never invokes the "main session" path.

4. **TUI main session tab** — The gateway creates and tracks the main session. The TUI never uses the `MainSessionID` from `ConnectResponse` to open a persistent tab. Claire's permanent conversation is architecturally present but not exposed in the UI.

5. **Agent cancel** — `handleAgentCancel` is a `// TODO` stub. Long-running agents cannot be cancelled via the protocol.
