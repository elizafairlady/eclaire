# OpenClaw Permissions & Exec Approvals

Complete reference for the execution approval system.

**Source files read:** `src/infra/exec-approvals.ts`, `src/infra/exec-approvals-allowlist.ts`, `src/infra/exec-approvals-analysis.ts`

---

## Core Types

### ExecHost

```typescript
type ExecHost = "sandbox" | "gateway" | "node";
```

Where a command executes:
- `"sandbox"` — Docker/Podman sandbox container
- `"gateway"` — Gateway host machine
- `"node"` — Companion node (macOS, iOS, Android)

### ExecTarget

```typescript
type ExecTarget = "auto" | ExecHost;
```

`"auto"` resolves to the appropriate host at runtime.

### ExecSecurity

```typescript
type ExecSecurity = "deny" | "allowlist" | "full";
```

Security policy for command execution:
- `"deny"` — Block all exec
- `"allowlist"` — Only allow commands matching allowlist patterns
- `"full"` — Allow all commands (default)

### ExecAsk

```typescript
type ExecAsk = "off" | "on-miss" | "always";
```

When to prompt for approval:
- `"off"` — Never ask (default)
- `"on-miss"` — Ask only when command does not match allowlist
- `"always"` — Always ask, even for allowlisted commands

## Approval Flow Types

### SystemRunApprovalBinding

```typescript
type SystemRunApprovalBinding = {
  argv: string[];
  cwd: string | null;
  agentId: string | null;
  sessionKey: string | null;
  envHash: string | null;
};
```

Captures the full execution context for approval decisions. `envHash` is a hash of the environment variables to detect changes.

### SystemRunApprovalFileOperand

```typescript
type SystemRunApprovalFileOperand = {
  argvIndex: number;          // Position in argv where file path appears
  path: string;               // Resolved file path
  sha256: string;             // SHA-256 hash of file content
};
```

When a command operates on a file (e.g., `bash script.sh`), the file's content hash is captured so the approval is bound to the exact file content. If the file changes, the approval no longer applies.

### SystemRunApprovalPlan

```typescript
type SystemRunApprovalPlan = {
  argv: string[];
  cwd: string | null;
  commandText: string;            // Full command text for display
  commandPreview?: string | null; // Shortened preview for UI
  agentId: string | null;
  sessionKey: string | null;
  mutableFileOperand?: SystemRunApprovalFileOperand | null;
};
```

### ExecApprovalRequestPayload

```typescript
type ExecApprovalRequestPayload = {
  command: string;
  commandPreview?: string | null;
  commandArgv?: string[];
  envKeys?: string[];                    // UI-safe env key preview
  systemRunBinding?: SystemRunApprovalBinding | null;
  systemRunPlan?: SystemRunApprovalPlan | null;
  cwd?: string | null;
  nodeId?: string | null;
  host?: string | null;
  security?: string | null;
  ask?: string | null;
  allowedDecisions?: readonly ExecApprovalDecision[];
  agentId?: string | null;
  resolvedPath?: string | null;
  sessionKey?: string | null;
  turnSourceChannel?: string | null;
  turnSourceTo?: string | null;
  turnSourceAccountId?: string | null;
  turnSourceThreadId?: string | number | null;
};
```

### ExecApprovalRequest

```typescript
type ExecApprovalRequest = {
  id: string;
  request: ExecApprovalRequestPayload;
  createdAtMs: number;
  expiresAtMs: number;
};
```

Default timeout: `DEFAULT_EXEC_APPROVAL_TIMEOUT_MS = 1_800_000` (30 minutes).

### ExecApprovalResolved

```typescript
type ExecApprovalResolved = {
  id: string;
  decision: ExecApprovalDecision;
  resolvedBy?: string | null;
  ts: number;
  request?: ExecApprovalRequest["request"];
};
```

## Allowlist System

### ExecAllowlistEntry

```typescript
type ExecAllowlistEntry = {
  id?: string;
  pattern: string;             // Command pattern (supports glob)
  source?: "allow-always";     // Auto-approved source
  commandText?: string;        // Full command for reference
  argPattern?: string;         // Argument-level pattern
  lastUsedAt?: number;
  lastUsedCommand?: string;
  lastResolvedPath?: string;
};
```

