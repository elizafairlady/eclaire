package cli

import (
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/spf13/cobra"
)

// newToolCall creates a fantasy.ToolCall with the given JSON input.
func newToolCall(input string) fantasy.ToolCall {
	return fantasy.ToolCall{Input: input}
}

var remindAt string

var remindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Manage reminders",
}

func init() {
	remindCmd.AddCommand(remindAddCmd, remindListCmd, remindDoneCmd)

	remindAddCmd.Flags().StringVar(&remindAt, "at", "", "when due (e.g. '2h', '30m', '1d', '2026-04-09 09:00')")
}

var remindAddCmd = &cobra.Command{
	Use:   "add <text...> --at <due>",
	Short: "Add a reminder",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if remindAt == "" {
			return fmt.Errorf("--at is required (e.g. --at 2h)")
		}

		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := tool.NewReminderStore(cfg.RemindersPath())
		t := tool.ReminderTool(store)

		input := fmt.Sprintf(`{"operation":"add","text":%q,"due":%q}`, strings.Join(args, " "), remindAt)

		resp, err := t.Run(cmd.Context(), newToolCall(input))
		if err != nil {
			return err
		}
		if resp.IsError {
			return fmt.Errorf("%s", resp.Content)
		}
		fmt.Println(resp.Content)
		return nil
	},
}

var remindListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending reminders",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := tool.NewReminderStore(cfg.RemindersPath())
		t := tool.ReminderTool(store)

		resp, err := t.Run(cmd.Context(), newToolCall(`{"operation":"list"}`))
		if err != nil {
			return err
		}
		fmt.Print(resp.Content)
		return nil
	},
}

var remindDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a reminder as done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := tool.NewReminderStore(cfg.RemindersPath())
		t := tool.ReminderTool(store)

		input := fmt.Sprintf(`{"operation":"done","id":%q}`, args[0])
		resp, err := t.Run(cmd.Context(), newToolCall(input))
		if err != nil {
			return err
		}
		if resp.IsError {
			return fmt.Errorf("%s", resp.Content)
		}
		fmt.Println(resp.Content)
		return nil
	},
}
