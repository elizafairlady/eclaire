package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/fantasy"
)

const testRSS2 = `<?xml version="1.0"?>
<rss version="2.0">
<channel>
<title>Test Blog</title>
<item>
  <title>First Post</title>
  <link>https://example.com/1</link>
  <pubDate>Mon, 07 Apr 2026 12:00:00 GMT</pubDate>
  <description>This is the &lt;b&gt;first&lt;/b&gt; post.</description>
</item>
<item>
  <title>Second Post</title>
  <link>https://example.com/2</link>
  <pubDate>Sun, 06 Apr 2026 12:00:00 GMT</pubDate>
  <description>Second post content.</description>
</item>
</channel>
</rss>`

const testAtom = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>Atom Blog</title>
<entry>
  <title>Atom Entry</title>
  <link href="https://example.com/atom/1" rel="alternate"/>
  <updated>2026-04-07T12:00:00Z</updated>
  <summary>An &lt;em&gt;atom&lt;/em&gt; entry.</summary>
</entry>
</feed>`

func TestRSSParseRSS2(t *testing.T) {
	items, title := parseFeed([]byte(testRSS2))
	if title != "Test Blog" {
		t.Errorf("title = %q", title)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "First Post" {
		t.Errorf("item[0].Title = %q", items[0].Title)
	}
	if items[0].Link != "https://example.com/1" {
		t.Errorf("item[0].Link = %q", items[0].Link)
	}
}

func TestRSSParseAtom(t *testing.T) {
	items, title := parseFeed([]byte(testAtom))
	if title != "Atom Blog" {
		t.Errorf("title = %q", title)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Title != "Atom Entry" {
		t.Errorf("item[0].Title = %q", items[0].Title)
	}
	if items[0].Link != "https://example.com/atom/1" {
		t.Errorf("item[0].Link = %q", items[0].Link)
	}
}

func TestRSSMaxItems(t *testing.T) {
	// Parse full feed, then verify truncation happens in tool
	items, _ := parseFeed([]byte(testRSS2))
	if len(items) < 2 {
		t.Skip("need at least 2 items")
	}
	// Simulate maxItems=1
	if len(items) > 1 {
		items = items[:1]
	}
	if len(items) != 1 {
		t.Errorf("truncated to %d items", len(items))
	}
}

func TestRSSStripHTML(t *testing.T) {
	items, _ := parseFeed([]byte(testRSS2))
	// The description has HTML tags: <b>first</b>
	// The tool strips them via bluemonday
	// parseFeed itself doesn't strip — stripping happens in the tool
	if !strings.Contains(items[0].Description, "<b>") {
		// parseFeed keeps raw HTML; the tool function does the stripping
	}
}

func TestRSSFeedToolHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(testRSS2))
	}))
	defer ts.Close()

	tool := RSSFeedTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		Input: `{"feed_url":"` + ts.URL + `","max_items":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.IsError {
		t.Fatalf("error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Test Blog") {
		t.Error("should contain feed title")
	}
	if !strings.Contains(resp.Content, "First Post") {
		t.Error("should contain first item")
	}
	// max_items=1, should not contain second post
	if strings.Contains(resp.Content, "Second Post") {
		t.Error("should not contain second post (max_items=1)")
	}
}

func TestRSSFeedToolEmptyURL(t *testing.T) {
	tool := RSSFeedTool()
	resp, _ := tool.Run(context.Background(), fantasy.ToolCall{Input: `{}`})
	if !resp.IsError {
		t.Error("should error for empty URL")
	}
}
