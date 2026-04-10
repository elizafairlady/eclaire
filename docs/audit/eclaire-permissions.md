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

### Approval Dialog (`internal/ui/approval_dialog.go`)
- TUI dialog for showing approval prompts to user
- Exists in code, has UI rendering

### Bus Topics
- `TopicApprovalRequest` and `TopicApprovalResult` defined

## What's Broken

### Permission Mode Hardcoded to Allow

The default `PermissionMode` is `PermissionAllow` which auto-allows everything. There is no configuration option to change this. The Gateway doesn't set it. The TUI doesn't set it. The CLI doesn't set it.

**Result**: The entire permission system — PermissionChecker, ApprovalGate, approval_dialog — is never triggered. The user has never seen an approval prompt.

### Missing Pieces

1. **No config option for permission mode** — Should be in config.yaml, defaulting to a mode that actually prompts
2. **No command-pattern matching** — Currently keyed by `agentID:toolName`, not glob patterns against command strings
3. **No "allow once" vs "allow for session" distinction** — The approved map is just a boolean
4. **No "deny" storage** — Can approve but not persistently deny
5. **Background work policy missing** — Cron jobs should not wait for interactive approval (OpenClaw's hard rule). Must use pre-approved patterns or fail.
6. **Workspace boundaries not enforced** — `CheckWorkspaceBoundary()` exists but agents are never constrained because mode is always Allow
7. **`extendRoots()` never called** — Boundary extension on approval never happens because approval never happens

## What Needs to Happen

1. Change default PermissionMode to something that prompts (e.g., WorkspaceWrite)
2. Add config.yaml option for permission_mode
3. Wire ApprovalGate into the Runner's permission check path
4. Store approval patterns in SessionMeta.ApprovalPatterns
5. Implement command-pattern matching (not just tool names)
6. Add background work policy (pre-approved patterns for cron/heartbeat jobs)
7. Exercise the approval dialog on a real TTY

## Reference

- OpenClaw permissions: `docs/reference/openclaw-permissions.md`
- Claw Code permissions: `docs/reference/clawcode-permissions.md`
- Crush permissions: `docs/reference/crush-services.md` (Permission service section)
