package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/bus"
	"github.com/elizafairlady/eclaire/internal/channel"
	"github.com/elizafairlady/eclaire/internal/config"
	"github.com/elizafairlady/eclaire/internal/hook"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/provider"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// Gateway is the central daemon process.
type Gateway struct {
	config   *config.Store
	listener net.Listener
	logger   *slog.Logger

	agents        *agent.Registry
	bus           *bus.Bus
	sessions      *persist.SessionStore
	mainSessionID string
	router        *provider.Router
	tools      *tool.Registry
	runner     *agent.Runner
	workspaces *agent.WorkspaceLoader
	tasks      *agent.TaskRegistry
	flows     *agent.FlowExecutor
	jobStore      *agent.JobStore
	jobExecutor   *agent.JobExecutor
	runLog        *agent.RunLog
	notifications *agent.NotificationStore
	reminders    *tool.ReminderStore
	reloadFn     func() tool.ReloadResult
	approvalGate *agent.ApprovalGate
	channels     *channel.Manager

	idleTimeout time.Duration
	idleTimer   *time.Timer
	startTime   time.Time

	mu    sync.Mutex
	conns map[string]*conn

	ctx    context.Context
	cancel context.CancelFunc
}

type conn struct {
	id      string
	netConn net.Conn
	encoder *json.Encoder
	mu      sync.Mutex
	ctx     context.Context    // cancelled when this connection closes
	cancel  context.CancelFunc // called in handleConn defer
}

// send writes an envelope to the connection, safe for concurrent use.
func (c *conn) send(env Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoder.Encode(env)
}

