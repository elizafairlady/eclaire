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

## What's Missing

### Main Session (Critical)

**Designed**: One global persistent session. Claire's permanent conversation home. Heartbeats run here. System awareness events accumulate here. Always accessible as a tab in TUI.

**Current**: `persist/session.go` has `GetOrCreateMain(agentID)` with `MainSessionID = "main"`. The function exists. But Gateway never calls it on startup. Sessions are created per-run and there is no persistent global session.

**Impact**:
- Heartbeats run in ephemeral sessions nobody sees
- No persistent conversation home for Claire
- System events have nowhere to accumulate
- Users can't maintain a long-running relationship with Claire across sessions

### Project Root Detection (Critical)

**Designed**: When TUI connects from a project directory (detected via `.eclaire/`, `.git/`), a project session is created or resumed. Project workspace files layer over global workspace.

**Current**: No project root detection exists anywhere. The TUI does not pass CWD to the gateway on connect. The gateway has no mechanism to detect which project the user is working in.

**Impact**:
- Project workspace layer (priority 30) never activates
- Self-modification (eclaire_manage) always writes to `~/.eclaire/`, never to project `.eclaire/`
- No project-scoped agent configurations
- No project-scoped conversation isolation

### Session Approval Patterns (Not Wired)

`SessionMeta.ApprovalPatterns` field exists (map[string][]string for agentID:toolName → glob patterns). But:
- `PermissionChecker` doesn't read from this field
- No code writes to this field
- Approval state is purely in-memory and lost on session close

### Session Fork

`persist/session.go` has session forking concept (ParentID, RootID) but no `ecl session fork` command or tool to trigger it.

## Reference

- OpenClaw sessions: `docs/reference/openclaw-sessions.md`
- Claw Code sessions: `docs/reference/clawcode-session.md`
