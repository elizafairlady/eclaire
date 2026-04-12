package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var (
	agentCreateRole  string
	agentCreateModel string
	agentCreateDesc  string
)

func init() {
	agentCmd.AddCommand(
		agentListCmd,
		agentCreateCmd,
		agentEditCmd,
		agentSpawnCmd,
		agentKillCmd,
	)

	agentCreateCmd.Flags().StringVar(&agentCreateRole, "role", "simple", "agent role (simple or complex)")
	agentCreateCmd.Flags().StringVar(&agentCreateModel, "model", "", "model override")
	agentCreateCmd.Flags().StringVar(&agentCreateDesc, "desc", "", "agent description")
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available agents",
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

		data, err := client.Call(ctx, gateway.MethodAgentList, nil)
		if err != nil {
			return err
		}

		var agents []agent.Info
		if err := json.Unmarshal(data, &agents); err != nil {
			return fmt.Errorf("parse agents: %w", err)
		}

		if len(agents) == 0 {
			fmt.Println("No agents configured.")
			fmt.Println("Add YAML agent definitions to ~/.eclaire/agents/")
			return nil
		}

		fmt.Printf("%-20s %-12s %-10s %s\n", "ID", "ROLE", "STATUS", "TOOLS")
		for _, a := range agents {
			tools := ""
			for i, t := range a.Tools {
				if i > 0 {
					tools += ", "
				}
				tools += t
			}
			fmt.Printf("%-20s %-12s %-10s %s\n", a.ID, a.Role, a.Status, tools)
		}
		return nil
	},
}

var agentCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Create a new agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := tool.NewReminderStore(cfg.RemindersPath()) // unused, just for manage deps pattern
		_ = store

		input := fmt.Sprintf(`{"operation":"agent_create","agent_id":%q,"agent_name":%q,"agent_role":%q,"agent_description":%q,"agent_model":%q}`,
			args[0], args[0], agentCreateRole, agentCreateDesc, agentCreateModel)

		// Use the manage tool directly (no gateway needed)
		manageTool := tool.ManageTool(tool.ManageDeps{
			AgentsDir: cfg.AgentsDir(),
			SkillsDir: cfg.SkillsDir(),
			FlowsDir:  cfg.FlowsDir(),
			CronPath:  cfg.CronPath(),
			Reload:    func() tool.ReloadResult { return tool.ReloadResult{} },
			CronList:  func() []tool.CronEntry { return nil },
			AgentList: func() []tool.AgentInfo { return nil },
		})

		resp, err := manageTool.Run(cmd.Context(), newToolCall(input))
		if err != nil {
			return err
		}
		if resp.IsError {
			return fmt.Errorf("%s", resp.Content)
		}
		fmt.Println(resp.Content)

		// Trigger reload if gateway is running
		client, err := gateway.Connect(cfg.SocketPath())
		if err == nil {
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client.Call(ctx, gateway.MethodGatewayReload, nil)
			fmt.Println("Gateway reloaded.")
		}

		return nil
	},
}

var agentEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit an agent's configuration",
	Long:  "Opens the agent's agent.yaml in $EDITOR, then reloads the gateway.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		agentPath := cfg.AgentsDir() + "/" + args[0] + "/agent.yaml"
		if _, err := os.Stat(agentPath); err != nil {
			return fmt.Errorf("agent %q not found at %s", args[0], agentPath)
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}

		editorCmd := exec.Command(editor, agentPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr
		if err := editorCmd.Run(); err != nil {
			return fmt.Errorf("editor: %w", err)
		}

		// Trigger reload if gateway is running
		client, err := gateway.Connect(cfg.SocketPath())
		if err == nil {
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client.Call(ctx, gateway.MethodGatewayReload, nil)
			fmt.Println("Gateway reloaded.")
		}

		return nil
	},
}

var agentSpawnCmd = &cobra.Command{
	Use:   "spawn [agent-type]",
	Short: "Spawn a persistent agent session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented — use 'ecl run -a %s' for agent sessions", args[0])
	},
}

var agentKillCmd = &cobra.Command{
	Use:   "kill [session-id]",
	Short: "Kill a running agent session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented — agent session management is pending")
	},
}
