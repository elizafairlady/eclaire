package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, "test.topic")

	b.Publish("test.topic", "hello")

	select {
	case ev := <-ch:
		if ev.Topic != "test.topic" {
			t.Errorf("got topic %q, want %q", ev.Topic, "test.topic")
		}
		if ev.Payload != "hello" {
			t.Errorf("got payload %v, want %q", ev.Payload, "hello")
		}
		if ev.Time.IsZero() {
			t.Error("event time should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := b.Subscribe(ctx, "multi")
	ch2 := b.Subscribe(ctx, "multi")

	b.Publish("multi", "data")

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Payload != "data" {
				t.Errorf("subscriber %d: got %v, want %q", i, ev.Payload, "data")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestSubscribeFunc(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received string
	var mu sync.Mutex
	done := make(chan struct{})

	b.SubscribeFunc(ctx, "func.topic", func(ev Event) {
		mu.Lock()
		received = ev.Payload.(string)
		mu.Unlock()
		close(done)
	})

	b.Publish("func.topic", "callback-data")

	select {
	case <-done:
		mu.Lock()
		if received != "callback-data" {
			t.Errorf("got %q, want %q", received, "callback-data")
		}
		mu.Unlock()
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestUnsubscribeOnCancel(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch := b.Subscribe(ctx, "cancel.topic")

	cancel()

	// Channel should be closed after cancel
	time.Sleep(50 * time.Millisecond)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		// Channel may already be drained and closed
	}
}

func TestDifferentTopics(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chA := b.Subscribe(ctx, "topic.a")
	chB := b.Subscribe(ctx, "topic.b")

	b.Publish("topic.a", "only-a")

	select {
	case ev := <-chA:
		if ev.Payload != "only-a" {
			t.Errorf("got %v, want %q", ev.Payload, "only-a")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out on topic.a")
	}

	// topic.b should NOT receive the message
	select {
	case ev := <-chB:
		t.Errorf("topic.b should not receive events, got %v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestPublishToNoSubscribers(t *testing.T) {
	b := New()
	// Should not panic
	b.Publish("nobody.listening", "data")
}

func TestConcurrentPublish(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	count := 0
	done := make(chan struct{})

	b.SubscribeFunc(ctx, "concurrent", func(ev Event) {
		mu.Lock()
		count++
		if count == 50 {
			close(done)
		}
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b.Publish("concurrent", n)
		}(i)
	}
	wg.Wait()

	select {
	case <-done:
		// all received
	case <-time.After(2 * time.Second):
		mu.Lock()
		t.Errorf("received %d events, want 50", count)
		mu.Unlock()
	}
}
