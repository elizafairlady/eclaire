package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
)

type viewInput struct {
	Path   string `json:"path" jsonschema:"description=Absolute path to the file to view"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Line number to start from (1-based, default: 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Max lines to show (default: 2000)"`
}

// ViewTool creates the enhanced file viewer tool with line numbers and image support.
func ViewTool() Tool {
	return NewTool("view", "View file contents with line numbers, or view images", TrustReadOnly, "fs",
		func(ctx context.Context, input viewInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			input.Path = expandPath(input.Path)
			info, err := os.Stat(input.Path)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			// Check for image files
			ext := strings.ToLower(filepath.Ext(input.Path))
			if isImageExt(ext) {
				return viewImage(input.Path, info)
			}

			// Size check (1MB max for text)
			if info.Size() > 1024*1024 {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: file too large (%d bytes, max 1MB)", info.Size())}, nil
			}

			data, err := os.ReadFile(input.Path)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}

			// Check for binary
			if !isTextContent(data) {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Binary file: %s (%d bytes)", input.Path, info.Size())}, nil
			}

			lines := strings.Split(string(data), "\n")

			offset := input.Offset
			if offset <= 0 {
				offset = 1
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 2000
			}

			start := offset - 1
			if start >= len(lines) {
				return fantasy.ToolResponse{Content: ""}, nil
			}
			end := start + limit
			if end > len(lines) {
				end = len(lines)
			}

			var numbered []string
			for i := start; i < end; i++ {
				line := lines[i]
				numbered = append(numbered, fmt.Sprintf("%6d\t%s", i+1, line))
			}

			return fantasy.ToolResponse{Content: strings.Join(numbered, "\n")}, nil
		},
	)
}

func isImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

func viewImage(path string, info os.FileInfo) (fantasy.ToolResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
	}
	mimeType := http.DetectContentType(data)
	encoded := base64.StdEncoding.EncodeToString(data)
	return fantasy.ToolResponse{
		Content: fmt.Sprintf("[Image: %s, %d bytes, %s]\nBase64: %s", path, info.Size(), mimeType, encoded),
	}, nil
}

func isTextContent(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	// Check first 512 bytes for null bytes (binary indicator)
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return false
		}
	}
	return true
}
