# eclaire Tests Audit

Audited: 2026-04-09

## Stats

- 379 test functions across ~52 test files
- All pass (`go test ./...` exit 0)
- `internal/agent/` tests take ~227s (includes live LLM tests)
- 3 live LLM tests in `live_phase_f_test.go`

## What's Solid

### Unit tests for parsing, data transformation, logic (8/10)
- Config YAML parsing, path handling, env var expansion
- Session JSONL event serialization/deserialization
- Message reconstruction from events
- Cron field parsing
- Task registry operations
- Binding matching (project, directory, glob)
- Context budget calculations
- Reminder/todo persistence

### Tool tests with real I/O (6/10)
- `TestLsTool`, `TestViewTool` — real temp directories, verify output
- `TestPatchTool` — validates hunk parsing and line insertion
- `TestReminderStore` — actual JSON persistence
- `TestMultiEditTool` — confirms file mutations

### Integration tests (7/10)
- Runner event emission pipeline
- Sub-agent delegation with event forwarding
- Session persistence round-trips
- Bus pub/sub delivery

## What's Tautological

### Behavioral tests with mocks (2/10)

The mock behavioral tests script LLM responses then assert those responses happened:

```go
// Example: TestBehavior_DirectRead
// Step 1: Tell mock to call read tool
{ToolCalls: []testutil.MockToolCall{
    {Name: "read", ID: "tc-1", Input: map[string]any{"path": target}},
}}
// Step 2: Assert read tool was called
if !hasEvent(events, agent.EventToolCall, func(ev) bool {
    return ev.ToolName == "read"
}) { t.Error(...) }
```

This proves: IF model returns a read tool call, THEN runner executes it.
This does NOT prove: Claude would actually call read for this task.

**~30+ tests in behavioral_test.go and mock_parity_test.go follow this pattern.**

### Stream event tests (2/10)

```go
if !hasEvent(events, agent.EventTextDelta, nil) {
    t.Error("should have text_delta event")
}
```

Asserts event list contains an entry. Would pass if EventTextDelta was defined anywhere.

## What's Missing

### No tests for critical features

| Feature | Tests | Gap |
|---------|-------|-----|
| Main session persistence | 0 | No test that main session survives restart |
| Project root detection | 0 | No test for .eclaire/.git detection |
| Unified scheduling (at/every) | 0 | No test for one-shot or interval jobs |
| Approval prompting | 0 | No test that permission dialog appears |
| Notification delivery | 0 | No test that notifications accumulate |
| Background job policy | 0 | No test that cron rejects interactive approval |
| Session resume across restart | 0 | No test that creates session, stops gateway, restarts, resumes |
| TUI on real TTY | 0 | No test with actual terminal |
| Conversation hydration | 0 | No test for context enrichment |
| One-shot task creation | 0 | No test for "do this in 30 minutes" |

### No error path tests

- Email tool: only tests deserialization, never IMAP connection failure
- RSS tool: mocks HTTP, never tests real feed parsing
- Gateway: never tests socket binding failure
- Jobs: never tests command execution failure

### Live LLM tests too loose

```go
// live_phase_f_test.go
lower := strings.ToLower(result.Content)
if !strings.Contains(lower, "deployment") && !strings.Contains(lower, "overdue") && !strings.Contains(lower, "review") {
    t.Errorf(...)
}
```

Passes if ANY of 3 keywords appear. Claude could say "your reminder" and fail. Or say something completely unrelated that happens to contain "review".

## What Needs to Change

1. **Delete mock behavioral tests** — They test nothing real. Replace with live LLM tests.
2. **Live tests from user** — User provides `ecl run` queries and expected behavior. Codify into tests.
3. **Build tag gating** — `//go:build live` for all LLM tests. `go test ./...` runs only mock/unit tests.
4. **Validate from session logs** — Read session events from disk, not from mock responses.
5. **Test error paths** — Add failure tests for every external integration.
6. **Test session lifecycle** — Create, persist, restart, resume, verify.
7. **Tighten assertions** — Replace loose keyword checks with structural validation.

## Test File Inventory

```
internal/agent/
  behavioral_test.go          Mock behavioral tests (tautological)
  behavioral_phase_f_test.go  Mock behavioral tests (tautological)
  behavioral_scheduling_test.go  Mock scheduling tests
  live_phase_f_test.go        3 live LLM tests (loose assertions)
  mock_parity_test.go         Mock parity tests (tautological)
  integration_test.go         Runner integration (solid)
  runner_test.go              Runner unit tests (solid)
  context_test.go             Context budget (solid)
  context_engine_test.go      Context engine (solid)
  workspace_test.go           Workspace loading (solid)
  scheduler_test.go           Scheduler due checks (solid)
  skills_test.go              Skill loading (solid)
  flow_test.go                Flow execution (solid)
  task_test.go                Task lifecycle (solid)
  loop_test.go                Loop detection (solid)
  binding_test.go             Binding matching (solid)
  loader_test.go              Agent loading (solid)
  registry_test.go            Registry resolution (solid)
  builtin_test.go             Built-in agent validation (solid)
  compaction_test.go          Compaction logic (solid)
  jobstore_test.go            Job persistence (solid)
  jobexec_test.go             Job execution (solid)
  runlog_test.go              Run log I/O (solid)
  notifications_test.go       Notification store (solid)

internal/tool/
  ls_test.go, view_test.go, patch_test.go, multiedit_test.go  (solid)
  memory_test.go, reminder_test.go, todos_test.go  (solid)
  rss_test.go, briefing_test.go, email_test.go  (missing error paths)
  manage_test.go, agent_test.go  (solid)
  jobs_test.go, task_status_test.go  (solid)
  registry_test.go, permission_test.go  (solid)

internal/persist/
  session_test.go, convert_test.go  (solid)

internal/gateway/
  gateway_test.go, protocol_test.go  (basic but solid)

internal/config/
  config_test.go, store_test.go  (solid)

internal/bus/
  bus_test.go  (solid)

internal/channel/
  channel_test.go  (solid)

internal/hook/
  hook_test.go  (solid)

internal/ui/
  app_test.go, markdown_test.go  (basic)
  dialog/overlay_test.go  (solid)
```
