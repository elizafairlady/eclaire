package cli

import (
	"fmt"

	"github.com/elizafairlady/eclaire/internal/tool"
	"github.com/spf13/cobra"
)

var briefingCmd = &cobra.Command{
	Use:   "briefing",
	Short: "Generate today's briefing",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := tool.NewReminderStore(cfg.RemindersPath())
		t := tool.BriefingTool(tool.BriefingDeps{
			Reminders:    store,
			WorkspaceDir: cfg.WorkspaceDir(),
			BriefingsDir: cfg.BriefingsDir(),
		})

		resp, err := t.Run(cmd.Context(), newToolCall(`{"operation":"generate"}`))
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
