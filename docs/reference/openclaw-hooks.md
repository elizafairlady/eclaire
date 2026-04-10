# OpenClaw Hooks System

Complete reference for hooks: installation, types, internal hooks, workspace hooks, Gmail integration, and bundled hooks.

**Source files read:** `src/hooks/types.ts`, `src/hooks/hooks.ts`, `src/hooks/install.ts`, `src/hooks/internal-hooks.ts`, `src/hooks/workspace.ts`, `src/hooks/policy.ts`, `src/hooks/loader.ts`, `src/hooks/module-loader.ts`, `src/hooks/gmail.ts`, `src/hooks/gmail-watcher.ts`, `src/hooks/fire-and-forget.ts`, `src/hooks/frontmatter.ts`, `src/hooks/config.ts`, `src/hooks/bundled-dir.ts`, `src/hooks/bundled/*/handler.ts`

---

## Core Types (`types.ts`)

### Hook

```typescript
type Hook = {
  name: string;
  description: string;
  source: "openclaw-bundled" | "openclaw-managed" | "openclaw-workspace" | "openclaw-plugin";
  pluginId?: string;
  filePath: string;       // Path to HOOK.md
  baseDir: string;        // Directory containing hook
  handlerPath: string;    // Path to handler module (handler.ts/js)
};

type HookSource = Hook["source"];
```

### HookEntry

```typescript
type HookEntry = {
  hook: Hook;
  frontmatter: ParsedHookFrontmatter;
  metadata?: OpenClawHookMetadata;
  invocation?: HookInvocationPolicy;
};
```

### OpenClawHookMetadata

```typescript
type OpenClawHookMetadata = {
  always?: boolean;
  hookKey?: string;
  emoji?: string;
  homepage?: string;
  events: string[];              // Events this hook handles
  export?: string;               // Export name (default: "default")
  os?: string[];                 // Supported platforms
  requires?: {
    bins?: string[];
    anyBins?: string[];
    env?: string[];
    config?: string[];
  };
  install?: HookInstallSpec[];
};
```

### HookInstallSpec

```typescript
type HookInstallSpec = {
  id?: string;
  kind: "bundled" | "npm" | "git";
  label?: string;
  package?: string;
  repository?: string;
  bins?: string[];
};
```

### HookInvocationPolicy

```typescript
type HookInvocationPolicy = {
  enabled: boolean;
};
```

### HookEligibilityContext

```typescript
type HookEligibilityContext = {
  remote?: {
    platforms: string[];
    hasBin: (bin: string) => boolean;
    hasAnyBin: (bins: string[]) => boolean;
    note?: string;
  };
};
```

### HookSnapshot

```typescript
type HookSnapshot = {
  hooks: Array<{ name: string; events: string[] }>;
  resolvedHooks?: Hook[];
  version?: number;
};
```

## Hook Events

Hooks register for specific event types via the `events` field in metadata. Events include:

- `command:new` — New command/message received
- `session:start` — Session started
- `session:end` — Session ended
- `message:sent` — Message sent to channel
- `gateway:startup` — Gateway started
- And others defined by the hook system

## Hook Sources

### Bundled Hooks (`bundled-dir.ts`, `hooks/bundled/`)

Ship with OpenClaw. Located in the hooks directory structure. Source: `"openclaw-bundled"`.

Known bundled hooks:

1. **boot-md** (`bundled/boot-md/handler.ts`) — Handles BOOT.md file processing on gateway startup. Runs BOOT.md instructions through the agent on first boot.

2. **bootstrap-extra-files** (`bundled/bootstrap-extra-files/handler.ts`) — Handles additional bootstrap file injection beyond the standard workspace files.

3. **command-logger** (`bundled/command-logger/handler.ts`) — Logs commands for debugging/audit.

4. **session-memory** (`bundled/session-memory/handler.ts`) — Session memory management. Includes transcript processing for memory extraction.

### Managed Hooks

Installed via package managers. Source: `"openclaw-managed"`.

### Workspace Hooks (`workspace.ts`)

Located in the agent's workspace directory. Source: `"openclaw-workspace"`.

### Plugin Hooks

Provided by installed plugins. Source: `"openclaw-plugin"`. Plugin ID tracked via `pluginId`.

