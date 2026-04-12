package cli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/elizafairlady/eclaire/internal/config"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/ui"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
	"github.com/spf13/cobra"
)

var daemonMode bool
var noProject bool

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
	Root.Flags().BoolVar(&noProject, "no-project", false, "skip project directory detection and init prompt")

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
		initCmd,
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

	cwd, _ := os.Getwd()

	// Offer to init project directory if not home and no .eclaire/ exists
	if !noProject {
		maybeInitProject(cwd)
	}

	// Send CWD to gateway and get session context
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

// maybeInitProject prompts the user to create .eclaire/ in the current directory
// if it doesn't exist and this isn't $HOME. Only prompts on a TTY.
func maybeInitProject(cwd string) {
	home, _ := os.UserHomeDir()
	if cwd == home || cwd == "/" {
		return
	}

	eclaireDir := filepath.Join(cwd, ".eclaire")
	if info, err := os.Stat(eclaireDir); err == nil && info.IsDir() {
		return // already exists
	}

	// Also check if a parent has .eclaire/ (we're in a subdirectory of an existing project)
	dir := cwd
	for {
		parent := filepath.Dir(dir)
		if parent == dir || parent == home {
			break
		}
		if info, err := os.Stat(filepath.Join(parent, ".eclaire")); err == nil && info.IsDir() {
			return // parent project exists
		}
		dir = parent
	}

	// Prompt on TTY
	fmt.Fprintf(os.Stderr, "No .eclaire/ found in %s\nCreate project directory? [Y/n] ", cwd)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))

	if line == "" || line == "y" || line == "yes" {
		if err := initProjectDir(eclaireDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to init project: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Created %s\n", eclaireDir)
		}
	}
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
