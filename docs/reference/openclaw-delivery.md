# OpenClaw Delivery & System Events

Complete reference for outbound message delivery and the system event queue.

**Source files read:** `src/infra/outbound/deliver.ts`, `src/infra/system-events.ts`, `src/infra/outbound/delivery-queue.ts`, `src/infra/outbound/payloads.ts`, `src/infra/outbound/send-deps.ts`, `src/infra/outbound/targets.ts`, `src/infra/outbound/identity.ts`

---

## Outbound Delivery (`outbound/deliver.ts`)

### OutboundDeliveryResult

```typescript
type OutboundDeliveryResult = {
  channel: Exclude<OutboundChannel, "none">;
  messageId: string;
  chatId?: string;
  channelId?: string;
  roomId?: string;
  conversationId?: string;
  timestamp?: number;
  toJid?: string;
  pollId?: string;
  meta?: Record<string, unknown>;    // Channel-specific fields
};
```

### Channel Handler Contract

Each channel provides a handler implementing:

```typescript
type ChannelHandler = {
  chunker: Chunker | null;                    // Text chunking function
  chunkerMode?: "text" | "markdown";         // Chunking strategy
  textChunkLimit?: number;                    // Max chars per chunk
  supportsMedia: boolean;

  // Required
  sendText: (text, overrides?) => Promise<OutboundDeliveryResult>;
  sendMedia: (caption, mediaUrl, overrides?) => Promise<OutboundDeliveryResult>;

  // Optional enhanced capabilities
  sanitizeText?: (payload: ReplyPayload) => string;
  normalizePayload?: (payload: ReplyPayload) => ReplyPayload | null;
  shouldSkipPlainTextSanitization?: (payload: ReplyPayload) => boolean;
  resolveEffectiveTextChunkLimit?: (fallbackLimit?) => number | undefined;
  sendPayload?: (payload, overrides?) => Promise<OutboundDeliveryResult>;
  sendFormattedText?: (text, overrides?) => Promise<OutboundDeliveryResult[]>;
  sendFormattedMedia?: (caption, mediaUrl, overrides?) => Promise<OutboundDeliveryResult>;
};

type Chunker = (text: string, limit: number) => string[];
```

Override options for all send methods:

```typescript
overrides?: {
  replyToId?: string | null;
  threadId?: string | number | null;
  audioAsVoice?: boolean;
}
```

### Channel Handler Params

```typescript
type ChannelHandlerParams = {
  cfg: OpenClawConfig;
  channel: Exclude<OutboundChannel, "none">;
  to: string;
  accountId?: string;
  replyToId?: string | null;
  threadId?: string | number | null;
  identity?: OutboundIdentity;
  deps?: OutboundSendDeps;
  gifPlayback?: boolean;
  forceDocument?: boolean;
  silent?: boolean;
  mediaAccess?: OutboundMediaAccess;
  gatewayClientScopes?: readonly string[];
};
```

### Delivery Pipeline

1. **Payload normalization** — `normalizeReplyPayloadsForDelivery()` converts reply payloads into `NormalizedOutboundPayload` entries
2. **Channel handler resolution** — `loadChannelOutboundAdapter()` loads the appropriate channel plugin's outbound adapter
3. **Chunking** — Long messages split by paragraph (`chunkByParagraph`) or markdown-aware chunking (`chunkMarkdownTextWithMode`), respecting channel-specific limits
4. **Media handling** — Media sent with leading caption via `sendMediaWithLeadingCaption()`; channel-level media support checked via `supportsMedia`
5. **Delivery queue** — Messages enqueued via `enqueueDelivery()`, acknowledged via `ackDelivery()`, failed via `failDelivery()`
6. **Hooks** — Pre/post delivery hooks fired via `fireAndForgetHook()` and `triggerInternalHook()`:
   - `buildCanonicalSentMessageHookContext()` — Canonical context for sent-message hooks
   - `toInternalMessageSentContext()` — Internal hook event
   - `toPluginMessageContext()` / `toPluginMessageSentEvent()` — Plugin hook events
