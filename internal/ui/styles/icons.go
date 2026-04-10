package styles

import "github.com/elizafairlady/eclaire/internal/agent"

// StatusIcon returns the styled icon for an agent status.
func StatusIcon(s Styles, status agent.Status) string {
	switch status {
	case agent.StatusRunning:
		return s.AgentRunning.Render(ToolPending)
	case agent.StatusIdle:
		return s.AgentIdle.Render(RadioOff)
	case agent.StatusWaiting:
		return s.AgentWaiting.Render(ToolPending)
	case agent.StatusError:
		return s.AgentError.Render(ToolError)
	case agent.StatusSpawning:
		return s.AgentIdle.Render(SpinnerIcon)
	default:
		return s.AgentIdle.Render(RadioOff)
	}
}
