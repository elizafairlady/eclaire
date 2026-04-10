package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage sessions",
}

func init() {
	sessionCmd.AddCommand(sessionListCmd, sessionContinueCmd, sessionCleanupCmd)
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions",
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

		data, err := client.Call(ctx, gateway.MethodSessionList, nil)
		if err != nil {
			return err
		}

		var sessions []persist.SessionMeta
		if err := json.Unmarshal(data, &sessions); err != nil {
			return fmt.Errorf("parse: %w", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions.")
			return nil
		}

		fmt.Printf("%-10s %-8s %-14s %-30s %-6s %-10s %s\n", "ID", "STATUS", "AGENT", "TITLE", "MSGS", "TOKENS", "UPDATED")
		for _, s := range sessions {
			title := s.Title
			if len(title) > 28 {
				title = title[:28] + ".."
			}
			parent := ""
			if s.ParentID != "" {
				parent = " (child of " + s.ParentID + ")"
			}
			fmt.Printf("%-10s %-8s %-14s %-30s %-6d %-10s %s%s\n",
				s.ID, s.Status, s.AgentID, title,
				s.MessageCount,
				formatTokens(s.TokensIn+s.TokensOut),
				s.UpdatedAt.Format("15:04"),
				parent,
			)
		}
		return nil
	},
}

func formatTokens(n int64) string {
	if n == 0 {
		return "-"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

var sessionContinueCmd = &cobra.Command{
	Use:   "continue [session-id] [prompt]",
	Short: "Continue an existing session",
	Args:  cobra.MinimumNArgs(2),
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

		sessionID := args[0]
		prompt := strings.Join(args[1:], " ")

		req := struct {
			SessionID string `json:"session_id"`
			Prompt    string `json:"prompt"`
		}{
			SessionID: sessionID,
			Prompt:    prompt,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, err := client.Stream(ctx, gateway.MethodSessionContinue, req)
		if err != nil {
			return fmt.Errorf("continue: %w", err)
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
				case agent.EventToolCall:
					fmt.Fprintf(cmd.ErrOrStderr(), "\n● %s\n", ev.ToolName)
				case agent.EventToolResult:
					fmt.Fprintf(cmd.ErrOrStderr(), "  → %s\n", strings.ReplaceAll(ev.Output, "\n", "\n    "))
				}
			case gateway.TypeResponse:
				if env.Error != nil {
					return fmt.Errorf("agent: %s", env.Error.Message)
				}
				fmt.Println()
				return nil
			}
		}
		return nil
	},
}

var sessionCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Archive stale sessions (active for >1h with no updates)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		store := persist.NewSessionStore(cfg.SessionsDir())
		marked, archived, err := store.CleanupStale(1 * time.Hour)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if marked == 0 {
			fmt.Println("No stale sessions found.")
		} else {
			fmt.Printf("Cleaned up %d stale sessions (%d archived).\n", marked, archived)
		}
		return nil
	},
}