// New creates a new Gateway.
func New(cfg *config.Store, logger *slog.Logger) (*Gateway, error) {
	timeout, err := time.ParseDuration(cfg.Merged().Gateway.IdleTimeout)
	if err != nil {
		timeout = 10 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	msgBus := bus.New()
	registry := agent.NewRegistry()
	sessionStore := persist.NewSessionStore(cfg.SessionsDir())

	// Create or load the persistent main session
	mainSession, err := sessionStore.GetOrCreateMain("orchestrator")
	if err != nil {
		logger.Warn("failed to create main session", "err", err)
	}
	mainSessionID := ""
	if mainSession != nil {
		mainSessionID = mainSession.ID
		logger.Info("main session ready", "id", mainSessionID)
	}

	// Clean up stale sessions from before session lifecycle was wired up.
	// Sessions that have been "active" for >1 hour with no updates are orphaned.
	if marked, archived, err := sessionStore.CleanupStale(1 * time.Hour); err != nil {
		logger.Warn("stale session cleanup failed", "err", err)
	} else if marked > 0 {
		logger.Info("cleaned up stale sessions", "marked", marked, "archived", archived)
	}

	// Initialize provider router
	router, err := provider.NewRouter(cfg.Merged(), logger)
	if err != nil {
		logger.Warn("failed to create provider router", "err", err)
	}

	// Register built-in tools
	toolReg := tool.NewRegistry()
	// File tools
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
	// HTTP tools
	toolReg.Register(tool.FetchTool())
	toolReg.Register(tool.DownloadTool())
	toolReg.Register(tool.SearchTool())
	toolReg.Register(tool.RSSFeedTool())
	// Background job tools
	toolReg.Register(tool.JobOutputTool())
	toolReg.Register(tool.JobKillTool())
	// Agent tools
	toolReg.Register(tool.TodosToolWithDeps(tool.TodosDeps{
		Sessions:      sessionStore,
		SessionIDFunc: agent.SessionFromContext,
	}))
	// Agent tool registered below after runner is created
	// Memory tools
	toolReg.Register(tool.MemoryWriteTool(cfg.WorkspaceDir()))
	toolReg.Register(tool.MemoryReadTool(cfg.WorkspaceDir()))
	// Reminder tool
	reminderStore := tool.NewReminderStore(cfg.RemindersPath())
	toolReg.Register(tool.ReminderTool(reminderStore))
	// Email tool (only registers if credentials file pattern exists)
	toolReg.Register(tool.EmailTool(tool.EmailDeps{
		CredentialsPath: cfg.EmailCredentialsPath(),
	}))
	// Briefing tool registered below after scheduler is created

	// Apply tool overrides from config
	for _, ov := range cfg.Merged().Tools.Overrides {
		toolReg.SetOverride(ov.AgentID, ov.Tool, tool.TrustTier(ov.Tier))
	}

	logger.Info("tools registered", "count", len(toolReg.All()))

	// Wire shell executor: logger, command policy, audit log
	shellLogger := logger.With("component", "shell")
	tool.DefaultExecutor.SetLogger(shellLogger)
	tool.DefaultExecutor.SetPolicy(tool.DefaultCommandPolicy())
	tool.DefaultExecutor.SetAuditLog(tool.NewAuditLog(logger.With("component", "audit")))

	// Register built-in agents first
	for _, a := range agent.BuiltinAgents() {
		if rerr := registry.Register(a); rerr != nil {
			logger.Warn("failed to register built-in agent", "id", a.ID(), "err", rerr)
		} else {
			logger.Info("registered built-in agent", "id", a.ID(), "role", a.Role())
		}
	}

	// Load user-defined YAML agents (can override built-ins by using different IDs)
	agents, err := agent.LoadAgentsDir(cfg.AgentsDir())
	if err != nil {
		logger.Warn("failed to load agents", "err", err)
	}
	for _, a := range agents {
		if rerr := registry.Register(a); rerr != nil {
			logger.Warn("failed to register agent", "id", a.ID(), "err", rerr)
		} else {
			logger.Info("registered user agent", "id", a.ID(), "role", a.Role())
		}
	}

	// Create workspace loader and context engine
	cwd, _ := os.Getwd()
	projectDir := ""
	if info, serr := os.Stat(filepath.Join(cwd, ".eclaire")); serr == nil && info.IsDir() {
		projectDir = filepath.Join(cwd, ".eclaire")
	}
	workspaces := agent.NewWorkspaceLoader(cfg.WorkspaceDir(), cfg.AgentsDir(), projectDir)
	skillLoader := agent.NewSkillLoader(cfg.SkillsDir(), cfg.AgentsDir(), projectDir)
	contextEngine := agent.NewContextEngine(router, workspaces, skillLoader)
	contextEngine.SetRegistry(registry)

	// Build hook runner from config
	var hookRunner *hook.Runner
	if hooks := cfg.Merged().Hooks; len(hooks) > 0 {
		var defs []hook.Definition
		for _, h := range hooks {
			defs = append(defs, hook.Definition{
				Event:   hook.Event(h.Event),
				Matcher: h.Matcher,
				Command: h.Command,
				Timeout: h.Timeout,
			})
		}
		hookRunner = hook.NewRunner(defs)
		logger.Info("hooks loaded", "count", len(defs))
	}

	permChecker := tool.NewPermissionChecker(toolReg)
	permChecker.SetAuditLogger(logger.With("component", "perm_audit"))
	approvalGate := agent.NewApprovalGate(msgBus)

	// Default workspace root to user home — users work across their whole home directory
	homeDir, _ := os.UserHomeDir()
	workspaceRoot := homeDir
	if workspaceRoot == "" {
		workspaceRoot = cwd
	}

	// System events queue — background work awareness for the main session.
	// Reference: OpenClaw src/infra/system-events.ts
	sysEvents := agent.NewSystemEventQueue()

	runner := &agent.Runner{
		Router:        router,
		Tools:         toolReg,
		Sessions:      sessionStore,
		Bus:           msgBus,
		Logger:        logger,
		Workspaces:    workspaces,
		ContextEngine: contextEngine,
		Registry:      registry,
		HookRunner:    hookRunner,
		PermChecker:   permChecker,
		Approver:      &approvalAdapter{gate: approvalGate},
		WorkspaceRoot: workspaceRoot,
		EclaireDir:    cfg.GlobalDir(),
		SystemEvents:  sysEvents,
	}

	// Create task registry and flow executor
	taskRegistry := agent.NewTaskRegistry()
	flowExecutor := &agent.FlowExecutor{
		Runner:   runner,
		Tasks:    taskRegistry,
		Registry: registry,
		Bus:      msgBus,
		Logger:   logger,
	}

	// Register agent tool now that runner exists
	toolReg.Register(tool.AgentTool(tool.SubAgentDeps{
		RunSubAgent: runner.RunSubAgent,
		ListAgents: func() []tool.AgentInfo {
			infos := registry.All()
			out := make([]tool.AgentInfo, len(infos))
			for i, a := range infos {
				out[i] = tool.AgentInfo{
					ID:          a.ID,
					Name:        a.Name,
					Description: a.Description,
					Role:        string(a.Role),
					BuiltIn:     a.BuiltIn,
				}
			}
			return out
		},
		Bus:    msgBus,
		Logger: logger,
	}))
	toolReg.Register(tool.TaskStatusTool(sessionStore))
	toolReg.Register(tool.SessionReadTool(sessionStore))

	// Create unified job store, run log, notifications, and job executor
	jobStore, err := agent.NewJobStore(cfg.JobsPath())
	if err != nil {
		logger.Warn("failed to load job store", "err", err)
		jobStore, _ = agent.NewJobStore(filepath.Join(os.TempDir(), "eclaire-jobs.json"))
	}
	runLog := agent.NewRunLog(cfg.RunsDir())
	notifStore, err := agent.NewNotificationStore(cfg.NotificationsPath())
	if err != nil {
		logger.Warn("failed to load notification store", "err", err)
	}
	if notifStore != nil {
		notifStore.SubscribeToBus(ctx, msgBus)
	}

	// Subscribe to bus events for system event awareness.
	// Cron/heartbeat completions → main session awareness.
	// Sub-agent completions → parent session awareness.
	// Reference: OpenClaw src/cron/isolated-agent/delivery-dispatch.ts
	msgBus.SubscribeFunc(ctx, bus.TopicBackgroundResult, func(ev bus.Event) {
		br, ok := ev.Payload.(bus.BackgroundResult)
		if !ok {
			return
		}
		if br.Source != "cron" && br.Source != "heartbeat" {
			return
		}
		summary := br.Content
		if len(summary) > 500 {
			summary = summary[:500] + "..."
		}
		text := fmt.Sprintf("%s '%s' %s: %s", br.Source, br.TaskName, br.Status, summary)
		sysEvents.Enqueue(persist.MainSessionID, text, br.Source, br.Source+":"+br.TaskName)
	})
	msgBus.SubscribeFunc(ctx, bus.TopicSubAgentCompleted, func(ev bus.Event) {
		se, ok := ev.Payload.(bus.SubAgentEvent)
		if !ok || se.ParentSessionID == "" {
			return
		}
		summary := se.Result
		if len(summary) > 300 {
			summary = summary[:300] + "..."
		}
		text := fmt.Sprintf("Sub-agent '%s' %s: %s", se.AgentID, se.Status, summary)
		sysEvents.Enqueue(se.ParentSessionID, text, "subagent", "subagent:"+se.TaskID)
	})

	jobExec := agent.NewJobExecutor(jobStore, runLog, notifStore, runner, registry, msgBus, logger)
	jobExec.SetMainSession(mainSessionID, sysEvents)

	// Wire reminder firing into job executor's tick loop
	jobExec.SetReminders(&reminderAdapter{store: reminderStore})

	// Create dreaming service (memory consolidation).
	// Reference: OpenClaw src/memory-host-sdk/dreaming.ts
	dreaming := agent.NewDreamingService(jobStore, jobExec, logger)
	if err := dreaming.EnsureJobs(); err != nil {
		logger.Warn("failed to ensure dreaming jobs", "err", err)
	}

	// Migrate legacy cron.yaml entries to unified job store
	if migrated := jobExec.MigrateLegacyCron(cfg.CronPath()); migrated > 0 {
		logger.Info("migrated legacy cron entries", "count", migrated)
	}

	// Sync HEARTBEAT.md tasks to job store
	if err := jobExec.SyncHeartbeatJobs(workspaces); err != nil {
		logger.Warn("failed to sync heartbeat jobs", "err", err)
	}

	// Register briefing tool — CronList pulls from unified job store
	toolReg.Register(tool.BriefingTool(tool.BriefingDeps{
		Reminders:    reminderStore,
		WorkspaceDir: cfg.WorkspaceDir(),
		BriefingsDir: cfg.BriefingsDir(),
		CronList: func() []tool.CronEntry {
			var out []tool.CronEntry
			for _, j := range jobStore.List() {
				if !j.Enabled {
					continue
				}
				schedule := ""
				switch j.Schedule.Kind {
				case agent.ScheduleCron:
					schedule = j.Schedule.Expr
				case agent.ScheduleEvery:
					schedule = "every " + j.Schedule.Every
				case agent.ScheduleAt:
					schedule = "at " + j.Schedule.At
				}
				out = append(out, tool.CronEntry{ID: j.ID, Schedule: schedule, AgentID: j.AgentID, Prompt: j.Prompt, Enabled: j.Enabled})
			}
			return out
		},
	}))

	// Register eclaire_manage tool — reload closure captures registry
	reloadFn := func() tool.ReloadResult {
		result := tool.ReloadResult{}
		agents, err := agent.LoadAgentsDir(cfg.AgentsDir())
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("load agents: %v", err))
		} else {
			for _, a := range agents {
				replaced := registry.Upsert(a)
				result.AgentsLoaded++
				if replaced {
					result.AgentsReplaced++
				}
			}
		}
		// Re-sync heartbeat jobs on reload
		if syncErr := jobExec.SyncHeartbeatJobs(workspaces); syncErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("sync heartbeat: %v", syncErr))
		}
		result.CronEntries = jobStore.Count()
		logger.Info("reload complete", "agents", result.AgentsLoaded, "jobs", result.CronEntries)
		return result
	}
	// Active flow runs tracked in memory for status queries
	flowRuns := struct {
		sync.RWMutex
		runs map[string]*tool.FlowRunInfo
	}{runs: make(map[string]*tool.FlowRunInfo)}

	toolReg.Register(tool.ManageTool(tool.ManageDeps{
		AgentsDir: cfg.AgentsDir(),
		SkillsDir: cfg.SkillsDir(),
		FlowsDir:  cfg.FlowsDir(),
		CronPath:  cfg.CronPath(),
		Reload:    reloadFn,
		CronList: func() []tool.CronEntry {
			var out []tool.CronEntry
			for _, j := range jobStore.List() {
				if !j.Enabled {
					continue
				}
				schedule := ""
				switch j.Schedule.Kind {
				case agent.ScheduleCron:
					schedule = j.Schedule.Expr
				case agent.ScheduleEvery:
					schedule = "every " + j.Schedule.Every
				case agent.ScheduleAt:
					schedule = "at " + j.Schedule.At
				}
				out = append(out, tool.CronEntry{ID: j.ID, Schedule: schedule, AgentID: j.AgentID, Prompt: j.Prompt, Enabled: j.Enabled})
			}
			return out
		},
		AgentList: func() []tool.AgentInfo {
			agents := registry.All()
			out := make([]tool.AgentInfo, len(agents))
			for i, a := range agents {
				out[i] = tool.AgentInfo{ID: a.ID, Name: a.Name, Description: a.Description, Role: string(a.Role), BuiltIn: a.BuiltIn}
			}
			return out
		},
		FlowList: func() []tool.FlowInfo {
			defs, err := agent.LoadFlowsDir(cfg.FlowsDir())
			if err != nil {
				return nil
			}
			out := make([]tool.FlowInfo, len(defs))
			for i, d := range defs {
				steps := make([]tool.FlowStep, len(d.Steps))
				for j, s := range d.Steps {
					steps[j] = tool.FlowStep{Name: s.Name, Agent: s.Agent, Prompt: s.Prompt}
				}
				out[i] = tool.FlowInfo{ID: d.ID, Name: d.Name, Description: d.Description, Steps: steps}
			}
			return out
		},
		FlowRun: func(flowID, input string) (*tool.FlowRunInfo, error) {
			def, err := agent.LoadFlowFile(filepath.Join(cfg.FlowsDir(), flowID+".yaml"))
			if err != nil {
				return nil, fmt.Errorf("load flow %q: %w", flowID, err)
			}

			// Run async
			info := &tool.FlowRunInfo{
				FlowID:     flowID,
				Status:     "running",
				TotalSteps: len(def.Steps),
			}

			go func() {
				run, runErr := flowExecutor.Run(ctx, *def, input, func(ev agent.StreamEvent) error {
					return nil // background run, no streaming
				})
				flowRuns.Lock()
				defer flowRuns.Unlock()
				if runErr != nil {
					info.Status = "failed"
					info.Error = runErr.Error()
				} else {
					info.ID = run.ID
					info.Status = string(run.Status)
					info.CurrentStep = run.CurrentStep
					info.StepOutputs = run.StepOutputs
				}
			}()

			// Generate a run ID immediately
			info.ID = fmt.Sprintf("flow_%s_%d", flowID, time.Now().UnixNano())
			flowRuns.Lock()
			flowRuns.runs[info.ID] = info
			flowRuns.Unlock()

			return info, nil
		},
		FlowGet: func(runID string) (*tool.FlowRunInfo, bool) {
			flowRuns.RLock()
			defer flowRuns.RUnlock()
			info, ok := flowRuns.runs[runID]
			return info, ok
		},
		WorkspaceDir: cfg.WorkspaceDir(),
		HeartbeatList: func() []tool.HeartbeatTaskInfo {
			tasks := jobExec.HeartbeatTaskList()
			out := make([]tool.HeartbeatTaskInfo, len(tasks))
			for i, t := range tasks {
				out[i] = tool.HeartbeatTaskInfo{
					Name:     t.Name,
					Interval: t.Interval,
					Agent:    t.Agent,
					Prompt:   t.Prompt,
					Status:   t.Status,
				}
				if !t.LastRun.IsZero() {
					out[i].LastRun = t.LastRun.Format("2006-01-02 15:04:05")
				}
				if !t.NextDue.IsZero() {
					out[i].NextDue = t.NextDue.Format("2006-01-02 15:04:05")
				}
			}
			return out
		},
		HeartbeatTrigger: func(ctx context.Context, name string) error {
			return jobExec.TriggerHeartbeatTask(ctx, name)
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
			dar := scheduleKind == "at" // default deleteAfterRun for at kind
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
			return jobToInfo(*j), nil
		},
		JobRemove: func(id string) error {
			_, err := jobStore.Remove(id)
			return err
		},
		JobList: func() []tool.JobInfo {
			jobs := jobStore.List()
			out := make([]tool.JobInfo, len(jobs))
			for i, j := range jobs {
				out[i] = jobToInfo(j)
			}
			return out
		},
		JobRun: func(ctx context.Context, id string) error {
			return jobExec.RunImmediate(ctx, id)
		},
		JobRuns: func(id string) []tool.JobRunLogEntry {
			entries, _ := runLog.Read(id, 20)
			out := make([]tool.JobRunLogEntry, len(entries))
			for i, e := range entries {
				out[i] = tool.JobRunLogEntry{
					Timestamp: e.Timestamp.Format("2006-01-02 15:04:05"),
					Status:    e.Status,
					Error:     e.Error,
					Summary:   e.Summary,
					Duration:  e.Duration.String(),
				}
			}
			return out
		},
		NotifyAdd: func(title, content, severity string) error {
			if notifStore == nil {
				return fmt.Errorf("notification store not available")
			}
			sev := agent.SeverityInfo
			switch severity {
			case "warning":
				sev = agent.SeverityWarning
			case "error":
				sev = agent.SeverityError
			case "debug":
				sev = agent.SeverityDebug
			}
			return notifStore.Add(agent.Notification{
				Severity: sev,
				Source:   "agent",
				Title:    title,
				Content:  content,
			})
		},
		DreamingEnable:  dreaming.Enable,
		DreamingDisable: dreaming.Disable,
		DreamingStatus: func() tool.DreamingStatusInfo {
			status := dreaming.Status()
			phases := make([]tool.DreamingPhaseInfo, len(status.Phases))
			for i, p := range status.Phases {
				phases[i] = tool.DreamingPhaseInfo{
					Phase:   string(p.Phase),
					Enabled: p.Enabled,
					Status:  p.Status,
				}
				if p.LastRun != nil {
					phases[i].LastRun = p.LastRun.Format("2006-01-02 15:04:05")
				}
				if p.NextRun != nil {
					phases[i].NextRun = p.NextRun.Format("2006-01-02 15:04:05")
				}
			}
			return tool.DreamingStatusInfo{Enabled: status.Enabled, Phases: phases}
		},
		DreamingTrigger: func(ctx context.Context, phase string) error {
			return dreaming.TriggerPhase(ctx, agent.DreamPhase(phase))
		},
		Logger: logger,
	}))

	// Channel manager — gateway is channel-agnostic.
	// Channels are registered externally via Channels().Register() before Start().
	channelMgr := channel.NewManager(func(_ context.Context, msg channel.InboundMessage) error {
		logger.Info("channel message", "channel", msg.ChannelID, "agent", msg.AgentID)
		return nil
	}, logger)

	return &Gateway{
		config:        cfg,
		logger:        logger,
		agents:        registry,
		bus:           msgBus,
		sessions:      sessionStore,
		mainSessionID: mainSessionID,
		router:        router,
		tools:         toolReg,
		runner:        runner,
		workspaces:    workspaces,
		tasks:         taskRegistry,
		flows:         flowExecutor,
		jobStore:      jobStore,
		jobExecutor:   jobExec,
		runLog:        runLog,
		notifications: notifStore,
		reminders:    reminderStore,
		reloadFn:      reloadFn,
		approvalGate:  approvalGate,
		channels:      channelMgr,
		idleTimeout:   timeout,
		conns:         make(map[string]*conn),
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// jobToInfo converts an agent.Job to a tool.JobInfo for display.
func jobToInfo(j agent.Job) tool.JobInfo {
	info := tool.JobInfo{
		ID:             j.ID,
		Name:           j.Name,
		ScheduleKind:   string(j.Schedule.Kind),
		AgentID:        j.AgentID,
		Prompt:         j.Prompt,
		Enabled:        j.Enabled,
		DeleteAfterRun: j.DeleteAfterRun,
		LastStatus:     j.State.LastStatus,
		LastError:      j.State.LastError,
	}
	switch j.Schedule.Kind {
	case agent.ScheduleAt:
		info.Schedule = j.Schedule.At
	case agent.ScheduleEvery:
		info.Schedule = j.Schedule.Every
	case agent.ScheduleCron:
		info.Schedule = j.Schedule.Expr
	}
	if j.State.NextRunAt != nil {
		info.NextRunAt = j.State.NextRunAt.Format("2006-01-02 15:04:05")
	}
	if j.State.LastRunAt != nil {
		info.LastRunAt = j.State.LastRunAt.Format("2006-01-02 15:04:05")
	}
	return info
}

// Start begins listening on the Unix socket and serving requests.
func (g *Gateway) Start() error {
	socketPath := g.config.SocketPath()

	// Clean up stale socket
	if _, err := os.Stat(socketPath); err == nil {
		os.Remove(socketPath)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	// Restrict socket to owner only — prevents other local users from
	// connecting to the gateway and issuing RPC commands.
	if err := os.Chmod(socketPath, 0600); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	g.listener = ln
	g.startTime = time.Now()

	// Write PID file
	if err := os.WriteFile(g.config.PIDPath(), []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		g.logger.Warn("failed to write PID file", "err", err)
	}

	// Start idle timer
	g.idleTimer = time.AfterFunc(g.idleTimeout, g.onIdle)

	g.logger.Info("gateway started",
		"socket", socketPath,
		"pid", os.Getpid(),
		"idle_timeout", g.idleTimeout,
	)

	// Run BOOT.md if needed (once per day)
	if g.jobExecutor != nil {
		go g.jobExecutor.RunBootIfNeeded(g.ctx, g.workspaces)
	}

	// Start unified job executor (handles all scheduling: heartbeat, cron, one-shots)
	if g.jobExecutor != nil {
		g.jobExecutor.Start(g.ctx)
	}

	// Broadcast approval requests to all connected clients
	go g.broadcastApprovalRequests()

	// Accept connections
	go g.acceptLoop()

	// Wait for shutdown
	<-g.ctx.Done()
	if g.jobExecutor != nil {
		g.jobExecutor.Stop()
	}
	return g.cleanup()
}

// Channels returns the channel manager for external channel registration.
// Channels should be registered before Start() is called.
func (g *Gateway) Channels() *channel.Manager {
	return g.channels
}

// Shutdown triggers a graceful shutdown.
func (g *Gateway) Shutdown() {
	g.logger.Info("gateway shutting down")
	g.cancel()
}

func (g *Gateway) acceptLoop() {
	for {
		c, err := g.listener.Accept()
		if err != nil {
			select {
			case <-g.ctx.Done():
				return
			default:
				g.logger.Error("accept", "err", err)
				continue
			}
		}
		go g.handleConn(c)
	}
}

func (g *Gateway) handleConn(c net.Conn) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	connCtx, connCancel := context.WithCancel(g.ctx)
	gc := &conn{id: id, netConn: c, encoder: json.NewEncoder(c), ctx: connCtx, cancel: connCancel}

	g.mu.Lock()
	g.conns[id] = gc
	g.mu.Unlock()

	defer func() {
		connCancel() // cancel all runs using this connection's context
		c.Close()
		g.mu.Lock()
		delete(g.conns, id)
		g.mu.Unlock()
	}()

	g.resetIdle()
	decoder := json.NewDecoder(c)

	for {
		var env Envelope
		if err := decoder.Decode(&env); err != nil {
			return // connection closed or broken
		}
		g.resetIdle()
		g.handleRequest(gc, env)
	}
}

func (g *Gateway) handleRequest(c *conn, env Envelope) {
	switch env.Method {
	case MethodGatewayStatus:
		g.respondStatus(c, env)
	case MethodApprovalRespond:
		var req struct {
			RequestID string `json:"request_id"`
			Approved  bool   `json:"approved"`
			Persist   bool   `json:"persist,omitempty"` // true = "always for session"
			Reason    string `json:"reason,omitempty"`
		}
		if err := json.Unmarshal(env.Data, &req); err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
			return
		}
		err := g.approvalGate.Respond(req.RequestID, agent.ApprovalResult{
			Approved: req.Approved,
			Persist:  req.Persist,
			Reason:   req.Reason,
		})
		if err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
			return
		}
		// Also resolve the corresponding notification if one exists
		if g.notifications != nil {
			for _, n := range g.notifications.List(agent.NotificationFilter{Source: "approval", UnreadOnly: true}) {
				if n.RefID == req.RequestID {
					g.notifications.Resolve(n.ID)
					break
				}
			}
		}
		g.respond(c, env, nil, nil)
	case MethodGatewayReload:
		result := g.reloadFn()
		data, _ := json.Marshal(result)
		g.respond(c, env, data, nil)
	case MethodGatewayShutdown:
		g.respond(c, env, nil, nil)
		g.Shutdown()

	// Agent methods
	case MethodAgentList:
		data, _ := json.Marshal(g.agents.All())
		g.respond(c, env, data, nil)
	case MethodAgentRun:
		go g.handleAgentRun(c, env)
	case MethodAgentStatus:
		g.handleAgentStatus(c, env)
	case MethodAgentCancel:
		g.handleAgentCancel(c, env)

	// Session methods
	case MethodSessionCreate:
		g.handleSessionCreate(c, env)
	case MethodSessionList:
		g.handleSessionList(c, env)
	case MethodSessionGet:
		g.handleSessionGet(c, env)
	case MethodSessionContinue:
		go g.handleSessionContinue(c, env)
	case MethodSessionMostRecent:
		g.handleSessionMostRecent(c, env)

	// Tool methods
	case MethodToolList:
		g.handleToolList(c, env)

	// Flow methods
	case MethodFlowRun:
		go g.handleFlowRun(c, env)
	case MethodFlowList:
		g.handleFlowList(c, env)
	case MethodFlowStatus:
		g.handleFlowStatus(c, env)

	// Task methods
	case MethodTaskList:
		g.handleTaskList(c, env)

	// Cron methods (direct, without manage tool)
	case MethodCronList:
		g.handleCronList(c, env)
	case MethodCronAdd:
		g.handleCronAdd(c, env)
	case MethodCronRemove:
		g.handleCronRemove(c, env)

	// Job methods (unified scheduling)
	case MethodJobList:
		jobs := g.jobStore.List()
		out := make([]tool.JobInfo, len(jobs))
		for i, j := range jobs {
			out[i] = jobToInfo(j)
		}
		data, _ := json.Marshal(out)
		g.respond(c, env, data, nil)
	case MethodJobAdd:
		var req struct {
			Name           string `json:"name"`
			ScheduleKind   string `json:"schedule_kind"`
			ScheduleValue  string `json:"schedule_value"`
			AgentID        string `json:"agent_id"`
			Prompt         string `json:"prompt"`
			SessionTarget  string `json:"session_target"`
			DeleteAfterRun *bool  `json:"delete_after_run"`
		}
		json.Unmarshal(env.Data, &req)
		sched := agent.JobSchedule{}
		switch req.ScheduleKind {
		case "at":
			sched.Kind = agent.ScheduleAt
			sched.At = req.ScheduleValue
		case "every":
			sched.Kind = agent.ScheduleEvery
			sched.Every = req.ScheduleValue
		case "cron":
			sched.Kind = agent.ScheduleCron
			sched.Expr = req.ScheduleValue
		}
		dar := req.ScheduleKind == "at"
		if req.DeleteAfterRun != nil {
			dar = *req.DeleteAfterRun
		}
		j, err := g.jobStore.Add(agent.Job{
			Name:           req.Name,
			Schedule:       sched,
			AgentID:        req.AgentID,
			Prompt:         req.Prompt,
			SessionTarget:  req.SessionTarget,
			DeleteAfterRun: dar,
		})
		if err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: err.Error()})
		} else {
			data, _ := json.Marshal(jobToInfo(*j))
			g.respond(c, env, data, nil)
		}
	case MethodJobRemove:
		var req struct {
			ID string `json:"id"`
		}
		json.Unmarshal(env.Data, &req)
		_, err := g.jobStore.Remove(req.ID)
		if err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
		} else {
			data, _ := json.Marshal(map[string]string{"status": "removed"})
			g.respond(c, env, data, nil)
		}
	case MethodJobRun:
		var req struct {
			ID string `json:"id"`
		}
		json.Unmarshal(env.Data, &req)
		if err := g.jobExecutor.RunImmediate(g.ctx, req.ID); err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
		} else {
			data, _ := json.Marshal(map[string]string{"status": "triggered"})
			g.respond(c, env, data, nil)
		}
	case MethodJobRuns:
		var req struct {
			ID string `json:"id"`
		}
		json.Unmarshal(env.Data, &req)
		entries, _ := g.runLog.Read(req.ID, 50)
		data, _ := json.Marshal(entries)
		g.respond(c, env, data, nil)
	case MethodNotificationList:
		if g.notifications != nil {
			var filter struct {
				IncludeResolved bool `json:"include_resolved"`
			}
			json.Unmarshal(env.Data, &filter)
			var notifs []agent.Notification
			if filter.IncludeResolved {
				notifs = g.notifications.List(agent.NotificationFilter{})
			} else {
				notifs = g.notifications.Pending()
			}
			data, _ := json.Marshal(notifs)
			g.respond(c, env, data, nil)
		} else {
			g.respond(c, env, []byte("[]"), nil)
		}
	case MethodNotificationDrain:
		if g.notifications != nil {
			data, _ := json.Marshal(g.notifications.Drain())
			g.respond(c, env, data, nil)
		} else {
			g.respond(c, env, []byte("[]"), nil)
		}

	case MethodNotificationAct:
		var req NotificationActRequest
		json.Unmarshal(env.Data, &req)
		result, err := g.handleNotificationAct(req)
		if err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: err.Error()})
		} else {
			data, _ := json.Marshal(result)
			g.respond(c, env, data, nil)
		}

	// Client connect (session context)
	case MethodConnect:
		g.handleConnect(c, env)

	// Config methods
	case MethodConfigGet:
		g.handleConfigGet(c, env)

	default:
		g.respond(c, env, nil, &ErrorPayload{
			Code:    404,
			Message: fmt.Sprintf("unknown method: %s", env.Method),
		})
	}
}

