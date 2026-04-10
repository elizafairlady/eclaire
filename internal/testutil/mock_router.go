package testutil

import (
	"context"
	"log/slog"
	"os"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/provider"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// MockRouter implements agent.ModelResolver for tests.
// Returns the MockModel directly so tests exercise ConversationRuntime.
type MockAgentFactory struct {
	Model *MockModel
}

// ResolveWithContext satisfies agent.ModelResolver.
func (f *MockAgentFactory) ResolveWithContext(_ context.Context, _ string) (*provider.ModelResolution, error) {
	return &provider.ModelResolution{
		Model:         f.Model,
		ContextWindow: 128000,
		ProviderID:    "mock",
	}, nil
}

// TestEnv is a complete test environment for integration tests.
type TestEnv struct {
	Runner   *agent.Runner
	Registry *agent.Registry
	Tools    *tool.Registry
	Sessions *persist.SessionStore
	Bus      *bus.Bus
	Model    *MockModel
	JobStore *agent.JobStore
	Logger   *slog.Logger
	Dir      string
}

// NewTestEnv creates a fully wired test environment with a mock LLM.
func NewTestEnv(dir string, model *MockModel) *TestEnv {
	logger := slog.Default()
	msgBus := bus.New()
	registry := agent.NewRegistry()
	sessionDir := dir + "/sessions"
	os.MkdirAll(sessionDir, 0o700)
	os.MkdirAll(dir+"/workspace", 0o700)
	sessionStore := persist.NewSessionStore(sessionDir)
	toolReg := tool.NewRegistry()

	// Register real tools
	toolReg.Register(tool.ShellTool())
	toolReg.Register(tool.ReadTool())
	toolReg.Register(tool.WriteTool())
	toolReg.Register(tool.EditTool())
	toolReg.Register(tool.GlobTool())
	toolReg.Register(tool.GrepTool())
	toolReg.Register(tool.LsTool())
	toolReg.Register(tool.TodosTool())
	toolReg.Register(tool.MemoryWriteTool(dir + "/workspace"))
	toolReg.Register(tool.MemoryReadTool(dir + "/workspace"))

	factory := &MockAgentFactory{Model: model}

	// Register built-in agents
	for _, a := range agent.BuiltinAgents() {
		registry.Register(a)
	}

	runner := &agent.Runner{
		Router:   factory,
		Tools:    toolReg,
		Sessions: sessionStore,
		Bus:      msgBus,
		Logger:   logger,
		Registry: registry,
	}

	// Register agent tool
	toolReg.Register(tool.AgentTool(tool.SubAgentDeps{
		RunSubAgent: runner.RunSubAgent,
		Bus:         msgBus,
		Logger:      logger,
	}))
	toolReg.Register(tool.TaskStatusTool(sessionStore))

	// Register eclaire_manage tool for self-improvement tests
	agentsDir := dir + "/agents"
	skillsDir := dir + "/skills"
	cronPath := dir + "/cron.yaml"
	os.MkdirAll(agentsDir, 0o700)
	os.MkdirAll(skillsDir, 0o700)

	// Job store for scheduling tests
	jobStore, _ := agent.NewJobStore(dir + "/jobs.json")

	toolReg.Register(tool.ManageTool(tool.ManageDeps{
		AgentsDir: agentsDir,
		SkillsDir: skillsDir,
		CronPath:  cronPath,
		Reload:    func() tool.ReloadResult { return tool.ReloadResult{} },
		CronList:  func() []tool.CronEntry { return nil },
		AgentList: func() []tool.AgentInfo { return nil },
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
				Name:            name,
				Schedule:        sched,
				AgentID:         agentID,
				Prompt:          prompt,
				SessionTarget:   sessionTarget,
				DeleteAfterRun:  dar,
				ContextMessages: contextMessages,
			})
			if err != nil {
				return tool.JobInfo{}, err
			}
			return tool.JobInfo{
				ID: j.ID, Name: j.Name,
				ScheduleKind: string(j.Schedule.Kind),
				AgentID: j.AgentID, Enabled: j.Enabled,
			}, nil
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

	// Wire permission checker
	permChecker := tool.NewPermissionChecker(toolReg)
	runner.PermChecker = permChecker
	runner.WorkspaceRoot = dir
	runner.EclaireDir = dir

	return &TestEnv{
		Runner:   runner,
		Registry: registry,
		Tools:    toolReg,
		Sessions: sessionStore,
		Bus:      msgBus,
		Model:    model,
		JobStore: jobStore,
		Logger:   logger,
		Dir:      dir,
	}
}
