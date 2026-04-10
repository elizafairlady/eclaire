package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MaxEventsPerSession is the maximum number of system events retained per session.
// Older events are dropped when the cap is reached.
// Reference: OpenClaw src/infra/system-events.ts — MAX_EVENTS = 20.
const MaxEventsPerSession = 20

// SystemEvent represents an ephemeral awareness event queued for a session.
// These are drained into the system prompt on the next agent turn.
type SystemEvent struct {
	Text       string
	Timestamp  time.Time
	Source     string // "cron", "heartbeat", "subagent", "dreaming", "system"
	ContextKey string // dedup key for same-source updates
}

type sessionQueue struct {
	events   []SystemEvent
	lastText string // for consecutive dedup
}

// SystemEventQueue is an in-memory, session-scoped, drain-on-consume event queue.
// Background work results (cron completions, heartbeat results, sub-agent completions)
// are enqueued here and drained into the system prompt on the next agent turn.
//
// Reference: OpenClaw src/infra/system-events.ts
type SystemEventQueue struct {
	mu     sync.Mutex
	queues map[string]*sessionQueue
}

// NewSystemEventQueue creates a new system event queue.
func NewSystemEventQueue() *SystemEventQueue {
	return &SystemEventQueue{
		queues: make(map[string]*sessionQueue),
	}
}

// Enqueue adds a system event to the session's queue.
// Returns false if the event was deduped (consecutive identical text).
// Drops oldest events if the queue exceeds MaxEventsPerSession.
func (q *SystemEventQueue) Enqueue(sessionKey, text, source, contextKey string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	sq := q.queues[sessionKey]
	if sq == nil {
		sq = &sessionQueue{}
		q.queues[sessionKey] = sq
	}

	// Consecutive dedup: skip if same text as last enqueued event
	if sq.lastText == text {
		return false
	}

	sq.events = append(sq.events, SystemEvent{
		Text:       text,
		Timestamp:  time.Now(),
		Source:     source,
		ContextKey: contextKey,
	})
	sq.lastText = text

	// Drop oldest if over cap
	if len(sq.events) > MaxEventsPerSession {
		sq.events = sq.events[len(sq.events)-MaxEventsPerSession:]
	}

	return true
}

// Drain returns all queued events for the session and clears the queue.
// This is the consumption point — events are removed after draining.
func (q *SystemEventQueue) Drain(sessionKey string) []SystemEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	sq := q.queues[sessionKey]
	if sq == nil || len(sq.events) == 0 {
		return nil
	}

	events := make([]SystemEvent, len(sq.events))
	copy(events, sq.events)

	// Clear the queue
	delete(q.queues, sessionKey)

	return events
}

// Peek returns all queued events without clearing them.
func (q *SystemEventQueue) Peek(sessionKey string) []SystemEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	sq := q.queues[sessionKey]
	if sq == nil {
		return nil
	}

	events := make([]SystemEvent, len(sq.events))
	copy(events, sq.events)
	return events
}

// HasEvents returns true if the session has pending events.
func (q *SystemEventQueue) HasEvents(sessionKey string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	sq := q.queues[sessionKey]
	return sq != nil && len(sq.events) > 0
}

// FormatDrained renders drained events as a prompt section.
// Returns empty string if events is nil/empty.
//
// Format matches OpenClaw's session-system-events.ts:
//
//	# System Events
//	System: [15:04:05] Cron job 'monitor' completed: ...
//	System: [15:04:12] Sub-agent 'research' completed: ...
func FormatDrained(events []SystemEvent) string {
	if len(events) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# System Events\n")
	for _, ev := range events {
		fmt.Fprintf(&sb, "System: [%s] %s\n", ev.Timestamp.Format("15:04:05"), ev.Text)
	}
	return sb.String()
}
