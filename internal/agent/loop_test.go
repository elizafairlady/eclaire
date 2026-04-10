package agent

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func makeToolStep(name, input, output string) fantasy.StepResult {
	return fantasy.StepResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.ToolCallContent{
					ToolCallID: "call-1",
					ToolName:   name,
					Input:      input,
				},
				fantasy.ToolResultContent{
					ToolCallID: "call-1",
					ToolName:   name,
					Result:     fantasy.ToolResultOutputContentText{Text: output},
				},
			},
		},
	}
}

func makeTextStep(text string) fantasy.StepResult {
	return fantasy.StepResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: text},
			},
		},
	}
}

func TestHasRepeatedToolCalls_NoLoop(t *testing.T) {
	var steps []fantasy.StepResult
	for i := range 10 {
		steps = append(steps, makeToolStep("shell", "ls", "file-"+string(rune('a'+i))))
	}
	if hasRepeatedToolCalls(steps, 10, 5) {
		t.Error("different outputs should not trigger loop detection")
	}
}

func TestHasRepeatedToolCalls_DetectsLoop(t *testing.T) {
	var steps []fantasy.StepResult
	for range 10 {
		steps = append(steps, makeToolStep("shell", "ls", "same-output"))
	}
	if !hasRepeatedToolCalls(steps, 10, 5) {
		t.Error("10 identical tool calls should trigger loop detection with maxRepeats=5")
	}
}

func TestHasRepeatedToolCalls_BelowWindow(t *testing.T) {
	var steps []fantasy.StepResult
	for range 5 {
		steps = append(steps, makeToolStep("shell", "ls", "same"))
	}
	if hasRepeatedToolCalls(steps, 10, 5) {
		t.Error("fewer steps than window should not trigger")
	}
}

func TestHasRepeatedToolCalls_TextOnlySteps(t *testing.T) {
	var steps []fantasy.StepResult
	for range 10 {
		steps = append(steps, makeTextStep("hello"))
	}
	if hasRepeatedToolCalls(steps, 10, 5) {
		t.Error("text-only steps should not trigger loop detection")
	}
}

func TestToolInteractionSignature_Empty(t *testing.T) {
	content := fantasy.ResponseContent{
		fantasy.TextContent{Text: "no tools"},
	}
	if sig := toolInteractionSignature(content); sig != "" {
		t.Errorf("expected empty signature for text-only content, got %q", sig)
	}
}

func TestToolInteractionSignature_Deterministic(t *testing.T) {
	content := fantasy.ResponseContent{
		fantasy.ToolCallContent{
			ToolCallID: "call-1",
			ToolName:   "shell",
			Input:      `{"command":"ls"}`,
		},
		fantasy.ToolResultContent{
			ToolCallID: "call-1",
			ToolName:   "shell",
			Result:     fantasy.ToolResultOutputContentText{Text: "files"},
		},
	}

	sig1 := toolInteractionSignature(content)
	sig2 := toolInteractionSignature(content)

	if sig1 != sig2 {
		t.Error("same content should produce same signature")
	}
	if sig1 == "" {
		t.Error("signature should not be empty for tool calls")
	}
}

func TestHasRepeatedToolCalls_MixedSteps(t *testing.T) {
	var steps []fantasy.StepResult
	// 5 unique, then 6 identical
	for i := range 5 {
		steps = append(steps, makeToolStep("shell", "cmd-"+string(rune('a'+i)), "unique"))
	}
	for range 6 {
		steps = append(steps, makeToolStep("shell", "ls", "same"))
	}
	// Window of last 10 has 4 unique + 6 identical
	if !hasRepeatedToolCalls(steps, 10, 5) {
		t.Error("6 identical in window of 10 should trigger with maxRepeats=5")
	}
}

// --- Tests for ConversationRuntime loop detection ---

func TestIsLooping_DetectsRepeats(t *testing.T) {
	history := make([]string, 10)
	for i := range 10 {
		history[i] = "same-signature"
	}
	if !isLooping(history, 10, 5) {
		t.Error("10 identical signatures should trigger with maxRepeats=5")
	}
}

