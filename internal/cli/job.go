package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage scheduled jobs (at/every/cron)",
}

var (
	jobAddKind    string
	jobAddValue   string
	jobAddAgent   string
	jobAddPrompt  string
	jobAddName    string
)

func init() {
	jobCmd.AddCommand(jobListCmd, jobAddCmd, jobRemoveCmd, jobRunCmd, jobRunsCmd)

	jobAddCmd.Flags().StringVarP(&jobAddKind, "kind", "k", "", "Schedule kind: at, every, or cron")
	jobAddCmd.Flags().StringVar(&jobAddValue, "at", "", "Schedule value for 'at' kind (timestamp or duration)")
	jobAddCmd.Flags().StringVar(&jobAddValue, "every", "", "Schedule value for 'every' kind (duration)")
	jobAddCmd.Flags().StringVar(&jobAddValue, "expr", "", "Schedule value for 'cron' kind (5-field expression)")
	jobAddCmd.Flags().StringVarP(&jobAddAgent, "agent", "a", "", "Agent to run")
	jobAddCmd.Flags().StringVarP(&jobAddPrompt, "prompt", "p", "", "Prompt for the job")
	jobAddCmd.Flags().StringVarP(&jobAddName, "name", "n", "", "Job name (optional)")
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all scheduled jobs",
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

		data, err := client.Call(ctx, gateway.MethodJobList, nil)
		if err != nil {
			return err
		}

		var jobs []tool.JobInfo
		json.Unmarshal(data, &jobs)

		if len(jobs) == 0 {
			fmt.Println("No scheduled jobs.")
			return nil
		}

		for _, j := range jobs {
			enabled := "enabled"
			if !j.Enabled {
				enabled = "disabled"
			}
			fmt.Printf("%-10s %-20s %-6s %-20s %-10s agent=%-12s next=%s\n",
				j.ID, j.Name, j.ScheduleKind, j.Schedule, enabled, j.AgentID, j.NextRunAt)
		}
		return nil
	},
}

var jobAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Schedule a new job",
	Long: `Schedule a new job with one of three schedule kinds:
  --kind at    --at "2h"          One-shot: fires once after duration or at timestamp
  --kind every --every "30m"      Recurring: fires at fixed interval
  --kind cron  --expr "0 7 * * *" Recurring: 5-field cron expression`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jobAddKind == "" {
			return fmt.Errorf("--kind is required (at, every, or cron)")
		}
		if jobAddAgent == "" {
			return fmt.Errorf("--agent is required")
		}
		if jobAddPrompt == "" {
			// Allow prompt as positional arg
			if len(args) > 0 {
				jobAddPrompt = strings.Join(args, " ")
			} else {
				return fmt.Errorf("--prompt is required")
			}
		}
		if jobAddValue == "" {
			return fmt.Errorf("schedule value is required (--at, --every, or --expr)")
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

		req := map[string]any{
			"name":           jobAddName,
			"schedule_kind":  jobAddKind,
			"schedule_value": jobAddValue,
			"agent_id":       jobAddAgent,
			"prompt":         jobAddPrompt,
		}
		reqData, _ := json.Marshal(req)

		data, err := client.Call(ctx, gateway.MethodJobAdd, reqData)
		if err != nil {
			return err
		}

		var info tool.JobInfo
		json.Unmarshal(data, &info)
		fmt.Printf("Scheduled job %s (%s %s) agent=%s\n", info.ID, info.ScheduleKind, info.Schedule, info.AgentID)
		if info.NextRunAt != "" {
			fmt.Printf("Next run: %s\n", info.NextRunAt)
		}
		return nil
	},
}

var jobRemoveCmd = &cobra.Command{
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

		reqData, _ := json.Marshal(map[string]string{"id": args[0]})
		_, err = client.Call(ctx, gateway.MethodJobRemove, reqData)
		if err != nil {
			return err
		}
		fmt.Printf("Removed job %s\n", args[0])
		return nil
	},
}

var jobRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Trigger a job immediately",
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

		reqData, _ := json.Marshal(map[string]string{"id": args[0]})
		_, err = client.Call(ctx, gateway.MethodJobRun, reqData)
		if err != nil {
			return err
		}
		fmt.Printf("Triggered job %s\n", args[0])
		return nil
	},
}

var jobRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "Show execution history for a job",
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

		reqData, _ := json.Marshal(map[string]string{"id": args[0]})
		data, err := client.Call(ctx, gateway.MethodJobRuns, reqData)
		if err != nil {
			return err
		}

		var entries []tool.JobRunLogEntry
		json.Unmarshal(data, &entries)

		if len(entries) == 0 {
			fmt.Printf("No execution history for job %s\n", args[0])
			return nil
		}

		for _, e := range entries {
			status := e.Status
			if e.Error != "" {
				status += ": " + e.Error
			}
			fmt.Printf("%-20s %-8s %s %s\n", e.Timestamp, status, e.Duration, e.Summary)
		}
		return nil
	},
}
