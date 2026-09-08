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
	dark    bool
	lines   map[int][]codeSpan
}

// chromaStyleName matches the preview's code theme.
func chromaStyleName(dark bool) string {
	if dark {
		return "gruvbox"
	}
	return "gruvbox-light"
}

// codeSpansFor computes (or reuses) the token spans of every fenced block.
func (a *App) codeSpansFor(buf *noteBuffer, lines []string) map[int][]codeSpan {
	dark := a.theme.Dark
	if buf.codeHL != nil && buf.codeHL.version == buf.ed.Version() && buf.codeHL.dark == dark {
		return buf.codeHL.lines
	}
	spans := highlightFences(lines, chromaStyleName(dark))
	buf.codeHL = &codeHighlights{version: buf.ed.Version(), dark: dark, lines: spans}
	return spans
}

// highlightFences finds ```lang blocks and tokenizes their bodies.
func highlightFences(lines []string, styleName string) map[int][]codeSpan {
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
		if lang != "" && j > i+1 {
			highlightBlock(out, lines[i+1:j], i+1, lang, style)
		}
		i = j + 1
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
