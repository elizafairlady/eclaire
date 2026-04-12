package testutil

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/config"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/provider"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// NewLiveTestEnv creates a test environment backed by a real LLM provider.
// Skips the test if OPENROUTER_API_KEY is not set.
func NewLiveTestEnv(t *testing.T, dir string) *TestEnv {
	t.Helper()
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set, skipping live test")
	}

	model := os.Getenv("ECLAIRE_TEST_MODEL")
	if model == "" {
		model = "anthropic/claude-sonnet-4"
	}

	logger := slog.Default()
	msgBus := bus.New()
	registry := agent.NewRegistry()

	sessionDir := dir + "/sessions"
	agentsDir := dir + "/agents"
	skillsDir := dir + "/skills"
	workspaceDir := dir + "/workspace"
	cronPath := dir + "/cron.yaml"

	os.MkdirAll(sessionDir, 0o700)
	os.MkdirAll(agentsDir, 0o700)
	os.MkdirAll(skillsDir, 0o700)
	os.MkdirAll(workspaceDir, 0o700)

	sessionStore := persist.NewSessionStore(sessionDir)

	// Build real provider config
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"openrouter": {
				Type:   "openrouter",
				APIKey: apiKey,
			},
		},
		Routing: map[string][]config.RouteEntry{
			"simple": {{
				Provider:      "openrouter",
				Model:         model,
				Priority:      1,
				ContextWindow: 200000,
			}},
			"complex": {{
				Provider:      "openrouter",
				Model:         model,
				Priority:      1,
				ContextWindow: 200000,
			}},
		},
	}

	router, err := provider.NewRouter(cfg, logger)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	toolReg := tool.NewRegistry()
	toolReg.Register(tool.ShellTool())
	toolReg.Register(tool.ReadTool())
	toolReg.Register(tool.WriteTool())
	toolReg.Register(tool.EditTool())
	toolReg.Register(tool.MultiEditTool())
	toolReg.Register(tool.ViewTool())
	toolReg.Register(tool.GlobTool())
	toolReg.Register(tool.GrepTool())
	toolReg.Register(tool.LsTool())
	toolReg.Register(tool.PatchTool())
	toolReg.Register(tool.FetchTool())
	toolReg.Register(tool.DownloadTool())
	toolReg.Register(tool.SearchTool())
	toolReg.Register(tool.RSSFeedTool())
	toolReg.Register(tool.JobOutputTool())
	toolReg.Register(tool.JobKillTool())
	toolReg.Register(tool.TodosTool())
	toolReg.Register(tool.MemoryWriteTool(workspaceDir))
	toolReg.Register(tool.MemoryReadTool(workspaceDir))

	// Reminder + briefing tools
	reminderStore := tool.NewReminderStore(dir + "/reminders.json")
	toolReg.Register(tool.ReminderTool(reminderStore))
	toolReg.Register(tool.BriefingTool(tool.BriefingDeps{
		Reminders:    reminderStore,
		WorkspaceDir: workspaceDir,
		BriefingsDir: workspaceDir + "/briefings",
	}))

	for _, a := range agent.BuiltinAgents() {
		registry.Register(a)
	}

	// Workspace loader + context engine + skills
	wsLoader := agent.NewWorkspaceLoader(workspaceDir, agentsDir, "")
	skillLoader := agent.NewSkillLoader(skillsDir, agentsDir, "")
	contextEngine := agent.NewContextEngine(router, wsLoader, skillLoader)

	// Permission checker
	permChecker := tool.NewPermissionChecker(toolReg)

	runner := &agent.Runner{
		Router:        router,
		Tools:         toolReg,
		Sessions:      sessionStore,
		Bus:           msgBus,
		Logger:        logger,
		Registry:      registry,
		Workspaces:    wsLoader,
		ContextEngine: contextEngine,
		PermChecker:   permChecker,
		WorkspaceRoot: dir,
		EclaireDir:    dir,
	}

	toolReg.Register(tool.AgentTool(tool.SubAgentDeps{
		RunSubAgent: runner.RunSubAgent,
		Bus:         msgBus,
		Logger:      logger,
	}))
	toolReg.Register(tool.TaskStatusTool(sessionStore))

	// Job store for scheduling
	jobStore, _ := agent.NewJobStore(dir + "/jobs.json")

	// eclaire_manage tool
	toolReg.Register(tool.ManageTool(tool.ManageDeps{
		AgentsDir: agentsDir,
		SkillsDir: skillsDir,
		CronPath:  cronPath,
		Reload:    func() tool.ReloadResult { return tool.ReloadResult{} },
		CronList:  func() []tool.CronEntry { return nil },
		AgentList: func() []tool.AgentInfo {
			infos := registry.All()
			out := make([]tool.AgentInfo, len(infos))
			for i, a := range infos {
				out[i] = tool.AgentInfo{ID: a.ID, Name: a.Name, Description: a.Description, Role: string(a.Role), BuiltIn: a.BuiltIn}
			}
			return out
		},
		JobAdd: func(name, scheduleKind, scheduleValue, agentID, prompt, sessionTarget string, deleteAfterRun *bool, contextMessages string) (tool.JobInfo, error) {
			sched := agent.JobSchedule{}
			switch scheduleKind {
			case "at":
				sched.Kind = agent.ScheduleAt
				sched.At = scheduleValue
			case "every":
				sched.Kind = agent.ScheduleEvery
				sched.Every = scheduleValue
			case "cron":
				sched.Kind = agent.ScheduleCron
				sched.Expr = scheduleValue
			}
			dar := scheduleKind == "at"
			if deleteAfterRun != nil {
				dar = *deleteAfterRun
			}
			j, err := jobStore.Add(agent.Job{
				Name: name, Schedule: sched, AgentID: agentID, Prompt: prompt,
				SessionTarget: sessionTarget, DeleteAfterRun: dar, ContextMessages: contextMessages,
			})
			if err != nil {
				return tool.JobInfo{}, err
			}
			return tool.JobInfo{ID: j.ID, Name: j.Name, ScheduleKind: string(j.Schedule.Kind), AgentID: j.AgentID, Enabled: j.Enabled}, nil
		},
		JobRemove: func(id string) error { _, err := jobStore.Remove(id); return err },
		JobList: func() []tool.JobInfo {
			jobs := jobStore.List()
			out := make([]tool.JobInfo, len(jobs))
			for i, j := range jobs {
				out[i] = tool.JobInfo{ID: j.ID, Name: j.Name, ScheduleKind: string(j.Schedule.Kind), AgentID: j.AgentID, Enabled: j.Enabled}
			}
			return out
		},
		JobRun:  func(_ context.Context, _ string) error { return nil },
		JobRuns: func(_ string) []tool.JobRunLogEntry { return nil },
		Logger:  logger,
	}))

	return &TestEnv{
		Runner:   runner,
		Registry: registry,
		Tools:    toolReg,
		Sessions: sessionStore,
		Bus:      msgBus,
		Model:    nil, // real provider, no mock
		JobStore: jobStore,
		Logger:   logger,
		Dir:      dir,
	}
}

