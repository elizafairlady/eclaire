package agent

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/elizafairlady/eclaire/internal/provider"
)

// ContextBudget tracks token usage against a context window.
type ContextBudget struct {
	ContextWindow int64
	UsedTokens    int64
}

// ShouldSummarize returns true when remaining tokens are below threshold.
func (b *ContextBudget) ShouldSummarize() bool {
	if b.ContextWindow <= 0 {
		return false
	}
	remaining := b.ContextWindow - b.UsedTokens
	if b.ContextWindow > 200000 {
		return remaining <= 40000
	}
	return float64(remaining) < float64(b.ContextWindow)*0.2
}

// RemainingTokens returns how many tokens are left.
func (b *ContextBudget) RemainingTokens() int64 {
	r := b.ContextWindow - b.UsedTokens
	if r < 0 {
		return 0
	}
	return r
}

// Summarize compresses messages into a summary using a small model.
func Summarize(ctx context.Context, router *provider.Router, messages []fantasy.Message) (string, error) {
	lm, err := router.Resolve(ctx, "simple")
	if err != nil {
		return "", fmt.Errorf("resolve summarizer model: %w", err)
	}

	// Build summary prompt from messages
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation, preserving:\n")
	sb.WriteString("- Key decisions and conclusions\n")
	sb.WriteString("- Code changes and file modifications\n")
	sb.WriteString("- Important context for continuing the work\n")
	sb.WriteString("- Any unresolved tasks or questions\n\n")
	sb.WriteString("Be concise but thorough.\n\n")
	sb.WriteString("---\n\n")

	for _, msg := range messages {
		role := string(msg.Role)
		for _, part := range msg.Content {
			if tp, ok := part.(fantasy.TextPart); ok {
				sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, tp.Text))
			}
		}
	}

	prompt := sb.String()
	if len(prompt) > 50000 {
		// Truncate middle if too long
		prompt = prompt[:25000] + "\n\n[...conversation truncated...]\n\n" + prompt[len(prompt)-25000:]
	}

	resp, err := lm.Generate(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			fantasy.NewUserMessage(prompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}

	return resp.Content.Text(), nil
}
