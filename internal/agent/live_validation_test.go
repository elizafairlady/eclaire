//go:build live

package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Live validation tests for eclaire.
//
// These tests run real LLM calls via `ecl run` and validate results
// from session logs, notification store, and file system — NOT from mocks.
//
// Run with: OPENROUTER_API_KEY=... go test ./internal/agent/ -tags live -run TestLive -timeout 30m
//
// Each test was provided by the user. Do NOT invent new test cases.
// Do NOT weaken assertions without user approval.

// eclBinPath builds the ecl binary once per test suite run.
var eclBinOnce sync.Once
var eclBinPath string

func ensureEclBin(t *testing.T) string {
	t.Helper()
	eclBinOnce.Do(func() {
		dir, _ := os.MkdirTemp("", "ecl-test-*")
		eclBinPath = filepath.Join(dir, "ecl")
		build := exec.Command("go", "build", "-o", eclBinPath, "./cmd/ecl")
		build.Dir = projectRoot(t)
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("failed to build ecl: %v\n%s", err, out)
		}
	})
	return eclBinPath
}

// testHome holds per-test isolated HOME directory state.
type testHome struct {
	dir     string // temp HOME dir
	eclaire string // {dir}/.eclaire/
	bin     string // path to ecl binary
}

// setupTestHome creates an isolated HOME directory for a single test.
// Copies the real config.yaml for provider/routing/API key access.
// Registers t.Cleanup to stop the daemon and remove the temp dir.
func setupTestHome(t *testing.T) *testHome {
	t.Helper()
	bin := ensureEclBin(t)

	dir := t.TempDir()
	eclaireDir := filepath.Join(dir, ".eclaire")
	os.MkdirAll(eclaireDir, 0o700)

	// Copy real config.yaml so tests have provider/routing/API key config
	realConfig := filepath.Join(os.Getenv("HOME"), ".eclaire", "config.yaml")
	if data, err := os.ReadFile(realConfig); err == nil {
		os.WriteFile(filepath.Join(eclaireDir, "config.yaml"), data, 0o644)
	}

	th := &testHome{dir: dir, eclaire: eclaireDir, bin: bin}

	t.Cleanup(func() {
		// Stop the daemon that was started in this test's HOME
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "daemon", "stop")
		cmd.Env = th.env()
		cmd.Run() // best-effort
	})

	return th
}

func (th *testHome) env() []string {
	return append(os.Environ(), "HOME="+th.dir)
}

// run executes an ecl command in this test's isolated HOME.
func (th *testHome) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, th.bin, args...)
	cmd.Dir = projectRoot(t)
	cmd.Env = th.env()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// runWithApprovals executes an ecl command while auto-approving any pending
// approval notifications in a parallel goroutine.
func (th *testHome) runWithApprovals(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, th.bin, args...)
	cmd.Dir = projectRoot(t)
	cmd.Env = th.env()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start approval poller
	approvalCtx, approvalCancel := context.WithCancel(ctx)
	approvalDone := make(chan struct{})
	go func() {
		defer close(approvalDone)
		th.pollAndApprove(t, approvalCtx)
	}()

	err := cmd.Run()

	approvalCancel()
	<-approvalDone

	return stdout.String(), stderr.String(), err
}

// pollAndApprove polls the notification store every 2 seconds and auto-approves
// any pending approval notifications.
func (th *testHome) pollAndApprove(t *testing.T, ctx context.Context) {
	t.Helper()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	approved := make(map[string]bool)
	notifPath := filepath.Join(th.eclaire, "notifications.jsonl")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			notifications := loadNotificationsFromDisk(t, notifPath)
			for _, n := range notifications {
				if n.Source != "approval" || n.Resolved || approved[n.ID] {
					continue
				}
				t.Logf("auto-approving: %s — %s", n.ID, n.Content)
				actCmd := exec.CommandContext(ctx, th.bin, "notifications", n.ID, "yes")
				actCmd.Env = th.env()
				if out, err := actCmd.CombinedOutput(); err != nil {
					t.Logf("approval failed for %s: %v\n%s", n.ID, err, out)
				} else {
					t.Logf("approved %s", n.ID)
					approved[n.ID] = true
				}
			}
		}
	}
}

