package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/spf13/cobra"
)

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Manage cron schedule",
}

func init() {
	cronCmd.AddCommand(cronListCmd, cronAddCmd, cronRemoveCmd)

	cronAddCmd.Flags().StringVarP(&cronAddSchedule, "schedule", "s", "", "5-field cron expression (e.g. '0 9 * * *')")
	cronAddCmd.Flags().StringVarP(&cronAddAgent, "agent", "a", "", "agent to run")
	cronAddCmd.Flags().StringVarP(&cronAddPrompt, "prompt", "p", "", "prompt for the cron job")
}

var cronListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cron entries",
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

		data, err := client.Call(ctx, gateway.MethodCronList, nil)
		if err != nil {
			return err
		}

		var entries []agent.CronEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println("No cron entries.")
			return nil
		}

		fmt.Printf("%-20s %-16s %-8s %-14s %s\n", "ID", "SCHEDULE", "ENABLED", "AGENT", "PROMPT")
		for _, e := range entries {
			enabled := "yes"
			if !e.Enabled {
				enabled = "no"
			}
			fmt.Printf("%-20s %-16s %-8s %-14s %s\n", e.ID, e.Schedule, enabled, e.AgentID, e.Prompt)
		}
		return nil
	},
}

var (
	cronAddSchedule string
	cronAddAgent    string
	cronAddPrompt   string
)

var cronAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Add or update a cron entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cronAddSchedule == "" || cronAddAgent == "" || cronAddPrompt == "" {
			return fmt.Errorf("--schedule, --agent, and --prompt are all required")
		}
		if len(strings.Fields(cronAddSchedule)) != 5 {
			return fmt.Errorf("schedule must be a 5-field cron expression")
		}

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

		req := gateway.CronAddRequest{
			ID:       args[0],
			Schedule: cronAddSchedule,
			AgentID:  cronAddAgent,
			Prompt:   cronAddPrompt,
		}

		_, err = client.Call(ctx, gateway.MethodCronAdd, req)
		if err != nil {
			return err
		}

		fmt.Printf("Cron entry %q added (schedule: %s, agent: %s).\n", args[0], cronAddSchedule, cronAddAgent)
		return nil
	},
}

var cronRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a cron entry",
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

		req := gateway.CronRemoveRequest{ID: args[0]}
		_, err = client.Call(ctx, gateway.MethodCronRemove, req)
		if err != nil {
			return err
		}

		fmt.Printf("Cron entry %q removed.\n", args[0])
		return nil
	},
}