func (g *Gateway) respondStatus(c *conn, env Envelope) {
	g.mu.Lock()
	clientCount := len(g.conns)
	g.mu.Unlock()

	status := GatewayStatus{
		PID:           os.Getpid(),
		Uptime:        time.Since(g.startTime).Truncate(time.Second).String(),
		ActiveAgents:  len(g.agents.All()),
		ActiveClients: clientCount,
		MainSessionID: g.mainSessionID,
	}

	data, _ := json.Marshal(status)
	g.respond(c, env, data, nil)
}

func (g *Gateway) respond(c *conn, req Envelope, data json.RawMessage, errPayload *ErrorPayload) {
	resp := Envelope{
		ID:    req.ID,
		Type:  TypeResponse,
		Data:  data,
		Error: errPayload,
	}
	if err := c.send(resp); err != nil {
		g.logger.Error("respond", "err", err)
	}
}

func (g *Gateway) handleConnect(c *conn, env Envelope) {
	var req ConnectRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid connect request"})
		return
	}

	resp := ConnectResponse{
		MainSessionID: g.mainSessionID,
	}

	// Detect project root by walking up from CWD
	if req.CWD != "" {
		projectRoot := detectProjectRoot(req.CWD)
		if projectRoot != "" {
			resp.ProjectRoot = projectRoot
			// Create or resume project session
			sess, err := g.sessions.CreateProject("orchestrator", projectRoot)
			if err != nil {
				g.logger.Warn("failed to create project session", "root", projectRoot, "err", err)
			} else {
				resp.ProjectSessionID = sess.ID
				g.logger.Info("project session ready", "root", projectRoot, "session", sess.ID)
			}
		}
	}

	data, _ := json.Marshal(resp)
	g.respond(c, env, data, nil)
}