func TestIsLooping_NoRepeats(t *testing.T) {
	var history []string
	for i := range 10 {
		history = append(history, fmt.Sprintf("sig-%d", i))
	}
	if isLooping(history, 10, 5) {
		t.Error("all unique signatures should not trigger")
	}
}

func TestIsLooping_BelowWindow(t *testing.T) {
	history := []string{"a", "a", "a"}
	if isLooping(history, 10, 5) {
		t.Error("fewer entries than window should not trigger")
	}
}

func TestHashToolIteration_Deterministic(t *testing.T) {
	calls := []toolCallInfo{
		{ID: "c1", Name: "shell", Input: `{"command":"ls"}`},
	}
	results := map[string]string{"c1": "file.txt"}

	h1 := hashToolIteration(calls, results)
	h2 := hashToolIteration(calls, results)
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == "" {
		t.Error("hash should not be empty for non-empty calls")
	}
}

func TestHashToolIteration_DiffersWithInput(t *testing.T) {
	calls1 := []toolCallInfo{{ID: "c1", Name: "shell", Input: `{"command":"ls"}`}}
	calls2 := []toolCallInfo{{ID: "c1", Name: "shell", Input: `{"command":"cat foo"}`}}
	results := map[string]string{"c1": "output"}

	if hashToolIteration(calls1, results) == hashToolIteration(calls2, results) {
		t.Error("different inputs should produce different hashes")
	}
}

func TestHashToolIteration_Empty(t *testing.T) {
	if sig := hashToolIteration(nil, nil); sig != "" {
		t.Errorf("empty calls should return empty signature, got %q", sig)
	}
}

func TestIsDegenerate_RepetitiveText(t *testing.T) {
	// "member member member member" repeated many times
	text := ""
	for range 50 {
		text += "member member member member "
	}
	if !isDegenerate(text, 20) {
		t.Error("highly repetitive text should be flagged as degenerate")
	}
}

func TestIsDegenerate_NormalText(t *testing.T) {
	text := "The Artemis II mission is NASA's first crewed flight of the Space Launch System and Orion spacecraft. " +
		"The crew consists of four astronauts who will orbit the Moon and return to Earth. " +
		"This mission will test critical life support systems and validate deep space navigation capabilities. " +
		"Launch is scheduled from Kennedy Space Center in Florida."
	if isDegenerate(text, 20) {
		t.Error("normal prose should not be flagged as degenerate")
	}
}

func TestIsDegenerate_ShortText(t *testing.T) {
	if isDegenerate("short", 10) {
		t.Error("short text should never be flagged as degenerate")
	}
}

func TestIsDegenerate_EmptyText(t *testing.T) {
	if isDegenerate("", 10) {
		t.Error("empty text should not be flagged")
	}
}

// --- False positive prevention tests ---
// These verify that normal agent output patterns are NOT falsely flagged.

func TestIsDegenerate_CodeWithRepeatedPatterns(t *testing.T) {
	// Code often has repeated patterns (imports, similar lines). Must not flag.
	code := `import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := step1(); err != nil {
		log.Fatal(err)
	}
	if err := step2(); err != nil {
		log.Fatal(err)
	}
	if err := step3(); err != nil {
		log.Fatal(err)
	}
	if err := step4(); err != nil {
		log.Fatal(err)
	}
	if err := step5(); err != nil {
		log.Fatal(err)
	}
}`
	if isDegenerate(code, 10) {
		t.Error("Go code with repeated error handling should not be flagged")
	}
}

func TestIsDegenerate_BulletedList(t *testing.T) {
	// Research reports often have bulleted lists with repeated structure
	text := "Key findings from the research:\n"
	for i := range 15 {
		text += fmt.Sprintf("- Finding %d: The analysis shows important results in this area.\n", i+1)
	}
	if isDegenerate(text, 20) {
		t.Error("bulleted list with varied content should not be flagged")
	}
}

func TestIsDegenerate_MarkdownTable(t *testing.T) {
	// Tables have repeated delimiters
	text := "| Name | Value | Status |\n|------|-------|--------|\n"
	for i := range 20 {
		text += fmt.Sprintf("| item%d | %d | active |\n", i, i*10)
	}
	if isDegenerate(text, 20) {
		t.Error("markdown table should not be flagged")
	}
}

