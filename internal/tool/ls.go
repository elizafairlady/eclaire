package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/fantasy"
)

type lsInput struct {
	Path   string   `json:"path,omitempty" jsonschema:"description=Directory to list (default: current directory)"`
	Depth  int      `json:"depth,omitempty" jsonschema:"description=Max depth to traverse (default: 3)"`
	Ignore []string `json:"ignore,omitempty" jsonschema:"description=Glob patterns to ignore"`
}

// LsTool creates the directory listing tool.
func LsTool() Tool {
	return NewTool("ls", "List directory contents in a tree structure", TrustReadOnly, "fs",
		func(ctx context.Context, input lsInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			root := expandPath(input.Path)
			if root == "" {
				root = "."
			}

			info, err := os.Stat(root)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			if !info.IsDir() {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %s is not a directory", root)}, nil
			}

			depth := input.Depth
			if depth <= 0 {
				depth = 3
			}

			var entries []string

			filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}

				rel, _ := filepath.Rel(root, path)
				if rel == "." {
					return nil
				}

				// Check depth
				parts := strings.Split(rel, string(filepath.Separator))
				if len(parts) > depth {
					if fi.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}

				// Check ignore patterns
				base := filepath.Base(path)
				for _, pattern := range input.Ignore {
					if matched, _ := filepath.Match(pattern, base); matched {
						if fi.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
				}

				// Skip hidden dirs (like .git)
				if fi.IsDir() && strings.HasPrefix(base, ".") && base != "." {
					return filepath.SkipDir
				}

				indent := strings.Repeat("  ", len(parts)-1)
				marker := "├── "
				if fi.IsDir() {
					entries = append(entries, fmt.Sprintf("%s%s%s/", indent, marker, base))
				} else {
					entries = append(entries, fmt.Sprintf("%s%s%s", indent, marker, base))
				}
				return nil
			})

			sort.Strings(entries)

			result := root + "/\n" + strings.Join(entries, "\n")
			return fantasy.ToolResponse{Content: result}, nil
		},
	)
}
