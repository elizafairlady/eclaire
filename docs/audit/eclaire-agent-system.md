# eclaire Agent System Audit

Audited: 2026-04-09, updated 2026-04-10

## Agent Interface (`internal/agent/agent.go`)

```go
type Agent interface {
    ID() string
    Name() string
    Description() string
    Init(ctx context.Context) error
    Shutdown(ctx context.Context) error
    Handle(ctx context.Context, req Request) (Response, error)
    Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
    Role() Role
    Bindings() []Binding
    RequiredTools() []string
    CredentialScope() string
}
```

Roles: RoleSimple, RoleComplex, RoleEmbed, RoleOrchestrator

## 5 Built-in Agents + Disk-Loaded Agents

Built-in agents have SOUL.md/AGENTS.md prompts embedded in Go code (`builtin.go`, `builtin_prompts.go`). Disk agents loaded from `~/.eclaire/agents/<id>/agent.yaml` + `workspace/*.md`.

| Agent | Role | Source | Model Route |
|-------|------|--------|------------|
| Orchestrator (Claire) | orchestrator | built-in | deepseek/deepseek-v3.2 |
| Coding | complex | built-in | anthropic/claude-sonnet-4.6 |
| Research | complex | built-in | (same as coding) |
| Sysadmin | complex | built-in | (same as coding) |
| Config | simple | built-in | ollama/gemma4:31b |
| Adversary | adversary | disk | x-ai/grok-4.20 |

**Dynamic discovery (updated 2026-04-10):** Context engine injects `<available_agents>` XML from registry at priority 89. Agent tool description built dynamically from registry. No hardcoded agent lists — add an agent on disk and it appears everywhere automatically. Validated end-to-end: DeepSeek orchestrator → Claude coding agent → Grok adversarial QA.

**Issue**: `builtinAgent.Handle()` and `Stream()` return dummy "use Runner" strings — dead code. Runner uses different paths via Router/fantasy.

**Issue**: Agent tool lists are suggestions, not enforced. Actual available tools determined by `runner.Tools.ForAgent()`.

**Issue**: `ModelOverride` on yaml agents bypasses routing table — uses model string as role, falls through to `resolveByModel()` instead of checking routing entries for the agent's actual role first.

## Runner (`internal/agent/runner.go`, ~700 lines)

The core execution engine. Orchestrates LLM streaming, tool execution, session management.

**Code exists for** (not validated by user):
- Single turn through agentic loop (`Run()`)
- Auto-compaction outer loop (`RunWithCompaction()`)
- Sub-agent delegation with event forwarding (`RunSubAgent()`)
- Tool hooking and permission integration
- Token tracking and usage aggregation
- Context window calculation and output token budgeting

**Issues**:
- `maxOutputTokens()` caps at 32,768 — no per-model validation
- `RunResult.Compactions` count is set but never read by callers
- `runLegacy()` exists for test mock compatibility — should be removed

## ConversationRuntime (`internal/agent/runtime.go`)

The agentic loop itself. Streams model, extracts tool calls, runs hooks, executes tools.

**Code exists for** (not validated by user):
- Model streaming with Fantasy
- Tool extraction from assistant messages
- Pre/post hook execution
- Permission checking integration
- Max 25 iterations by default
- Stop conditions (budget exhaustion, repeated tool calls)

## Context Engine (`internal/agent/context_engine.go`, ~700 lines)

Assembles system prompts from workspace files with priorities.

**Code exists for** (not validated by user):
- 14 prompt sections with priorities (runtime[100] > SOUL[95] > ... > overrides[40])
- Three prompt modes: full, minimal, none
- Bootstrap limits: 20k chars/file, 150k total
- Git context injection
- Instruction file discovery
- Skill injection
- Compaction with structured analysis

**Issues**:
- Section priorities don't determine final prompt ORDER — sections appended in call order
- `CompactPrompt()` has partial bubble sort instead of `sort.Slice()`
- `sectionIncluded()` minimal mode whitelist includes inappropriate sections for sub-agents

## Workspace Loader (`internal/agent/workspace.go`, ~260 lines)

**Code exists for** (not validated by user):
- 4-layer loading: embedded[0] → global[10] → agent[20] → project[30]
- Memory loading with daily log indexing
- Standing orders loading
- Boot tracking (.boot_ran file)

**Issue**: Layer 4 (project) never activates — projectDir is never set.

## Skills (`internal/agent/skills.go`, ~193 lines)

**Code exists for** (not validated by user):
- 3-level hierarchy: global[10] → agent[20] → project[30]
- SKILL.md parsing with YAML frontmatter
- Serialization to XML for prompt injection
- Max 150 skills, 30k bytes budget

## Agent Loading (`internal/agent/loader.go`, ~176 lines)

**Code exists for** (not validated by user):
- Directory-based agents: `~/.eclaire/agents/<id>/agent.yaml` + workspace/
- Workspace file loading alongside agent definition
- Registry integration

**Missing**: Hot-reload on filesystem change.

## Registry (`internal/agent/registry.go`, ~141 lines)

**Code exists for** (not validated by user):
- Thread-safe map of Agent implementations
- Context-aware resolution via Bindings
- Priority-based binding selection

## Reference

- OpenClaw agents: `docs/reference/openclaw-agents.md`
- OpenClaw config: `docs/reference/openclaw-config.md`
- Claw Code conversation: `docs/reference/clawcode-conversation.md`
