package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/markusmobius/go-trafilatura"
)

type fetchInput struct {
	URL     string            `json:"url" jsonschema:"description=URL to fetch"`
	Method  string            `json:"method,omitempty" jsonschema:"description=HTTP method (GET, POST, etc.). Default: GET"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers to send"`
	Body    string            `json:"body,omitempty" jsonschema:"description=Request body (for POST/PUT)"`
	Timeout int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds (default: 30)"`
}

// FetchTool creates the HTTP fetch tool.
func FetchTool() Tool {
	return NewTool("fetch", "Make an HTTP request and return the response", TrustModify, "http",
		func(ctx context.Context, input fetchInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			method := input.Method
			if method == "" {
				method = "GET"
			}

			timeout := 30 * time.Second
			if input.Timeout > 0 {
				timeout = time.Duration(input.Timeout) * time.Second
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var bodyReader io.Reader
			if input.Body != "" {
				bodyReader = strings.NewReader(input.Body)
			}

			req, err := http.NewRequestWithContext(ctx, method, input.URL, bodyReader)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error creating request: %v", err)}, nil
			}

			for k, v := range input.Headers {
				req.Header.Set(k, v)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error: %v", err)}, nil
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024)) // 100KB max
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Error reading response: %v", err)}, nil
			}

			content := string(body)
			contentType := resp.Header.Get("Content-Type")

			// Extract readable text from HTML using trafilatura
			if strings.Contains(contentType, "text/html") || looksLikeHTML(content) {
				extracted := extractWithTrafilatura(body)
				if extracted != "" {
					content = extracted
				}
			}

			result := fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, content)
			return fantasy.ToolResponse{Content: result}, nil
		},
	)
}

// extractWithTrafilatura uses go-trafilatura to extract readable content from HTML.
func extractWithTrafilatura(html []byte) string {
	result, err := trafilatura.Extract(bytes.NewReader(html), trafilatura.Options{
		EnableFallback: true,
	})
	if err != nil || result == nil {
		return ""
	}

	var sb strings.Builder
	if result.Metadata.Title != "" {
		sb.WriteString("# " + result.Metadata.Title + "\n\n")
	}
	if result.Metadata.Author != "" {
		sb.WriteString("Author: " + result.Metadata.Author + "\n")
	}
	if result.Metadata.Date.String() != "" {
		sb.WriteString("Date: " + result.Metadata.Date.String() + "\n")
	}
	if result.Metadata.Description != "" {
		sb.WriteString("Description: " + result.Metadata.Description + "\n")
	}
	if sb.Len() > 0 {
		sb.WriteString("\n---\n\n")
	}
	sb.WriteString(result.ContentText)
	return sb.String()
}

// looksLikeHTML checks if content starts with HTML markers.
func looksLikeHTML(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "<!") || strings.HasPrefix(trimmed, "<html") || strings.HasPrefix(trimmed, "<HTML")
}
