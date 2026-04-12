package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/elizafairlady/eclaire/internal/ui/dialog"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
)

// ApprovalRequestMsg is sent to the TUI when a tool needs user approval.
type ApprovalRequestMsg struct {
	RequestID   string
	AgentID     string
	Action      string
	Description string
}

// ApprovalResponseMsg carries the user's decision back to the gateway.
type ApprovalResponseMsg struct {
	RequestID string
	Approved  bool
	Persist   bool // true = "always for session"
}

// approvalDialog is a dialog that asks the user to approve or deny a tool action.
type approvalDialog struct {
	id          string
	requestID   string
	description string
	action      string
	agentID     string
	responseCh  chan ApprovalResponseMsg
}

func newApprovalDialog(msg ApprovalRequestMsg, responseCh chan ApprovalResponseMsg) *approvalDialog {
	return &approvalDialog{
		id:          "approval-" + msg.RequestID,
		requestID:   msg.RequestID,
		description: msg.Description,
		action:      msg.Action,
		agentID:     msg.AgentID,
		responseCh:  responseCh,
	}
}

func (d *approvalDialog) ID() string { return d.id }

func (d *approvalDialog) HandleMsg(msg tea.Msg) dialog.Action {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			d.responseCh <- ApprovalResponseMsg{RequestID: d.requestID, Approved: true}
			return dialog.CloseAction{}
		case "a", "A":
			d.responseCh <- ApprovalResponseMsg{RequestID: d.requestID, Approved: true, Persist: true}
			return dialog.CloseAction{}
		case "n", "N", "esc":
			d.responseCh <- ApprovalResponseMsg{RequestID: d.requestID, Approved: false}
			return dialog.CloseAction{}
		}
	}
	return dialog.NoneAction{}
}

func (d *approvalDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	width := area.Dx() - 8
	if width < 30 {
		width = 30
	}
	if width > 80 {
		width = 80
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Yellow).
		Render("⚠ Permission Required")

	body := fmt.Sprintf("Agent: %s\nAction: %s\n\n%s", d.agentID, d.action, d.description)

	prompt := lipgloss.NewStyle().
		Foreground(styles.FgBase).
		Bold(true).
		Render("[Y] Allow once  [A] Always for session  [N] Deny")

	content := title + "\n\n" + body + "\n\n" + prompt

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Yellow).
		Padding(1, 2).
		Width(width).
		Render(content)

	// Center the box
	lines := lipgloss.Height(box)
	x := area.Min.X + (area.Dx()-lipgloss.Width(box))/2
	y := area.Min.Y + (area.Dy()-lines)/2
	if x < area.Min.X {
		x = area.Min.X
	}
	if y < area.Min.Y {
		y = area.Min.Y
	}

	uv.NewStyledString(box).Draw(scr, uv.Rect(x, y, x+lipgloss.Width(box), y+lines))
	return nil
}