// RunAgent is a convenience to run an agent and collect all events.
func (e *TestEnv) RunAgent(t *testing.T, agentID, prompt string) (*agent.RunResult, []agent.StreamEvent) {
	t.Helper()

	a, ok := e.Registry.Get(agentID)
	if !ok {
		t.Fatalf("agent %q not found", agentID)
	}

	var events []agent.StreamEvent
	emit := func(ev agent.StreamEvent) error {
		events = append(events, ev)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := e.Runner.Run(ctx, agent.RunConfig{
		AgentID:        agentID,
		Agent:          a,
		Prompt:         prompt,
		PermissionMode: tool.PermissionAllow, // tests explicitly bypass permissions
	}, emit)
	if err != nil {
		t.Fatalf("Run(%s, %q): %v", agentID, prompt, err)
	}

	return result, events
}

// RunAgentWithConfig runs an agent with a custom RunConfig and collects all events.
func (e *TestEnv) RunAgentWithConfig(t *testing.T, cfg agent.RunConfig) (*agent.RunResult, []agent.StreamEvent) {
	t.Helper()

	var events []agent.StreamEvent
	emit := func(ev agent.StreamEvent) error {
		events = append(events, ev)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := e.Runner.Run(ctx, cfg, emit)
	if err != nil {
		t.Fatalf("Run(%s): %v", cfg.AgentID, err)
	}

	return result, events
}