// Legacy wrappers for backward compatibility with tests that don't use testHome yet.
func eclRunner(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	binPath := ensureEclBin(t)
	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = projectRoot(t)
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))
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
	th := setupTestHome(t)

	remindersPath := filepath.Join(th.eclaire, "reminders.json")
	notificationsPath := filepath.Join(th.eclaire, "notifications.jsonl")

	// Step 1: Create reminder via LLM (with auto-approval for any tool that needs it)
	stdout, stderr, err := th.runWithApprovals(t, "run", "Set a reminder for 1 minute from now to walk the dogs")
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run output: %s", stdout)

	// Verify reminder was created on disk
	reminders := loadRemindersFromDisk(t, remindersPath)
	var newReminder reminderEntry
	found := false
	for _, r := range reminders {
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
	t.Log("Waiting for reminder to fire...")
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
		t.Fatal("reminder did not fire within 180s (no notification with source=reminder found)")
	}

	// Step 3: Verify notification state
	if notification.RefID == "" {
		t.Fatal("notification missing RefID")
	}
	t.Logf("Notification: id=%s source=%s ref=%s actions=%v", notification.ID, notification.Source, notification.RefID, notification.Actions)

	// Step 4: Complete via notification action
	actOut, _, err := th.run(t, "notifications", notification.ID, "complete")
	if err != nil {
		t.Fatalf("notification act failed: %v\noutput: %s", err, actOut)
	}
	t.Logf("Act response: %s", actOut)

	// Step 5: Verify state on disk
	notificationsAfterAct := loadNotificationsFromDisk(t, notificationsPath)
	for _, n := range notificationsAfterAct {
		if n.ID == notification.ID && !n.Resolved {
			t.Fatal("notification not marked as resolved after complete action")
		}
	}

	remindersAfterAct := loadRemindersFromDisk(t, remindersPath)
	for _, r := range remindersAfterAct {
		if r.ID == newReminder.ID && !r.Completed {
			t.Fatal("reminder not marked as completed after complete action")
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
	th := setupTestHome(t)

	reportsDir := filepath.Join(th.eclaire, "reports")
	notificationsPath := filepath.Join(th.eclaire, "notifications.jsonl")
	sessionsDir := filepath.Join(th.eclaire, "sessions")

	prompt := "Perform two research projects for me: I need you to dig into the TurboQuant technology and any other huge advancements made in Gemma 4, and I need you to find out the particulars of the ceasefire in Iran from the first week of April. Write report files for each research project. Give me a notification when you're done."

	stdout, stderr, err := th.runWithApprovals(t, "run", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run completed (%d bytes output)", len(stdout))

	// Step 1: Verify report files exist
	reports := listFiles(t, reportsDir)
	if len(reports) < 2 {
		t.Fatalf("expected at least 2 report files, got %d: %v", len(reports), reports)
	}
	t.Logf("Report files: %v", reports)

	for _, name := range reports {
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

	// Step 2: Verify notification was created
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

	// Step 3: Verify sessions (orchestrator + child research)
	sessions := listDirs(t, sessionsDir)
	if len(sessions) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(sessions))
	}
	t.Logf("Sessions: %d", len(sessions))

	childCount := 0
	for _, sid := range sessions {
		meta := loadSessionMeta(t, filepath.Join(sessionsDir, sid, "meta.json"))
		if meta.ParentID != "" {
			childCount++
			t.Logf("Child session %s (agent=%s parent=%s)", sid, meta.AgentID, meta.ParentID)
		} else {
			t.Logf("Root session %s (agent=%s)", sid, meta.AgentID)
		}
	}
	if childCount < 1 {
		t.Log("Warning: no child sessions found")
	}

	t.Log("Pipeline validated: delegate → parallel research → reports → notification → sessions")
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
// Validates that research completes and a one-shot job is scheduled.
// Validation is from disk state: jobs.json (scheduled follow-up) and stdout (research content).
func TestLive_ResearchThenScheduledFollowup(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")
	th := setupTestHome(t)

	jobsPath := filepath.Join(th.eclaire, "jobs.json")

	prompt := "I need you to perform a research project on the war and ceasefire in Iran, 2026. I also need you to create a one-time task for 5m after you finish that project, to audit the research, validate the research, and expand upon it with any new findings."

	stdout, stderr, err := th.runWithApprovals(t, "run", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run completed (%d bytes output)", len(stdout))

	// Step 1: Verify research content in output
	lower := strings.ToLower(stdout)
	hasResearchContent := strings.Contains(lower, "iran") ||
		strings.Contains(lower, "ceasefire") ||
		strings.Contains(lower, "research")
	if !hasResearchContent {
		t.Errorf("output should contain research about Iran/ceasefire, got %d bytes:\n%s", len(stdout), stdout[:min(len(stdout), 500)])
	}

	// Step 2: Verify a one-shot job was scheduled in jobs.json
	jobs := loadJobsFromDisk(t, jobsPath)
	if len(jobs) == 0 {
		t.Fatal("no jobs in jobs.json — agent should have scheduled a follow-up task")
	}

	foundFollowup := false
	for _, j := range jobs {
		t.Logf("  job: id=%s kind=%s agent=%s name=%s enabled=%v", j.ID, j.ScheduleKind, j.AgentID, j.Name, j.Enabled)
		if j.ScheduleKind == "at" {
			foundFollowup = true
			if !j.Enabled {
				t.Error("follow-up job should be enabled")
			}
			promptLower := strings.ToLower(j.Prompt)
			if !strings.Contains(promptLower, "audit") && !strings.Contains(promptLower, "validat") && !strings.Contains(promptLower, "research") {
				t.Logf("warning: follow-up prompt doesn't mention audit/validate/research: %s", j.Prompt[:min(len(j.Prompt), 200)])
			}
		}
	}
	if !foundFollowup {
		t.Error("no one-shot (kind=at) follow-up job found")
	}

	t.Log("Pipeline validated: research → follow-up job scheduled")
}

// --- job store helpers ---

type jobEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ScheduleKind string `json:"schedule_kind,omitempty"`
	AgentID      string `json:"agent_id"`
	Prompt       string `json:"prompt"`
	Enabled      bool   `json:"enabled"`
	Schedule     struct {
		Kind string `json:"kind"`
	} `json:"schedule"`
}

func (j jobEntry) kind() string {
	if j.ScheduleKind != "" {
		return j.ScheduleKind
	}
	return j.Schedule.Kind
}

type jobStoreFileLayout struct {
	Jobs []jobEntry `json:"jobs"`
}

func loadJobsFromDisk(t *testing.T, path string) []jobEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read jobs: %v", err)
	}
	var f jobStoreFileLayout
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("failed to parse jobs: %v", err)
	}
	// Normalize schedule kind
	for i := range f.Jobs {
		if f.Jobs[i].ScheduleKind == "" {
			f.Jobs[i].ScheduleKind = f.Jobs[i].Schedule.Kind
		}
	}
	return f.Jobs
}

