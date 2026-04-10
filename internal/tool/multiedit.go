package tool

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/fantasy"
)

type multiEditInput struct {
	Path  string   `json:"path" jsonschema:"description=Absolute path to the file to edit"`
	Edits []editOp `json:"edits" jsonschema:"description=Sequential edit operations to apply"`
}

type editOp struct {
	OldString string `json:"old_string" jsonschema:"description=Exact text to find"`
	NewString string `json:"new_string" jsonschema:"description=Text to replace with"`
}

// MultiEditTool creates the batch file edit tool.
func MultiEditTool() Tool {
	return NewTool("multiedit", "Apply multiple edits to a file atomically", TrustModify, "fs",
		func(ctx context.Context, input multiEditInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			data, err := os.ReadFile(input.Path)
			if err != nil {
				// First edit with empty old_string = create file
				if os.IsNotExist(err) && len(input.Edits) > 0 && input.Edits[0].OldString == "" {
					content := input.Edits[0].NewString
					for _, edit := range input.Edits[1:] {
						if !strings.Contains(content, edit.OldString) {
							return fantasy.ToolResponse{Content: fmt.Sprintf("Error: old_string not found after creation: %q", truncateStr(edit.OldString, 50))}, nil
						}
						content = strings.Replace(content, edit.OldString, edit.NewString, 1)
					}
					if err := os.WriteFile(input.Path, []byte(content), 0o644); err != nil {
						return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
					}
					return fantasy.ToolResponse{Content: fmt.Sprintf("Created %s with %d edits applied", input.Path, len(input.Edits))}, nil
				}
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			content := string(data)
			applied := 0
			var failures []string

			for i, edit := range input.Edits {
				if !strings.Contains(content, edit.OldString) {
					failures = append(failures, fmt.Sprintf("edit %d: old_string not found", i))
					continue
				}
				count := strings.Count(content, edit.OldString)
				if count > 1 {
					failures = append(failures, fmt.Sprintf("edit %d: old_string matches %d times, must be unique", i, count))
					continue
				}
				content = strings.Replace(content, edit.OldString, edit.NewString, 1)
				applied++
			}

			if err := os.WriteFile(input.Path, []byte(content), 0o644); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
			}

			result := fmt.Sprintf("Applied %d/%d edits to %s", applied, len(input.Edits), input.Path)
			if len(failures) > 0 {
				result += "\nFailures:\n" + strings.Join(failures, "\n")
			}
			return fantasy.ToolResponse{Content: result}, nil
		},
	)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
