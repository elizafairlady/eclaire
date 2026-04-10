package bus

// Well-known topics.
const (
	TopicAgentStarted   = "agent.started"
	TopicAgentCompleted = "agent.completed"
	TopicAgentError     = "agent.error"
	TopicAgentHeartbeat = "agent.heartbeat"

	TopicToolCall   = "tool.call"
	TopicToolResult = "tool.result"

	TopicApprovalRequest = "approval.request"
	TopicApprovalResult  = "approval.result"

	TopicProviderSwitch = "provider.switch"
	TopicProviderError  = "provider.error"

	TopicStreamDelta = "stream.delta"

	// Sub-agent lifecycle
	TopicSubAgentStarted   = "subagent.started"
	TopicSubAgentCompleted = "subagent.completed"

	// Heartbeat and cron
	TopicHeartbeatStarted   = "heartbeat.started"
	TopicHeartbeatCompleted = "heartbeat.completed"
	TopicCronStarted        = "cron.started"
	TopicCronCompleted      = "cron.completed"

	// Compaction
	TopicCompaction = "compaction"

	// Flows
	TopicFlowStarted   = "flow.started"
	TopicFlowCompleted = "flow.completed"

	// Reload
	TopicReloadCompleted = "reload.completed"

	// Memory
	TopicMemoryWritten = "memory.written"

	// Background task results — general mechanism for ANY background task
	// (heartbeat, cron, boot) to deliver results to connected clients.
	TopicBackgroundResult = "background.result"
)

// BackgroundResult is published when ANY background task completes with results.
// This is the general delivery mechanism — everything from heartbeat tasks to cron
// jobs to BOOT.md can publish here, and the gateway forwards to connected clients.
type BackgroundResult struct {
	Source   string `json:"source"`    // "heartbeat", "cron", "boot", "reminder", "approval"
	TaskName string `json:"task_name"` // e.g. "iran-monitor", cron entry ID, reminder text
	AgentID  string `json:"agent_id"`
	Status   string `json:"status"`  // "completed", "error"
	Content  string `json:"content"` // the actual result text
	Error    string `json:"error,omitempty"`
	RefID    string `json:"ref_id,omitempty"` // source-specific reference (reminder ID, approval ID)
	OneShot  bool   `json:"one_shot,omitempty"`
}

// AgentEvent is published for agent lifecycle events.
type AgentEvent struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// ToolEvent is published for tool call/result events.
type ToolEvent struct {
	AgentID  string `json:"agent_id"`
	ToolName string `json:"tool_name"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

// SubAgentEvent is published when a sub-agent starts or completes.
type SubAgentEvent struct {
	TaskID          string `json:"task_id"`
	AgentID         string `json:"agent_id"`
	ParentAgentID   string `json:"parent_agent_id"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	SessionID       string `json:"session_id"`
	Status          string `json:"status"`
	Result          string `json:"result,omitempty"`
}

// HeartbeatEvent is published for heartbeat lifecycle.
type HeartbeatEvent struct {
	Items    int    `json:"items"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

// FlowEvent is published for flow lifecycle.
type FlowEvent struct {
	FlowID string `json:"flow_id"`
	Name   string `json:"name"`
	Steps  int    `json:"steps,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CronEvent is published for cron execution.
type CronEvent struct {
	EntryID   string `json:"entry_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}
