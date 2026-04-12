package ui

import (
	"regexp"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// trailingANSISpaces matches trailing sequences of (optional ANSI escape + space + reset)
// that glamour uses to pad lines to full width.
var trailingANSISpaces = regexp.MustCompile(`(\x1b\[[0-9;]*m *\x1b\[0?m *)+$`)

// stripTrailingANSISpaces removes glamour's line-padding: trailing runs of
// ANSI-colored spaces that pad each line to the word-wrap width.
func stripTrailingANSISpaces(line string) string {
	return trailingANSISpaces.ReplaceAllString(line, "")
}

func pstr(s string) *string { return &s }
func pbool(b bool) *bool    { return &b }
func puint(u uint) *uint    { return &u }

// markdownStyle returns a custom glamour style using eclaire's color palette.
// Unlike the stock "dark" theme, this does NOT set Document BackgroundColor,
// so glamour won't pad every line to full width with colored spaces.
// Modeled on Crush's custom ansi.StyleConfig.
// Hex color constants — match the eclaire palette in styles/styles.go.
// Defined here as raw strings because glamour's ansi.StyleConfig wants *string,
// not lipgloss color.Color interfaces.
const (
	colFgBase    = "#cdd6f4"
	colFgMuted   = "#6c7086"
	colBgSurface = "#313244"
	colPrimary   = "#cba6f7"
	colSecondary = "#f9e2af"
	colBlue      = "#89b4fa"
	colGreen     = "#a6e3a1"
	colGreenDark = "#74c7ab"
	colRed       = "#f38ba8"
	colBorder    = "#45475a"
)

func markdownStyle() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: pstr(colFgBase),
			},
			// NO Margin, NO BackgroundColor — prevents full-width line padding
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      puint(1),
			IndentToken: pstr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       pstr(colBlue),
				Bold:        pbool(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: " ",
				Suffix: " ",
				Color:  pstr(colSecondary),
				Bold:   pbool(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "## "},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "### "},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "#### "},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "##### "},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  pstr(colGreenDark),
				Bold:   pbool(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{CrossedOut: pbool(true)},
		Emph:          ansi.StylePrimitive{Italic: pbool(true)},
		Strong:        ansi.StylePrimitive{Bold: pbool(true)},
		HorizontalRule: ansi.StylePrimitive{
			Color:  pstr(colBorder),
			Format: "\n--------\n",
		},
		Item:        ansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration: ansi.StylePrimitive{BlockPrefix: ". "},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link:     ansi.StylePrimitive{Color: pstr(colFgMuted), Underline: pbool(true)},
		LinkText: ansi.StylePrimitive{Color: pstr(colGreenDark), Bold: pbool(true)},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           pstr(colRed),
				BackgroundColor: pstr(colBgSurface),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: pstr(colFgBase),
				},
				Margin: puint(2),
			},
			Chroma: &ansi.Chroma{
				Text:            ansi.StylePrimitive{Color: pstr(colFgBase)},
				Comment:         ansi.StylePrimitive{Color: pstr(colFgMuted)},
				Keyword:         ansi.StylePrimitive{Color: pstr(colBlue)},
				KeywordType:     ansi.StylePrimitive{Color: pstr(colPrimary)},
				Operator:        ansi.StylePrimitive{Color: pstr(colRed)},
				Punctuation:     ansi.StylePrimitive{Color: pstr(colSecondary)},
				Name:            ansi.StylePrimitive{Color: pstr(colFgBase)},
				NameFunction:    ansi.StylePrimitive{Color: pstr(colGreenDark)},
				NameClass:       ansi.StylePrimitive{Color: pstr(colFgBase), Bold: pbool(true)},
				LiteralNumber:   ansi.StylePrimitive{Color: pstr(colGreen)},
				LiteralString:   ansi.StylePrimitive{Color: pstr(colSecondary)},
				GenericDeleted:  ansi.StylePrimitive{Color: pstr(colRed)},
				GenericInserted: ansi.StylePrimitive{Color: pstr(colGreen)},
				GenericEmph:     ansi.StylePrimitive{Italic: pbool(true)},
				GenericStrong:   ansi.StylePrimitive{Bold: pbool(true)},
			},
		},
		Table: ansi.StyleTable{},
		DefinitionDescription: ansi.StylePrimitive{BlockPrefix: "\n "},
	}
}

// markdownRenderer caches glamour renderers by width for efficient reuse.
type markdownRenderer struct {
	mu    sync.Mutex
	cache map[int]*glamour.TermRenderer
}

func newMarkdownRenderer() *markdownRenderer {
	return &markdownRenderer{
		cache: make(map[int]*glamour.TermRenderer),
	}
}

// Render converts markdown to styled terminal output.
// Falls back to raw content on error.
func (r *markdownRenderer) Render(content string, width int) string {
	if content == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	// Round to nearest 10 to bound cache size
	width = ((width + 5) / 10) * 10

	r.mu.Lock()
	renderer, ok := r.cache[width]
	if !ok {
		renderer, _ = glamour.NewTermRenderer(
			glamour.WithStyles(markdownStyle()),
			glamour.WithWordWrap(width),
		)
		r.cache[width] = renderer
	}
	r.mu.Unlock()

	if renderer == nil {
		return content
	}

	result, err := renderer.Render(content)
	if err != nil {
		return content
	}

	// Glamour pads every line to the word-wrap width with foreground-colored spaces.
	// Strip trailing whitespace from each line to prevent ANSI bloat and visual noise.
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		lines[i] = stripTrailingANSISpaces(line)
	}
	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
