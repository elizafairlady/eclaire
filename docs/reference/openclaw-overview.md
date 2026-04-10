# OpenClaw Overview

Reference documentation for the OpenClaw project structure, architecture, and subsystems.

**Source files read:** `package.json`, `README.md`, `VISION.md`, `CLAUDE.md`, `find /tmp/openclaw/src -type d -maxdepth 2`

---

## Project Identity

- **Name:** OpenClaw
- **Version:** 2026.4.9 (date-based versioning)
- **License:** MIT
- **Repository:** `github.com/openclaw/openclaw`
- **Binary:** `openclaw` (via `openclaw.mjs`)
- **Package type:** ESM (`"type": "module"`)
- **Runtime:** Node 24 (recommended) or Node 22.16+
- **Language:** TypeScript (strict typing, no `any`)
- **Build:** pnpm + tsdown, Bun optional for TypeScript execution
- **Linting:** Oxlint + Oxfmt
- **Testing:** Vitest with V8 coverage (70% thresholds)
- **Total .ts files:** ~6200

## What OpenClaw Is

OpenClaw is a **personal AI assistant** you run on your own devices. It is a local-first gateway with a control plane for sessions, channels, tools, and events. The product is the assistant, not the gateway. It connects to 25+ messaging platforms (WhatsApp, Telegram, Slack, Discord, Google Chat, Signal, iMessage, BlueBubbles, IRC, Microsoft Teams, Matrix, Feishu, LINE, Mattermost, Nextcloud Talk, Nostr, Synology Chat, Tlon, Twitch, Zalo, Zalo Personal, WeChat, WebChat) and supports voice wake, talk mode, live canvas, and companion apps (macOS, iOS, Android).

## Architecture

Gateway daemon on a port (default 18789) with WebSocket control plane. Sessions, presence, config, cron, webhooks, Control UI, and Canvas host. Pi agent runtime in RPC mode with tool streaming and block streaming. Multi-agent routing: inbound channels/accounts/peers route to isolated agents.

## Published Plugin SDK Subpaths

```
openclaw/plugin-sdk          — main entry
openclaw/plugin-sdk/core     — core types
openclaw/plugin-sdk/provider-setup
openclaw/plugin-sdk/sandbox
openclaw/plugin-sdk/self-hosted-provider-setup
openclaw/plugin-sdk/routing
openclaw/plugin-sdk/runtime
openclaw/plugin-sdk/runtime-doctor
openclaw/plugin-sdk/runtime-env
```

Plus 40+ additional SDK subpaths visible in `package.json` exports.

## Source Subsystems (src/)

### Core Infrastructure

| Subsystem | Path | Description |
|-----------|------|-------------|
| **gateway** | `src/gateway/` | WebSocket gateway server, NDJSON protocol, server methods, bridge protocol |
| **gateway/protocol** | `src/gateway/protocol/` | Typed control-plane wire protocol schema |
| **gateway/server** | `src/gateway/server/` | Server implementation |
| **gateway/server-methods** | `src/gateway/server-methods/` | RPC method handlers |
| **daemon** | `src/daemon/` | Daemon lifecycle, test helpers |
| **config** | `src/config/` | YAML config loading, session config, types, merging |
| **config/sessions** | `src/config/sessions/` | Session store, metadata, reset policies, maintenance, targets, transcripts |
| **infra** | `src/infra/` | Infrastructure: exec approvals, outbound delivery, system events, TLS, network, time formatting |
| **infra/outbound** | `src/infra/outbound/` | Outbound message delivery pipeline |
| **infra/format-time** | `src/infra/format-time/` | Time formatting utilities |
| **infra/net** | `src/infra/net/` | Network utilities |
| **infra/tls** | `src/infra/tls/` | TLS configuration |
| **logging** | `src/logging/` | Subsystem-scoped logging, test helpers |
| **shared** | `src/shared/` | String coercion, global singletons, record coercion, network utils |
| **utils** | `src/utils/` | General utilities |
| **types** | `src/types/` | Shared type definitions |
| **security** | `src/security/` | Security audits, external content policies |
| **secrets** | `src/secrets/` | Secret resolution, target registry, runtime shared |
| **process** | `src/process/` | Command queue, lane execution, supervisor |
| **process/supervisor** | `src/process/supervisor/` | Process supervisor |

