# eclaire

Personal AI gateway. Her name is Claire.

eclaire is a daemon + TUI + CLI that puts an AI executive assistant on your local machine. Claire orchestrates specialist agents — coding, research, sysadmin, and whatever else you teach her — through a Unix socket gateway. She reads your email, monitors RSS feeds, manages reminders, runs scheduled background work, and builds new tools for herself.

Built for one user on one machine. If you want to use it, fork it and make it yours.

## Building

```
go build -o ecl ./cmd/ecl
```

Requires Go 1.26+ and an API key for OpenRouter or a local Ollama instance.

## Usage

```
ecl                               # TUI — talk to Claire
ecl run "what's on my plate today"
ecl run -a coding "fix the parser"
ecl run -a research "what happened in Iran this week"
ecl run --continue                # resume last session
ecl run -n "project-x" "prompt"   # named session

ecl daemon start|stop|status
ecl agent list
ecl session list
ecl job list|add|remove|run|runs
ecl cron list|add|remove
ecl notifications
ecl flow list|run|create|status
ecl system-prompt -a coding -m minimal
```

## Architecture

Gateway daemon on a Unix socket, NDJSON wire protocol. The TUI and CLI are clients.

```
ecl (TUI/CLI)  ──unix socket──>  gateway daemon
                                   ├── agent registry (5 built-in + user-defined)
                                   ├── 26 tools (shell, files, web, email, RSS, memory, ...)
                                   ├── session store (JSONL, persistent main session)
                                   ├── job executor (at/every/cron scheduling)
                                   ├── notification store
                                   ├── permission system (approval gate)
                                   └── provider router (Ollama, OpenRouter)
```

Claire is the orchestrator. She delegates to specialist agents, each running in their own session with their own tools and approval state. Agents are templates — multiple instances can run simultaneously.

## Agents

| Agent | Role | What it does |
|-------|------|-------------|
| Claire (orchestrator) | EA | Delegates, manages workflow, runs the show |
| coding | Pair programmer | Reads/writes code, runs tests, does the work |
| research | Investigator | Multi-source research with citations |
| sysadmin | Ops | System monitoring, service management, logs |
| config | Self-modification | Creates new agents, skills, schedules |

Create your own: `ecl agent create myagent` or drop a directory at `~/.eclaire/agents/myagent/` with `agent.yaml` and workspace files.

## Workspace

Layered files that shape Claire's personality, knowledge, and behavior:

```
~/.eclaire/workspace/
  SOUL.md        # who Claire is
  AGENTS.md      # how she operates
  USER.md        # who you are
  BOOT.md        # morning startup routine
  HEARTBEAT.md   # periodic checks
  MEMORY.md      # persistent memory
```

Project-level overrides go in `.eclaire/workspace/` inside your project directory.

## Scheduling

Three kinds of scheduled work, all through one system:

- **at** — one-shot: "do this in 30 minutes", "do this at 3pm tomorrow"
- **every** — recurring interval: "check my email every 6 hours"
- **cron** — standard 5-field cron expressions

Jobs persist to disk, survive restarts, and create notifications on completion. Claire can schedule her own work.

## Status

Pre-0.1. The bones are solid — gateway, agents, tools, sessions, permissions, scheduling, notifications, TUI all exist and compile. ~34k lines of Go, ~500 tests. Not everything has been validated end-to-end on a real workload yet.

What works:
- Agent execution with streaming, tool calling, and auto-compaction
- Permission system (PermissionWriteOnly — dangerous tools require approval)
- Unified job scheduling with run logs
- Persistent notifications with CLI resolution
- Memory system with three-phase dreaming (consolidation during idle time)
- Session lifecycle with persistence and resumption
- CWD-based project detection

What's next:
- Unify the legacy scheduler into the job system
- TUI: main session tab, notification drain on connect
- Per-connection project workspace loading
- Live validation tests against real LLM behavior

## Design Lineage

eclaire takes architectural cues from three codebases:

- [OpenClaw](https://github.com/openclaw/openclaw) — gateway model, scheduling, workspace layering, delivery, composability. The architecture reference.
- [Claw Code](https://github.com/ultraworkers/claw-code) (Rust crates) — agentic loop, permission enforcement, hooks, compaction, session model. The harness reference.
- [Crush](https://github.com/charmbracelet/crush) — TUI patterns only. Ultraviolet Draw, scrollback, markdown rendering.

LLM abstraction via [Fantasy](https://charm.land/fantasy).

## Config

Copy `config.yaml.example` to `~/.eclaire/config.yaml` and adjust. See `examples/` for agent definitions and workspace files.

Minimal config — just needs at least one provider:

```yaml
providers:
  ollama:
    type: ollama
    base_url: "http://localhost:11434"

routing:
  simple:
    - provider: ollama
      model: "qwen3.5:latest"
  complex:
    - provider: ollama
      model: "gemma4:31b"
  orchestrator:
    - provider: ollama
      model: "gemma4:31b"
```

## Testing

```
go test ./...                                              # unit + mock tests
OPENROUTER_API_KEY=... go test ./internal/agent/ -tags live -timeout 10m  # live LLM tests
```
