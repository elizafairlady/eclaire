package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/gateway"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/spf13/cobra"
	"github.com/charmbracelet/x/term"
)

var (
	runAgent       string
	runContinue    string
	runSessionName string
	runBackground  bool
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a non-interactive single prompt",
	Args:  cobra.MinimumNArgs(0),
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

		agentID := runAgent
		if agentID == "" {
			agentID = "orchestrator"
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Session continuation mode
		if runContinue != "" {
			return runContinueSession(ctx, cmd, client, agentID, args)
		}

		// Normal run mode — prompt required
		if len(args) == 0 {
			return fmt.Errorf("prompt required (use --continue to resume a session)")
		}

		prompt := strings.Join(args, " ")
		cwd, _ := os.Getwd()
		req := gateway.AgentRunRequest{
			AgentID:    agentID,
			Prompt:     prompt,
			CWD:        cwd,
			Title:      runSessionName,
			Background: runBackground,
		}

		if runBackground {
			// Fire and forget — gateway runs it, approvals/results go to notifications
			_, err := client.Call(ctx, gateway.MethodAgentRun, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Started background run (agent=%s). Check results with: ecl notifications\n", agentID)
			return nil
		}

		return streamRun(ctx, cmd, client, gateway.MethodAgentRun, req)
	},
}

func init() {
	runCmd.Flags().StringVarP(&runAgent, "agent", "a", "", "agent to use")
	runCmd.Flags().StringVarP(&runContinue, "continue", "c", "", "resume session (bare = most recent, or session ID)")
	runCmd.Flags().Lookup("continue").NoOptDefVal = "latest"
	runCmd.Flags().StringVarP(&runSessionName, "name", "n", "", "name the session")
	runCmd.Flags().BoolVar(&runBackground, "background", false, "run in background (no interactive approval, results via ecl notifications)")
}

func runContinueSession(ctx context.Context, cmd *cobra.Command, client *gateway.Client, agentID string, args []string) error {
	var sessionID string

	if runContinue == "latest" {
		// Find most recent session
		reqData := struct {
			AgentID string `json:"agent_id"`
		}{AgentID: agentID}

		resp, err := client.Call(ctx, gateway.MethodSessionMostRecent, reqData)
		if err != nil {
			return fmt.Errorf("find recent session: %w", err)
		}
		var meta persist.SessionMeta
		if err := json.Unmarshal(resp, &meta); err != nil {
			return fmt.Errorf("parse session: %w", err)
		}
		sessionID = meta.ID
		fmt.Fprintf(cmd.ErrOrStderr(), "Resuming session %s: %s\n", meta.ID, meta.Title)
	} else {
		sessionID = runContinue
		fmt.Fprintf(cmd.ErrOrStderr(), "Resuming session %s\n", sessionID)
	}

	prompt := "Continue from where we left off."
	if len(args) > 0 {
		prompt = strings.Join(args, " ")
	}

	req := struct {
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
	}{
		SessionID: sessionID,
		Prompt:    prompt,
	}

	return streamRun(ctx, cmd, client, gateway.MethodSessionContinue, req)
}

func streamRun(ctx context.Context, cmd *cobra.Command, client *gateway.Client, method string, req any) error {
	ch, err := client.Stream(ctx, method, req)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	// Listen for approval requests in parallel — only if stdin is a terminal.
	// Without a TTY, approvals go through `ecl notifications` instead.
	if term.IsTerminal(os.Stdin.Fd()) {
		go handleCLIApprovals(ctx, client)
	}

	for env := range ch {
		switch env.Type {
		case gateway.TypeStream:
			var ev agent.StreamEvent
			if err := json.Unmarshal(env.Data, &ev); err != nil {
				continue
			}
			switch ev.Type {
			case agent.EventTextDelta:
				fmt.Print(ev.Delta)
			case agent.EventToolCall:
				fmt.Fprintf(cmd.ErrOrStderr(), "\n● %s\n", ev.ToolName)
			case agent.EventToolResult:
				fmt.Fprintf(cmd.ErrOrStderr(), "  → %s\n", strings.ReplaceAll(ev.Output, "\n", "\n    "))
			case agent.EventStepFinish:
				if ev.Usage != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  [%d in / %d out tokens]\n", ev.Usage.InputTokens, ev.Usage.OutputTokens)
				}
			case "compaction":
				fmt.Fprintf(cmd.ErrOrStderr(), "  [compacted: %s]\n", ev.Output)
			case agent.EventError:
				fmt.Fprintf(cmd.ErrOrStderr(), "ERROR: %s\n", ev.Error)
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
}

// handleCLIApprovals reads approval requests from the gateway event stream
// and prompts the user on stdin for y/n.
func handleCLIApprovals(ctx context.Context, client *gateway.Client) {
	events := client.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-events:
			if !ok {
				return
			}
			// Check if this is an approval request
			var approvalReq struct {
				ID          string `json:"id"`
				AgentID     string `json:"agent_id"`
				Action      string `json:"action"`
				Description string `json:"description"`
			}
			if json.Unmarshal(env.Data, &approvalReq) != nil || approvalReq.ID == "" {
				continue
			}

			// Prompt user
			fmt.Fprintf(os.Stderr, "\n⚡ Approval needed: %s\n", approvalReq.Description)
			fmt.Fprintf(os.Stderr, "  Allow? [y/N] ")

			var answer string
			fmt.Scanln(&answer)
			approved := strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y")

			// Send response
			resp := map[string]any{
				"request_id": approvalReq.ID,
				"approved":   approved,
			}
			respData, _ := json.Marshal(resp)
			client.Call(ctx, gateway.MethodApprovalRespond, respData)

			if approved {
				fmt.Fprintf(os.Stderr, "  ✓ Approved\n")
			} else {
				fmt.Fprintf(os.Stderr, "  ✗ Denied\n")
			}
		}
	}
}
