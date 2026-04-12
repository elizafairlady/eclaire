package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"
)

type memoryWriteInput struct {
	Content string `json:"content" jsonschema:"description=Text to save to memory"`
	Type    string `json:"type" jsonschema:"description=Memory type: curated (long-term MEMORY.md) or daily (today's log),enum=curated,daily"`
}

type memoryReadInput struct {
	Query string `json:"query,omitempty" jsonschema:"description=Search term to find in memory files"`
	Date  string `json:"date,omitempty" jsonschema:"description=Specific daily log date (YYYY-MM-DD)"`
}

// MemoryWriteTool creates the memory write tool.
// workspaceDir is ~/.eclaire/workspace/
func MemoryWriteTool(workspaceDir string) Tool {
	return NewTool("memory_write", "Save information to persistent memory (MEMORY.md or daily log)", TrustModify, "memory",
		func(ctx context.Context, input memoryWriteInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.Content == "" {
				return fantasy.ToolResponse{Content: "Error: content is required"}, nil
			}

			if warning := checkMemoryInjection(input.Content); warning != "" {
				return fantasy.ToolResponse{Content: "BLOCKED: " + warning}, nil
			}

			var path string
			switch input.Type {
			case "curated":
				path = filepath.Join(workspaceDir, "MEMORY.md")
			case "daily":
				memDir := filepath.Join(workspaceDir, "memory")
				os.MkdirAll(memDir, 0o700)
				path = filepath.Join(memDir, time.Now().Format("2006-01-02")+".md")
			default:
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: type must be 'curated' or 'daily', got %q", input.Type)}, nil
			}

			os.MkdirAll(filepath.Dir(path), 0o700)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			defer f.Close()

			_, err = f.WriteString(input.Content + "\n")
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
			}

			return fantasy.ToolResponse{Content: fmt.Sprintf("Saved to %s memory", input.Type)}, nil
		},
	)
}

// checkMemoryInjection detects prompt injection patterns in memory content.
// Memory files are re-injected into system prompts — any role markers,
// system directives, or instruction-like content could override agent behavior.
// Returns a description of the problem, or "" if clean.
func checkMemoryInjection(content string) string {
	lower := strings.ToLower(content)

	// Role markers that could confuse model context boundaries
	roleMarkers := []string{
		"<|system|>", "<|user|>", "<|assistant|>",
		"[system]", "[inst]", "[/inst]",
		"<<sys>>", "<</sys>>",
		"\nsystem:", "\nuser:", "\nassistant:",
		"human:", "assistant:",
	}
	for _, marker := range roleMarkers {
		if strings.Contains(lower, marker) {
			return fmt.Sprintf("content contains role marker %q which could be used for prompt injection", marker)
		}
	}

	// Instruction-like directives that try to override behavior
	directives := []string{
		"ignore previous instructions",
		"ignore all previous",
		"disregard previous",
		"you are now",
		"new instructions:",
		"override:",
		"from now on",
		"forget everything",
	}
	for _, d := range directives {
		if strings.Contains(lower, d) {
			return fmt.Sprintf("content contains directive pattern %q which could be used for prompt injection", d)
		}
	}

	// Excessive length for a single memory entry
	if len(content) > 10000 {
		return "content exceeds 10000 character limit for a single memory entry"
	}

	return ""
}

// MemoryWriteDailyOnlyTool creates a memory_write tool restricted to daily logs.
// Used during memory flush before compaction to prevent accidental MEMORY.md overwrites.
// Reference: OpenClaw src/auto-reply/reply/memory-flush.ts — append-only guard.
func MemoryWriteDailyOnlyTool(workspaceDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool("memory_write",
		"Save information to today's daily log. Only type='daily' is allowed during memory flush.",
		func(ctx context.Context, input memoryWriteInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.Content == "" {
				return fantasy.ToolResponse{Content: "Error: content is required"}, nil
			}
			if input.Type != "daily" {
				return fantasy.ToolResponse{Content: "Error: during memory flush, only type='daily' is allowed"}, nil
			}
			if warning := checkMemoryInjection(input.Content); warning != "" {
				return fantasy.ToolResponse{Content: "BLOCKED: " + warning}, nil
			}

			memDir := filepath.Join(workspaceDir, "memory")
			os.MkdirAll(memDir, 0o700)
			path := filepath.Join(memDir, time.Now().Format("2006-01-02")+".md")

			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			defer f.Close()

			_, err = f.WriteString(input.Content + "\n")
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
			}

			return fantasy.ToolResponse{Content: "Saved to daily memory"}, nil
		},
	)
}

// MemoryReadTool creates the memory read tool.
func MemoryReadTool(workspaceDir string) Tool {
	return NewTool("memory_read", "Read from persistent memory (MEMORY.md or daily logs)", TrustReadOnly, "memory",
		func(ctx context.Context, input memoryReadInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Read specific daily log
			if input.Date != "" {
				path := filepath.Join(workspaceDir, "memory", input.Date+".md")
				data, err := os.ReadFile(path)
				if err != nil {
					return fantasy.ToolResponse{Content: fmt.Sprintf("No daily log for %s", input.Date)}, nil
				}
				return fantasy.ToolResponse{Content: string(data)}, nil
			}

			// Search across memory files
			var results []string

			// Read MEMORY.md
			memPath := filepath.Join(workspaceDir, "MEMORY.md")
			if data, err := os.ReadFile(memPath); err == nil {
				content := string(data)
				if input.Query == "" || strings.Contains(strings.ToLower(content), strings.ToLower(input.Query)) {
					results = append(results, "## MEMORY.md\n"+content)
				}
			}

			// Read daily logs
			memDir := filepath.Join(workspaceDir, "memory")
			entries, _ := os.ReadDir(memDir)
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(memDir, entry.Name()))
				if err != nil {
					continue
				}
				content := string(data)
				if input.Query == "" || strings.Contains(strings.ToLower(content), strings.ToLower(input.Query)) {
					date := strings.TrimSuffix(entry.Name(), ".md")
					results = append(results, "## "+date+"\n"+content)
				}
			}

			if len(results) == 0 {
				if input.Query != "" {
					return fantasy.ToolResponse{Content: fmt.Sprintf("No memory entries matching %q", input.Query)}, nil
				}
				return fantasy.ToolResponse{Content: "No memory entries found"}, nil
			}

			return fantasy.ToolResponse{Content: strings.Join(results, "\n---\n")}, nil
		},
	)
}