func TestIsDegenerate_RealDegenerateOutput(t *testing.T) {
	// The actual failure we observed: "Reid a. a single-crew member member member..."
	text := "Reid a. a single-crew " + strings.Repeat("member ", 100)
	if !isDegenerate(text, 20) {
		t.Error("100x repeated 'member' should be flagged as degenerate")
	}
}

func TestIsDegenerate_JSON(t *testing.T) {
	// JSON output with repeated structure
	text := `{"results": [`
	for i := range 20 {
		if i > 0 {
			text += ","
		}
		text += fmt.Sprintf(`{"id": %d, "name": "item %d", "status": "active"}`, i, i)
	}
	text += `]}`
	if isDegenerate(text, 20) {
		t.Error("JSON with varied content should not be flagged")
	}
}

func TestIsDegenerate_LongNormalProse(t *testing.T) {
	// A long research report should never be flagged
	paragraphs := []string{
		"The Artemis program represents NASA's next giant leap in space exploration.",
		"Building on decades of experience from Mercury, Gemini, Apollo, and the Space Shuttle programs.",
		"The Space Launch System is the most powerful rocket ever built by NASA.",
		"Orion is designed to carry astronauts beyond low Earth orbit for the first time since Apollo.",
		"International partnerships with ESA, JAXA, and CSA strengthen the program.",
		"The lunar Gateway will serve as a multi-purpose outpost orbiting the Moon.",
		"Artemis II will be the first crewed test flight of the integrated SLS and Orion system.",
		"The mission will send four astronauts on a trajectory around the Moon.",
		"Critical systems including life support and navigation will be validated during the flight.",
		"Recovery operations in the Pacific Ocean will conclude the ten-day mission.",
	}
	text := strings.Join(paragraphs, "\n\n")
	if isDegenerate(text, 20) {
		t.Error("research prose should never be flagged as degenerate")
	}
}

func TestIsLooping_FalsePositive_SimilarButDifferent(t *testing.T) {
	// Tool calls that are similar but produce different results should not loop-detect
	var history []string
	for i := range 10 {
		calls := []toolCallInfo{{ID: "c1", Name: "web_search", Input: fmt.Sprintf(`{"query":"topic page %d"}`, i)}}
		results := map[string]string{"c1": fmt.Sprintf("result %d", i)}
		history = append(history, hashToolIteration(calls, results))
	}
	if isLooping(history, 10, 5) {
		t.Error("different search queries with different results should not trigger loop detection")
	}
}

func TestIsLooping_SameInputDifferentOutput(t *testing.T) {
	// Same tool call but different results each time (e.g., checking status)
	var history []string
	for i := range 10 {
		calls := []toolCallInfo{{ID: "c1", Name: "shell", Input: `{"command":"date"}`}}
		results := map[string]string{"c1": fmt.Sprintf("2026-04-10 %02d:00:00", i)}
		history = append(history, hashToolIteration(calls, results))
	}
	if isLooping(history, 10, 5) {
		t.Error("same command with different output should not trigger loop detection")
	}
}

// --- Real-world test fixtures from actual eclaire sessions ---

func TestIsDegenerate_RealArtemisOutput(t *testing.T) {
	// The actual degenerate output from the Artemis II research session (session 8e4362d5)
	text := "---\n\n# Research Report: Artemis II Mission\n\n## Summary\n" +
		"Artemis II is NASA's first crewed flight of the Space Launch System (SLS) and Orion spacecraft, " +
		"designed as a high-earth orbit (HEO) earth-flyby mission. This mission will not land on the Moon, " +
		"but will send four astronauts aboard Orion spacecraft around the Moon and back to Earth.\n\n" +
		"## Key Findings\n- **Crew Members and Roles**:\n    - **Reid a. a single-crew " +
		strings.Repeat("member ", 80)
	if !isDegenerate(text, 20) {
		t.Error("real Artemis II degenerate output should be detected")
	}
}

