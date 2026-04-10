# eclaire Project Tasks

From now to 1.0. Organized by milestone. Nothing is marked done without user validation.

**Status key**: `[ ]` not started, `[~]` in progress, `[x]` done (user validated)

---

## Milestone 0: Validation Framework

Everything starts here. No code changes until live tests exist and the user can validate independently.

### Live Test Queries (from user)

These are the minimum acceptance tests. Each must be runnable via `ecl run` and independently verifiable by the user in the TUI. Each gets codified into a `//go:build live` test.

**Test 1: Reminder → Notification Pipeline**
```
ecl run "Set a reminder for 5m from now to walk the dogs"
```
- Acceptance: The reminder creates a notification consumable by `ecl notification`
- Validates: Reminder tool, notification store, notification CLI
- Live test validates: notification appears in `ecl notification` output after 5m

**Test 2: Parallel Agent Research**
```
ecl run "Perform two research projects for me: I need you to dig into the TurboQuant technology and any other huge advancements made in Gemma 4, and I need you to find out the particulars of the ceasefire in Iran from the first week of April"
```
- Acceptance: Two research threads run in parallel, both produce substantive results
- Validates: Sub-agent delegation, parallel execution, research agent capability
- Live test validates: Two sub-agent events in session log, both with non-trivial content

**Test 3: Research → Scheduled Follow-up**
```
ecl run "I need you to perform a research project on the war and ceasefire in Iran, 2026. I also need you to create a one-time task for 5m after you finish that project, to audit the research, validate the research, and expand upon it with any new findings."
```
- Acceptance: Research completes, then a one-shot job is scheduled 5m later that audits it
- Validates: Research agent, job scheduling (at kind), job execution, session continuity
- Live test validates: Research content in session, job created in job store, job executes after delay

**Test 4: Coding Agent — Full Project**
```
ecl run -a coding "Write a game in python with pygame in /tmp/test-game using uv. The game is just a yellow ball with physics bouncing within a rotating red square, and the physics must work correctly. Make the game able to be run headless so tests can be run on the points to see if we've broken the rules of the simulation, the ball has fallen out, etc."
```
- Acceptance: Working Python game with headless mode and physics tests
- Validates: Coding agent capability, file creation, shell execution, test writing
- Live test validates: `/tmp/test-game` exists, `uv run python main.py --headless` exits 0, tests pass

### Framework Tasks

- [ ] Create `internal/agent/live_validation_test.go` with `//go:build live` tag
- [ ] Helper: start gateway daemon in test process
- [ ] Helper: run `ecl run "prompt"` and capture stdout + session ID
- [ ] Helper: read session events from disk by session ID
- [ ] Helper: assert tool calls from events (not mocks)
- [ ] Helper: assert notification exists in notification store
- [ ] Helper: assert job exists in job store
- [ ] Helper: assert files exist on disk
- [ ] Codify Test 1 (reminder → notification)
- [ ] Codify Test 2 (parallel research)
- [ ] Codify Test 3 (research → scheduled follow-up)
- [ ] Codify Test 4 (coding agent full project)
- [ ] Run all 4 tests, show results to user for validation

---

## Milestone 1: Fix What's Broken

Before building new features, fix what exists but doesn't work.

- [ ] Remove Scheduler, keep only JobExecutor
  - Reference: `docs/reference/openclaw-scheduling.md`
  - Audit: `docs/audit/eclaire-scheduling.md`
- [ ] Migrate heartbeat tasks to Jobs (kind: "every")
- [ ] Migrate cron entries to Jobs (kind: "cron")
- [ ] Keep BOOT.md as special startup job
- [ ] Delete dead code: HeartbeatTask.Once, BackgroundResult.OneShot, builtinAgent.Handle/Stream
- [ ] Fix context engine section ordering (use sort.Slice, not bubble sort)
- [ ] Fix context engine minimal mode whitelist (remove inappropriate sections)
- [ ] Subscribe NotificationStore to bus in Gateway
- [ ] Fix TUI digit-to-rune bugs (use fmt.Sprintf)
- [ ] Fix TUI markdown width caching

---

## Milestone 2: Main Session

- [ ] Create main session on Gateway startup (GetOrCreateMain)
  - Reference: `docs/reference/openclaw-sessions.md`
  - Audit: `docs/audit/eclaire-sessions.md`
- [ ] Main session survives gateway restarts (load from disk)
- [ ] Heartbeat/job results route to main session as system events
- [ ] TUI shows main session as always-accessible tab
- [ ] `ecl run` with no project context connects to main session

