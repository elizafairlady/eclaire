package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/elizafairlady/eclaire/internal/agent"
	"github.com/elizafairlady/eclaire/internal/ui/chat"
	"github.com/elizafairlady/eclaire/internal/ui/styles"
)

// Test the actual rendering path: text deltas create a live AssistantMessageItem
// in the chatList with markdown rendering. No separate streaming text path.
func TestStreamingToMarkdownTransition(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{MainSessionID: "main-sess"})

	tabID := "main"
	cl := app.chatList(tabID)
	cl.SetSize(80, 40)

	// Step 1: User sends message
	cl.Add(chat.NewUserMessage("u1", "hello"))
	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."

	// Step 2: Text delta arrives — creates live AssistantMessageItem in chatList
	app.handleStreamEvent(tabID, agent.StreamEvent{
		Type:  agent.EventTextDelta,
		Delta: "# Hello World\n\nThis is **bold** text.",
	})

	// Live item should exist in chatList
	liveItem := cl.GetAssistant("live_" + tabID)
	if liveItem == nil {
		t.Fatal("live AssistantMessageItem should exist in chatList after first text delta")
	}

	// During streaming: markdown should already be rendering (not raw text)
	duringRender := cl.Render("")
	t.Logf("During streaming render:\n%s", duringRender)

	// Should contain glamour-rendered content (ANSI codes), not raw markdown
	if strings.Contains(duringRender, "# Hello") {
		t.Error("during streaming, raw markdown '# Hello' should not appear — glamour should render it")
	}
	if !strings.Contains(duringRender, "Hello") || !strings.Contains(duringRender, "World") {
		t.Error("during streaming, rendered output should contain 'Hello' and 'World'")
	}
	if !strings.Contains(duringRender, "bold") {
		t.Error("during streaming, rendered output should contain 'bold'")
	}

	// Step 3: Stream completes
	app.Update(streamDoneMsg{tabID: tabID})

	// After completion: same content stays, streaming state cleaned up
	if _, ok := app.streaming[tabID]; ok {
		t.Error("streaming should be cleared after streamDoneMsg")
	}
	if app.busy[tabID] {
		t.Error("busy should be false after streamDoneMsg")
	}

	afterRender := cl.Render("")
	t.Logf("After streamDoneMsg render:\n%s", afterRender)

	// Content should be identical — no re-render, no loss
	if !strings.Contains(afterRender, "Hello") || !strings.Contains(afterRender, "World") {
		t.Error("after completion, rendered output should still contain 'Hello' and 'World'")
	}

	// 2 items: user message + live assistant message
	if cl.Len() != 2 {
		t.Errorf("chatList should have 2 items (user + assistant), got %d", cl.Len())
	}
}

// If streamDoneMsg never arrives, the live AssistantMessageItem still has
// markdown-rendered content visible. No silent failure.
func TestStreamDoneNeverArrives(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s, AppOptions{MainSessionID: "main-sess"})

	tabID := "main"
	cl := app.chatList(tabID)
	cl.SetSize(80, 40)

	app.busy[tabID] = true

	// Text arrives but stream never completes
	app.handleStreamEvent(tabID, agent.StreamEvent{
		Type:  agent.EventTextDelta,
		Delta: "Here is some **important** content.",
	})

	// Content should be visible even without streamDoneMsg
	rendered := cl.Render("")
	if !strings.Contains(rendered, "important") {
		t.Error("content should be visible even if streamDoneMsg never arrives")
	}

	// Should be glamour-rendered (not raw markdown)
	if strings.Contains(rendered, "**important**") {
		t.Error("content should be glamour-rendered, not raw markdown with ** markers")
	}
}

func TestStreamDoneMsgClearsBusy(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	tabID := "main"

	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."

	// Simulate a text delta so streaming map gets populated
	app.handleStreamEvent(tabID, agent.StreamEvent{
		Type:  agent.EventTextDelta,
		Delta: "some response text",
	})

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

	// No assistant message should be added (no text deltas arrived)
	if app.chatList(tabID).Len() != 0 {
		t.Error("should not add assistant message when no text was streamed")
	}
}

