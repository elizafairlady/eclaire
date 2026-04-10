package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"charm.land/fantasy"
)

type searchInput struct {
	Query      string `json:"query" jsonschema:"description=Search query"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"description=Maximum results to return (1-20, default: 10)"`
}

// SearchTool creates the web search tool. Uses parallel execution.
func SearchTool() Tool {
	at := fantasy.NewParallelAgentTool("web_search", "Search the web and return results",
		func(ctx context.Context, input searchInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			maxResults := input.MaxResults
			if maxResults <= 0 {
				maxResults = 10
			}
			if maxResults > 20 {
				maxResults = 20
			}

			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			results, err := searchDuckDuckGo(ctx, input.Query, maxResults)
			if err != nil {
				return fantasy.ToolResponse{Content: fmt.Sprintf("Search error: %v", err)}, nil
			}

			if len(results) == 0 {
				return fantasy.ToolResponse{Content: "No results found."}, nil
			}

			return fantasy.ToolResponse{Content: strings.Join(results, "\n\n")}, nil
		},
	)
	return Wrap(at, TrustReadOnly, "search")
}

func searchDuckDuckGo(ctx context.Context, query string, maxResults int) ([]string, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "eclaire/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}

	return parseDDGResults(string(body), maxResults), nil
}

var (
	ddgResultRe = regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>([^<]*)</a>`)
	ddgSnippetRe = regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>([^<]*)</a>`)
)

func parseDDGResults(html string, max int) []string {
	links := ddgResultRe.FindAllStringSubmatch(html, max)
	snippets := ddgSnippetRe.FindAllStringSubmatch(html, max)

	var results []string
	for i, link := range links {
		if i >= max {
			break
		}
		title := strings.TrimSpace(link[2])
		href := strings.TrimSpace(link[1])
		snippet := ""
		if i < len(snippets) {
			snippet = strings.TrimSpace(snippets[i][1])
		}
		results = append(results, fmt.Sprintf("**%s**\n%s\n%s", title, href, snippet))
	}
	return results
}
