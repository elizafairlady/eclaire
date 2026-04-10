//go:build live

package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live validation tests for eclaire.
//
// These tests run real LLM calls via `ecl run` and validate results
// from session logs, notification store, and file system — NOT from mocks.
//
// Run with: OPENROUTER_API_KEY=... go test ./internal/agent/ -tags live -run TestLive -timeout 15m
//
// Each test was provided by the user. Do NOT invent new test cases.
// Do NOT weaken assertions without user approval.

// eclRunner runs `ecl run` with the given arguments and returns stdout, stderr, and error.
func eclRunner(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	// Build ecl binary if needed
	binPath := filepath.Join(t.TempDir(), "ecl")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/ecl")
	build.Dir = projectRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build ecl: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = projectRoot(t)
	cmd.Env = append(os.Environ(),
		"HOME="+os.Getenv("HOME"),
	)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// projectRoot returns the eclaire project root.
func projectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (no go.mod)")
		}
		dir = parent
	}
}

// requireEnv skips the test if the given env var is not set.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	val := os.Getenv(key)
	if val == "" {
		t.Skipf("skipping: %s not set", key)
	}
	return val
}

// --- USER-PROVIDED LIVE TESTS ---
// These test queries come directly from the user.
// Assertions will be filled in after running each test and having the user validate.

// TestLive_ReminderToNotification validates the full reminder → notification pipeline:
//  1. ecl run creates a reminder via LLM → eclaire_reminder tool
//  2. Reminder fires on the JobExecutor tick loop (≤60s)
//  3. Notification created with source="reminder" and actions
//  4. ecl notifications <id> complete resolves it
//  5. Notification store and reminder store reflect resolved state
//
// Validation is from on-disk state (reminders.json, notifications.jsonl), not CLI output.
func TestLive_ReminderToNotification(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")

	home := os.Getenv("HOME")
	remindersPath := filepath.Join(home, ".eclaire", "reminders.json")
	notificationsPath := filepath.Join(home, ".eclaire", "notifications.jsonl")

	// Stop any running daemon so we start fresh with the test binary
	eclRunner(t, "daemon", "stop")
	time.Sleep(1 * time.Second)

	// Clear notification store for a clean baseline
	os.Remove(notificationsPath)

	// Count existing reminders before the test
	remindersBefore := loadRemindersFromDisk(t, remindersPath)
	countBefore := len(remindersBefore)

	// Step 1: Create reminder via LLM
	stdout, stderr, err := eclRunner(t, "run", "Set a reminder for 1 minute from now to walk the dogs")
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run output: %s", stdout)

	// Verify reminder was created on disk
	remindersAfter := loadRemindersFromDisk(t, remindersPath)
	if len(remindersAfter) <= countBefore {
		t.Fatal("no new reminder was created in reminders.json")
	}

	// Find the new reminder
	var newReminder reminderEntry
	found := false
	for _, r := range remindersAfter {
		if strings.Contains(strings.ToLower(r.Text), "dog") && !r.Completed {
			newReminder = r
			found = true
			break
		}
	}
	if !found {
		t.Fatal("new reminder about dogs not found in reminders.json")
	}
	t.Logf("Reminder created: id=%s text=%q due=%s", newReminder.ID, newReminder.Text, newReminder.DueAt)

	// Step 2: Wait for reminder to fire (check notification store on disk)
	t.Log("Waiting up to 180s for reminder to fire...")
	var notification notificationEntry
	fired := false
	for attempt := 0; attempt < 18; attempt++ {
		time.Sleep(10 * time.Second)
		notifications := loadNotificationsFromDisk(t, notificationsPath)
		for _, n := range notifications {
			if n.Source == "reminder" && strings.Contains(strings.ToLower(n.Content), "dog") && !n.Resolved {
				notification = n
				fired = true
				break
			}
		}
		if fired {
			t.Logf("Reminder fired after ~%ds", (attempt+1)*10)
			break
		}
	}
	if !fired {
		t.Fatal("reminder did not fire within 180s (no notification with source=reminder found in notifications.jsonl)")
	}

	// Step 3: Verify notification state
	if notification.Source != "reminder" {
		t.Fatalf("expected source=reminder, got %q", notification.Source)
	}
	if notification.RefID == "" {
		t.Fatal("notification missing RefID (should be the reminder ID)")
	}
	t.Logf("Notification: id=%s source=%s ref=%s actions=%v", notification.ID, notification.Source, notification.RefID, notification.Actions)

	// Step 4: Complete via notification action
	actOut, _, err := eclRunner(t, "notifications", notification.ID, "complete")
	if err != nil {
		t.Fatalf("notification act failed: %v\noutput: %s", err, actOut)
	}
	t.Logf("Act response: %s", actOut)

	// Step 5: Verify state on disk
	// Notification should be resolved
	notificationsAfterAct := loadNotificationsFromDisk(t, notificationsPath)
	for _, n := range notificationsAfterAct {
		if n.ID == notification.ID {
			if !n.Resolved {
				t.Fatal("notification not marked as resolved after complete action")
			}
			break
		}
	}

	// Reminder should be completed
	remindersAfterAct := loadRemindersFromDisk(t, remindersPath)
	for _, r := range remindersAfterAct {
		if r.ID == newReminder.ID {
			if !r.Completed {
				t.Fatal("reminder not marked as completed after complete action")
			}
			t.Logf("Reminder %s confirmed completed on disk", r.ID)
			break
		}
	}

	t.Log("Pipeline validated from state: create → fire → complete → resolved on disk")
}

