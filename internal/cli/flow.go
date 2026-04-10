package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/spf13/cobra"
)

var flowCmd = &cobra.Command{
	Use:   "flow",
	Short: "Manage and run flow pipelines",
}

func init() {
	flowCmd.AddCommand(flowListCmd, flowRunCmd, flowCreateCmd, flowStatusCmd)
}

var flowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available flows",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		client, err := gateway.Connect(cfg.SocketPath())
		if err != nil {
			return fmt.Errorf("gateway not running (try: ecl daemon start)")
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data, err := client.Call(ctx, gateway.MethodFlowList, nil)
		if err != nil {
			return err
		}

		var flows []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Steps       int    `json:"steps"`
		}
		if err := json.Unmarshal(data, &flows); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		if len(flows) == 0 {
			fmt.Println("No flows defined.")
			fmt.Println("Create one with: ecl flow create <id>")
			return nil
		}

		fmt.Printf("%-20s %-20s %-6s %s\n", "ID", "NAME", "STEPS", "DESCRIPTION")
		for _, f := range flows {
			fmt.Printf("%-20s %-20s %-6d %s\n", f.ID, f.Name, f.Steps, f.Description)
		}
		return nil
	},
}

var flowRunCmd = &cobra.Command{
	Use:   "run <flow-id> [input...]",
	Short: "Run a flow pipeline",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		client, err := gateway.EnsureGateway(cfg.SocketPath(), cfg.PIDPath())
		if err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
		defer client.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req := gateway.FlowRunRequest{
			FlowID: args[0],
		}
		if len(args) > 1 {
			req.Input = strings.Join(args[1:], " ")
		}

		ch, err := client.Stream(ctx, gateway.MethodFlowRun, req)
		if err != nil {
			return fmt.Errorf("flow run: %w", err)
		}

		for env := range ch {
			switch env.Type {
			case gateway.TypeStream:
				var ev agent.StreamEvent
				if json.Unmarshal(env.Data, &ev) != nil {
					continue
				}
				switch ev.Type {
				case agent.EventTextDelta:
					fmt.Print(ev.Delta)
				case "flow_started", "flow_step_started", "flow_step_completed", "flow_completed":
					fmt.Fprintf(cmd.ErrOrStderr(), "● %s\n", ev.Output)
				case agent.EventToolCall:
					fmt.Fprintf(cmd.ErrOrStderr(), "  ▸ %s\n", ev.ToolName)
				case agent.EventToolResult:
					fmt.Fprintf(cmd.ErrOrStderr(), "    → %s\n", strings.ReplaceAll(ev.Output, "\n", "\n      "))
				case agent.EventError:
					fmt.Fprintf(cmd.ErrOrStderr(), "ERROR: %s\n", ev.Error)
				}
			case gateway.TypeResponse:
				if env.Error != nil {
					return fmt.Errorf("flow: %s", env.Error.Message)
				}
				fmt.Println()
				return nil
			}
		}
		return nil
	},
}

var flowCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Scaffold a new flow definition",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		flowID := args[0]
		flowsDir := cfg.FlowsDir()
		os.MkdirAll(flowsDir, 0o755)

		path := fmt.Sprintf("%s/%s.yaml", flowsDir, flowID)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("flow %q already exists at %s", flowID, path)
		}

		scaffold := fmt.Sprintf(`id: %s
name: %s
description: ""
steps:
  - name: step-1
    agent: orchestrator
    prompt: "{{.Input}}"
`, flowID, flowID)

		if err := os.WriteFile(path, []byte(scaffold), 0o644); err != nil {
			return err
		}

		fmt.Printf("Created flow scaffold at %s\n", path)
		fmt.Println("Edit the file to define your pipeline steps, then run with: ecl flow run", flowID)
		return nil
	},
}

var flowStatusCmd = &cobra.Command{
	Use:   "status <run-id>",
	Short: "Check status of a flow run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		client, err := gateway.Connect(cfg.SocketPath())
		if err != nil {
			return fmt.Errorf("gateway not running (try: ecl daemon start)")
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req := gateway.FlowStatusRequest{RunID: args[0]}
		data, err := client.Call(ctx, gateway.MethodFlowStatus, req)
		if err != nil {
			return err
		}

		var steps []struct {
			ID     string `json:"id"`
			Agent  string `json:"agent_id"`
			Status string `json:"status"`
			Output string `json:"output,omitempty"`
			Error  string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &steps); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		fmt.Printf("Flow run: %s (%d steps)\n\n", args[0], len(steps))
		for i, s := range steps {
			status := s.Status
			errStr := ""
			if s.Error != "" {
				errStr = fmt.Sprintf(" error=%s", s.Error)
			}
			fmt.Printf("  %d. [%s] %s (agent: %s)%s\n", i+1, status, s.ID, s.Agent, errStr)
			if s.Output != "" {
				fmt.Printf("     → %s\n", s.Output)
			}
		}
		return nil
	},
}
