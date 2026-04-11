package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/spf13/cobra"
)

// cronEntry is a local type for deserializing the gateway's cron.list response.
// The gateway returns jobs from the unified store in this format.
type cronEntry struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	AgentID  string `json:"agent_id"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
}

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Manage scheduled jobs (legacy alias for 'ecl job')",
}

func init() {
	cronCmd.AddCommand(cronListCmd, cronAddCmd, cronRemoveCmd)

	cronAddCmd.Flags().StringVarP(&cronAddSchedule, "schedule", "s", "", "5-field cron expression (e.g. '0 9 * * *')")
	cronAddCmd.Flags().StringVarP(&cronAddAgent, "agent", "a", "", "agent to run")
	cronAddCmd.Flags().StringVarP(&cronAddPrompt, "prompt", "p", "", "prompt for the cron job")
}

var cronListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled jobs",
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

		var entries []cronEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println("No scheduled jobs.")
			return nil
		}

		fmt.Printf("%-20s %-20s %-8s %-14s %s\n", "ID", "SCHEDULE", "ENABLED", "AGENT", "PROMPT")
		for _, e := range entries {
			enabled := "yes"
			if !e.Enabled {
				enabled = "no"
			}
			fmt.Printf("%-20s %-20s %-8s %-14s %s\n", e.ID, e.Schedule, enabled, e.AgentID, e.Prompt)
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
	Short: "Add or update a cron job",
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

		fmt.Printf("Cron job %q added (schedule: %s, agent: %s).\n", args[0], cronAddSchedule, cronAddAgent)
		return nil
	},
}

var cronRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a scheduled job",
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

		fmt.Printf("Job %q removed.\n", args[0])
		return nil
	},
}
