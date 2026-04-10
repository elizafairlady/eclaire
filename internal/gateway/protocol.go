package gateway

import "encoding/json"

// MessageType identifies the kind of message in the wire protocol.
type MessageType string

const (
	TypeRequest  MessageType = "request"
	TypeResponse MessageType = "response"
	TypeStream   MessageType = "stream"
	TypeEvent    MessageType = "event"
)

// Envelope is the wire format for all gateway communication.
// Serialized as NDJSON over Unix socket.
type Envelope struct {
	ID     string          `json:"id"`
	Type   MessageType     `json:"type"`
	Method string          `json:"method,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *ErrorPayload   `json:"error,omitempty"`
}

// ErrorPayload carries error information in responses.
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPC method constants.
const (
	MethodAgentRun    = "agent.run"
	MethodAgentList   = "agent.list"
	MethodAgentStatus = "agent.status"
	MethodAgentCancel = "agent.cancel"
	MethodAgentSpawn  = "agent.spawn"
	MethodAgentKill   = "agent.kill"

	MethodSessionCreate     = "session.create"
	MethodSessionList       = "session.list"
	MethodSessionGet        = "session.get"
	MethodSessionContinue   = "session.continue"
	MethodSessionMostRecent = "session.most_recent"

	MethodApprovalRespond = "approval.respond"

	MethodToolList = "tool.list"

	MethodConfigGet = "config.get"

	MethodFlowRun    = "flow.run"
	MethodFlowList   = "flow.list"
	MethodFlowStatus = "flow.status"

	MethodTaskList = "task.list"

	MethodCronList   = "cron.list"
	MethodCronAdd    = "cron.add"
	MethodCronRemove = "cron.remove"

	MethodJobList   = "job.list"
	MethodJobAdd    = "job.add"
	MethodJobRemove = "job.remove"
	MethodJobRun    = "job.run"
	MethodJobRuns   = "job.runs"

	MethodNotificationList  = "notification.list"
	MethodNotificationDrain = "notification.drain"
	MethodNotificationAct   = "notification.act"

	MethodConnect = "client.connect"

	MethodGatewayStatus   = "gateway.status"
	MethodGatewayShutdown = "gateway.shutdown"
	MethodGatewayReload   = "gateway.reload"
)

// NotificationActRequest is the payload for notification.act.
type NotificationActRequest struct {
	ID     string `json:"id"`     // notification ID
	Action string `json:"action"` // source-specific action (complete, dismiss, snooze, yes, always, no)
	Value  string `json:"value,omitempty"` // optional value (e.g., snooze duration)
}

// FlowRunRequest is the payload for flow.run.
type FlowRunRequest struct {
	FlowID string `json:"flow_id"`
	Input  string `json:"input,omitempty"`
}

// FlowStatusRequest is the payload for flow.status.
type FlowStatusRequest struct {
	RunID string `json:"run_id"`
}

// CronAddRequest is the payload for cron.add.
type CronAddRequest struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	AgentID  string `json:"agent_id"`
	Prompt   string `json:"prompt"`
}

// CronRemoveRequest is the payload for cron.remove.
type CronRemoveRequest struct {
	ID string `json:"id"`
}

// GatewayStatus is the response payload for gateway.status.
type GatewayStatus struct {
	PID            int    `json:"pid"`
	Uptime         string `json:"uptime"`
	ActiveAgents   int    `json:"active_agents"`
	ActiveClients  int    `json:"active_clients"`
	MainSessionID  string `json:"main_session_id,omitempty"`
}

// ConnectRequest is sent by the TUI/client on initial connection.
type ConnectRequest struct {
	CWD string `json:"cwd"`
}

// ConnectResponse returns session context for the connecting client.
type ConnectResponse struct {
	MainSessionID    string `json:"main_session_id"`
	ProjectSessionID string `json:"project_session_id,omitempty"`
	ProjectRoot      string `json:"project_root,omitempty"`
}

// AgentRunRequest is the payload for agent.run.
type AgentRunRequest struct {
	AgentID    string `json:"agent_id"`
	Prompt     string `json:"prompt"`
	SessionID  string `json:"session_id,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	Title      string `json:"title,omitempty"`
	Background bool   `json:"background,omitempty"` // run without interactive approval; timeouts create notifications
}

// StreamEvent is the typed payload within stream envelopes.
type StreamEvent struct {
	Type       string `json:"type"` // text_delta, tool_call, tool_result, step_finish, error
	Delta      string `json:"delta,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`
	Usage      *Usage `json:"usage,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Usage tracks token consumption for a step or session.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Stream event type constants.
const (
	StreamTextDelta  = "text_delta"
	StreamToolCall   = "tool_call"
	StreamToolResult = "tool_result"
	StreamStepFinish = "step_finish"
	StreamError      = "error"
)