### ExecApprovalsAgent

```typescript
type ExecApprovalsAgent = ExecApprovalsDefaults & {
  allowlist?: ExecAllowlistEntry[];
};
```

### ExecApprovalsDefaults

```typescript
type ExecApprovalsDefaults = {
  security?: ExecSecurity;
  ask?: ExecAsk;
  askFallback?: ExecSecurity;      // Fallback when ask times out (default: "full")
  autoAllowSkills?: boolean;       // Auto-allow skill commands (default: false)
};
```

## Approvals File

### ExecApprovalsFile

```typescript
type ExecApprovalsFile = {
  version: 1;
  socket?: {
    path?: string;
    token?: string;
  };
  defaults?: ExecApprovalsDefaults;
  agents?: Record<string, ExecApprovalsAgent>;
};
```

Stored at `~/.openclaw/exec-approvals.json` with SHA-256 hash tracking for change detection.

### ExecApprovalsSnapshot

```typescript
type ExecApprovalsSnapshot = {
  path: string;
  exists: boolean;
  raw: string | null;
  file: ExecApprovalsFile;
  hash: string;
};
```

### ExecApprovalsResolved

```typescript
type ExecApprovalsResolved = {
  path: string;
  socketPath: string;
  token: string;
  defaults: Required<ExecApprovalsDefaults>;
  agent: Required<ExecApprovalsDefaults>;
  agentSources: {
    security: string | null;
    ask: string | null;
    askFallback: string | null;
  };
  allowlist: ExecAllowlistEntry[];
  file: ExecApprovalsFile;
};
```

## Socket Communication

The approval system uses a Unix socket at `~/.openclaw/exec-approvals.sock` for real-time approval requests between the agent runtime and the gateway/UI.

Communication via `requestJsonlSocket()` for NDJSON-over-socket messaging.

## Default Values

```typescript
const DEFAULT_SECURITY: ExecSecurity = "full";
const DEFAULT_ASK: ExecAsk = "off";
const DEFAULT_EXEC_APPROVAL_ASK_FALLBACK: ExecSecurity = "full";
const DEFAULT_AUTO_ALLOW_SKILLS = false;
const DEFAULT_SOCKET = "~/.openclaw/exec-approvals.sock";
const DEFAULT_FILE = "~/.openclaw/exec-approvals.json";
const DEFAULT_EXEC_APPROVAL_TIMEOUT_MS = 1_800_000;  // 30 minutes
```

## Normalization Functions

```typescript
function normalizeExecHost(value?: string | null): ExecHost | null
function normalizeExecTarget(value?: string | null): ExecTarget | null
function normalizeExecSecurity(value?: string | null): ExecSecurity | null
function normalizeExecAsk(value?: string | null): ExecAsk | null
```

All perform lowercase normalization and return `null` for unrecognized values.

## File Operand Hashing

When a command includes a file operand (e.g., `bash script.sh`, `node app.js`), the file content is hashed with SHA-256. This hash is stored in the `SystemRunApprovalFileOperand` and used to:

1. Bind the approval to the exact file content at approval time
2. Invalidate the approval if the file is modified after approval
3. Prevent a compromised file from executing under a stale approval

The `argvIndex` field identifies which argv position contains the file path, enabling the system to re-hash and verify at execution time.

## Agent-Scoped Configuration

Per-agent approval config is stored under `agents[agentId]` in the approvals file. Resolution merges agent-specific settings over global defaults:

1. Agent-specific `security` / `ask` / `askFallback` / `autoAllowSkills`
2. Agent-specific `allowlist[]`
3. Global `defaults.*`
4. Hardcoded defaults

The `agentSources` field in `ExecApprovalsResolved` tracks where each resolved value came from for diagnostic purposes.

## Cron Job Exec Policy

Cron jobs (background work) cannot wait for interactive approval. This matches the hard rule: exec is denied when cron runs cannot wait for interactive approval. Cron jobs must use pre-approved policies (allowlist patterns or `security: "full"`).
