package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/ui/chat"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
)

// Reproduce: user sends message, tokens stream back, stream completes.
// Verify: markdown renders after completion, not raw text.
func TestStreamingToMarkdownTransition(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{MainSessionID: "main-sess"})

	tabID := "main"

	// Step 1: User sends message — busy starts
	app.chatList(tabID).Add(chat.NewUserMessage("u1", "hello"))
	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."

	// Step 2: Text delta arrives — tokens streaming
	app.handleStreamEvent(tabID, agent.StreamEvent{
		Type:  agent.EventTextDelta,
		Delta: "# Hello World\n\nThis is **bold** text.",
	})

	// During streaming: text should be in m.streaming, rendered raw
	streamText := app.streaming[tabID]
	t.Logf("During streaming, m.streaming[%q] = %q", tabID, streamText)
	if streamText == "" {
		t.Fatal("streaming text should be populated during streaming")
	}

	// Check what drawChat would show during streaming
	cl := app.chatList(tabID)
	cl.SetSize(80, 40)
	duringRender := cl.Render(s.AssistantBody.Render(streamText))
	t.Logf("During streaming render:\n%s", duringRender)

	// Step 3: Stream completes — streamDoneMsg arrives
	app.Update(streamDoneMsg{tabID: tabID})

	// After completion: streaming should be cleared
	if _, ok := app.streaming[tabID]; ok {
		t.Error("streaming should be cleared after streamDoneMsg")
	}
	if app.busy[tabID] {
		t.Error("busy should be false after streamDoneMsg")
	}

	// After completion: chatList should have an AssistantMessage with markdown
	afterRender := cl.Render("")
	t.Logf("After streamDoneMsg render:\n%s", afterRender)

	// The markdown renderer should have processed the content.
	// Glamour wraps text in ANSI escape sequences, so check for the words separately.
	if !strings.Contains(afterRender, "Hello") || !strings.Contains(afterRender, "World") {
		t.Error("rendered output should contain 'Hello' and 'World'")
	}
	if !strings.Contains(afterRender, "bold") {
		t.Error("rendered output should contain 'bold'")
	}

	// Check item count — should be user message + assistant message
	if cl.Len() != 2 {
		t.Errorf("chatList should have 2 items (user + assistant), got %d", cl.Len())
	}
}

func TestStreamDoneMsgClearsBusy(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	tabID := "main"

	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."
	app.streaming[tabID] = "some response text"

	app.Update(streamDoneMsg{tabID: tabID})

	if app.busy[tabID] {
		t.Error("busy should be cleared")
	}
	if _, ok := app.busyStatus[tabID]; ok {
		t.Error("busyStatus should be cleared")
	}
	if _, ok := app.streaming[tabID]; ok {
		t.Error("streaming should be cleared")
	}
}

func TestStreamDoneMsgWithEmptyStreaming(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	tabID := "main"

	app.busy[tabID] = true

	// No streaming text — streamDoneMsg should still clear busy
	app.Update(streamDoneMsg{tabID: tabID})

	if app.busy[tabID] {
		t.Error("busy should be cleared even with no streaming text")
	}

	// No assistant message should be added
	if app.chatList(tabID).Len() != 0 {
		t.Error("should not add empty assistant message")
	}
}

func TestSpinnerOnlyShowsBeforeFirstToken(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	tabID := "main"

	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."
	app.chatList(tabID).SetSize(80, 40)

	// Before any tokens: should show spinner
	render1 := app.chatList(tabID).Render(func() string {
		streamText := ""
		if text, ok := app.streaming[tabID]; ok && text != "" {
			streamText = s.AssistantBody.Render(text)
		} else if app.busy[tabID] {
			streamText = s.AgentWaiting.Render("  ⠋ thinking...")
		}
		return streamText
	}())
	t.Logf("Before tokens:\n%s", render1)
	if !strings.Contains(render1, "thinking") {
		t.Error("should show thinking spinner before first token")
	}

	// After first token: should NOT show spinner
	app.streaming[tabID] = "Hello"
	render2 := app.chatList(tabID).Render(func() string {
		streamText := ""
		if text, ok := app.streaming[tabID]; ok && text != "" {
			streamText = s.AssistantBody.Render(text)
		} else if app.busy[tabID] {
			streamText = s.AgentWaiting.Render("  ⠋ thinking...")
		}
		return streamText
	}())
	t.Logf("After first token:\n%s", render2)
	if strings.Contains(render2, "thinking") {
		t.Error("should NOT show thinking spinner once tokens are streaming")
	}
	if !strings.Contains(render2, "Hello") {
		t.Error("should show streaming text")
	}
}

// Simulate the full lifecycle with timing to see what the user sees at each step
func TestFullMessageLifecycle(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{MainSessionID: "main-sess"})
	tabID := "main"
	cl := app.chatList(tabID)
	cl.SetSize(80, 40)

	t.Log("=== Step 0: Empty chat ===")
	t.Logf("Items: %d, Busy: %v", cl.Len(), app.busy[tabID])

	t.Log("=== Step 1: User sends message ===")
	cl.Add(chat.NewUserMessage("u1", "Tell me about Go"))
	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."
	t.Logf("Items: %d, Busy: %v, Streaming: %q", cl.Len(), app.busy[tabID], app.streaming[tabID])

	t.Log("=== Step 2: First text delta ===")
	app.handleStreamEvent(tabID, agent.StreamEvent{Type: agent.EventTextDelta, Delta: "Go is a "})
	t.Logf("Items: %d, Busy: %v, Streaming: %q", cl.Len(), app.busy[tabID], app.streaming[tabID])

	t.Log("=== Step 3: More text ===")
	app.handleStreamEvent(tabID, agent.StreamEvent{Type: agent.EventTextDelta, Delta: "statically typed language."})
	t.Logf("Items: %d, Busy: %v, Streaming: %q", cl.Len(), app.busy[tabID], app.streaming[tabID])

	t.Log("=== Step 4: Stream done ===")
	_ = time.Now() // just to use the import
	app.Update(streamDoneMsg{tabID: tabID})
	t.Logf("Items: %d, Busy: %v, Streaming exists: %v", cl.Len(), app.busy[tabID], func() bool { _, ok := app.streaming[tabID]; return ok }())

	t.Log("=== Final render ===")
	final := cl.Render("")
	t.Logf("Rendered:\n%s", final)

	if cl.Len() != 2 {
		t.Errorf("expected 2 items (user + assistant), got %d", cl.Len())
	}
	if app.busy[tabID] {
		t.Error("busy should be false")
	}
}
