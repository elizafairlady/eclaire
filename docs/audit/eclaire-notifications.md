# eclaire Notifications Audit

Audited: 2026-04-09

## What Exists (not validated by user)

### NotificationStore (`internal/agent/notifications.go`, ~262 lines)

**Persistence**: JSONL file at `~/.eclaire/notifications.jsonl`

**Notification struct**:
- ID (UUID), Severity (debug/info/warning/error)
- Source (heartbeat/cron/agent/system), SessionID, AgentID
- Title, Content, Read (bool)
- CreatedAt

**Operations**:
- `Add()` — Creates notification, appends to JSONL, adds to in-memory cache
- `List()` — All notifications (optional severity/source filter)
- `Pending()` — Unread notifications
- `Drain()` — Read all unread, mark as read, return them
- `MarkRead(id)` — Mark single as read
- `MarkAllRead()` — Mark all as read

**In-memory cache**: Bounded at 1000 entries, loaded from disk on startup.

### Bus Integration (Designed)

`SubscribeToBus()` method exists — subscribes to `TopicBackgroundResult`, creates Notification from each event.

### CLI

`ecl notifications` command exists in `internal/cli/notification.go`.

## What Works (updated 2026-04-10)

### Bus Subscription Active

`NotificationStore.SubscribeToBus()` IS called in Gateway startup. Job completions from JobExecutor create notifications. Approval requests create notifications with RefID linking to ApprovalGate pending map.

### CLI Integration Active

`ecl notifications` lists pending notifications. `ecl notifications <id> yes/no/always` resolves pending approvals. Mark as read/dismiss works.

## What's Still Missing

### No TUI Integration

The TUI never calls `Drain()` on connect. There is no notification indicator in the TUI. There is no notification panel. Background work results are invisible to TUI users (CLI users can see them).

### No Channel Delivery

The channel plugin interface exists (`internal/channel/channel.go`) but no channels are implemented.

## What Needs to Happen

1. TUI drains notifications on connect — show count indicator + summary
2. Add notification panel or inline display in TUI
3. Filter by severity, source, time range in CLI
4. Eventually: channel delivery for push notifications

## Reference

- OpenClaw delivery: `docs/reference/openclaw-delivery.md`
- OpenClaw system events: `docs/reference/openclaw-delivery.md` (system events section)
