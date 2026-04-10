# OpenClaw Skills System

Complete reference for skill discovery, loading, filtering, and serialization.

**Source files read:** `src/agents/skills/types.ts`, `src/agents/skills/source.ts`, `src/agents/skills/skill-contract.ts`, `src/agents/skills/serialize.ts`, `src/agents/skills/filter.ts`, `src/agents/skills/agent-filter.ts`, `src/agents/skills/config.ts`, `src/agents/skills/refresh.ts`, `src/agents/skills/plugin-skills.ts`, `src/agents/skills/local-loader.ts`, `src/agents/skills/workspace.ts`, `src/agents/skills/bundled-dir.ts`, `src/agents/skills/frontmatter.ts`, `src/agents/skills/command-specs.ts`

---

## Core Types

### Skill (contract)

```typescript
// From skill-contract.ts — the canonical skill shape
type Skill = {
  name: string;
  description: string;
  source: string;              // "openclaw-bundled" | "openclaw-managed" | "openclaw-workspace" | "openclaw-plugin" | etc.
  pluginId?: string;
  filePath: string;            // Path to SKILL.md
  baseDir: string;             // Directory containing skill
};
```

### SkillEntry

```typescript
type SkillEntry = {
  skill: Skill;
  frontmatter: ParsedSkillFrontmatter;
  metadata?: OpenClawSkillMetadata;
  invocation?: SkillInvocationPolicy;
  exposure?: SkillExposure;
};
```

### OpenClawSkillMetadata

```typescript
type OpenClawSkillMetadata = {
  always?: boolean;              // Always-loaded skill
  skillKey?: string;             // Unique key for dedup
  primaryEnv?: string;           // Primary env var (e.g., OPENAI_API_KEY)
  emoji?: string;                // Display emoji
  homepage?: string;             // URL
  os?: string[];                 // Supported OS platforms
  requires?: {
    bins?: string[];             // Required binaries (all must exist)
    anyBins?: string[];          // Required binaries (at least one)
    env?: string[];              // Required env vars
    config?: string[];           // Required config keys
  };
  install?: SkillInstallSpec[];  // Installation instructions
};
```

### SkillInstallSpec

```typescript
type SkillInstallSpec = {
  id?: string;
  kind: "brew" | "node" | "go" | "uv" | "download";
  label?: string;
  bins?: string[];
  os?: string[];
  formula?: string;              // brew formula
  package?: string;              // npm/go/uv package
  module?: string;               // go module
  url?: string;                  // download URL
  archive?: string;              // archive type
  extract?: boolean;
  stripComponents?: number;
  targetDir?: string;
};
```

### SkillInvocationPolicy

```typescript
type SkillInvocationPolicy = {
  userInvocable: boolean;            // Can be invoked by user via /command
  disableModelInvocation: boolean;   // Model cannot invoke this skill
};
```

### SkillExposure

```typescript
type SkillExposure = {
  includeInRuntimeRegistry: boolean;
  includeInAvailableSkillsPrompt: boolean;
  userInvocable: boolean;
};
```

### SkillCommandSpec

```typescript
type SkillCommandSpec = {
  name: string;
  skillName: string;
  description: string;
  dispatch?: SkillCommandDispatchSpec;
  promptTemplate?: string;           // Native prompt template
  sourceFilePath?: string;           // Source markdown path
};

type SkillCommandDispatchSpec = {
  kind: "tool";
  toolName: string;                  // AnyAgentTool.name
  argMode?: "raw";                   // Forward raw args to tool
};
```

### SkillSnapshot

```typescript
type SkillSnapshot = {
  prompt: string;                    // Serialized <available_skills> XML
  skills: Array<{
    name: string;
    primaryEnv?: string;
    requiredEnv?: string[];
  }>;
  skillFilter?: string[];            // Normalized agent-level filter
  resolvedSkills?: Skill[];
  version?: number;
};
```

### SkillsInstallPreferences

```typescript
type SkillsInstallPreferences = {
  preferBrew: boolean;
  nodeManager: "npm" | "pnpm" | "yarn" | "bun";
};
```

### SkillEligibilityContext

