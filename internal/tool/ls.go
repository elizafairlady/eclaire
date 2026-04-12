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
	return NewTool("ls", "List directory contents as an indented file tree. Directories end with /.", TrustReadOnly, "fs",
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

			var sb strings.Builder
			sb.WriteString(root + "/\n")
			buildTree(&sb, root, "", depth, input.Ignore)

			return fantasy.ToolResponse{Content: sb.String()}, nil
		},
	)
}

// buildTree recursively renders a directory tree with proper box-drawing connectors.
func buildTree(sb *strings.Builder, dir, prefix string, depth int, ignore []string) {
	if depth <= 0 {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Filter: skip hidden, skip ignored
	var visible []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		skip := false
		for _, pattern := range ignore {
			if matched, _ := filepath.Match(pattern, name); matched {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		visible = append(visible, e)
	}

	// Sort: directories first, then files, each group alphabetical
	sort.Slice(visible, func(i, j int) bool {
		di, dj := visible[i].IsDir(), visible[j].IsDir()
		if di != dj {
			return di
		}
		return visible[i].Name() < visible[j].Name()
	})

	for i, e := range visible {
		isLast := i == len(visible)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "╰── "
			childPrefix = "    "
		}

		name := e.Name()
		if e.IsDir() {
			sb.WriteString(prefix + connector + name + "/\n")
			buildTree(sb, filepath.Join(dir, name), prefix+childPrefix, depth-1, ignore)
		} else {
			sb.WriteString(prefix + connector + name + "\n")
		}
	}
}