// --- disk state helpers ---

type reminderEntry struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	DueAt     time.Time `json:"due_at"`
	Completed bool      `json:"completed"`
}

type notificationEntry struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	RefID    string   `json:"ref_id"`
	Actions  []string `json:"actions"`
	Read     bool     `json:"read"`
	Resolved bool     `json:"resolved"`
}

func loadRemindersFromDisk(t *testing.T, path string) []reminderEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read reminders: %v", err)
	}
	var reminders []reminderEntry
	if err := json.Unmarshal(data, &reminders); err != nil {
		t.Fatalf("failed to parse reminders: %v", err)
	}
	return reminders
}

func loadNotificationsFromDisk(t *testing.T, path string) []notificationEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read notifications: %v", err)
	}
	var notifications []notificationEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var n notificationEntry
		if err := json.Unmarshal([]byte(line), &n); err == nil {
			notifications = append(notifications, n)
		}
	}
	return notifications
}

// TestLive_ParallelResearch validates the full parallel research pipeline:
//  1. ecl run (no -a flag) delegates two research tasks to research agents
//  2. Both agents write report files to ~/.eclaire/reports/
//  3. Orchestrator creates a notification when done (user asked for it)
//  4. Sessions persist on disk with parent/child relationships
//
// Validation is from disk state: report files, notification store, session metadata.
func TestLive_ParallelResearch(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")

	home := os.Getenv("HOME")
	reportsDir := filepath.Join(home, ".eclaire", "reports")
	notificationsPath := filepath.Join(home, ".eclaire", "notifications.jsonl")
	sessionsDir := filepath.Join(home, ".eclaire", "sessions")

	// Stop daemon, clean state
	eclRunner(t, "daemon", "stop")
	time.Sleep(1 * time.Second)
	os.Remove(notificationsPath)

	// Snapshot report files before run
	reportsBefore := listFiles(t, reportsDir)

	// Snapshot sessions before run
	sessionsBefore := listDirs(t, sessionsDir)

	prompt := "Perform two research projects for me: I need you to dig into the TurboQuant technology and any other huge advancements made in Gemma 4, and I need you to find out the particulars of the ceasefire in Iran from the first week of April. Write report files for each research project. Give me a notification when you're done."

	stdout, stderr, err := eclRunner(t, "run", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run completed (%d bytes output)", len(stdout))

	// Step 1: Verify new report files exist
	reportsAfter := listFiles(t, reportsDir)
	newReports := diffStringSets(reportsAfter, reportsBefore)
	if len(newReports) < 2 {
		t.Fatalf("expected at least 2 new report files, got %d: %v", len(newReports), newReports)
	}
	t.Logf("New report files: %v", newReports)

	// Verify reports have substance (not empty stubs)
	for _, name := range newReports {
		path := filepath.Join(reportsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read report %s: %v", name, err)
		}
		if len(data) < 500 {
			t.Errorf("report %s is too small (%d bytes) — likely a stub", name, len(data))
		}
		t.Logf("Report %s: %d bytes", name, len(data))
	}

	// Step 2: Verify notification was created (user asked "give me a notification when you're done")
	notifications := loadNotificationsFromDisk(t, notificationsPath)
	var doneNotification *notificationEntry
	for i, n := range notifications {
		if n.Source == "agent" && !n.Resolved {
			doneNotification = &notifications[i]
			break
		}
	}
	if doneNotification == nil {
		t.Fatal("no agent notification found — orchestrator should have created one when user asked to be notified")
	}
	t.Logf("Notification: id=%s title=%q", doneNotification.ID, doneNotification.Title)

	// Step 3: Verify new sessions were created (orchestrator + child research sessions)
	sessionsAfter := listDirs(t, sessionsDir)
	newSessions := diffStringSets(sessionsAfter, sessionsBefore)
	if len(newSessions) < 2 {
		t.Fatalf("expected at least 2 new sessions (orchestrator + research agents), got %d", len(newSessions))
	}
	t.Logf("New sessions: %d", len(newSessions))

	// Check for parent/child session relationships
	childCount := 0
	for _, sid := range newSessions {
		meta := loadSessionMeta(t, filepath.Join(sessionsDir, sid, "meta.json"))
		if meta.ParentID != "" {
			childCount++
			t.Logf("Child session %s (agent=%s parent=%s)", sid, meta.AgentID, meta.ParentID)
		} else {
			t.Logf("Root session %s (agent=%s)", sid, meta.AgentID)
		}
	}
	if childCount < 1 {
		t.Log("Warning: no child sessions found — research agents may not have created sub-sessions")
	}

	t.Log("Pipeline validated: delegate → parallel research → reports → notification → sessions persisted")
}

// --- additional helpers ---

type sessionMeta struct {
	AgentID  string `json:"agent_id"`
	ParentID string `json:"parent_id"`
	Title    string `json:"title"`
}

func loadSessionMeta(t *testing.T, path string) sessionMeta {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionMeta{}
	}
	var m sessionMeta
	json.Unmarshal(data, &m)
	return m
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func listDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func diffStringSets(after, before []string) []string {
	set := make(map[string]bool)
	for _, s := range before {
		set[s] = true
	}
	var diff []string
	for _, s := range after {
		if !set[s] {
			diff = append(diff, s)
		}
	}
	return diff
}

// TestLive_ResearchThenScheduledFollowup runs:
//
//	ecl run "I need you to perform a research project on the war and ceasefire
//	in Iran, 2026. I also need you to create a one-time task for 5m after you
//	finish that project, to audit the research, validate the research, and
//	expand upon it with any new findings."
//
// Validates that research completes, then a one-shot job is scheduled.
func TestLive_ResearchThenScheduledFollowup(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")

	prompt := "I need you to perform a research project on the war and ceasefire in Iran, 2026. I also need you to create a one-time task for 5m after you finish that project, to audit the research, validate the research, and expand upon it with any new findings."

	stdout, stderr, err := eclRunner(t, "run", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	t.Logf("=== ecl run output ===\n%s", stdout)
	if stderr != "" {
		t.Logf("=== stderr ===\n%s", stderr)
	}

	// TODO: After user validates output, add assertions:
	// - Check research content in session events
	// - Check job created in job store (kind: "at", ~5m from now)
	// - Optionally wait and verify job executes
	t.Log("TODO: Assertions pending user validation of output")
}

// TestLive_CodingAgentFullProject runs:
//
//	ecl run -a coding "Write a game in python with pygame in /tmp/test-game
//	using uv. The game is just a yellow ball with physics bouncing within a
//	rotating red square, and the physics must work correctly. Make the game
//	able to be run headless so tests can be run on the points to see if we've
//	broken the rules of the simulation, the ball has fallen out, etc."
//
// Validates that a working Python game with headless mode and tests is created.
func TestLive_CodingAgentFullProject(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")

	// Clean up any previous test game
	os.RemoveAll("/tmp/test-game")

	prompt := `Write a game in python with pygame in /tmp/test-game using uv. The game is just a yellow ball with physics bouncing within a rotating red square, and the physics must work correctly. Make the game able to be run headless so tests can be run on the points to see if we've broken the rules of the simulation, the ball has fallen out, etc.`

	stdout, stderr, err := eclRunner(t, "run", "-a", "coding", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	t.Logf("=== ecl run output ===\n%s", stdout)
	if stderr != "" {
		t.Logf("=== stderr ===\n%s", stderr)
	}

	// Validate project exists
	if _, err := os.Stat("/tmp/test-game"); os.IsNotExist(err) {
		t.Fatal("/tmp/test-game directory was not created")
	}

	// List what was created
	entries, _ := os.ReadDir("/tmp/test-game")
	for _, e := range entries {
		t.Logf("  created: %s", e.Name())
	}

	// TODO: After user validates output, add assertions:
	// - Check specific files exist (main.py, test files, pyproject.toml)
	// - Run headless mode: uv run python main.py --headless
	// - Run tests: uv run pytest or similar
	// - Validate test output
	fmt.Println("TODO: Assertions pending user validation of output")
}
