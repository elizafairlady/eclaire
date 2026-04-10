# OpenClaw Channels

Complete reference for the channel plugin system and integrations.

**Source files read:** `src/channels/plugins/types.ts`, `src/channels/plugins/types.plugin.ts`, `src/channels/plugins/types.core.ts`, `src/channels/plugins/types.adapters.ts`, `src/channels/allowlists/`, `src/channels/transport/`, `src/channels/web/`

---

## ChannelId Type

`ChannelId` is a string union type representing all supported messaging platforms. Each channel has a unique lowercase identifier used throughout the system.

## Supported Channels (25+)

From the README and source code, OpenClaw supports:

1. **WhatsApp** — via web bridge (`src/web/`)
2. **Telegram** — core channel (`src/telegram/` referenced in CLAUDE.md)
3. **Slack** — core channel
4. **Discord** — core channel
5. **Google Chat** — core channel
6. **Signal** — core channel
7. **iMessage** — legacy core channel
8. **BlueBubbles** — iMessage via BlueBubbles bridge
9. **IRC** — bundled plugin channel
10. **Microsoft Teams** — core channel
11. **Matrix** — bundled plugin channel
12. **Feishu** — bundled plugin channel
13. **LINE** — bundled plugin channel
14. **Mattermost** — bundled plugin channel
15. **Nextcloud Talk** — bundled plugin channel
16. **Nostr** — bundled plugin channel
17. **Synology Chat** — bundled plugin channel
18. **Tlon** — bundled plugin channel
19. **Twitch** — bundled plugin channel
20. **Zalo** — bundled plugin channel
21. **Zalo Personal** — bundled plugin channel (ZaloUser)
22. **WeChat** — bundled plugin channel
23. **WebChat** — web-based chat
24. **macOS** — companion app
25. **iOS/Android** — companion nodes
26. **Voice Call** — bundled plugin channel

## Channel Plugin Contract (`types.plugin.ts`)

### ChannelPlugin

```typescript
type ChannelPlugin<ResolvedAccount = any, Probe = unknown, Audit = unknown> = {
  id: ChannelId;
  meta: ChannelMeta;
  capabilities: ChannelCapabilities;
  defaults?: {
    queue?: { debounceMs?: number };
  };
  reload?: { configPrefixes: string[]; noopPrefixes?: string[] };
  setupWizard?: ChannelSetupWizard | ChannelSetupWizardAdapter;
  configSchema?: ChannelConfigSchema;

  // Adapter slots (all optional except config)
  config: ChannelConfigAdapter<ResolvedAccount>;
  setup?: ChannelSetupAdapter;
  pairing?: ChannelPairingAdapter;
  security?: ChannelSecurityAdapter<ResolvedAccount>;
  groups?: ChannelGroupAdapter;
  lifecycle?: ChannelLifecycleAdapter;
  outbound?: ChannelOutboundAdapter;
  gateway?: ChannelGatewayAdapter;
  status?: ChannelStatusAdapter;
  auth?: ChannelAuthAdapter;
  secrets?: ChannelSecretsAdapter;
  allowlist?: ChannelAllowlistAdapter;
  elevated?: ChannelElevatedAdapter;
  heartbeat?: ChannelHeartbeatAdapter;
  directory?: ChannelDirectoryAdapter;
  command?: ChannelCommandAdapter;
  resolver?: ChannelResolverAdapter;
  approval?: ChannelApprovalAdapter;
  doctor?: ChannelDoctorAdapter;
  conversationBinding?: ChannelConversationBindingSupport;
  configuredBindingProvider?: ChannelConfiguredBindingProvider;
};
```

### ChannelMeta

```typescript
type ChannelMeta = {
  // Channel display name, description, icon
};
```

### ChannelCapabilities

```typescript
type ChannelCapabilities = {
  // Feature flags: threading, media, reactions, polls, etc.
};
```

## Adapter Types (15+)

### ChannelConfigAdapter

Account resolution, configuration validation, and config read/write.

### ChannelSetupAdapter

```typescript
type ChannelSetupAdapter = {
  resolveAccountId?: (params) => string;
  resolveBindingAccountId?: (params) => string | undefined;
  applyAccountName?: (params) => OpenClawConfig;
  applyAccountConfig: (params) => OpenClawConfig;
  afterAccountConfigWritten?: (params) => Promise<void> | void;
  validateInput?: (params) => string | null;
};
```

### ChannelOutboundAdapter / ChannelOutboundContext

Handles outbound message delivery to the channel.

```typescript
type ChannelOutboundContext = {
  // Resolved account, target, threading context for outbound delivery
};
```

### ChannelSecurityAdapter

DM policy enforcement, pairing, allowlists.

```typescript
type ChannelSecurityDmPolicy = "pairing" | "open";
```

### ChannelPairingAdapter

DM pairing code generation and verification for unknown senders.

### ChannelGroupAdapter

