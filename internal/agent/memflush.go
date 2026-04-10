package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/persist"
	"github.com/elizafairlady/eclaire/internal/provider"
	"github.com/elizafairlady/eclaire/internal/tool"
)

// memoryFlushState tracks per-session context hashes to prevent duplicate flushes.
// Reference: OpenClaw src/auto-reply/reply/memory-flush.ts — computeContextHash.
type memoryFlushState struct {
	mu           sync.Mutex
	lastHashByID map[string]string
}

func newMemoryFlushState() *memoryFlushState {
	return &memoryFlushState{
		lastHashByID: make(map[string]string),
	}
}

// shouldFlush returns true if the session's context has changed since the last flush.
func (s *memoryFlushState) shouldFlush(sessionID string, messages []fantasy.Message) bool {
	hash := contextHash(messages)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHashByID[sessionID] != hash
}

// recordFlush records the current context hash after a successful flush.
func (s *memoryFlushState) recordFlush(sessionID string, messages []fantasy.Message) {
	hash := contextHash(messages)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHashByID[sessionID] = hash
}

// contextHash computes SHA-256 of message count + last 3 user/assistant messages.
// Used for dedup: skip flush if context hasn't changed since last flush.
// Reference: OpenClaw src/auto-reply/reply/memory-flush.ts — computeContextHash.
func contextHash(messages []fantasy.Message) string {
	var relevant []fantasy.Message
	for _, m := range messages {
		if m.Role == fantasy.MessageRoleUser || m.Role == fantasy.MessageRoleAssistant {
			relevant = append(relevant, m)
		}
	}
	if len(relevant) > 3 {
		relevant = relevant[len(relevant)-3:]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d:", len(messages))
	for i, m := range relevant {
		text := extractText(m)
		fmt.Fprintf(&sb, "[%d:%s]%s\x00", i, m.Role, text)
	}

	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// extractText gets the first text content from a message.
func extractText(m fantasy.Message) string {
	for _, part := range m.Content {
		if tp, ok := part.(fantasy.TextPart); ok {
			return tp.Text
		}
	}
	return ""
}

const (
	flushTimeout       = 30 * time.Second
	flushMaxIterations = 3
)

// ensureFlushState lazily initializes the flush state.
func (r *Runner) ensureFlushState() *memoryFlushState {
	if r.flushState == nil {
		r.flushState = newMemoryFlushState()
	}
	return r.flushState
}

// flushMemory runs a restricted mini-agent turn to save important context
// before compaction erases it. Only memory_write with type=daily is available.
//
// Reference: OpenClaw src/auto-reply/reply/memory-flush.ts
func (r *Runner) flushMemory(ctx context.Context, cfg RunConfig, sessionID string, messages []fantasy.Message, emit func(StreamEvent) error) error {
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()

	// Resolve model
	type contextResolver interface {
		ResolveWithContext(ctx context.Context, role string) (*provider.ModelResolution, error)
	}
	resolver, ok := r.Router.(contextResolver)
	if !ok {
		return fmt.Errorf("router does not support model resolution")
	}
	resolution, err := resolver.ResolveWithContext(ctx, "orchestrator")
	if err != nil {
		return fmt.Errorf("resolve model for flush: %w", err)
	}

	// Build context: last 10 messages for the model to see what needs saving
	contextMsgs := messages
	if len(contextMsgs) > 10 {
		contextMsgs = contextMsgs[len(contextMsgs)-10:]
	}

	flushPrompt := "The conversation is about to be compacted to save context space. " +
		"Save any important facts, decisions, file paths, or pending work items to today's daily log. " +
		"Be concise. Only save information that would be lost and is not already in memory. " +
		"Use memory_write with type=daily."

	var flushMessages []fantasy.Message
	flushMessages = append(flushMessages, contextMsgs...)
	flushMessages = append(flushMessages, fantasy.NewUserMessage(flushPrompt))

	// Create restricted tool: daily-only memory write
	memTool := tool.MemoryWriteDailyOnlyTool(r.WorkspaceRoot)

	rt := &ConversationRuntime{
		Model:         resolution.Model,
		SystemPrompt:  "You are a memory flush agent. Save important conversation context to today's daily log before compaction. Use memory_write with type=daily. Be concise.",
		Tools:         []fantasy.AgentTool{memTool},
		MaxIterations: flushMaxIterations,
		ContextWindow: resolution.ContextWindow,
		AgentID:       cfg.AgentID,
		Logger:        r.Logger,
	}

	_, _, err = rt.RunTurn(ctx, flushMessages, func(ev StreamEvent) error {
		// Suppress flush events from reaching the user
		return nil
	})
	if err != nil {
		return fmt.Errorf("memory flush turn: %w", err)
	}

	// Persist flush event to session
	r.Sessions.Append(sessionID, persist.EventMemoryFlush, persist.MessageData{
		Content: "Context saved to daily memory before compaction",
	})

	r.flushState.recordFlush(sessionID, messages)
	return nil
}
