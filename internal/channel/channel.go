// Package channel defines the plugin abstraction for external messaging platforms.
//
// Channels are NOT the gateway's own transport (Unix socket). The TUI/CLI connect
// directly to the gateway — they are native clients, not channels.
//
// Channels are external messaging platforms where Claire can receive and send messages:
// Signal, Telegram, email, IRC, webhook, Fediverse, etc. Each channel plugin speaks
// the external platform's protocol and translates to/from the gateway's internal model.
//
// Reference: OpenClaw src/channels/plugins/types.ts — ChannelPlugin contract.
// In OpenClaw, the web UI connects directly to the gateway WebSocket (not a channel).
// Channels are Signal, Telegram, WhatsApp, etc.
package channel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Channel is a plugin for an external messaging platform.
// Each channel connects eclaire to one external system (Signal, Telegram, email, etc.).
type Channel interface {
	// ID returns the unique identifier (e.g. "signal", "telegram", "email").
	ID() string
	// Name returns the display name.
	Name() string
	// Start connects to the external platform and begins listening for inbound messages.
	// The deliver function routes inbound messages to the gateway for processing.
	Start(ctx context.Context, deliver DeliverFunc) error
	// Send delivers an outbound message to a user through this platform.
	Send(ctx context.Context, msg OutboundMessage) error
	// Stop disconnects from the external platform.
	Stop() error
}

// DeliverFunc routes inbound messages from an external channel to the gateway.
type DeliverFunc func(ctx context.Context, msg InboundMessage) error

// InboundMessage is a message arriving from an external platform.
type InboundMessage struct {
	ChannelID string            `json:"channel_id"`
	SenderID  string            `json:"sender_id"`  // platform-specific user ID
	Text      string            `json:"text"`
	SessionID string            `json:"session_id,omitempty"` // empty = new session
	AgentID   string            `json:"agent_id,omitempty"`   // empty = orchestrator
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// OutboundMessage is a message from Claire delivered through an external platform.
type OutboundMessage struct {
	RecipientID string `json:"recipient_id,omitempty"` // platform-specific recipient
	Text        string `json:"text"`
	SessionID   string `json:"session_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

// Manager manages registered channel plugins and routes messages.
type Manager struct {
	channels map[string]Channel
	deliver  DeliverFunc
	logger   *slog.Logger
	mu       sync.RWMutex
}

// NewManager creates a channel manager. The deliver function routes inbound
// messages from external channels to the gateway for processing.
func NewManager(deliver DeliverFunc, logger *slog.Logger) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		deliver:  deliver,
		logger:   logger,
	}
}

// Register adds a channel plugin.
func (m *Manager) Register(ch Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[ch.ID()]; exists {
		return fmt.Errorf("channel %q already registered", ch.ID())
	}
	m.channels[ch.ID()] = ch
	m.logger.Info("channel registered", "id", ch.ID(), "name", ch.Name())
	return nil
}

// Start connects all registered channels to their external platforms.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if err := ch.Start(ctx, m.deliver); err != nil {
			return fmt.Errorf("start channel %q: %w", ch.ID(), err)
		}
		m.logger.Info("channel started", "id", ch.ID())
	}
	return nil
}

// Stop disconnects all channels.
func (m *Manager) Stop() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstErr error
	for _, ch := range m.channels {
		if err := ch.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Send delivers a message through a specific channel.
func (m *Manager) Send(ctx context.Context, channelID string, msg OutboundMessage) error {
	m.mu.RLock()
	ch, ok := m.channels[channelID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("channel %q not found", channelID)
	}
	return ch.Send(ctx, msg)
}

// Broadcast delivers a message to ALL channels (for proactive notifications).
func (m *Manager) Broadcast(ctx context.Context, msg OutboundMessage) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstErr error
	for _, ch := range m.channels {
		if err := ch.Send(ctx, msg); err != nil && firstErr == nil {
			firstErr = err
			m.logger.Warn("broadcast failed", "channel", ch.ID(), "err", err)
		}
	}
	return firstErr
}

// Get returns a channel by ID.
func (m *Manager) Get(id string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[id]
	return ch, ok
}

// Count returns the number of registered channels.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.channels)
}
