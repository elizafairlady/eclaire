package tool

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/fantasy"
)

type patchInput struct {
	Path  string `json:"path" jsonschema:"description=Absolute path to the file to patch"`
	Patch string `json:"patch" jsonschema:"description=Unified diff patch content"`
}

// PatchTool creates the unified diff patch application tool.
func PatchTool() Tool {
	return NewTool("apply_patch", "Apply a unified diff patch to a file", TrustModify, "fs",
		func(ctx context.Context, input patchInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			data, err := os.ReadFile(input.Path)
			if err != nil && !os.IsNotExist(err) {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error reading: %v", err)}, nil
			}

			lines := strings.Split(string(data), "\n")
			hunks, err := parseHunks(input.Patch)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error parsing patch: %v", err)}, nil
			}

			result, err := applyHunks(lines, hunks)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error applying patch: %v", err)}, nil
			}

			if err := os.WriteFile(input.Path, []byte(strings.Join(result, "\n")), 0o644); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
			}

			return fantasy.ToolResponse{Content: fmt.Sprintf("Applied %d hunk(s) to %s", len(hunks), input.Path)}, nil
		},
	)
}

type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []diffLine
}

type diffLine struct {
	op   byte // ' ', '+', '-'
	text string
}

func parseHunks(patch string) ([]hunk, error) {
	var hunks []hunk
	lines := strings.Split(patch, "\n")

	i := 0
	// Skip file headers
	for i < len(lines) {
		if strings.HasPrefix(lines[i], "@@") {
			break
		}
		i++
	}

	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}

		h, err := parseHunkHeader(lines[i])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		i++

		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			line := lines[i]
			if len(line) == 0 {
				h.lines = append(h.lines, diffLine{op: ' ', text: ""})
			} else {
				op := line[0]
				text := line[1:]
				if op != '+' && op != '-' && op != ' ' {
					// Treat as context
					op = ' '
					text = line
				}
				h.lines = append(h.lines, diffLine{op: op, text: text})
			}
			i++
		}

		hunks = append(hunks, h)
	}

	return hunks, nil
}

func parseHunkHeader(line string) (hunk, error) {
	// @@ -oldStart,oldCount +newStart,newCount @@
	var h hunk
	line = strings.TrimPrefix(line, "@@")
	line = strings.TrimSpace(line)
	if idx := strings.Index(line, "@@"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return h, fmt.Errorf("invalid hunk header")
	}

	old := strings.TrimPrefix(parts[0], "-")
	new_ := strings.TrimPrefix(parts[1], "+")

	oldParts := strings.SplitN(old, ",", 2)
	h.oldStart, _ = strconv.Atoi(oldParts[0])
	h.oldCount = 1
	if len(oldParts) > 1 {
		h.oldCount, _ = strconv.Atoi(oldParts[1])
	}

	newParts := strings.SplitN(new_, ",", 2)
	h.newStart, _ = strconv.Atoi(newParts[0])
	h.newCount = 1
	if len(newParts) > 1 {
		h.newCount, _ = strconv.Atoi(newParts[1])
	}

	return h, nil
}

func applyHunks(original []string, hunks []hunk) ([]string, error) {
	result := make([]string, len(original))
	copy(result, original)
	offset := 0

	for _, h := range hunks {
		pos := h.oldStart - 1 + offset
		if pos < 0 {
			pos = 0
		}

		// Build new lines for this hunk
		var newLines []string
		oldConsumed := 0

		for _, dl := range h.lines {
			switch dl.op {
			case ' ':
				newLines = append(newLines, dl.text)
				oldConsumed++
			case '+':
				newLines = append(newLines, dl.text)
			case '-':
				oldConsumed++
			}
		}

		// Replace old range with new lines
		end := pos + oldConsumed
		if end > len(result) {
			end = len(result)
		}

		var out []string
		out = append(out, result[:pos]...)
		out = append(out, newLines...)
		out = append(out, result[end:]...)
		result = out

		offset += len(newLines) - oldConsumed
	}

	return result, nil
}