```typescript
type SkillEligibilityContext = {
  remote?: {
    platforms: string[];
    hasBin: (bin: string) => boolean;
    hasAnyBin: (bins: string[]) => boolean;
    note?: string;
  };
};
```

## 7-Source Discovery

Skills are discovered from 7 sources with a 3-level priority hierarchy:

### Priority Levels

1. **Project-level (priority 30)** — Skills in the project's `.openclaw/skills/` directory
2. **Agent-level (priority 20)** — Agent-specific skill allowlists
3. **Global-level (priority 10)** — Global workspace, bundled, managed, plugin skills

### Sources

1. **Bundled skills** (`bundled-dir.ts`) — Ships with OpenClaw in the `skills/` directory. Source: `"openclaw-bundled"`.
2. **Managed skills** — Installed via ClawHub. Source: `"openclaw-managed"`.
3. **Workspace skills** (`workspace.ts`) — In the agent's workspace directory. Source: `"openclaw-workspace"`.
4. **Plugin skills** (`plugin-skills.ts`) — Provided by installed plugins. Source: `"openclaw-plugin"`. Plugin ID tracked via `pluginId` field.
5. **Project skills** — In the project's `.openclaw/skills/` directory.
6. **Local skills** (`local-loader.ts`) — Loaded from local filesystem paths.
7. **Environment-overridden skills** (`env-overrides.ts`) — Skills from environment variable paths.

### Deduplication

When skills from multiple sources share the same name, higher-priority sources win. Deduplication is by skill name (case-insensitive).

## Skill Discovery Flow

### `resolveSkillSource(skill) -> string`

Resolves the canonical source from the skill object, checking `skill.source` first, then legacy `skill.sourceInfo.source`.

### Loading

Each skill is loaded from a `SKILL.md` file:
1. Parse YAML frontmatter via `parseFrontmatter()`
2. Extract metadata (always, skillKey, primaryEnv, emoji, etc.)
3. Resolve invocation policy
4. Resolve exposure settings
5. Build `SkillEntry`

### Frontmatter (`frontmatter.ts`)

Skills use YAML frontmatter in their SKILL.md files:

```markdown
---
name: web-search
description: Search the web for information
primaryEnv: GOOGLE_API_KEY
emoji: 🔍
---

Skill content here...
```

## Filtering

### Agent-Level Filter (`agent-filter.ts`)

```typescript
function resolveEffectiveAgentSkillFilter(cfg: OpenClawConfig, agentId: string): string[] | undefined
```

Resolves the skill allowlist for a specific agent:
1. Agent-specific `skills[]` in `agents.list[].skills`
2. Global default `agents.defaults.skills[]`
3. `undefined` = unrestricted (all skills available)

### Runtime Filter (`filter.ts`)

Skills are filtered by:
- Agent allowlist match
- OS platform eligibility
- Binary requirements (`requires.bins`, `requires.anyBins`)
- Environment variable requirements (`requires.env`)
- Config key requirements (`requires.config`)

## Serialization (`serialize.ts`)

Skills are serialized as XML in the system prompt at priority 65:

```xml
<available_skills>
  <skill name="web-search" primaryEnv="GOOGLE_API_KEY">
    Search the web for information
  </skill>
  ...
</available_skills>
```

## Limits

From the CLAUDE.md description and source code analysis:
- **Candidates** — Total discovered skills before filtering
- **Loaded** — Skills passing all eligibility checks
- **Prompt chars** — Serialized XML character budget for the system prompt

## Refresh (`refresh.ts`)

Skill refresh watches for changes:
- File system changes in skill directories
- Config changes to skill allowlists
- Plugin skill registration changes

Refresh state tracked via `refresh-state.ts`.

## Plugin Skills (`plugin-skills.ts`)

Plugins register skills via the plugin runtime:
- Plugin ID tracked on each skill entry
- Plugin skills have source `"openclaw-plugin"`
- Eligible for the same filtering as other skills
- Plugin skill registration tested via `plugin-skills.test.ts`

## Runtime Config (`runtime-config.ts`)

Runtime configuration for skill loading and caching.

## Tools Dir (`tools-dir.ts`)

Skills can contribute tool definitions from their directory structure.

## Bundled Context (`bundled-context.ts`)

Context injection for bundled skills.
