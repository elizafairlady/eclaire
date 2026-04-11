# eclaire Sessions Audit

Audited: 2026-04-09

## What Exists (not validated by user)

### Session Persistence
- **File**: `internal/persist/session.go`
- Directory-per-session: `~/.eclaire/sessions/<id>/meta.json` + `events.jsonl`
- JSONL event log with types: user_message, assistant_message, tool_call, tool_result, system_message, compaction
- Event sequencing with Seq field
- Atomic session creation with UUID
- Parent/child session linking (ParentID, RootID in SessionMeta)
- Session resume: `ecl run --continue` or `ecl run -c <id>`
- Session naming: `ecl run -n "name"`

### SessionMeta Fields
- ID, ParentID, RootID, AgentID, Title, Status
- CreatedAt, UpdatedAt, MessageCount
- InputTokens, OutputTokens
- ProjectRoot (exists but never set)
- ApprovalPatterns (exists but never used)

## What Works (updated 2026-04-10)

### Main Session

`GetOrCreateMain("orchestrator")` IS called on gateway startup. Main session persists across restarts (loaded from disk). `SystemEventQueue` drains background work awareness into main session prompt. Session lifecycle works: status transitions (active/completed/error), stale session reaping via `reaper.go`.

**Still missing**: Main session not shown as permanent TUI tab. Heartbeats don't run in main session. `ecl run` with no project context doesn't connect to main session.

### Project Root Detection

`detectProjectRoot()` in gateway.go walks up from CWD looking for `.eclaire/` or `.git/`. CLI sends CWD on connect via `ConnectWithCWD()`. Gateway creates project sessions via `handleConnect()`.

**Still broken**: Workspace loader's `projectDir` is set once at daemon startup (based on daemon CWD), not updated per-client-connection. A client connecting from a different project directory won't get that project's workspace files. Self-modification still always writes to `~/.eclaire/`.

### Session Approval Patterns (Not Wired)

`SessionMeta.ApprovalPatterns` field exists but:
- `PermissionChecker` doesn't read from this field
- No code writes to this field
- Approval state is purely in-memory and lost on session close

### Session Fork

`persist/session.go` has session forking concept (ParentID, RootID) but no `ecl session fork` command or tool to trigger it.

## Reference

- OpenClaw sessions: `docs/reference/openclaw-sessions.md`
- Claw Code sessions: `docs/reference/clawcode-session.md`
