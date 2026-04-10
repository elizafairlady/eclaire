package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the gateway daemon",
}

func init() {
	daemonCmd.AddCommand(
		daemonStartCmd,
		daemonStopCmd,
		daemonStatusCmd,
	)
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if err := cfg.EnsureDirs(); err != nil {
			return err
		}

		client, err := gateway.EnsureGateway(cfg.SocketPath(), cfg.PIDPath())
		if err != nil {
			return err
		}
		defer client.Close()

		fmt.Println("Gateway started.")
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the gateway daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(cfg.PIDPath())
		if err != nil {
			return fmt.Errorf("gateway not running (no PID file)")
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("invalid PID file")
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process: %w", err)
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("signal: %w", err)
		}

		// Wait for socket to disappear
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(cfg.SocketPath()); os.IsNotExist(err) {
				fmt.Println("Gateway stopped.")
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}

		fmt.Println("Gateway stop signal sent.")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show gateway daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		client, err := gateway.Connect(cfg.SocketPath())
		if err != nil {
			fmt.Println("Gateway: not running")
			return nil
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data, err := client.Call(ctx, gateway.MethodGatewayStatus, nil)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}

		var status gateway.GatewayStatus
		if err := json.Unmarshal(data, &status); err != nil {
			return fmt.Errorf("parse status: %w", err)
		}

		fmt.Printf("Gateway: running\n")
		fmt.Printf("  PID:     %d\n", status.PID)
		fmt.Printf("  Uptime:  %s\n", status.Uptime)
		fmt.Printf("  Agents:  %d\n", status.ActiveAgents)
		fmt.Printf("  Clients: %d\n", status.ActiveClients)
		return nil
	},
}
