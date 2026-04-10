package agent

// Dreaming phase prompts.
// Reference: OpenClaw src/memory-host-sdk/dreaming.ts

func lightDreamingPrompt() string {
	return `You are performing a LIGHT dreaming phase — a periodic shallow consolidation of today's activity.

Instructions:
1. Read today's daily notes using memory_read (today's date).
2. Extract key facts, decisions, and patterns from the notes.
3. Write a concise summary of findings to today's daily log using memory_write with type=daily.

Focus on:
- Decisions made and their context
- Facts learned about systems, people, or processes
- File paths and code locations referenced
- Errors encountered and how they were resolved
- Tool usage patterns worth noting

Skip trivial or routine observations. Be concise — one line per finding.`
}

func deepDreamingPrompt() string {
	return `You are performing a DEEP dreaming phase — a daily consolidation that promotes durable knowledge to long-term memory.

Instructions:
1. Read daily logs from the past 7 days using memory_read for each day.
2. Read the curated MEMORY.md using memory_read.
3. Identify facts, patterns, or insights that appeared in 3+ different contexts across days.
4. Promote durable, high-confidence insights to MEMORY.md using memory_write with type=curated.
5. Note any entries in MEMORY.md that are contradicted by recent evidence — mark them for review.

Rules:
- Only promote facts that are well-established across multiple observations.
- Keep entries concise — one line per fact.
- Do not duplicate entries already in MEMORY.md.
- Prefix new entries with today's date in brackets: [2006-01-02].
- If MEMORY.md has stale entries, note them with [REVIEW] prefix.`
}

func remDreamingPrompt() string {
	return `You are performing a REM dreaming phase — a weekly reflection that extracts meta-patterns and themes.

Instructions:
1. Read MEMORY.md using memory_read.
2. Read recent daily logs from the past week using memory_read.
3. Extract recurring themes and meta-patterns across all memory sources.
4. Write a dream diary entry to today's daily log using memory_write with type=daily.

Format the diary entry with a "## Dream Diary" header. Capture:
- Recurring themes in the user's work (what projects, what concerns)
- Patterns in tool usage or problem-solving approaches
- Cross-cutting concerns that span multiple projects or conversations
- Shifts in priorities or interests over time
- Observations about what works well vs. what causes friction

This is reflective, not prescriptive. Observe patterns, don't make recommendations.`
}