7. **Transcript mirroring** — When configured, `resolveMirroredTranscriptText()` creates transcript entries
8. **Channel bootstrap** — Dynamic channel handler loading via `loadChannelBootstrapRuntime()`

### Chunk Modes

```typescript
function resolveChunkMode(channel): "text" | "markdown"
function resolveTextChunkLimit(channel, handler): number
```

Each channel has a default chunk mode and limit. Markdown-capable channels use markdown-aware chunking that preserves code blocks and headings.

### Sendable Reply Parts

`resolveSendableOutboundReplyParts()` from `plugin-sdk/reply-payload` splits complex reply payloads into individually sendable parts (text, media, polls, etc.).

## System Events (`system-events.ts`)

Lightweight in-memory queue for human-readable system events prefixed to the next prompt. Intentionally non-persistent (ephemeral). Session-scoped.

### SystemEvent

```typescript
type SystemEvent = {
  text: string;
  ts: number;
  contextKey?: string | null;
  deliveryContext?: DeliveryContext;
  trusted?: boolean;         // Default: true
};
```

### Queue Implementation

- Global map of `SessionQueue` keyed by session key
- `MAX_EVENTS = 20` per session
- Consecutive duplicate text suppression via `lastText` tracking
- Context key tracking via `lastContextKey` for change detection

```typescript
type SessionQueue = {
  queue: SystemEvent[];
  lastText: string | null;
  lastContextKey: string | null;
};
```

### Functions

#### `enqueueSystemEvent(text, options) -> boolean`

Adds event to session queue. Requires `sessionKey`. Trims text, skips empty. Skips consecutive duplicates. Drops oldest when queue exceeds 20. Returns `true` if event was enqueued.

```typescript
type SystemEventOptions = {
  sessionKey: string;
  contextKey?: string | null;
  deliveryContext?: DeliveryContext;
  trusted?: boolean;
};
```

#### `drainSystemEventEntries(sessionKey) -> SystemEvent[]`

Returns and removes all events for the session. Clears `lastText`, `lastContextKey`, and removes the session queue entirely. This is the primary consumer path -- events are drained into the next prompt.

#### `drainSystemEvents(sessionKey) -> string[]`

Convenience wrapper returning only the text strings.

#### `peekSystemEventEntries(sessionKey) -> SystemEvent[]`

Non-destructive read. Returns cloned events.

#### `peekSystemEvents(sessionKey) -> string[]`

Non-destructive text-only read.

#### `hasSystemEvents(sessionKey) -> boolean`

Returns `true` if any events are queued.

#### `isSystemEventContextChanged(sessionKey, contextKey?) -> boolean`

Returns `true` if the given context key differs from the last enqueued event's context key. Used to detect when the context has shifted (e.g., different cron job firing).

#### `resolveSystemEventDeliveryContext(events) -> DeliveryContext | undefined`

Merges delivery contexts from multiple events into a single context. Used when draining events to determine the aggregate delivery target.

### Usage Pattern

1. Background systems (cron, heartbeat, hooks) call `enqueueSystemEvent()` on the target session
2. On the next agent turn, events are drained via `drainSystemEvents()` and injected as context
3. Events are ephemeral -- lost on restart if not yet drained
4. Duplicate text is suppressed to avoid flooding the prompt with repeated notifications

## DeliveryContext

```typescript
type DeliveryContext = {
  channel?: string;
  to?: string;
  accountId?: string;
  threadId?: string | number;
  groupId?: string;
  groupChannel?: string;
  space?: string;
};
```

Normalized and merged via `normalizeDeliveryContext()` and `mergeDeliveryContext()`.

## Outbound Targets

```typescript
type OutboundChannel = ChannelId | "none";
```

`"none"` indicates no delivery target (event stays in-memory only).

## Outbound Identity

```typescript
type OutboundIdentity = {
  // Channel-specific sender identity for outbound messages
};
```

## Outbound Session Context

```typescript
type OutboundSessionContext = {
  // Session-level context for delivery decisions
};
```
