package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"
)

// BriefingDeps holds everything the briefing tool needs.
type BriefingDeps struct {
	Reminders   *ReminderStore
	WorkspaceDir string // ~/.eclaire/workspace/
	BriefingsDir string // ~/.eclaire/workspace/briefings/
	CronList    func() []CronEntry
}

type briefingInput struct {
	Operation string `json:"operation" jsonschema:"description=Operation: generate"`
}

// BriefingTool creates the eclaire_briefing tool.
func BriefingTool(deps BriefingDeps) Tool {
	return NewTool("eclaire_briefing",
		"Generate a structured daily briefing. Operations: generate.",
		TrustModify, "briefing",
		func(ctx context.Context, input briefingInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch input.Operation {
			case "generate", "":
				return handleBriefingGenerate(deps)
			default:
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("unknown operation %q; valid: generate", input.Operation),
				), nil
			}
		},
	)
}

func handleBriefingGenerate(deps BriefingDeps) (fantasy.ToolResponse, error) {
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02")

	var sections []string
	sections = append(sections, fmt.Sprintf("# Daily Briefing — %s", today))

	// Reminders section
	if deps.Reminders != nil {
		overdue, _ := deps.Reminders.Overdue()
		pending, _ := deps.Reminders.Pending()

		if len(overdue) > 0 || len(pending) > 0 {
			var sb strings.Builder
			sb.WriteString("## Reminders\n")

			if len(overdue) > 0 {
				sb.WriteString(fmt.Sprintf("\n**%d overdue:**\n", len(overdue)))
				for _, r := range overdue {
					sb.WriteString(fmt.Sprintf("- [%s] %s (was due %s)\n", r.ID, r.Text, r.DueAt.Format("2006-01-02 15:04")))
				}
			}

			// Upcoming today
			var todayItems []Reminder
			endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
			for _, r := range pending {
				if !r.DueAt.Before(now) && r.DueAt.Before(endOfDay) {
					todayItems = append(todayItems, r)
				}
			}
			if len(todayItems) > 0 {
				sb.WriteString(fmt.Sprintf("\n**%d due today:**\n", len(todayItems)))
				for _, r := range todayItems {
					sb.WriteString(fmt.Sprintf("- [%s] %s (due %s)\n", r.ID, r.Text, r.DueAt.Format("15:04")))
				}
			}

			// Upcoming later
			var laterItems []Reminder
			for _, r := range pending {
				if r.DueAt.After(endOfDay) {
					laterItems = append(laterItems, r)
				}
			}
			if len(laterItems) > 0 {
				sb.WriteString(fmt.Sprintf("\n**%d upcoming:**\n", len(laterItems)))
				limit := 10
				if len(laterItems) < limit {
					limit = len(laterItems)
				}
				for _, r := range laterItems[:limit] {
					sb.WriteString(fmt.Sprintf("- [%s] %s (due %s)\n", r.ID, r.Text, r.DueAt.Format("2006-01-02 15:04")))
				}
				if len(laterItems) > 10 {
					sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(laterItems)-10))
				}
			}

			sections = append(sections, sb.String())
		} else {
			sections = append(sections, "## Reminders\n\nNo pending reminders.")
		}
	}

	// Yesterday's daily memory
	dailyPath := filepath.Join(deps.WorkspaceDir, "daily", yesterday+".md")
	if data, err := os.ReadFile(dailyPath); err == nil && len(data) > 0 {
		content := string(data)
		sections = append(sections, fmt.Sprintf("## Yesterday's Notes (%s)\n\n%s", yesterday, content))
	}

	// Cron status
	if deps.CronList != nil {
		entries := deps.CronList()
		if len(entries) > 0 {
			var sb strings.Builder
			sb.WriteString("## Scheduled Jobs\n\n")
			for _, e := range entries {
				enabled := "active"
				if !e.Enabled {
					enabled = "disabled"
				}
				sb.WriteString(fmt.Sprintf("- **%s** [%s] (%s) → %s\n", e.ID, e.Schedule, enabled, e.AgentID))
			}
			sections = append(sections, sb.String())
		}
	}

	// System info
	hostname, _ := os.Hostname()
	sections = append(sections, fmt.Sprintf("## System\n\n- Host: %s\n- Time: %s\n- Good morning.",
		hostname, now.Format("15:04 MST")))

	briefing := strings.Join(sections, "\n\n---\n\n")

	// Save briefing to file
	os.MkdirAll(deps.BriefingsDir, 0o755)
	briefingPath := filepath.Join(deps.BriefingsDir, today+".md")
	os.WriteFile(briefingPath, []byte(briefing), 0o644)

	return fantasy.ToolResponse{
		Content: briefing,
	}, nil
}