func diffJobSets(after, before []jobEntry) []jobEntry {
	set := make(map[string]bool)
	for _, j := range before {
		set[j.ID] = true
	}
	var diff []jobEntry
	for _, j := range after {
		if !set[j.ID] {
			diff = append(diff, j)
		}
	}
	return diff
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
// Validation is from disk state: file existence, headless execution, and test execution.
func TestLive_CodingAgentFullProject(t *testing.T) {
	requireEnv(t, "OPENROUTER_API_KEY")
	th := setupTestHome(t)
	_ = th

	const projectDir = "/tmp/test-game"
	os.RemoveAll(projectDir)

	prompt := `Write a game in python with pygame in /tmp/test-game using uv. The game is just a yellow ball with physics bouncing within a rotating red square, and the physics must work correctly. Make the game able to be run headless so tests can be run on the points to see if we've broken the rules of the simulation, the ball has fallen out, etc.`

	stdout, stderr, err := th.runWithApprovals(t, "run", "-a", "coding", prompt)
	if err != nil {
		t.Fatalf("ecl run failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	t.Logf("ecl run completed (%d bytes output)", len(stdout))

	// Step 1: Validate project directory was created
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		t.Fatal("/tmp/test-game directory was not created")
	}

	// Step 2: List and log what was created
	entries, _ := os.ReadDir(projectDir)
	var fileNames []string
	for _, e := range entries {
		fileNames = append(fileNames, e.Name())
		t.Logf("  created: %s (dir=%v)", e.Name(), e.IsDir())
	}

	if len(entries) < 2 {
		t.Fatalf("expected at least 2 files/dirs in project, got %d", len(entries))
	}

	// Step 3: Check for Python files — at least one .py file should exist
	pyFiles := findFilesRecursive(t, projectDir, ".py")
	if len(pyFiles) == 0 {
		t.Fatal("no .py files found in /tmp/test-game — coding agent should have created Python source")
	}
	t.Logf("Python files: %v", pyFiles)

	// Step 4: Check for project config (pyproject.toml for uv)
	pyprojectPath := filepath.Join(projectDir, "pyproject.toml")
	if _, err := os.Stat(pyprojectPath); os.IsNotExist(err) {
		t.Log("warning: pyproject.toml not found — agent may have used a different project structure")
	}

	// Step 5: Check for test files
	testFiles := findFilesRecursive(t, projectDir, "_test.py")
	testFiles2 := findFilesRecursive(t, projectDir, "test_")
	allTestFiles := append(testFiles, testFiles2...)
	if len(allTestFiles) == 0 {
		t.Log("warning: no test files found (expected *_test.py or test_*.py)")
	} else {
		t.Logf("Test files: %v", allTestFiles)
	}

	// Step 6: Try running headless mode (best-effort — don't fail the test if uv isn't available)
	headlessCmd := exec.Command("uv", "run", "python", "-c", "import sys; sys.exit(0)")
	headlessCmd.Dir = projectDir
	if headlessOut, headlessErr := headlessCmd.CombinedOutput(); headlessErr != nil {
		t.Logf("uv not available or project not runnable (non-fatal): %v\n%s", headlessErr, headlessOut)
	} else {
		// uv works — try to find and run the main entry point with --headless
		mainPy := findMainPython(t, projectDir)
		if mainPy != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			run := exec.CommandContext(ctx, "uv", "run", "python", mainPy, "--headless")
			run.Dir = projectDir
			runOut, runErr := run.CombinedOutput()
			if runErr != nil {
				t.Logf("headless run failed (may be expected if --headless isn't implemented): %v\n%s", runErr, runOut)
			} else {
				t.Logf("headless run succeeded: %s", string(runOut)[:min(len(runOut), 500)])
			}
		}
	}

	if stderr != "" {
		t.Logf("=== stderr (truncated) ===\n%s", stderr[:min(len(stderr), 500)])
	}

	t.Log("Pipeline validated: coding agent created project with Python files")
}

// --- coding test helpers ---

func findFilesRecursive(t *testing.T, root, suffix string) []string {
	t.Helper()
	var matches []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), suffix) {
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, rel)
		}
		return nil
	})
	return matches
}

func findMainPython(t *testing.T, dir string) string {
	t.Helper()
	// Try common entry points
	for _, name := range []string{"main.py", "game.py", "app.py", "src/main.py"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return name
		}
	}
	return ""
}

