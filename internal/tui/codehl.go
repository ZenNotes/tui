package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Syntax highlighting for fenced code in the editor: each fenced block is
// tokenized with chroma once per buffer version and the token colors are
// applied on top of the fence style, so code reads the way it does in the
// preview and in the desktop app.

type codeSpan struct {
	start, end int
	fg         string
	bold       bool
	italic     bool
}

type codeHighlights struct {
	version int
	style   string
	engine  string
	lines   map[int][]codeSpan
}

// registerChromaStyle builds the code style of a theme and registers it
// under a name the editor and Glamour's code blocks both look up. The roles
// follow the desktop's syntax colors: accent keywords, green strings, yellow
// numbers and constants, blue functions, purple types, red tags. Registering
// again under the same name replaces the style, so an edited custom theme
// takes effect when it is selected again.
func registerChromaStyle(t Theme) string {
	name := "zennotes-" + t.ID
	fg, dim, muted := string(t.Fg), string(t.FgDim), string(t.FgMuted)
	accent, soft := string(t.Accent), string(t.AccentSoft)
	red, green, yellow := string(t.Red), string(t.Green), string(t.Yellow)
	blue, purple := string(t.Blue), string(t.Purple)
	style, err := chroma.NewStyle(name, chroma.StyleEntries{
		chroma.Background:        fg,
		chroma.Text:              fg,
		chroma.Error:             red,
		chroma.Comment:           "italic " + muted,
		chroma.CommentPreproc:    "italic " + soft,
		chroma.Keyword:           "bold " + accent,
		chroma.KeywordConstant:   yellow,
		chroma.KeywordType:       purple,
		chroma.Operator:          dim,
		chroma.Punctuation:       dim,
		chroma.Name:              fg,
		chroma.NameAttribute:     purple,
		chroma.NameBuiltin:       blue,
		chroma.NameClass:         purple,
		chroma.NameConstant:      yellow,
		chroma.NameDecorator:     "italic " + soft,
		chroma.NameFunction:      blue,
		chroma.NameLabel:         purple,
		chroma.NameTag:           red,
		chroma.LiteralString:     green,
		chroma.LiteralNumber:     yellow,
		chroma.GenericDeleted:    red,
		chroma.GenericInserted:   green,
		chroma.GenericHeading:    "bold " + accent,
		chroma.GenericSubheading: "bold " + accent,
		chroma.GenericEmph:       "italic",
		chroma.GenericStrong:     "bold",
	})
	if err != nil {
		if t.Dark {
			return "gruvbox"
		}
		return "gruvbox-light"
	}
	styles.Register(style)
	return name
}

// codeSpansFor computes (or reuses) the token spans of every fenced and
// display-math block.
func (a *App) codeSpansFor(buf *noteBuffer, lines []string) map[int][]codeSpan {
	style, engine := a.theme.ChromaStyle, a.prefs.TerminalMath
	if buf.codeHL != nil && buf.codeHL.version == buf.ed.Version() && buf.codeHL.style == style && buf.codeHL.engine == engine {
		return buf.codeHL.lines
	}
	spans := highlightFences(lines, style, engine)
	buf.codeHL = &codeHighlights{version: buf.ed.Version(), style: style, engine: engine, lines: spans}
	return spans
}

// highlightFences finds ```lang blocks and display-math blocks and tokenizes
// their bodies. $$…$$ bodies are lexed as tex under either engine: $$ holds
// LaTeX on every ZenNotes surface (the desktop renders it with KaTeX), and
// chroma's typst lexer marks each of its backslashes as an error, in red.
// engine is prefs.TerminalMath; under "latex" block-level \[…\] and the
// display environments are lexed as tex too.
func highlightFences(lines []string, styleName, engine string) map[int][]codeSpan {
	out := map[int][]codeSpan{}
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	i := 0
	for i < len(lines) {
		if !fenceRe.MatchString(lines[i]) {
			i++
			continue
		}
		lang := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[i]), "`~"))
		if sp := strings.IndexAny(lang, " \t{"); sp >= 0 {
			lang = lang[:sp]
		}
		j := i + 1
		for j < len(lines) && !fenceRe.MatchString(lines[j]) {
			j++
		}
		if lang == "tikz" {
			// chroma has no tikz lexer; the fence body is TeX.
			lang = "tex"
		}
		if lang != "" && j > i+1 {
			highlightBlock(out, lines[i+1:j], i+1, lang, style)
		}
		i = j + 1
	}
	for _, b := range splitBlocks(lines, engine == "latex") {
		if b.kind != "math" && b.kind != "tex" {
			continue
		}
		// The opener is line b.start; the closer, when there is one, is b.end-1.
		// A one-line block (`$$ x $$`, `\[ x \]`) has no body and stays plain.
		end := b.end
		if end-b.start > 1 && (b.kind == "tex" || strings.HasSuffix(strings.TrimSpace(lines[end-1]), "$$")) {
			end--
		}
		if end > b.start+1 {
			highlightBlock(out, lines[b.start+1:end], b.start+1, "tex", style)
		}
	}
	return out
}

func highlightBlock(out map[int][]codeSpan, body []string, firstLine int, lang string, style *chroma.Style) {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return
	}
	lexer = chroma.Coalesce(lexer)
	iter, err := lexer.Tokenise(nil, strings.Join(body, "\n")+"\n")
	if err != nil {
		return
	}
	line := firstLine
	col := 0
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		entry := style.Get(tok.Type)
		fg := ""
		if entry.Colour.IsSet() {
			fg = entry.Colour.String()
		}
		for _, part := range strings.SplitAfter(tok.Value, "\n") {
			if part == "" {
				continue
			}
			text := strings.TrimSuffix(part, "\n")
			n := len([]rune(text))
			if n > 0 && (fg != "" || entry.Bold == chroma.Yes || entry.Italic == chroma.Yes) {
				out[line] = append(out[line], codeSpan{start: col, end: col + n, fg: fg, bold: entry.Bold == chroma.Yes, italic: entry.Italic == chroma.Yes})
			}
			col += n
			if strings.HasSuffix(part, "\n") {
				line++
				col = 0
			}
		}
	}
}

// codeSpanAt finds the span covering a column, if any.
func codeSpanAt(spans []codeSpan, col int) (codeSpan, bool) {
	for _, s := range spans {
		if col >= s.start && col < s.end {
			return s, true
		}
	}
	return codeSpan{}, false
}
