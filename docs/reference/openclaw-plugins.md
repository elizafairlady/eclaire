# OpenClaw Plugin System

Complete reference for the plugin architecture, SDK, and runtime.

**Source files read:** `src/plugins/types.ts`, `src/plugins/cli.ts`, `src/plugins/commands.ts`, `src/plugins/bundled-dir.ts`, `src/plugins/bundled-sources.ts`, `src/plugins/bundled-plugin-metadata.ts`, `src/plugins/config-state.ts`, `src/plugins/config-policy.ts`, `src/plugins/compaction-provider.ts`, `src/plugins/capability-provider-runtime.ts`, `src/plugins/channel-plugin-ids.ts`, `src/plugins/contracts/registry.ts`, `src/plugin-sdk/core.ts`, `src/plugin-sdk/provider-entry.ts`, `src/plugin-sdk/channel-contract.ts`, `src/plugin-sdk/routing.ts`, `src/plugin-sdk/sandbox.ts`, `src/plugin-sdk/runtime.ts`

---

## Plugin Architecture

OpenClaw uses a plugin-first architecture where core stays lean and optional capability ships as plugins. Plugins are distributed as npm packages and loaded at runtime.

### Key Principles

- Core must stay extension-agnostic
- Extensions cross into core only through `openclaw/plugin-sdk/*`
- No hardcoded extension/provider/channel ID lists in core
- Plugin-specific behavior belongs to the owning extension
- Backwards-compatible, versioned contracts
- Extension test coverage belongs in the owning extension package

## Plugin Types (`types.ts`)

### PluginLogger

```typescript
type PluginLogger = {
  debug?: (message: string) => void;
  info: (message: string) => void;
  warn: (message: string) => void;
  error?: (message: string) => void;
};
```

### ProviderAuthOptionBag

```typescript
type ProviderAuthOptionBag = {
  token?: string;
  tokenProvider?: string;
  secretInputMode?: SecretInputMode;
  [key: string]: unknown;
};
```

### Key Plugin Interfaces

The plugin types file imports and re-exports types from many subsystems, indicating the breadth of plugin integration points:

- **Model/Provider:** `Api`, `Model`, `ModelRegistry`, `ModelCatalogEntry`, `FailoverReason`, `ProviderRequestTransportOverrides`
- **Agent:** `PromptMode`, `ToolFsPolicy`, `AnyAgentTool`, `ThinkLevel`, `ProviderSystemPromptContribution`
- **Channels:** `ChannelId`, `ChannelPlugin`
- **Auth:** `ApiKeyCredential`, `AuthProfileCredential`, `OAuthCredential`, `AuthProfileStore`
- **Reply:** `ReplyDispatchKind`, `ReplyDispatcher`, `ReplyPayload`
- **Config:** `OpenClawConfig`, `CliBackendConfig`, `ModelProviderAuthMode`, `ModelProviderConfig`
- **Gateway:** `OperatorScope`, `GatewayRequestHandler`
- **Hooks:** `InternalHookHandler`, `HookEntry`
- **Media:** `ImageGenerationProvider`, `VideoGenerationProvider`, `MusicGenerationProvider`, `MediaUnderstandingProvider`
- **TTS:** Speech provider types (synthesis, voice listing, telephony)
- **Realtime:** Transcription and voice bridge provider types
- **Security:** `SecurityAuditFinding`
- **Web:** `RuntimeWebFetchMetadata`, `RuntimeWebSearchMetadata`
- **Wizard:** `WizardPrompter`
- **CLI:** `Command` (Commander.js)

## Plugin SDK (`plugin-sdk/`)

The public SDK contains 100+ modules organized by capability area:

### SDK Subpath Exports