Group chat detection, context resolution.

### ChannelLifecycleAdapter

Channel startup, shutdown, reconnection.

### ChannelGatewayAdapter / ChannelGatewayContext

Gateway-level channel integration.

### ChannelStatusAdapter

Channel health checks, connection status.

### ChannelAuthAdapter

Channel authentication and token management.

### ChannelSecretsAdapter

Secret resolution for channel credentials.

### ChannelAllowlistAdapter

Per-channel allowlist management.

### ChannelElevatedAdapter

Elevated permission handling.

### ChannelHeartbeatAdapter / ChannelHeartbeatDeps

Heartbeat delivery support.

### ChannelDirectoryAdapter

```typescript
type ChannelDirectoryEntry = {
  // Contact/conversation directory entry
};
type ChannelDirectoryEntryKind = string;
```

### ChannelCommandAdapter

In-channel command handling.

### ChannelResolverAdapter

```typescript
type ChannelResolveKind = string;
type ChannelResolveResult = {
  // Resolution of channel-specific identifiers
};
```

### ChannelApprovalAdapter

```typescript
type ChannelApprovalCapability = {
  // Approval prompt delivery via channel
};
type ChannelApprovalForwardTarget = {
  channel: string;
  to: string;
  accountId?: string | null;
  threadId?: string | number | null;
  source?: "session" | "target";
};
type ChannelApprovalInitiatingSurfaceState = ChannelActionAvailabilityState;
```

### ChannelConfiguredBindingProvider

Conversation-level binding configuration.

```typescript
type ChannelConfiguredBindingMatch = {
  // Binding match criteria
};
type ChannelConfiguredBindingConversationRef = {
  // Conversation reference for binding
};
type ChannelConversationBindingSupport = {
  // Support for conversation-level bindings
};
```

## Additional Channel Types

### ChannelConfigSchema

```typescript
type ChannelConfigSchema = {
  schema: Record<string, unknown>;               // JSON-schema-like config description
  uiHints?: Record<string, ChannelConfigUiHint>;
  runtime?: ChannelConfigRuntimeSchema;
};

type ChannelConfigUiHint = {
  label?: string;
  help?: string;
  tags?: string[];
  advanced?: boolean;
  sensitive?: boolean;
  placeholder?: string;
  itemTemplate?: unknown;
};
```

### Channel Action Types

```typescript
type ChannelActionAvailabilityState =
  | { kind: "enabled" }
  | { kind: "disabled" }
  | { kind: "unsupported" };

type ChannelCapabilitiesDisplayTone = "default" | "muted" | "success" | "warn" | "error";

type ChannelCapabilitiesDisplayLine = {
  text: string;
  tone?: ChannelCapabilitiesDisplayTone;
};

type ChannelCapabilitiesDiagnostics = {
  lines?: ChannelCapabilitiesDisplayLine[];
  details?: Record<string, unknown>;
};
```

### Message Actions

```typescript
type ChannelMessageActionName = /* string union from message-action-names.ts */;
type ChannelMessageCapability = /* from message-capabilities.ts */;
```

### Outbound Target

```typescript
type ChannelOutboundTargetMode = /* target resolution mode */;
type ChannelOutboundPayloadHint = /* payload format hints */;
type ChannelOutboundTargetRef = /* target reference */;
```

### Messaging & Streaming

```typescript
type ChannelMessagingAdapter = { /* inbound message handling */ };
type ChannelStreamingAdapter = { /* streaming message support */ };
type ChannelThreadingAdapter = { /* thread/topic support */ };
type ChannelMentionAdapter = { /* @mention handling */ };
type ChannelMessageToolDiscovery = { /* per-channel tool discovery */ };
type ChannelMessageToolSchemaContribution = { /* tool schema extensions */ };
```

### Agent Integration

```typescript
type ChannelAgentTool = { /* channel-specific agent tools */ };
type ChannelAgentToolFactory = { /* factory for channel tools */ };
type ChannelAgentPromptAdapter = { /* channel-specific prompt contributions */ };
```

### Polling

```typescript
type ChannelPollContext = { /* poll creation/management context */ };
type ChannelPollResult = { /* poll result data */ };
```

### Account State

```typescript
type ChannelAccountSnapshot = { /* point-in-time account state */ };
type ChannelAccountState = { /* current account connection state */ };
```

## Channel Config Resolution

Channel configuration resolves through multiple layers:
1. Global config at `channels.<channelId>.*`
2. Per-account config at `channels.<channelId>.accounts.<accountId>.*`
3. Plugin defaults from the `ChannelPlugin.defaults` field
4. Dynamic reload via `reload.configPrefixes` (hot-reload on config key changes)

## Allowlists

Per-channel allowlists control which senders can interact with the bot:
- `allowFrom` — Array of allowed sender identifiers
- `"*"` wildcard allows all senders
- DM pairing (`dmPolicy="pairing"`) is the default for unknown senders
