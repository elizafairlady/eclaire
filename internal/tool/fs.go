package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
)

// expandPath resolves ~ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

type readInput struct {
	Path   string `json:"path" jsonschema:"description=Absolute path to the file to read"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Line number to start reading from (1-based)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum number of lines to read"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"description=Absolute path to the file to write"`
	Content string `json:"content" jsonschema:"description=Content to write to the file"`
}

type editInput struct {
	Path      string `json:"path" jsonschema:"description=Absolute path to the file to edit"`
	OldString string `json:"old_string" jsonschema:"description=Exact text to find and replace"`
	NewString string `json:"new_string" jsonschema:"description=Text to replace the old string with"`
}

type globInput struct {
	Pattern string `json:"pattern" jsonschema:"description=Glob pattern to match files (e.g. **/*.go)"`
	Path    string `json:"path,omitempty" jsonschema:"description=Directory to search in"`
}

type grepInput struct {
	Pattern string `json:"pattern" jsonschema:"description=Regular expression pattern to search for"`
	Path    string `json:"path,omitempty" jsonschema:"description=Directory or file to search in"`
	Glob    string `json:"glob,omitempty" jsonschema:"description=Glob pattern to filter files"`
}

// ReadTool creates the file read tool.
func ReadTool() Tool {
	return NewTool("read", "Read the contents of a file", TrustReadOnly, "fs",
		func(ctx context.Context, input readInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			data, err := os.ReadFile(input.Path)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			content := string(data)
			if input.Offset > 0 || input.Limit > 0 {
				lines := strings.Split(content, "\n")
				start := 0
				if input.Offset > 0 {
					start = input.Offset - 1
				}
				if start >= len(lines) {
					return fantasy.ToolResponse{Content: ""}, nil
				}
				end := len(lines)
				if input.Limit > 0 && start+input.Limit < end {
					end = start + input.Limit
				}
				lines = lines[start:end]

				var numbered []string
				for i, line := range lines {
					numbered = append(numbered, fmt.Sprintf("%d\t%s", start+i+1, line))
				}
				content = strings.Join(numbered, "\n")
			}

			return fantasy.ToolResponse{Content: content}, nil
		},
	)
}

// WriteTool creates the file write tool.
func WriteTool() Tool {
	return NewTool("write", "Write content to a file (creates or overwrites)", TrustModify, "fs",
		func(ctx context.Context, input writeInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			dir := filepath.Dir(input.Path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error creating directory: %v", err)}, nil
			}

			if err := os.WriteFile(input.Path, []byte(input.Content), 0o644); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			return fantasy.ToolResponse{Content: fmt.Sprintf("Wrote %d bytes to %s", len(input.Content), input.Path)}, nil
		},
	)
}

// EditTool creates the file edit tool.
func EditTool() Tool {
	return NewTool("edit", "Replace exact text in a file", TrustModify, "fs",
		func(ctx context.Context, input editInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			data, err := os.ReadFile(input.Path)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			content := string(data)
			if !strings.Contains(content, input.OldString) {
				return fantasy.ToolResponse{Content: "Error: old_string not found in file"}, nil
			}

			count := strings.Count(content, input.OldString)
			if count > 1 {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: old_string matches %d times, must be unique", count)}, nil
			}

			newContent := strings.Replace(content, input.OldString, input.NewString, 1)
			if err := os.WriteFile(input.Path, []byte(newContent), 0o644); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			return fantasy.ToolResponse{Content: fmt.Sprintf("Edited %s", input.Path)}, nil
		},
	)
}

// GlobTool creates the file glob tool.
func GlobTool() Tool {
	return NewTool("glob", "Find files matching a glob pattern", TrustReadOnly, "fs",
		func(ctx context.Context, input globInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			root := input.Path
			if root == "" {
				root = "."
			}

			var matches []string
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				matched, _ := filepath.Match(input.Pattern, filepath.Base(path))
				if matched {
					matches = append(matches, path)
				}
				if len(matches) >= 200 {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			return fantasy.ToolResponse{Content: strings.Join(matches, "\n")}, nil
		},
	)
}

// GrepTool creates the content search tool.
func GrepTool() Tool {
	return NewTool("grep", "Search file contents using ripgrep pattern", TrustReadOnly, "fs",
		func(ctx context.Context, input grepInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			args := []string{"-n", "--color=never", "-e", input.Pattern}
			if input.Glob != "" {
				args = append(args, "--glob", input.Glob)
			}
			path := input.Path
			if path == "" {
				path = "."
			}
			args = append(args, path)

			// Try rg first, fall back to grep -rn
			cmd := "rg"
			out, err := execTool(ctx, cmd, args...)
			if err != nil {
				// Fallback to grep
				grepArgs := []string{"-rn", input.Pattern}
				if input.Path != "" {
					grepArgs = append(grepArgs, input.Path)
				}
				out, _ = execTool(ctx, "grep", grepArgs...)
			}

			return fantasy.ToolResponse{Content: out}, nil
		},
	)
}

func execTool(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
