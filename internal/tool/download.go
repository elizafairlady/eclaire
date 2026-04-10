package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"charm.land/fantasy"
)

type downloadInput struct {
	URL      string `json:"url" jsonschema:"description=URL to download from"`
	FilePath string `json:"file_path" jsonschema:"description=Local path to save the file"`
	Timeout  int    `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (default: 60, max: 600)"`
}

// DownloadTool creates the file download tool. Uses parallel execution.
func DownloadTool() Tool {
	at := fantasy.NewParallelAgentTool("download", "Download a file from a URL to a local path",
		func(ctx context.Context, input downloadInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			timeout := 60 * time.Second
			if input.Timeout > 0 {
				t := time.Duration(input.Timeout) * time.Second
				if t > 600*time.Second {
					t = 600 * time.Second
				}
				timeout = t
			}

			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "GET", input.URL, nil)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			req.Header.Set("User-Agent", "eclaire/1.0")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: HTTP %d %s", resp.StatusCode, resp.Status)}, nil
			}

			dir := filepath.Dir(input.FilePath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error creating directory: %v", err)}, nil
			}

			f, err := os.Create(input.FilePath)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error creating file: %v", err)}, nil
			}
			defer f.Close()

			n, err := io.Copy(f, resp.Body)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error writing: %v", err)}, nil
			}

			ct := resp.Header.Get("Content-Type")
			return fantasy.ToolResponse{Content: fmt.Sprintf("Downloaded %d bytes to %s (Content-Type: %s)", n, input.FilePath, ct)}, nil
		},
	)
	return Wrap(at, TrustModify, "http")
}