## Internal Hooks (`internal-hooks.ts`)

Built-in hooks that run without external module loading:

```typescript
type InternalHookHandler = {
  // Handler function signature for internal hooks
};
```

Functions:
- `createInternalHookEvent()` — Creates an internal hook event
- `triggerInternalHook()` — Triggers an internal hook synchronously

Internal hooks are used for core behaviors that must always run (message sending, delivery tracking, etc.) without depending on the external hook loading system.

## Hook Installation (`install.ts`, `install.runtime.ts`)

Hook installation supports three kinds:
- **bundled** — Already available in the OpenClaw package
- **npm** — Install via npm/pnpm/yarn/bun
- **git** — Clone from git repository

Installation flow:
1. Resolve install spec from hook metadata
2. Check if already installed
3. Execute installation
4. Verify handler module exists

## Hook Loading (`loader.ts`, `module-loader.ts`)

### Discovery

Hooks are discovered from:
1. Bundled hook directory
2. Managed hooks directory
3. Workspace hooks directory
4. Plugin-registered hooks

### Module Loading

Handler modules are loaded dynamically via `module-loader.ts`:
1. Resolve handler path from hook definition
2. Dynamic import of handler module
3. Extract named export (default: `"default"`)
4. Validate handler signature

## Hook Policy (`policy.ts`)

Determines hook eligibility at runtime:
- Platform check (OS compatibility)
- Binary requirements
- Environment variable requirements
- Config key requirements
- Enabled/disabled state

## Hook Configuration (`config.ts`)

Config model: `hooks.internal.entries` is the canonical public hook config model.

Legacy: `hooks.internal.handlers` is compatibility-only input and must not be re-exposed in public surfaces.

## Fire and Forget (`fire-and-forget.ts`)

```typescript
function fireAndForgetHook(hookEvent): void
```

Executes a hook without awaiting completion. Errors are caught and logged but do not propagate. Used for non-critical hook invocations (e.g., logging, telemetry).

## Gmail Integration (`gmail.ts`, `gmail-watcher.ts`, `gmail-ops.ts`)

### Gmail Watcher

Long-running background process that monitors a Gmail inbox:
- Watches for new emails matching configured filters
- Converts emails to system events or agent turns
- Manages OAuth tokens and refresh lifecycle

Files:
- `gmail.ts` — Core Gmail hook implementation
- `gmail-watcher.ts` — Watcher lifecycle management
- `gmail-watcher-lifecycle.ts` — Start/stop/reconnect
- `gmail-watcher-errors.ts` — Error classification
- `gmail-ops.ts` — Gmail API operations
- `gmail-setup-utils.ts` — Setup and configuration helpers

### Gmail Setup

Setup utilities for configuring Gmail integration:
- OAuth credential setup
- Filter configuration
- Watch/push notification setup

## Hook Frontmatter (`frontmatter.ts`)

Hooks use YAML frontmatter in their HOOK.md files, similar to skills:

```markdown
---
name: my-hook
description: Custom hook for...
events:
  - command:new
  - session:start
---

Hook documentation here...
```

## Hook Import URL (`import-url.ts`)

Resolves import URLs for hook modules, supporting:
- Local file paths
- npm package references
- Git repository references

## Hook Status (`hooks-status.ts`)

Status reporting for installed and active hooks.

## Hook Update (`update.ts`)

Update mechanism for managed hooks (npm/git sources).

## Message Hook Mappers (`message-hook-mappers.ts`)

Mappers that convert between internal message formats and hook event formats:

- `buildCanonicalSentMessageHookContext()` — Canonical context for sent-message hooks
- `toInternalMessageSentContext()` — Maps to internal hook format
- `toPluginMessageContext()` — Maps to plugin hook format
- `toPluginMessageSentEvent()` — Maps to plugin sent event format

## Hook Runtime Eligibility

A hook is eligible to run when:
1. Its `invocation.enabled` is `true`
2. Current OS is in `metadata.os[]` (or no OS restriction)
3. All `requires.bins[]` exist on the system
4. At least one of `requires.anyBins[]` exists
5. All `requires.env[]` environment variables are set
6. All `requires.config[]` config keys exist
7. The hook's events include the current event type
