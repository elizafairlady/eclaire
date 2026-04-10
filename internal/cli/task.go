package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/spf13/cobra"
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "View background tasks",
}

func init() {
	tasksCmd.AddCommand(tasksListCmd, tasksAuditCmd)
}

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent tasks",
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

		data, err := client.Call(ctx, gateway.MethodTaskList, nil)
		if err != nil {
			return err
		}

		var tasks []*agent.Task
		if err := json.Unmarshal(data, &tasks); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		if len(tasks) == 0 {
			fmt.Println("No tasks.")
			return nil
		}

		fmt.Printf("%-24s %-14s %-10s %-10s %s\n", "ID", "AGENT", "STATUS", "AGE", "FLOW")
		for _, t := range tasks {
			age := time.Since(t.CreatedAt).Truncate(time.Second).String()
			flow := t.FlowID
			if flow == "" {
				flow = "-"
			}
			fmt.Printf("%-24s %-14s %-10s %-10s %s\n", truncate(t.ID, 24), t.AgentID, t.Status, age, flow)
		}
		return nil
	},
}

var tasksAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show failed tasks in the last 24 hours",
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

		data, err := client.Call(ctx, gateway.MethodTaskList, nil)
		if err != nil {
			return err
		}

		var tasks []*agent.Task
		if err := json.Unmarshal(data, &tasks); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		cutoff := time.Now().Add(-24 * time.Hour)
		var failures []*agent.Task
		for _, t := range tasks {
			if t.Status == agent.TaskFailed && t.UpdatedAt.After(cutoff) {
				failures = append(failures, t)
			}
		}

		if len(failures) == 0 {
			fmt.Println("No failures in last 24h.")
			return nil
		}

		fmt.Printf("%d failed tasks:\n\n", len(failures))
		for _, t := range failures {
			fmt.Printf("  %s (%s) — %s\n", t.ID, t.AgentID, t.Error)
			fmt.Printf("    at %s\n\n", t.UpdatedAt.Format("2006-01-02 15:04"))
		}
		return nil
	},
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