// detectProjectRoot walks up from dir looking for .eclaire/ or .git/.
// Returns the directory containing the marker, or empty string if none found.
func detectProjectRoot(dir string) string {
	// Don't treat home directory as a project root
	home, _ := os.UserHomeDir()

	for {
		if dir == "/" || dir == "." || dir == home {
			return ""
		}
		// Check for .eclaire/
		if info, err := os.Stat(filepath.Join(dir, ".eclaire")); err == nil && info.IsDir() {
			return dir
		}
		// Check for .git/
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (info.IsDir() || !info.IsDir()) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func (g *Gateway) resetIdle() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.idleTimer != nil {
		g.idleTimer.Reset(g.idleTimeout)
	}
}

func (g *Gateway) onIdle() {
	// Don't shut down if any agents are running
	if g.agents != nil && g.agents.HasRunning() {
		g.logger.Info("idle timeout deferred, agent running")
		g.resetIdle()
		return
	}
	if g.jobStore != nil && g.jobStore.Count() > 0 {
		g.resetIdle()
		return
	}
	g.logger.Info("idle timeout reached, shutting down")
	g.Shutdown()
}

func (g *Gateway) cleanup() error {
	if g.listener != nil {
		g.listener.Close()
	}
	os.Remove(g.config.SocketPath())
	os.Remove(g.config.PIDPath())
	g.logger.Info("gateway stopped")
	return nil
}


func (g *Gateway) handleAgentRun(c *conn, env Envelope) {
	var req AgentRunRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	// Find the agent
	a, ok := g.agents.Get(req.AgentID)
	if !ok {
		cwd, _ := os.Getwd()
		var err error
		a, err = g.agents.Resolve(cwd, "")
		if err != nil {
			g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: fmt.Sprintf("agent not found: %s", req.AgentID)})
			return
		}
	}

	if g.runner == nil || g.router == nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: "no providers configured"})
		return
	}

	// Instance registration happens inside Runner.Run() keyed by session ID.
	// No singleton status tracking here.

	// Emit function sends stream events to the client
	emitFn := func(ev agent.StreamEvent) error {
		data, _ := json.Marshal(ev)
		streamEnv := Envelope{
			ID:   env.ID,
			Type: TypeStream,
			Data: data,
		}
		return c.send(streamEnv)
	}

	// Determine project context from session metadata or client CWD.
	// Guard: $HOME is never a project root — ~/.eclaire/ is the global config dir.
	home, _ := os.UserHomeDir()
	var projectRoot string
	wsRoots := []string{g.config.GlobalDir()} // ~/.eclaire/ always allowed
	if req.SessionID != "" {
		if meta, err := g.sessions.GetMeta(req.SessionID); err == nil && meta.ProjectRoot != "" {
			projectRoot = meta.ProjectRoot
		}
	}
	if projectRoot == "" && req.CWD != "" {
		projectRoot = detectProjectRoot(req.CWD)
	}
	// Never treat $HOME as a project root
	if projectRoot == home {
		projectRoot = ""
	}
	if projectRoot != "" {
		wsRoots = append(wsRoots, projectRoot)
	}

	// .eclaire/ path — only set if .eclaire/ actually exists at the project root.
	// Never set to ~/.eclaire/ (the global config dir).
	var projectDir string
	if projectRoot != "" {
		eclairePath := filepath.Join(projectRoot, ".eclaire")
		if eclairePath != g.config.GlobalDir() {
			if info, serr := os.Stat(eclairePath); serr == nil && info.IsDir() {
				projectDir = eclairePath
			}
		}
	}

	cfg := agent.RunConfig{
		AgentID:        a.ID(),
		Agent:          a,
		Prompt:         req.Prompt,
		SessionID:      req.SessionID,
		Title:          req.Title,
		PermissionMode: tool.PermissionWriteOnly,
		WorkspaceRoots: wsRoots,
		ProjectRoot:    projectRoot,
		ProjectDir:     projectDir,
		Compaction:     agent.DefaultCompactionConfig(),
	}

	// If continuing an existing session, rebuild conversation history from events
	if req.SessionID != "" {
		events, _ := g.sessions.ReadEvents(req.SessionID)
		if len(events) > 0 {
			cfg.History = persist.RebuildMessages(events)
		}
	}

	result, err := g.runner.Run(g.ctx, cfg, emitFn)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: fmt.Sprintf("agent error: %v", err)})
		return
	}

	data, _ := json.Marshal(map[string]any{
		"content":    result.Content,
		"session_id": result.SessionID,
		"steps":      result.Steps,
		"usage": map[string]int64{
			"input_tokens":  result.TotalUsage.InputTokens,
			"output_tokens": result.TotalUsage.OutputTokens,
		},
	})
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleAgentStatus(c *conn, env Envelope) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}
	a, ok := g.agents.Get(req.AgentID)
	if !ok {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: "agent not found"})
		return
	}
	info := agent.Info{
		ID:          a.ID(),
		Name:        a.Name(),
		Description: a.Description(),
		Role:        a.Role(),
		Tools:       a.RequiredTools(),
	}
	data, _ := json.Marshal(info)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleAgentCancel(_ *conn, _ Envelope) {
	// TODO: wire cancel map for active runs
}

