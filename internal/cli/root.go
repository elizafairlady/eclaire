package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/elizafairlady/eclaire/internal/config"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/ui"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
	"github.com/spf13/cobra"
)

var daemonMode bool

// Root is the root cobra command.
var Root = &cobra.Command{
	Use:   "ecl",
	Short: "Personal AI orchestration CLI",
	Long:  "eclaire - a minimalist personal AI orchestration system",
	RunE:  runInteractive,
}

func init() {
	Root.PersistentFlags().BoolVar(&daemonMode, "daemon", false, "run as gateway daemon (internal)")
	Root.PersistentFlags().MarkHidden("daemon")

	Root.AddCommand(
		daemonCmd,
		runCmd,
		agentCmd,
		sessionCmd,
		systemPromptCmd,
		cronCmd,
		jobCmd,
		flowCmd,
		remindCmd,
		briefingCmd,
		tasksCmd,
		notificationsCmd,
	)
}

// Execute runs the root command.
func Execute() {
	if err := Root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runInteractive(cmd *cobra.Command, args []string) error {
	if daemonMode {
		return runDaemon()
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	client, err := gateway.EnsureGateway(cfg.SocketPath(), cfg.PIDPath())
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	defer client.Close()

	// Send CWD to gateway and get session context
	cwd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	connectResp, connectErr := client.ConnectWithCWD(ctx, cwd)
	cancel()

	opts := ui.AppOptions{
		BriefingsDir:  cfg.BriefingsDir(),
		RemindersPath: cfg.RemindersPath(),
	}
	if connectErr == nil && connectResp != nil {
		opts.MainSessionID = connectResp.MainSessionID
		opts.ProjectSessionID = connectResp.ProjectSessionID
		opts.ProjectRoot = connectResp.ProjectRoot
	}

	s := styles.Default()
	model := ui.NewApp(client, s, opts)

	p := tea.NewProgram(model)

	_, err = p.Run()
	return err
}

func runDaemon() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	logFile, err := os.OpenFile(
		cfg.LogDir()+"/gateway.log",
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Merged().Gateway.LogLevel),
	}))

	gw, err := gateway.New(cfg, logger)
	if err != nil {
		return err
	}

	return gw.Start()
}

func loadConfig() (*config.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	return config.Load(cwd)
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
