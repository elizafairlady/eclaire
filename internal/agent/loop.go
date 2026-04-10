package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"charm.land/fantasy"
)

const (
	loopWindowSize = 10
	loopMaxRepeats = 5
)

// hasRepeatedToolCalls checks whether the agent is stuck in a loop by examining
// the last windowSize steps. Returns true if any tool-call signature appears
// more than maxRepeats times.
func hasRepeatedToolCalls(steps []fantasy.StepResult, windowSize, maxRepeats int) bool {
	if len(steps) < windowSize {
		return false
	}

	window := steps[len(steps)-windowSize:]
	counts := make(map[string]int)

	for _, step := range window {
		sig := toolInteractionSignature(step.Content)
		if sig == "" {
			continue
		}
		counts[sig]++
		if counts[sig] > maxRepeats {
			return true
		}
	}
	return false
}

// toolInteractionSignature computes a SHA-256 hash of all tool calls and their
// results in a single step. Returns empty string if no tool calls.
func toolInteractionSignature(content fantasy.ResponseContent) string {
	toolCalls := content.ToolCalls()
	if len(toolCalls) == 0 {
		return ""
	}

	// Index tool results by their ID.
	resultsByID := make(map[string]fantasy.ToolResultContent)
	for _, tr := range content.ToolResults() {
		resultsByID[tr.ToolCallID] = tr
	}

	h := sha256.New()
	for _, tc := range toolCalls {
		output := ""
		if tr, ok := resultsByID[tc.ToolCallID]; ok {
			output = toolResultText(tr.Result)
		}
		io.WriteString(h, tc.ToolName)
		io.WriteString(h, "\x00")
		io.WriteString(h, tc.Input)
		io.WriteString(h, "\x00")
		io.WriteString(h, output)
		io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// toolResultText extracts a string representation of a tool result.
func toolResultText(content fantasy.ToolResultOutputContent) string {
	if content == nil {
		return ""
	}
	return fmt.Sprintf("%v", content)
}

// --- Functions for ConversationRuntime loop detection ---

// hashToolIteration computes a SHA-256 signature for one iteration's tool calls + results.
// Used by RunTurn to build a step history for loop detection.
func hashToolIteration(calls []toolCallInfo, results map[string]string) string {
	if len(calls) == 0 {
		return ""
	}
	h := sha256.New()
	for _, tc := range calls {
		io.WriteString(h, tc.Name)
		io.WriteString(h, "\x00")
		io.WriteString(h, tc.Input)
		io.WriteString(h, "\x00")
		io.WriteString(h, results[tc.ID])
		io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// isLooping checks if the last windowSize entries in history have any signature
// repeated more than maxRepeats times.
func isLooping(history []string, windowSize, maxRepeats int) bool {
	if len(history) < windowSize {
		return false
	}
	window := history[len(history)-windowSize:]
	counts := make(map[string]int)
	for _, sig := range window {
		counts[sig]++
		if counts[sig] > maxRepeats {
			return true
		}
	}
	return false
}

// isDegenerate returns true if text contains excessive repetition.
// Splits into 4-word ngrams; if any appears more than threshold times, it's degenerate.
func isDegenerate(text string, threshold int) bool {
	if len(text) < 100 {
		return false
	}
	words := splitWords(text)
	if len(words) < 8 {
		return false
	}
	const ngramSize = 4
	counts := make(map[string]int)
	for i := 0; i <= len(words)-ngramSize; i++ {
		key := words[i] + " " + words[i+1] + " " + words[i+2] + " " + words[i+3]
		counts[key]++
		if counts[key] > threshold {
			return true
		}
	}
	return false
}

// splitWords splits text on whitespace, returning non-empty tokens.
func splitWords(text string) []string {
	var words []string
	start := -1
	for i, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start >= 0 {
				words = append(words, text[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, text[start:])
	}
	return words
}