---

## Milestone 3: Project Sessions

- [ ] Project root detection from CWD (look for `.eclaire/`, `.git/`)
  - Reference: `docs/reference/openclaw-sessions.md`, `docs/reference/clawcode-session.md`
  - Audit: `docs/audit/eclaire-sessions.md`
- [ ] TUI passes CWD to gateway on connect
- [ ] Create or resume project session when connecting from project directory
- [ ] Project workspace layer loaded from `<project_root>/.eclaire/workspace/`
- [ ] Main session sees awareness events from project sessions

---

## Milestone 4: Unified Scheduling

- [ ] `eclaire_manage job_add` accepts all three schedule kinds (at/every/cron)
  - Reference: `docs/reference/openclaw-scheduling.md`
  - Audit: `docs/audit/eclaire-scheduling.md`
- [ ] `eclaire_manage job_remove`, `job_list`, `job_runs`, `job_run`
- [ ] `ecl job add/remove/list/runs/run` CLI
- [ ] Session target routing (main vs isolated)
- [ ] Startup catchup for missed recurring jobs
- [ ] Context message embedding at job creation time
- [ ] Max retry limit for recurring jobs
- [ ] Live test: Test 3 (research → scheduled follow-up) passes

---

## Milestone 5: Permissions

- [ ] Default PermissionMode changed from Allow to something that prompts
  - Reference: `docs/reference/openclaw-permissions.md`, `docs/reference/clawcode-permissions.md`
  - Audit: `docs/audit/eclaire-permissions.md`
- [ ] Config option for permission_mode in config.yaml
- [ ] Approval dialog wired end-to-end (TUI shows prompt, user responds)
- [ ] Session stores approved command patterns (not just tool names)
- [ ] "Allow once" / "allow for session" / "deny" options
- [ ] Background jobs use pre-approved patterns (cannot wait for interactive approval)
- [ ] Persistent sessions maintain approvals across resumptions

---

## Milestone 6: Notification System

- [ ] Cron/job completions create notifications
  - Reference: `docs/reference/openclaw-delivery.md`
  - Audit: `docs/audit/eclaire-notifications.md`
- [ ] Heartbeat alerts create notifications (not routine OK)
- [ ] TUI drains notifications on connect
- [ ] Notification panel or indicator in TUI
- [ ] `ecl notifications` CLI with filtering
- [ ] Live test: Test 1 (reminder → notification) passes

---

## Milestone 7: Agent Completeness

Each specialist agent must be as capable as the standalone tool it replaces.

- [ ] Coding agent: full pair programming capability (file creation, editing, shell, tests)
  - Live test: Test 4 (coding agent full project) passes
- [ ] Research agent: multi-source investigation, citation, synthesis
  - Live test: Test 2 (parallel research) passes
- [ ] Sysadmin agent: system monitoring, service management, log analysis
- [ ] Config agent: self-modification, agent creation, skill creation
- [ ] Orchestrator (Claire): delegation, conversation management, scope awareness

---

## Milestone 8: Self-Improvement

- [ ] Claire creates agents via eclaire_manage agent_create
- [ ] Claire creates skills via eclaire_manage skill_create
- [ ] Claire creates and schedules pipelines (flows)
- [ ] Claire modifies her own workspace files
- [ ] Self-modification writes to project `.eclaire/` when in project context
- [ ] Tool creation via interpreted tools or plugin system

---

## Milestone 9: TUI Polish

- [ ] Item-level key event handlers (KeyEventHandler interface)
  - Reference: `docs/reference/crush-tui.md`, `docs/reference/crush-chat.md`
  - Audit: `docs/audit/eclaire-tui.md`
- [ ] Lazy list rendering with item-aware scrolling
- [ ] Animation framework for tool spinners
- [ ] Custom glamour theme from style palette
- [ ] Mouse text selection (single/double/triple click, drag)
- [ ] Completions popup for files and commands
- [ ] Tab model: main + project + agent sessions simultaneously
- [ ] Test everything on a real TTY

---

## Milestone 10: 1.0

- [ ] Onboarding flow (first-run where Claire learns about her owner)
- [ ] Channel plugins (Signal, Telegram, etc.)
- [ ] Calendar tool (CalDAV)
- [ ] Conversation hydration tool
- [ ] Fediverse tool
- [ ] Memory dreaming (reference OpenClaw 3-phase system)
- [ ] MCP integration
- [ ] LSP integration
- [ ] Everything composable — validated by user creating new agents/tools/pipelines