func (g *Gateway) handleSessionCreate(c *conn, env Envelope) {
	var req struct {
		Title   string `json:"title"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}
	meta, err := g.sessions.Create(req.AgentID, req.Title, "")
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}
	data, _ := json.Marshal(meta)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleSessionList(c *conn, env Envelope) {
	sessions, err := g.sessions.List()
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}
	data, _ := json.Marshal(sessions)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleSessionGet(c *conn, env Envelope) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}
	meta, err := g.sessions.GetMeta(req.SessionID)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
		return
	}
	// Return meta + events
	events, _ := g.sessions.ReadEvents(req.SessionID)
	resp := struct {
		*persist.SessionMeta
		Events []persist.SessionEvent `json:"events"`
	}{meta, events}
	data, _ := json.Marshal(resp)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleSessionMostRecent(c *conn, env Envelope) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	json.Unmarshal(env.Data, &req) // ignore error — agentID is optional

	sessions, err := g.sessions.List()
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}

	for _, s := range sessions {
		if req.AgentID == "" || s.AgentID == req.AgentID {
			data, _ := json.Marshal(s)
			g.respond(c, env, data, nil)
			return
		}
	}

	g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: "no sessions found"})
}

func (g *Gateway) handleSessionContinue(c *conn, env Envelope) {
	var req struct {
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	meta, err := g.sessions.GetMeta(req.SessionID)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
		return
	}

	agentID := meta.AgentID
	if agentID == "" {
		agentID = "orchestrator"
	}
	a, ok := g.agents.Get(agentID)
	if !ok {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: fmt.Sprintf("agent %q not found", agentID)})
		return
	}

	if g.runner == nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: "no providers configured"})
		return
	}

	events, _ := g.sessions.ReadEvents(req.SessionID)
	history := persist.RebuildMessages(events)

	// Instance registration happens inside Runner.Run() keyed by session ID.

	emitFn := func(ev agent.StreamEvent) error {
		evData, _ := json.Marshal(ev)
		return c.send(Envelope{ID: env.ID, Type: TypeStream, Data: evData})
	}

	// Project context from session metadata
	home, _ := os.UserHomeDir()
	wsRoots := []string{g.config.GlobalDir()}
	var projectRoot, projectDir string
	if meta.ProjectRoot != "" && meta.ProjectRoot != home {
		projectRoot = meta.ProjectRoot
		wsRoots = append(wsRoots, projectRoot)
		eclairePath := filepath.Join(projectRoot, ".eclaire")
		if eclairePath != g.config.GlobalDir() {
			if info, serr := os.Stat(eclairePath); serr == nil && info.IsDir() {
				projectDir = eclairePath
			}
		}
	}

	cfg := agent.RunConfig{
		AgentID:        a.ID(),
		Agent:          a,
		Prompt:         req.Prompt,
		SessionID:      meta.ID,
		History:        history,
		PermissionMode: tool.PermissionWriteOnly,
		WorkspaceRoots: wsRoots,
		ProjectRoot:    projectRoot,
		ProjectDir:     projectDir,
		Compaction:     agent.DefaultCompactionConfig(),
	}

	result, err := g.runner.Run(g.ctx, cfg, emitFn)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}

	respData, _ := json.Marshal(map[string]any{
		"content":    result.Content,
		"session_id": result.SessionID,
		"steps":      result.Steps,
	})
	g.respond(c, env, respData, nil)
}

type ToolInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Tier     int    `json:"tier"`
}

func (g *Gateway) handleToolList(c *conn, env Envelope) {
	all := g.tools.All()
	infos := make([]ToolInfo, len(all))
	for i, t := range all {
		infos[i] = ToolInfo{
			Name:     t.Info().Name,
			Category: t.Category(),
			Tier:     int(t.TrustTier()),
		}
	}
	data, _ := json.Marshal(infos)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleConfigGet(c *conn, env Envelope) {
	cfg := g.config.Merged()
	safe := *cfg
	safeProviders := make(map[string]config.ProviderConfig)
	for k, v := range safe.Providers {
		if v.APIKey != "" {
			v.APIKey = "***"
		}
		safeProviders[k] = v
	}
	safe.Providers = safeProviders
	data, _ := json.Marshal(safe)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleFlowRun(c *conn, env Envelope) {
	var req FlowRunRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	def, err := agent.LoadFlowFile(filepath.Join(g.config.FlowsDir(), req.FlowID+".yaml"))
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: fmt.Sprintf("flow %q not found: %v", req.FlowID, err)})
		return
	}

	emitFn := func(ev agent.StreamEvent) error {
		data, _ := json.Marshal(ev)
		return c.send(Envelope{ID: env.ID, Type: TypeStream, Data: data})
	}

	run, err := g.flows.Run(g.ctx, *def, req.Input, emitFn)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}

	data, _ := json.Marshal(map[string]any{
		"run_id":       run.ID,
		"status":       run.Status,
		"current_step": run.CurrentStep,
		"total_steps":  len(def.Steps),
		"step_outputs": run.StepOutputs,
	})
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleFlowList(c *conn, env Envelope) {
	defs, err := agent.LoadFlowsDir(g.config.FlowsDir())
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 500, Message: err.Error()})
		return
	}

	type flowInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Steps       int    `json:"steps"`
	}
	out := make([]flowInfo, len(defs))
	for i, d := range defs {
		out[i] = flowInfo{ID: d.ID, Name: d.Name, Description: d.Description, Steps: len(d.Steps)}
	}
	data, _ := json.Marshal(out)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleFlowStatus(c *conn, env Envelope) {
	var req FlowStatusRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	tasks := g.tasks.ListByFlow(req.RunID)
	if len(tasks) == 0 {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: fmt.Sprintf("flow run %q not found", req.RunID)})
		return
	}

	type stepInfo struct {
		ID      string `json:"id"`
		Agent   string `json:"agent_id"`
		Status  string `json:"status"`
		Output  string `json:"output,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	steps := make([]stepInfo, len(tasks))
	for i, t := range tasks {
		steps[i] = stepInfo{ID: t.ID, Agent: t.AgentID, Status: string(t.Status), Output: t.Output, Error: t.Error}
	}
	data, _ := json.Marshal(steps)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleTaskList(c *conn, env Envelope) {
	tasks := g.tasks.List()
	data, _ := json.Marshal(tasks)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleCronList(c *conn, env Envelope) {
	// Legacy cron.list now returns jobs from unified store
	jobs := g.jobStore.List()
	type cronEntry struct {
		ID       string `json:"id"`
		Schedule string `json:"schedule"`
		AgentID  string `json:"agent_id"`
		Prompt   string `json:"prompt"`
		Enabled  bool   `json:"enabled"`
	}
	var entries []cronEntry
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		schedule := ""
		switch j.Schedule.Kind {
		case agent.ScheduleCron:
			schedule = j.Schedule.Expr
		case agent.ScheduleEvery:
			schedule = "every " + j.Schedule.Every
		case agent.ScheduleAt:
			schedule = "at " + j.Schedule.At
		}
		entries = append(entries, cronEntry{ID: j.ID, Schedule: schedule, AgentID: j.AgentID, Prompt: j.Prompt, Enabled: j.Enabled})
	}
	data, _ := json.Marshal(entries)
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleCronAdd(c *conn, env Envelope) {
	var req CronAddRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	// Create a cron job in the unified store
	id := "cron-" + req.ID
	if _, exists := g.jobStore.Get(id); exists {
		// Update existing
		g.jobStore.Update(id, func(j *agent.Job) {
			j.Schedule.Expr = req.Schedule
			j.AgentID = req.AgentID
			j.Prompt = req.Prompt
			j.Enabled = true
		})
	} else {
		g.jobStore.Add(agent.Job{
			ID:   id,
			Name: "cron: " + req.ID,
			Schedule: agent.JobSchedule{
				Kind: agent.ScheduleCron,
				Expr: req.Schedule,
			},
			AgentID:        req.AgentID,
			Prompt:         req.Prompt,
			SessionTarget:  "isolated",
			Enabled:        true,
			DeleteAfterRun: false,
		})
	}

	data, _ := json.Marshal(map[string]int{"jobs": g.jobStore.Count()})
	g.respond(c, env, data, nil)
}

func (g *Gateway) handleCronRemove(c *conn, env Envelope) {
	var req CronRemoveRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 400, Message: "invalid request"})
		return
	}

	// Try with "cron-" prefix first, then bare ID
	id := "cron-" + req.ID
	if _, exists := g.jobStore.Get(id); !exists {
		id = req.ID
	}
	_, err := g.jobStore.Remove(id)
	if err != nil {
		g.respond(c, env, nil, &ErrorPayload{Code: 404, Message: err.Error()})
		return
	}

	data, _ := json.Marshal(map[string]int{"jobs": g.jobStore.Count()})
	g.respond(c, env, data, nil)
}

