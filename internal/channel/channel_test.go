package channel

import (
	"context"
	"log/slog"
	"testing"
)

type mockChannel struct {
	id      string
	name    string
	started bool
	stopped bool
	sent    []OutboundMessage
}

func (m *mockChannel) ID() string   { return m.id }
func (m *mockChannel) Name() string { return m.name }

func (m *mockChannel) Start(_ context.Context, _ DeliverFunc) error {
	m.started = true
	return nil
}

func (m *mockChannel) Send(_ context.Context, msg OutboundMessage) error {
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockChannel) Stop() error {
	m.stopped = true
	return nil
}

func TestManagerRegisterAndStart(t *testing.T) {
	logger := slog.Default()
	mgr := NewManager(func(_ context.Context, _ InboundMessage) error { return nil }, logger)

	ch1 := &mockChannel{id: "signal", name: "Signal"}
	ch2 := &mockChannel{id: "telegram", name: "Telegram"}

	if err := mgr.Register(ch1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Register(ch2); err != nil {
		t.Fatal(err)
	}

	// Duplicate registration fails
	if err := mgr.Register(ch1); err == nil {
		t.Error("expected error on duplicate registration")
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !ch1.started || !ch2.started {
		t.Error("channels should be started")
	}
	if mgr.Count() != 2 {
		t.Errorf("expected 2 channels, got %d", mgr.Count())
	}
}

func TestManagerSendAndBroadcast(t *testing.T) {
	logger := slog.Default()
	mgr := NewManager(func(_ context.Context, _ InboundMessage) error { return nil }, logger)

	signal := &mockChannel{id: "signal", name: "Signal"}
	email := &mockChannel{id: "email", name: "Email"}
	mgr.Register(signal)
	mgr.Register(email)

	ctx := context.Background()

	// Send to specific channel
	if err := mgr.Send(ctx, "signal", OutboundMessage{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(signal.sent) != 1 {
		t.Errorf("signal should have 1 message, got %d", len(signal.sent))
	}
	if len(email.sent) != 0 {
		t.Error("email should have no messages")
	}

	// Broadcast to all
	if err := mgr.Broadcast(ctx, OutboundMessage{Text: "alert"}); err != nil {
		t.Fatal(err)
	}
	if len(signal.sent) != 2 {
		t.Errorf("signal should have 2 messages, got %d", len(signal.sent))
	}
	if len(email.sent) != 1 {
		t.Errorf("email should have 1 message, got %d", len(email.sent))
	}
}

func TestManagerSendUnknownChannel(t *testing.T) {
	mgr := NewManager(func(_ context.Context, _ InboundMessage) error { return nil }, slog.Default())
	if err := mgr.Send(context.Background(), "nonexistent", OutboundMessage{}); err == nil {
		t.Error("expected error for unknown channel")
	}
}

func TestManagerStop(t *testing.T) {
	mgr := NewManager(func(_ context.Context, _ InboundMessage) error { return nil }, slog.Default())
	ch := &mockChannel{id: "signal", name: "Signal"}
	mgr.Register(ch)
	mgr.Start(context.Background())
	mgr.Stop()
	if !ch.stopped {
		t.Error("channel should be stopped")
	}
}