From `package.json`:
- `openclaw/plugin-sdk` — Main entry
- `openclaw/plugin-sdk/core` — Core types
- `openclaw/plugin-sdk/provider-setup` — Provider setup helpers
- `openclaw/plugin-sdk/sandbox` — Sandbox integration
- `openclaw/plugin-sdk/self-hosted-provider-setup` — Self-hosted provider setup
- `openclaw/plugin-sdk/routing` — Session routing
- `openclaw/plugin-sdk/runtime` — Runtime helpers
- `openclaw/plugin-sdk/runtime-doctor` — Doctor integration
- `openclaw/plugin-sdk/runtime-env` — Runtime environment

Plus 40+ additional subpaths for specific capabilities.

### SDK Module Categories

#### Channel Integration
- `channel-contract.ts` — Channel plugin contract
- `channel-core.ts` — Core channel types
- `channel-entry-contract.ts` — Channel entry validation
- `channel-config-helpers.ts` — Config resolution helpers
- `channel-config-schema.ts` — Config schema generation
- `channel-lifecycle.ts` — Lifecycle management
- `channel-pairing.ts` — DM pairing
- `channel-policy.ts` — Security policy
- `channel-reply-pipeline.ts` — Reply pipeline
- `channel-runtime.ts` — Runtime context
- `channel-runtime-context.ts` — Context resolution
- `channel-send-result.ts` — Send result types
- `channel-setup.ts` — Setup helpers
- `channel-status.ts` — Status reporting
- `channel-streaming.ts` — Streaming support
- `channel-targets.ts` — Target resolution
- `channel-plugin-common.ts` — Shared utilities

#### Provider Integration
- `provider-entry.ts` — Provider plugin entry
- `provider-auth.ts` — Auth management
- `provider-catalog-shared.ts` — Model catalog
- `provider-model-shared.ts` — Model types
- `provider-setup.ts` — Provider setup

#### Approval System
- `approval-runtime.ts` — Approval runtime
- `approval-handler-runtime.ts` — Handler runtime
- `approval-handler-adapter-runtime.ts` — Adapter runtime
- `approval-gateway-runtime.ts` — Gateway integration
- `approval-delivery-helpers.ts` — Delivery helpers
- `approval-delivery-runtime.ts` — Delivery runtime
- `approval-client-helpers.ts` — Client helpers
- `approval-client-runtime.ts` — Client runtime
- `approval-auth-helpers.ts` — Auth helpers
- `approval-auth-runtime.ts` — Auth runtime
- `approval-native-helpers.ts` — Native helpers
- `approval-native-runtime.ts` — Native runtime
- `approval-reply-runtime.ts` — Reply runtime
- `approval-approvers.ts` — Approver registry
- `approval-renderers.ts` — Approval UI renderers

#### Browser
- `browser-bridge.ts` — Browser bridge
- `browser-cdp.ts` — Chrome DevTools Protocol
- `browser-config.ts` — Browser configuration
- `browser-config-runtime.ts` — Runtime config
- `browser-host-inspection.ts` — Host inspection
- `browser-maintenance.ts` — Browser maintenance
- `browser-node-host.ts` — Node-based browser
- `browser-node-runtime.ts` — Node runtime
- `browser-profiles.ts` — Profile management
- `browser-security-runtime.ts` — Security
- `browser-setup-tools.ts` — Setup tools

#### Account & Config
- `account-core.ts` — Account management
- `account-helpers.ts` — Account helpers
- `account-id.ts` — Account ID resolution
- `account-resolution.ts` — Resolution
- `agent-config-primitives.ts` — Config primitives
- `agent-runtime.ts` — Agent runtime

#### Routing & Sessions
- `routing.ts` — Session routing
- `allow-from.ts` — Allowlist management
- `allowlist-config-edit.ts` — Allowlist editing

#### ACP (Agent Communication Protocol)
- `acp-runtime.ts` — ACP runtime
- `acp-binding-runtime.ts` — ACP binding
- `acpx.ts` — ACPX extension

## Plugin Discovery & Loading (`plugins/`)

### Bundled Plugin Discovery