func TestIsDegenerate_RealWebSearchResult(t *testing.T) {
	// Real DuckDuckGo search result (from session 8e4362d5)
	text := `**Artemis II: NASA's First Crewed Lunar Flyby in 50 Years - NASA**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.nasa.gov%2Fmission%2Fartemis-ii%2F

**Artemis II - Wikipedia**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FArtemis_II

**NASA Artemis II launch explained: What to know about rocket, mission**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.usatoday.com%2Fstory%2Fnews%2F

**Everything you need to know about NASA's Artemis II mission**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.bbc.com%2Fnews%2Farticles%2F

**Artemis II Mission 2026 - Crew, Launch Updates & Live Feed**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fastronomywatch.org%2Fartemis-ii%2F

**Artemis II | Mission, Crew, Launch, Speed, & Moon | Britannica**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.britannica.com%2Ftopic%2FArtemis-II

**NASA Artemis II launch date: when moon mission starts, crew, details**
//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.houstonchronicle.com%2Fnews%2F`
	if isDegenerate(text, 20) {
		t.Error("real web search results with repeated URL patterns should NOT be flagged")
	}
}

func TestIsDegenerate_RealFetchResult(t *testing.T) {
	// Real fetch output from NASA.gov (from session 8e4362d5) — has repeated nav elements
	text := `HTTP 200 200 OK

# Artemis II: NASA's First Crewed Lunar Flyby in 50 Years - NASA

Author: Dacia Massengill; Thalia K Patrinos
Date: 2023-02-28

Explore Search News & Events News & Events News Releases Recently Published Video Series on NASA+ ` +
		`Podcasts & Audio Blogs Newsletters Social Media Media Resources Events Upcoming Launches & Landings ` +
		`Virtual Guest Program Multimedia Multimedia NASA+ Images NASA Live NASA Apps Podcasts Image of the Day ` +
		`e-Books Interactives STEM Multimedia NASA Brand & Usage Guidelines NASA+ Search Suggested Searches ` +
		`Climate Change Artemis Expedition 64 Mars perseverance SpaceX Crew-2 International Space Station ` +
		`View All Topics A-Z Home Missions Humans in Space Earth The Solar System The Universe Science ` +
		`Aeronautics Technology Learning Resources About NASA`
	if isDegenerate(text, 20) {
		t.Error("real NASA.gov fetch with nav elements should NOT be flagged")
	}
}

func TestIsDegenerate_RealIranReport(t *testing.T) {
	// Simulate the iran_crisis.md report structure — repeated section headers
	text := "# Iran Ceasefire Crisis Report\n\n"
	for i := range 10 {
		text += fmt.Sprintf("## Update %d (2026-04-10 %02d:00)\n\n"+
			"Diplomatic talks continue between the parties involved. "+
			"The ceasefire agreement remains fragile as both sides exchange demands. "+
			"International mediators expressed cautious optimism about progress.\n\n", i+1, 8+i)
	}
	if isDegenerate(text, 20) {
		t.Error("structured report with varied content should NOT be flagged")
	}
}

func TestIsDegenerate_SwitchStatement(t *testing.T) {
	// Model writes repetitive but valid code — long switch statement
	text := "```go\nswitch op {\n"
	ops := []string{"add", "subtract", "multiply", "divide", "modulo", "power", "sqrt", "abs", "negate", "round",
		"floor", "ceil", "sin", "cos", "tan", "log", "exp", "min", "max", "avg"}
	for _, op := range ops {
		text += fmt.Sprintf("case %q:\n\treturn %s(a, b)\n", op, op)
	}
	text += "}\n```"
	if isDegenerate(text, 20) {
		t.Error("repetitive but valid switch statement code should NOT be flagged")
	}
}

func TestIsDegenerate_GrepOutput(t *testing.T) {
	// Real grep output with many matching lines — repeated "runner.go:" prefix
	text := ""
	for i := range 30 {
		text += fmt.Sprintf("internal/agent/runner.go:%d: func (r *Runner) method%d() error {\n", 100+i*10, i)
	}
	if isDegenerate(text, 20) {
		t.Error("grep output with repeated file prefix should NOT be flagged")
	}
}