### Agent Runtime

| Subsystem | Path | Description |
|-----------|------|-------------|
| **agents** | `src/agents/` | Agent scope, auth profiles, bash tools, exec runtime, model auth, system prompt, workspace, apply-patch, subagent spawn, failover, usage |
| **agents/auth-profiles** | `src/agents/auth-profiles/` | OAuth, API key rotation, credential state, identity, profiles, policy, store |
| **agents/cli-runner** | `src/agents/cli-runner/` | CLI backend execution |
| **agents/command** | `src/agents/command/` | Agent command handling, session resolution |
| **agents/pi-embedded-helpers** | `src/agents/pi-embedded-helpers/` | Pi embedded agent helpers |
| **agents/pi-embedded-runner** | `src/agents/pi-embedded-runner/` | Embedded Pi agent runner: run loop, compaction, model selection, failover, payloads, lanes |
| **agents/pi-hooks** | `src/agents/pi-hooks/` | Pi agent hooks |
| **agents/sandbox** | `src/agents/sandbox/` | Agent sandbox (Docker) |
| **agents/schema** | `src/agents/schema/` | Agent schema definitions |
| **agents/skills** | `src/agents/skills/` | Skill discovery (7 sources), filtering, serialization, plugin skills, frontmatter, workspace skills |
| **agents/tools** | `src/agents/tools/` | Agent tool definitions |
| **agents/test-helpers** | `src/agents/test-helpers/` | Agent test utilities |

### Scheduling & Jobs

| Subsystem | Path | Description |
|-----------|------|-------------|
| **cron** | `src/cron/` | Scheduling types, schedule computation, normalization, run log, delivery plan, heartbeat policy, parsing, active jobs, session target |
| **cron/service** | `src/cron/service/` | Cron service: timer loop, operations, job CRUD, state, store, lock, timeout policy, normalization |
| **cron/isolated-agent** | `src/cron/isolated-agent/` | Isolated agent job execution: delivery dispatch, run execution, session management, model selection, skills snapshot, subagent followup |

### Channels & Messaging

| Subsystem | Path | Description |
|-----------|------|-------------|
| **channels** | `src/channels/` | Channel plugin types, allowlists, transport, web |
| **channels/plugins** | `src/channels/plugins/` | Channel plugin contract: types (core, plugin, adapters), outbound, bootstrap registry |
| **channels/allowlists** | `src/channels/allowlists/` | Per-channel allowlist management |
| **channels/transport** | `src/channels/transport/` | Transport layer |
| **channels/web** | `src/channels/web/` | Web channel (WhatsApp web) |
| **routing** | `src/routing/` | Session key derivation, agent ID normalization, multi-agent routing |
| **sessions** | `src/sessions/` | Session ID resolution, chat types, send policy, lifecycle events, transcript events, model overrides, labels |

### Plugins & SDK

| Subsystem | Path | Description |
|-----------|------|-------------|
| **plugins** | `src/plugins/` | Plugin loader, manifest validation, registry, contracts, config state, capabilities, bundled sources, channel catalog, CLI backends, compaction providers, commands |
| **plugins/contracts** | `src/plugins/contracts/` | Contract tests for all bundled plugins |
| **plugins/runtime** | `src/plugins/runtime/` | Plugin runtime types and execution |
| **plugins/test-helpers** | `src/plugins/test-helpers/` | Plugin test utilities |
| **plugin-sdk** | `src/plugin-sdk/` | Public plugin SDK: 100+ modules covering channels, providers, approvals, browser, routing, sandbox, runtime |

### Hooks

| Subsystem | Path | Description |
|-----------|------|-------------|
| **hooks** | `src/hooks/` | Hook loader, internal hooks, workspace hooks, policy, fire-and-forget, Gmail integration, bundled hooks |
| **hooks/bundled** | `src/hooks/bundled/` | Bundled hooks: boot-md, bootstrap-extra-files, command-logger, session-memory |

### Memory & AI

| Subsystem | Path | Description |
|-----------|------|-------------|
| **memory-host-sdk** | `src/memory-host-sdk/` | Memory dreaming (3-phase: light/deep/REM), config resolution, workspace |
| **memory-host-sdk/host** | `src/memory-host-sdk/host/` | Memory host implementation |
| **context-engine** | `src/context-engine/` | Context engine: system prompt assembly |