- `bundled-dir.ts` — Resolves bundled plugin directory paths
- `bundled-sources.ts` — Discovers bundled plugin sources
- `bundled-plugin-metadata.ts` — Reads plugin manifests
- `bundled-plugin-scan.ts` — Scans for available plugins
- `bundled-compat.ts` — Backward compatibility layer

### Plugin Manifest

- `bundle-manifest.ts` — Manifest parsing and validation
- Plugin manifests declare: id, capabilities, channels, providers, hooks, tools
- Validated via contract tests in `plugins/contracts/`

### Plugin Config

- `config-state.ts` — Plugin configuration state tracking
- `config-policy.ts` — Plugin enablement/disablement policies
- `config-contracts.ts` — Config contract enforcement
- `config-schema.ts` — Config schema generation
- `config-normalization-shared.ts` — Shared normalization

### Plugin Registration

- `captured-registration.ts` — Registration capture for testing
- `command-registration.ts` — Command registration
- `command-registry-state.ts` — Command registry state

### Plugin Runtime

- `plugins/runtime/types.ts` — `PluginRuntime` type
- Provides runtime context for plugins including config, logger, and capability access

### Plugin Capabilities

- `capability-provider-runtime.ts` — Provider capability runtime
- `bundled-capability-runtime.ts` — Bundled capability runtime
- `bundled-capability-metadata.ts` — Capability metadata (contract-tested)

### Channel Plugin Registry

- `channel-catalog-registry.ts` — Channel catalog registration
- `channel-plugin-ids.ts` — Channel plugin ID management
- `bundled-channel-runtime.ts` — Bundled channel runtime
- `bundled-channel-config-metadata.ts` — Channel config metadata

### Compaction Provider

- `compaction-provider.ts` — Plugin-provided compaction summarization

### CLI Backend Registration

- `cli-backends.runtime.ts` — CLI backend registration
- `cli-registry-loader.ts` — Registry loading

### MCP Integration

- `bundle-mcp.ts` — MCP (Model Context Protocol) bridge via mcporter

### LSP Integration

- `bundle-lsp.ts` — Language Server Protocol integration

### ClawHub

- `clawhub.ts` — ClawHub skill/plugin marketplace integration

## Plugin Contract Tests (`plugins/contracts/`)

Extensive contract tests verify plugin compliance:

### Registry Contracts
- `registry.ts` — Plugin registration contracts
- `loader.contract.test.ts` — Loader contract tests

### Provider Contracts (per-provider)
Contract tests exist for every bundled provider:
- Anthropic, OpenAI, Google, Groq, Microsoft, Mistral, xAI, Perplexity, Together, Moonshot, MiniMax, Replicate, Zhipu, Requesty, Chutes, Deepinfra, Sambanova, Qwen, Siliconflow, Volcengine, Fal, ComfyUI, ElevenLabs, Deepgram, Rime, Kokoro, Speechify, Whisper, Brave, DuckDuckGo, Exa, Firecrawl, Google Search, Tavily, SearXNG, and more.

### Boundary Contracts
- `boundary-invariants.test.ts` — Architecture boundary enforcement
- `extension-package-project-boundaries.test.ts` — Package boundary validation
- `config-footprint-guardrails.test.ts` — Config footprint limits
- `plugin-entry-guardrails.test.ts` — Plugin entry point validation

### Web Search Contracts
- Per-provider web search contract tests (Brave, DuckDuckGo, Exa, Firecrawl, Google, MiniMax, Moonshot, Perplexity, SearXNG, Tavily, xAI)

## Plugin API

### Public Artifacts

- `public-artifacts.ts` — Public API surface tracking
- `api-builder.ts` — API builder utilities

### Build Smoke

- `build-smoke-entry.ts` — Build smoke test entry point

## Cache Controls

- `cache-controls.ts` — Plugin-level cache control headers and policies

## Config Activation

- `config-activation-shared.ts` — Shared activation logic for plugin enablement based on config state
