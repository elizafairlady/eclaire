package agent

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestCompactStructured(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)

	messages := []fantasy.Message{
		fantasy.NewUserMessage("Read the file main.go"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "I'll read main.go for you."},
			fantasy.ToolCallPart{ToolCallID: "tc1", ToolName: "read", Input: `{"path":"main.go"}`},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "tc1", Output: fantasy.ToolResultOutputContentText{Text: "package main\nfunc main() {}"}},
		}},
		fantasy.NewUserMessage("Now edit it to add a print statement"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "I'll add a print statement."},
			fantasy.ToolCallPart{ToolCallID: "tc2", ToolName: "edit", Input: `{"path":"main.go"}`},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "tc2", Output: fantasy.ToolResultOutputContentText{Text: "OK"}},
		}},
		fantasy.NewUserMessage("Build and test it"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "Building now."},
			fantasy.ToolCallPart{ToolCallID: "tc3", ToolName: "shell", Input: `{"command":"go build"}`},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "tc3", Output: fantasy.ToolResultOutputContentText{Text: "Build successful"}},
		}},
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "Build succeeded. All good."},
		}},
	}

	result, err := engine.Compact(context.Background(), messages, 4)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected compaction result")
	}

	if result.RemovedCount != len(messages)-4 {
		t.Errorf("removed = %d, want %d", result.RemovedCount, len(messages)-4)
	}

	// Compacted history = continuation + 4 preserved
	if len(result.CompactedHistory) != 5 {
		t.Errorf("compacted history = %d messages, want 5", len(result.CompactedHistory))
	}

	// Summary should mention tools used
	if !strings.Contains(result.Summary, "read") {
		t.Error("summary should mention read tool")
	}
	if !strings.Contains(result.Summary, "edit") {
		t.Error("summary should mention edit tool")
	}

	// Summary should include user requests
	if !strings.Contains(result.Summary, "Read the file") {
		t.Error("summary should include user request text")
	}

	if !strings.HasPrefix(result.Summary, "[Session summary]") {
		t.Error("summary should start with [Session summary]")
	}
}

func TestCompactNotEnoughMessages(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	messages := []fantasy.Message{
		fantasy.NewUserMessage("hello"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "hi"}}},
	}
	result, err := engine.Compact(context.Background(), messages, 4)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("should not compact when fewer messages than preserve count")
	}
}

func TestCompactWithPriorSummary(t *testing.T) {
	engine := NewContextEngine(nil, nil, nil)
	messages := []fantasy.Message{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "[Session summary]\n## Previously Compacted\nUser asked about X."},
		}},
		fantasy.NewUserMessage("What about Z?"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Z is related."}}},
		fantasy.NewUserMessage("OK thanks"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Welcome."}}},
		fantasy.NewUserMessage("New question"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Sure."}}},
	}

	result, err := engine.Compact(context.Background(), messages, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected compaction")
	}
	if !strings.Contains(result.Summary, "Previously Compacted") {
		t.Error("should preserve prior summary section")
	}
}

func TestAnalyzeMessages(t *testing.T) {
	messages := []fantasy.Message{
		fantasy.NewUserMessage("first request"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.ToolCallPart{ToolCallID: "1", ToolName: "read", Input: `{"path":"/tmp/test.go"}`},
		}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "1", Output: fantasy.ToolResultOutputContentText{Text: "content of /tmp/test.go"}},
		}},
		fantasy.NewUserMessage("second request"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "done"},
			fantasy.ToolCallPart{ToolCallID: "2", ToolName: "shell"},
		}},
	}

	a := analyzeMessages(messages)
	if a.userCount != 2 {
		t.Errorf("userCount = %d, want 2", a.userCount)
	}
	if a.assistantCount != 2 {
		t.Errorf("assistantCount = %d, want 2", a.assistantCount)
	}
	if a.toolCount != 1 {
		t.Errorf("toolCount = %d, want 1", a.toolCount)
	}
	if len(a.toolNames) != 2 {
		t.Errorf("toolNames = %v, want 2 tools", a.toolNames)
	}
	if len(a.userRequests) != 2 {
		t.Errorf("userRequests = %d, want 2", len(a.userRequests))
	}
}

func TestShouldCompactStop(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int64
		inputTokens   int64
		outputTokens  int64
		want          bool
	}{
		{"zero window", 0, 50000, 10000, false},
		{"plenty of room", 200000, 50000, 10000, false},
		{"at threshold", 200000, 150000, 20000, true},
		{"over threshold", 200000, 180000, 10000, true},
		{"large window 40k threshold", 300000, 250000, 20000, true},
		{"large window plenty", 300000, 100000, 10000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &fantasy.Usage{InputTokens: tt.inputTokens, OutputTokens: tt.outputTokens}
			got := shouldCompactStop(tt.contextWindow, usage)
			if got != tt.want {
				t.Errorf("shouldCompactStop(%d, {in:%d, out:%d}) = %v, want %v",
					tt.contextWindow, tt.inputTokens, tt.outputTokens, got, tt.want)
			}
		})
	}
}

func TestDefaultCompactionConfig(t *testing.T) {
	cfg := DefaultCompactionConfig()
	if !cfg.Enabled {
		t.Error("should be enabled by default")
	}
	if cfg.ThresholdToks != 100_000 {
		t.Errorf("threshold = %d, want 100000", cfg.ThresholdToks)
	}
	if cfg.PreserveCount != 4 {
		t.Errorf("preserve = %d, want 4", cfg.PreserveCount)
	}
}

func TestCompactionConfigZeroValue(t *testing.T) {
	var cfg CompactionConfig
	if cfg.Enabled {
		t.Error("zero-value should be disabled")
	}
}