// broadcastApprovalRequests subscribes to approval request bus events
// and forwards them to all connected clients. Also creates notifications
// so background runs' approval requests are visible via ecl notifications.
func (g *Gateway) broadcastApprovalRequests() {
	ch := g.bus.Subscribe(g.ctx, bus.TopicApprovalRequest)
	for ev := range ch {
		req, ok := ev.Payload.(agent.ApprovalRequest)
		if !ok {
			continue
		}

		// Create notification with RefID linking back to the approval gate's pending map.
		// This enables `ecl notifications <id> yes` to resolve the approval.
		if g.notifications != nil {
			g.notifications.Add(agent.Notification{
				Severity: agent.SeverityError,
				Source:   "approval",
				Title:    fmt.Sprintf("Approval needed: Agent %q wants to use tool %q", req.AgentID, req.Action),
				Content:  fmt.Sprintf("%s\n\nActions: yes (allow once), always (allow for session), no (deny)", req.Description),
				AgentID:  req.AgentID,
				RefID:    req.ID,
				Actions:  agent.ActionsForSource("approval"),
			})
		}

		// Broadcast as TypeEvent with approval fields at top level.
		// CLI's handleCLIApprovals() parses `id` directly from Data.
		// TUI's handleGatewayEvent() checks `event_type` field.
		eventData, _ := json.Marshal(map[string]any{
			"event_type":  "approval_request",
			"id":          req.ID,
			"agent_id":    req.AgentID,
			"action":      req.Action,
			"description": req.Description,
			"details":     req.Details,
		})
		env := Envelope{
			Type: TypeEvent,
			Data: eventData,
		}
		g.mu.Lock()
		for _, c := range g.conns {
			c.send(env) //nolint: errcheck
		}
		g.mu.Unlock()
	}
}

