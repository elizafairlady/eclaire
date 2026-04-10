package tool

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/microcosm-cc/bluemonday"
)

type rssInput struct {
	FeedURL  string `json:"feed_url" jsonschema:"description=RSS or Atom feed URL"`
	MaxItems int    `json:"max_items,omitempty" jsonschema:"description=Maximum items to return (1-50, default 20)"`
}

// RSSFeedTool creates a tool that fetches and parses RSS/Atom feeds.
func RSSFeedTool() Tool {
	sanitizer := bluemonday.StrictPolicy()

	return NewTool("rss_feed", "Fetch and parse an RSS or Atom feed, returning items with title, link, date, and description", TrustReadOnly, "http",
		func(ctx context.Context, input rssInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if input.FeedURL == "" {
				return fantasy.NewTextErrorResponse("feed_url is required"), nil
			}
			maxItems := input.MaxItems
			if maxItems <= 0 {
				maxItems = 20
			}
			if maxItems > 50 {
				maxItems = 50
			}

			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "GET", input.FeedURL, nil)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid URL: %v", err)), nil
			}
			req.Header.Set("User-Agent", "eclaire/0.1")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("fetch failed: %v", err)), nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("HTTP %d", resp.StatusCode)), nil
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("read failed: %v", err)), nil
			}

			items, feedTitle := parseFeed(body)
			if len(items) == 0 {
				return fantasy.ToolResponse{Content: "No items found in feed."}, nil
			}

			if len(items) > maxItems {
				items = items[:maxItems]
			}

			var sb strings.Builder
			if feedTitle != "" {
				sb.WriteString(fmt.Sprintf("# %s (%d items)\n\n", feedTitle, len(items)))
			} else {
				sb.WriteString(fmt.Sprintf("# Feed (%d items)\n\n", len(items)))
			}

			for i, item := range items {
				sb.WriteString(fmt.Sprintf("%d. **%s**", i+1, item.Title))
				if item.Date != "" {
					sb.WriteString(fmt.Sprintf(" (%s)", item.Date))
				}
				sb.WriteString("\n")
				if item.Link != "" {
					sb.WriteString(fmt.Sprintf("   %s\n", item.Link))
				}
				if item.Description != "" {
					desc := sanitizer.Sanitize(item.Description)
					desc = strings.TrimSpace(desc)
					if desc != "" {
						sb.WriteString(fmt.Sprintf("   %s\n", desc))
					}
				}
				sb.WriteString("\n")
			}

			return fantasy.ToolResponse{Content: sb.String()}, nil
		},
	)
}

type feedItem struct {
	Title       string
	Link        string
	Date        string
	Description string
}

// parseFeed detects RSS 2.0 or Atom and extracts items.
func parseFeed(data []byte) ([]feedItem, string) {
	// Try RSS 2.0 first
	var rss rssDoc
	if err := xml.Unmarshal(data, &rss); err == nil && rss.Channel.Title != "" {
		var items []feedItem
		for _, item := range rss.Channel.Items {
			items = append(items, feedItem{
				Title:       item.Title,
				Link:        item.Link,
				Date:        item.PubDate,
				Description: item.Description,
			})
		}
		return items, rss.Channel.Title
	}

	// Try Atom
	var atom atomDoc
	if err := xml.Unmarshal(data, &atom); err == nil && atom.Title != "" {
		var items []feedItem
		for _, entry := range atom.Entries {
			link := ""
			for _, l := range entry.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			items = append(items, feedItem{
				Title:       entry.Title,
				Link:        link,
				Date:        entry.Updated,
				Description: entry.Summary,
			})
		}
		return items, atom.Title
	}

	return nil, ""
}

// RSS 2.0 XML structures
type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// Atom XML structures
type atomDoc struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Links   []atomLink `xml:"link"`
	Updated string     `xml:"updated"`
	Summary string     `xml:"summary"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}
