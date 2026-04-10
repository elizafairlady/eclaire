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

## What's Broken

### Never Subscribed

`NotificationStore.SubscribeToBus()` is **never called** by the Gateway. The store is created but sits empty. No cron completions, heartbeat results, job executions, or system events ever create notifications.

### No TUI Integration

The TUI never calls `Drain()` on connect. There is no notification indicator in the TUI. There is no notification panel. Background work results are invisible to the user.

### No Channel Delivery

The channel plugin interface exists (`internal/channel/channel.go`) but no channels are implemented. When channels exist (Signal, Telegram, etc.), notifications should push to them too.

## What Needs to Happen

1. Call `NotificationStore.SubscribeToBus(ctx, bus)` in Gateway startup
2. JobExecutor publishes `BackgroundResult` to bus after every job completion
3. TUI drains notifications on connect — show count indicator + summary
4. Add notification panel or inline display in TUI
5. Eventually: channel delivery for push notifications

## Reference

- OpenClaw delivery: `docs/reference/openclaw-delivery.md`
- OpenClaw system events: `docs/reference/openclaw-delivery.md` (system events section)