// approvalAdapter adapts agent.ApprovalGate to tool.Approver interface.
type approvalAdapter struct {
	gate *agent.ApprovalGate
}

func (a *approvalAdapter) Request(ctx context.Context, agentID, action, description string, details any) (tool.ApprovalResult, error) {
	result, err := a.gate.Request(ctx, agentID, action, description, details)
	if err != nil {
		return tool.ApprovalResult{}, err
	}
	return tool.ApprovalResult{Approved: result.Approved, Reason: result.Reason}, nil
}

func (g *Gateway) handleNotificationAct(req NotificationActRequest) (map[string]string, error) {
	if g.notifications == nil {
		return nil, fmt.Errorf("notification store not available")
	}
	n := g.notifications.Get(req.ID)
	if n == nil {
		return nil, fmt.Errorf("notification %q not found", req.ID)
	}

	switch n.Source {
	case "reminder":
		return g.actOnReminder(n, req.Action, req.Value)
	case "approval":
		return g.actOnApproval(n, req.Action)
	default:
		if req.Action == "dismiss" {
			g.notifications.Resolve(req.ID)
			return map[string]string{"status": "dismissed"}, nil
		}
		return nil, fmt.Errorf("no actions available for source %q", n.Source)
	}
}

func (g *Gateway) actOnReminder(n *agent.Notification, action, value string) (map[string]string, error) {
	if g.reminders == nil {
		return nil, fmt.Errorf("reminder store not available")
	}

	switch action {
	case "complete":
		all, err := g.reminders.Load()
		if err != nil {
			return nil, err
		}
		for i := range all {
			if all[i].ID == n.RefID {
				all[i].Completed = true
				g.reminders.Save(all)
				g.notifications.Resolve(n.ID)
				return map[string]string{"status": "completed", "reminder": n.RefID}, nil
			}
		}
		// Already completed by FireOverdue
		g.notifications.Resolve(n.ID)
		return map[string]string{"status": "completed", "reminder": n.RefID}, nil

	case "dismiss":
		g.notifications.Resolve(n.ID)
		return map[string]string{"status": "dismissed"}, nil

	case "snooze":
		dur := "15m"
		if value != "" {
			dur = value
		}
		all, err := g.reminders.Load()
		if err != nil {
			return nil, err
		}
		for i := range all {
			if all[i].ID == n.RefID {
				d, pErr := time.ParseDuration(dur)
				if pErr != nil {
					return nil, fmt.Errorf("invalid snooze duration %q: %v", dur, pErr)
				}
				all[i].DueAt = time.Now().Add(d)
				all[i].Completed = false
				g.reminders.Save(all)
				g.notifications.Resolve(n.ID)
				return map[string]string{
					"status":   "snoozed",
					"reminder": n.RefID,
					"until":    all[i].DueAt.Format("15:04"),
				}, nil
			}
		}
		return nil, fmt.Errorf("reminder %q not found", n.RefID)

	default:
		return nil, fmt.Errorf("unknown reminder action %q (available: complete, dismiss, snooze)", action)
	}
}

