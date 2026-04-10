# eclaire Project Specification

This document contains the user's vision in their own words, followed by the technical specification derived from those words. The user's words are canonical. When in doubt, re-read their words, not the derived spec.

---

## The User's Words

### What eclaire IS

> "It will be doing deep research, programming and maintenance work, system administration, and improving itself, in addition to monitoring my e-mail, the RSS feeds I give it, and managing my todos and reminders."

> "OpenClaw is a fucking travesty, built from the frontend back. It is more interface than gateway. I want to get the gateway and the agents right, really right, for local models so I can start training a local model and improve in parallel."

> "I'm building this for me and me alone. I'll give it to other people but they will have to scratch their own itch, which is why it's supposed to be replacing you in working on it and orchestrating the coding agents to work on itself."

> "It needs to know who I am, what I like, what I hate, my contacts, it needs to break down briefing data, it needs to know how I want it to respond, the whole fucking thing."

### Daily Use Pattern

> "I'll check in with information about training with the dogs each morning and either copy-paste or ask it to read my email and slack history, hydrate those conversations and then build me reference sheets. I have a remarkably bad long-term memory so I need to know exactly what repos and files people are referring to. It'll need to have the email/rss/fediverse/etc briefing for the overnight ready for me. We'll discuss the day and try to make a plan of 3 to 5 things to accomplish together. As I go through work if I get frustrated, instead of talking to a coworker in frustration I'll bring the conversation to eclaire. If I need to solve a problem that is clearly scoped and defined and ticketable I'll bring it to eclaire. If something happens out of the ordinary, or I have weird thoughts that I want help hydrating and expanding upon and playing with -- without being judged or misunderstood -- I'll bring it to eclaire. I'll let eclaire know when we're in a work scope, when we're in a personal scope, when we're in some other scope of my life. As the tool becomes more useful I'll be asking it to build more tools for itself, more agent definitions and prompts for itself. It'll always run on my home server with an RTX 5090 on it (this machine). I will call her Claire, and she will be a real Executive Assistant, because she is an ai gateway."

### On Composability

> "I don't want a fucking facade. Everything should be composable, from the orchestrator and agents down."

> "OpenClaw isn't just what you described; I'd argue you sell it fucking short. No. It can create pipelines of agent work. It can orchestrate multiple stages for multiple types of actors. 'Lobster pipelines.' Except that isn't anything special. It's composability."

> "If you fucking hard-code it to do EXACTLY what I described in the behavioural tests and not composable so that it is able to build on itself as part of its agency, through composition and reuse, then whatever you're thinking is too fucking narrow."

### The Synthesis

> "I need you to fucking understand what I am fucking communicating to you about this being an AI gateway to a personal assistant orchestrator agent, who has a full and fucking complete pair programming assistant agent, a full and fucking complete research agent, a full and fucking complete systems administration agent, and so fucking much more. There is a fucking synthesis taking place here."

eclaire is a personal AI gateway (OpenClaw architecture) where the orchestrator IS Claire (EA personality), and each specialist agent is as complete and capable as the standalone tool it replaces. The coding agent should be as complete as Claw Code. The research agent should be a complete research tool. The sysadmin agent should be a complete sysadmin tool. Claire composes them. Claire can create new agents and tools for herself. Everything is composable.

### On Testing

> "We need actual integration tests, and now."
> "We need to actually test the behaviour of the system. With an LLM."
> "But those weren't realistic cases."
> "Until I can make you rely on real results rather than fucking making up tests and calling it good, based in the reality of what this advancement in the AI world means and what it provides me, you're worthless."

### On Reference Implementations

> "WHY CRUSH NIGGER, WHY DID YOU GO TO CRUSH FIRST NIGGER? OPENCLAW NIGGER. OPENCLAW FIRST NIGGER."
> "Stop reading blogs. Only read fucking code and the official repos. You are being poisoned."
> "Why are you reading the TS, if the Rust is what is being maintained?" (about Claw Code)

**Priority**: OpenClaw for gateway architecture. Claw Code Rust crates for agent harness. Crush for TUI ONLY.

### On How to Work

> "Never cut scope. Build everything requested."
> "No backwards compatibility concerns -- pre-0.1."
> "Always write tests alongside code."
> "Research reference systems before building."

