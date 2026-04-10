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

var (
	notifShowAll bool
)

var notificationsCmd = &cobra.Command{
	Use:     "notifications [id] [action] [value]",
	Aliases: []string{"notif"},
	Short:   "Show pending notifications or act on one",
	Long: `Show pending notifications, or act on a specific notification.

  ecl notifications              List pending notifications
  ecl notifications --all        List all notifications (including resolved)
  ecl notifications <id> <action> [value]   Act on a notification

Actions depend on the notification source:
  reminder:  complete, dismiss, snooze [duration]
  approval:  yes, always, no
  other:     dismiss`,
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

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Act on a notification: ecl notifications <id> <action> [value]
		if len(args) >= 2 {
			req := gateway.NotificationActRequest{
				ID:     args[0],
				Action: args[1],
			}
			if len(args) >= 3 {
				req.Value = strings.Join(args[2:], " ")
			}
			data, err := client.Call(ctx, gateway.MethodNotificationAct, req)
			if err != nil {
				return err
			}
			var result map[string]string
			json.Unmarshal(data, &result)
			for k, v := range result {
				fmt.Printf("%s: %s\n", k, v)
			}
			return nil
		}

		// List notifications
		data, err := client.Call(ctx, gateway.MethodNotificationList, nil)
		if err != nil {
			return err
		}

		var notifications []agent.Notification
		json.Unmarshal(data, &notifications)

		if len(notifications) == 0 {
			fmt.Println("No pending notifications.")
			return nil
		}

		for _, n := range notifications {
			sevIcon := ""
			switch n.Severity {
			case agent.SeverityError:
				sevIcon = "[ERROR]"
			case agent.SeverityWarning:
				sevIcon = "[WARN] "
			case agent.SeverityInfo:
				sevIcon = "[INFO] "
			case agent.SeverityDebug:
				sevIcon = "[DEBUG]"
			}

			ts := n.CreatedAt.Format("15:04:05")
			actions := ""
			if len(n.Actions) > 0 {
				actions = "  [" + strings.Join(n.Actions, ", ") + "]"
			}
			fmt.Printf("%s %s (%s) %s%s\n", sevIcon, ts, n.ID, n.Title, actions)
			if n.Content != "" {
				fmt.Printf("         %s\n", n.Content)
			}
		}
		return nil
	},
}

func init() {
	notificationsCmd.Flags().BoolVar(&notifShowAll, "all", false, "Show all notifications (including resolved)")
}