func (g *Gateway) actOnApproval(n *agent.Notification, action string) (map[string]string, error) {
	if g.approvalGate == nil {
		return nil, fmt.Errorf("approval gate not available")
	}

	// Resolve the notification regardless of whether the approval gate still has
	// a pending entry. The gate entry may be gone if the session died or the
	// daemon restarted — that's fine, just clean up the stale notification.
	resolve := func(status string, extra ...string) (map[string]string, error) {
		g.notifications.Resolve(n.ID)
		result := map[string]string{"status": status}
		for i := 0; i+1 < len(extra); i += 2 {
			result[extra[i]] = extra[i+1]
		}
		return result, nil
	}

	switch action {
	case "yes":
		g.approvalGate.Respond(n.RefID, agent.ApprovalResult{Approved: true})
		return resolve("approved")
	case "always":
		g.approvalGate.Respond(n.RefID, agent.ApprovalResult{Approved: true, Persist: true, Reason: "always"})
		return resolve("approved", "persist", "always")
	case "no":
		g.approvalGate.Respond(n.RefID, agent.ApprovalResult{Approved: false})
		return resolve("denied")
	default:
		return nil, fmt.Errorf("unknown approval action %q (available: yes, always, no)", action)
	}
}

// reminderAdapter adapts tool.ReminderStore to agent.ReminderFirer interface.
type reminderAdapter struct {
	store *tool.ReminderStore
}

func (ra *reminderAdapter) FireOverdue() ([]agent.FiredReminder, error) {
	fired, err := ra.store.FireOverdue()
	if err != nil {
		return nil, err
	}
	out := make([]agent.FiredReminder, len(fired))
	for i, r := range fired {
		out[i] = agent.FiredReminder{ID: r.ID, Text: r.Text}
	}
	return out, nil
}