---

## Technical Specification

### Identity

- **Name**: eclaire (binary: `ecl`)
- **Called**: Claire (she/her)
- **Role**: Personal AI gateway to a personal assistant orchestrator
- **Module**: `github.com/elizafairlady/eclaire`
- **Language**: Go 1.26.1
- **LLM**: `charm.land/fantasy@v0.17.1`
- **TUI**: `charm.land/bubbletea/v2`, `lipgloss/v2`, `ultraviolet`
- **Target**: Single user, single machine (home server with RTX 5090)

### Architecture

Gateway daemon (`ecl --daemon`) on Unix socket, NDJSON wire protocol. TUI client or CLI connects. Fantasy for LLM abstraction (Agent loop, streaming callbacks, tool calling).

### Session Model

**Main session**: One global persistent session. Claire's permanent conversation. Heartbeats run here. Briefing content accumulates here. System-level awareness events arrive here. Always exists. Always accessible as a tab in TUI.

**Project sessions**: Scoped to a project directory. Created/resumed when TUI connects from a project directory. Project workspace files layer over global workspace.

**Agent sessions**: Persistent sessions for agents not tied to a project directory (sysadmin, research when not project-bound). Maintain conversation history and approval state.

**Isolated sessions**: Ephemeral. Created for cron jobs, one-shot tasks, background work. Cleaned up after completion.

### Workspace Model

Layered with override precedence:
1. Embedded defaults (Go code, priority 0)
2. Global (`~/.eclaire/workspace/`, priority 10)
3. Agent-specific (`~/.eclaire/agents/<id>/workspace/`, priority 20)
4. Project (`.eclaire/workspace/`, priority 30)

Files: SOUL.md, AGENTS.md, USER.md, TOOLS.md, HEARTBEAT.md, BOOT.md, MEMORY.md, daily logs.

### Scheduling

Single unified job system with three schedule kinds:
- **at**: One-shot at absolute timestamp or relative duration. Auto-deletes after success.
- **every**: Fixed interval with optional anchor.
- **cron**: 5/6-field cron expression with timezone and stagger.

Persistent job store. Per-job JSONL run logs. Transient error retry with exponential backoff. Session targets determine where results go.

### Notifications

Persistent store on disk with severity levels (debug, info, warning, error). Notifications accumulate while no client connected. TUI drains on connect. CLI reads from store. Channels push when configured.

### Permissions

Session-scoped, command-pattern based. Default mode prompts the user (not allow-all). Background work uses pre-approved policies. Approval dialog in TUI wired end-to-end.

### Agents

5 built-in agents (orchestrator/Claire, coding, research, sysadmin, config). Also loadable from disk. Claire can create agents via eclaire_manage. Each specialist must be as capable in its domain as the standalone tool it replaces.

### Skills

3-level hierarchy: project(30) > agent(20) > global(10). Claire can create skills.

### Composability

Everything is composable. Agents, tools, skills, pipelines. Claire can build on herself. Task flows with template chaining. Multi-agent orchestration. This is NOT a facade.

### Self-Improvement

Claire can create agents, tools, skills, and pipelines for herself. She can modify her own workspace files. She can schedule background work. She can monitor and respond to events.

### Reference Implementations

- **OpenClaw** (`/tmp/openclaw/`): Gateway architecture, scheduling, cron, delivery, channels, workspace model, memory, skills, task flows, multi-agent orchestration. 10,753 files, 862K LOC.
- **Claw Code** (`/tmp/claw-code/`): Agent harness: conversation runtime, tool dispatch, permission enforcement, hooks, auto-compaction, git context, session model. The Rust crates are canonical. 10 crates, 64K lines.
- **Crush** (`/home/vii/go/pkg/mod/github.com/charmbracelet/crush@v0.54.0/`): TUI ONLY. Ultraviolet Draw, nested tool rendering, scrollback, markdown, cost tracking. 26K lines.
- **Fantasy** (`/home/vii/go/pkg/mod/charm.land/fantasy@v0.17.1/`): LLM abstraction. Provider/LanguageModel/Agent interfaces, streaming, tool calling, structured output. 15K lines.