### Media & Generation

| Subsystem | Path | Description |
|-----------|------|-------------|
| **media** | `src/media/` | Media pipeline: images/audio/video, size caps, load options |
| **media-understanding** | `src/media-understanding/` | Media understanding providers |
| **media-generation** | `src/media-generation/` | Shared media generation |
| **image-generation** | `src/image-generation/` | Image generation providers |
| **video-generation** | `src/video-generation/` | Video generation providers |
| **music-generation** | `src/music-generation/` | Music generation providers |
| **tts** | `src/tts/` | Text-to-speech providers |
| **realtime-voice** | `src/realtime-voice/` | Realtime voice bridging |
| **realtime-transcription** | `src/realtime-transcription/` | Realtime transcription |

### User-Facing

| Subsystem | Path | Description |
|-----------|------|-------------|
| **cli** | `src/cli/` | Cobra-style subcommands: cron-cli, daemon-cli, gateway-cli, node-cli, nodes-cli, program, send-runtime, shared, update-cli |
| **commands** | `src/commands/` | High-level commands: agent, channel-setup, channels, doctor, gateway-status, models, onboard, setup, status-all |
| **tui** | `src/tui/` | Terminal UI components and theme |
| **web** | `src/web/` | Web frontend |
| **interactive** | `src/interactive/` | Interactive message handling, payload construction |
| **wizard** | `src/wizard/` | Onboarding wizard |
| **terminal** | `src/terminal/` | Terminal utilities: table rendering, ANSI, palette |
| **markdown** | `src/markdown/` | Markdown processing |

### Specialized

| Subsystem | Path | Description |
|-----------|------|-------------|
| **auto-reply** | `src/auto-reply/` | Auto-reply pipeline: chunking, templating, thinking levels, reply dispatch |
| **auto-reply/reply** | `src/auto-reply/reply/` | Reply dispatcher implementation |
| **acp** | `src/acp/` | Agent Communication Protocol |
| **acp/control-plane** | `src/acp/control-plane/` | ACP control plane |
| **acp/runtime** | `src/acp/runtime/` | ACP runtime |
| **bindings** | `src/bindings/` | External bindings |
| **bootstrap** | `src/bootstrap/` | Bootstrap files and initialization |
| **canvas-host** | `src/canvas-host/` | Canvas host + A2UI |
| **canvas-host/a2ui** | `src/canvas-host/a2ui/` | A2UI bundle |
| **chat** | `src/chat/` | Chat types and utilities |
| **compat** | `src/compat/` | Backward compatibility |
| **docs** | `src/docs/` | Internal docs generation |
| **flows** | `src/flows/` | Task flows |
| **i18n** | `src/i18n/` | Internationalization |
| **link-understanding** | `src/link-understanding/` | URL link understanding |
| **mcp** | `src/mcp/` | MCP (Model Context Protocol) support |
| **node-host** | `src/node-host/` | Node host for companion apps |
| **pairing** | `src/pairing/` | DM pairing codes and allowlists |
| **scripts** | `src/scripts/` | Internal scripts |
| **tasks** | `src/tasks/` | Task flow registry, task executor |
| **web-fetch** | `src/web-fetch/` | Web fetching |
| **web-search** | `src/web-search/` | Web search |

### Testing

| Subsystem | Path | Description |
|-----------|------|-------------|
| **test-helpers** | `src/test-helpers/` | Shared test utilities |
| **test-utils** | `src/test-utils/` | Additional test utilities |

## Key Patterns

- **Plugin-first architecture:** Core stays lean; capability ships as plugins. Extensions cross into core only through `openclaw/plugin-sdk/*`.
- **Prompt cache stability:** Ordering must be deterministic. Legacy cleanup preserves recent prompt bytes.
- **Security defaults:** DMs use pairing policy by default. Exec approvals with allowlists. Sandbox support.
- **Multi-channel:** All changes must consider all built-in + extension channels.
- **Date-based versioning:** `vYYYY.M.D` for stable, `vYYYY.M.D-beta.N` for prerelease.
