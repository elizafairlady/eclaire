# eclaire Permissions Audit

Audited: 2026-04-09

## What Exists (not validated by user)

### PermissionChecker (`internal/tool/permission.go`)
- Trust tiers: ReadOnly, Modify, Dangerous
- Per-session approved map (keyed by `agentID:toolName`)
- `CheckWorkspaceBoundary()` with path prefix matching
- `extendRoots()` to dynamically extend workspace boundaries on approval

### ApprovalGate (`internal/agent/approval.go`)
- UUID-based request IDs
- Bus-based blocking: publishes approval request, waits on channel for response
- Pending approval tracking with cleanup

### Approval Dialog (integrated into `internal/ui/app.go`)
- TUI approval prompts integrated into main app model (approval_dialog.go removed)
- Renders inline when connected, via TypeEvent broadcasts

### Bus Topics
- `TopicApprovalRequest` and `TopicApprovalResult` defined

## What Works (updated 2026-04-10)

### Permission Mode is PermissionWriteOnly

`PermissionWriteOnly` is the default for ALL runs — gateway.go sets it at lines 1145 and 1331 for both `handleAgentRun` and `handleSessionContinue`. Also set in scheduler.go (lines 240, 541) and jobexec.go (line 304). ReadOnly and Modify tools auto-allowed; Dangerous tools prompt.

**Result**: The permission system IS triggered. ApprovalGate blocks on dangerous tool calls. Gateway broadcasts approval requests as `TypeEvent` with `event_type: "approval_request"`. TUI has approval dialog wired. CLI detects TTY and prompts inline or falls back to `ecl notifications <id> yes/no`.

### Still Missing

1. **No config option for permission mode** — Hardcoded to PermissionWriteOnly, not configurable in config.yaml
2. ~~**No command-pattern matching**~~ — RESOLVED: `ApprovePattern()` + `ExtractCommandPattern()` with AST-based pattern extraction (binary/subcommand level)
3. ~~**No "allow once" vs "allow for session" distinction**~~ — RESOLVED: `ApprovalResult.Persist` distinguishes once vs always-for-session
4. ~~**No "deny" storage**~~ — RESOLVED: Deny rejects, no pattern stored (clean behavior)
5. ~~**SessionMeta.ApprovalPatterns not wired**~~ — RESOLVED: Written on Persist=true, loaded in runner.go on session resume
6. **Background work pre-approval** — Background jobs use PermissionWriteOnly and block on dangerous tools, but no pre-approval configurable per job
7. **Approval dialog untested on real TTY**

## What Needs to Happen

1. Add config.yaml option for permission_mode
2. Add background work pre-approval per job or per agent
3. Exercise the approval dialog on a real TTY

## Reference

- OpenClaw permissions: `docs/reference/openclaw-permissions.md`
- Claw Code permissions: `docs/reference/clawcode-permissions.md`
- Crush permissions: `docs/reference/crush-services.md` (Permission service section)