func TestSpinnerOnlyShowsBeforeFirstToken(t *testing.T) {
	s := styles.Default()
	app := NewApp(nil, s)
	tabID := "main"

	app.busy[tabID] = true
	app.busyStatus[tabID] = "thinking..."
	app.chatList(tabID).SetSize(80, 40)

	// Before any tokens: spinner should appear as streamText
	streamText := ""
	if _, hasTokens := app.streaming[tabID]; !hasTokens && app.busy[tabID] {
		streamText = s.AgentWaiting.Render("  ⠋ thinking...")
	}
	render1 := app.chatList(tabID).Render(streamText)
	t.Logf("Before tokens:\n%s", render1)
	if !strings.Contains(render1, "thinking") {
		t.Error("should show thinking spinner before first token")
	}

	// After first token: spinner should NOT appear
	app.handleStreamEvent(tabID, agent.StreamEvent{
		Type:  agent.EventTextDelta,
		Delta: "Hello",
	})
	streamText = ""
	if _, hasTokens := app.streaming[tabID]; !hasTokens && app.busy[tabID] {
		streamText = s.AgentWaiting.Render("  ⠋ thinking...")
	}
	render2 := app.chatList(tabID).Render(streamText)
	t.Logf("After first token:\n%s", render2)
	if strings.Contains(render2, "thinking") {
		t.Error("should NOT show thinking spinner once tokens are streaming")
	}
	if !strings.Contains(render2, "Hello") {
		t.Error("should show streaming text")
	}
}

// Test render caching: second render at same width should not re-run glamour.
func TestAssistantMessageRenderCache(t *testing.T) {
	renderCalls := 0
	msg := chat.NewAssistantMessage("test", "# Hello", func(content string, width int) string {
		renderCalls++
		return "rendered:" + content
	})

	// First render: cache miss
	r1 := msg.Render(80)
	if renderCalls != 1 {
		t.Errorf("expected 1 render call, got %d", renderCalls)
	}

	// Second render at same width: cache hit
	r2 := msg.Render(80)
	if renderCalls != 1 {
		t.Errorf("expected still 1 render call (cache hit), got %d", renderCalls)
	}
	if r1 != r2 {
		t.Error("cached render should return same result")
	}

	// SetContent invalidates cache
	msg.SetContent("# Updated")
	msg.Render(80)
	if renderCalls != 2 {
		t.Errorf("expected 2 render calls after SetContent, got %d", renderCalls)
	}

	// Different width also invalidates
	msg.Render(120)
	if renderCalls != 3 {
		t.Errorf("expected 3 render calls after width change, got %d", renderCalls)
	}
}

// Test per-line left padding on assistant messages.
func TestAssistantMessageLeftPadding(t *testing.T) {
	msg := chat.NewAssistantMessage("test", "line1\nline2\nline3", func(content string, width int) string {
		return content // passthrough, no markdown
	})
	rendered := msg.Render(80)
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line %d should have 2-space left padding, got %q", i, line)
		}
	}
}

// Full lifecycle: user message → text deltas → stream done
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

	// First delta should create live item
	if cl.Len() != 2 {
		t.Errorf("expected 2 items after first delta (user + live assistant), got %d", cl.Len())
	}

	t.Log("=== Step 3: More text ===")
	app.handleStreamEvent(tabID, agent.StreamEvent{Type: agent.EventTextDelta, Delta: "statically typed language."})
	t.Logf("Items: %d, Busy: %v, Streaming: %q", cl.Len(), app.busy[tabID], app.streaming[tabID])

	// Should still be 2 items (live item updated, not a new one)
	if cl.Len() != 2 {
		t.Errorf("expected still 2 items after second delta, got %d", cl.Len())
	}

	t.Log("=== Step 4: Stream done ===")
	_ = time.Now()
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
	if !strings.Contains(final, "Go is a statically typed") {
		t.Error("final render should contain the full text")
	}
}
