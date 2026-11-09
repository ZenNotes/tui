package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/ZenNotes/tui/internal/termsafe"
)

// The reading view can render through Glamour, the renderer behind Glow.
// Every Markdown block is rendered on its own so each screen row still
// knows the source line it came from, which the split mode, task toggling
// and link following depend on.

// glamourState caches one renderer per style and width.
type glamourState struct {
	style string
	width int
	theme string
	r     *glamour.TermRenderer
	err   error
}

// glamourStyleNames lists the built-in styles, for completion and help.
var glamourStyleNames = []string{"auto", "zennotes", "dark", "light", "dracula", "tokyo-night", "pink", "ascii", "notty"}

// resolveGlamourStyle turns a name or JSON path into a style config with
// the document margin removed, since the pane already pads the text.
func resolveGlamourStyle(name string, th Theme) (ansi.StyleConfig, error) {
	var cfg ansi.StyleConfig
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "", "auto", "system", "zennotes":
		cfg = zennotesStyle(th)
	default:
		if builtin, ok := styles.DefaultStyles[key]; ok {
			cfg = *builtin
			break
		}
		path := name
		if strings.HasPrefix(path, "~/") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[2:])
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("preview style %q is not a built-in style or a readable JSON file", name)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("preview style %s: %w", path, err)
		}
	}
	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	return cfg, nil
}

// glamourRenderer returns the cached renderer for the active style.
func (a *App) glamourRenderer(width int) (*glamour.TermRenderer, error) {
	style := a.prefs.PreviewStyle
	if a.glamour != nil && a.glamour.style == style && a.glamour.width == width && a.glamour.theme == a.theme.ID {
		return a.glamour.r, a.glamour.err
	}
	st := &glamourState{style: style, width: width, theme: a.theme.ID}
	cfg, err := resolveGlamourStyle(style, a.theme)
	if err != nil {
		st.err = err
	} else {
		st.r, st.err = glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width), glamour.WithPreservedNewLines())
	}
	a.glamour = st
	return st.r, st.err
}

// mdBlock is one renderable slice of a note.
type mdBlock struct {
	kind  string // front, fence, math, tex, table, list, quote, heading, hr, para, blank
	start int    // first source line
	end   int    // one past the last source line
	// listChild marks a block indented under a list item that it had to leave,
	// such as an embed's card, so its indent does not read as code.
	listChild bool
}

// itemMarkerPattern is how a rendered list item starts.
const itemMarkerPattern = `(?:[•◦▪\-*+]|\d+\.|\[[ xX✓✔]\]|☐|☑)`

var (
	listLineRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	blankRe       = regexp.MustCompile(`^\s*$`)
	wikiTokenRe   = regexp.MustCompile(`⟦([^⟧]*)⟧`)
	renderedTagRe = regexp.MustCompile(`(^|[\s(])(#[\p{L}\p{N}_][\p{L}\p{N}_/-]*)`)
	renderedMeta  = regexp.MustCompile(`(^|\s)(due:\S+|!(?:high|med|low)\b|@[\w-]+(?::\S+)?|scheduled:\S+)`)
	sgrRe         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	taskStateRe   = regexp.MustCompile(`(?m)^(\s*)([-*+]|\d+[.)])\s+\[([/>\-])\]\s?(.*)$`)
	calloutLineRe = regexp.MustCompile(`(?m)^\s{0,3}>\s*\[!(\w+)\]\s*(.*)$`)
	itemMarkerRe  = regexp.MustCompile(`^\s*` + itemMarkerPattern + `\s`)
	texOpenRe     = regexp.MustCompile(`^ {0,3}\\\[\s*$`)
	texLineRe     = regexp.MustCompile(`^ {0,3}\\\[.*\\\]\s*$`)
	texEnvRe      = regexp.MustCompile(`^ {0,3}\\begin\{((?:equation|align|gather|multline|alignat|flalign|eqnarray)\*?|tikzpicture)\}`)
)

// texBlockEnd returns one past the last line of a block-level \[…\] or
// display environment (texEnvRe) that starts at line i, or -1 when none
// starts there or it is never closed, so an unclosed opener stays text.
func texBlockEnd(lines []string, i int) int {
	if texLineRe.MatchString(lines[i]) {
		return i + 1
	}
	if texOpenRe.MatchString(lines[i]) {
		for j := i + 1; j < len(lines); j++ {
			if strings.HasSuffix(strings.TrimSpace(lines[j]), `\]`) {
				return j + 1
			}
		}
		return -1
	}
	m := texEnvRe.FindStringSubmatch(lines[i])
	if m == nil {
		return -1
	}
	end := `\end{` + m[1] + `}`
	for j := i; j < len(lines); j++ {
		if strings.Contains(lines[j], end) {
			return j + 1
		}
	}
	return -1
}

// splitBlocks walks the source lines and groups them into blocks. With
// latex, block-level \[…\] and the display environments become "tex" blocks.
func splitBlocks(lines []string, latex bool) []mdBlock {
	blocks := []mdBlock{}
	i := 0
	if len(lines) > 0 && lines[0] == "---" {
		j := 1
		for j < len(lines) && lines[j] != "---" && lines[j] != "..." {
			j++
		}
		if j < len(lines) {
			blocks = append(blocks, mdBlock{kind: "front", start: 0, end: j + 1})
			i = j + 1
		}
	}
	for i < len(lines) {
		line := lines[i]
		switch {
		case blankRe.MatchString(line):
			j := i
			for j < len(lines) && blankRe.MatchString(lines[j]) {
				j++
			}
			blocks = append(blocks, mdBlock{kind: "blank", start: i, end: j})
			i = j
		case isFenceOpen(line):
			char, n, _ := fenceOpens(line)
			j := i + 1
			for j < len(lines) && !fenceCloses(lines[j], char, n) {
				j++
			}
			if j < len(lines) {
				j++
			}
			blocks = append(blocks, mdBlock{kind: "fence", start: i, end: j})
			i = j
		case mathInlineOpen.MatchString(line):
			blocks = append(blocks, mdBlock{kind: "math", start: i, end: i + 1})
			i++
		case mathFenceRe.MatchString(line):
			j := i + 1
			for j < len(lines) && !strings.HasSuffix(strings.TrimSpace(lines[j]), "$$") {
				j++
			}
			if j < len(lines) {
				j++
			}
			blocks = append(blocks, mdBlock{kind: "math", start: i, end: j})
			i = j
		case latex && texBlockEnd(lines, i) > 0:
			j := texBlockEnd(lines, i)
			blocks = append(blocks, mdBlock{kind: "tex", start: i, end: j})
			i = j
		case tableRowRe.MatchString(line):
			j := i
			for j < len(lines) && tableRowRe.MatchString(lines[j]) {
				j++
			}
			blocks = append(blocks, mdBlock{kind: "table", start: i, end: j})
			i = j
		case hrRe.MatchString(line):
			blocks = append(blocks, mdBlock{kind: "hr", start: i, end: i + 1})
			i++
		case headingRe.MatchString(line):
			blocks = append(blocks, mdBlock{kind: "heading", start: i, end: i + 1})
			i++
		case quoteRe.MatchString(line):
			j := i
			for j < len(lines) && quoteRe.MatchString(lines[j]) {
				j++
			}
			blocks = append(blocks, mdBlock{kind: "quote", start: i, end: j})
			i = j
		case listLineRe.MatchString(line):
			j := listBlockEnd(lines, i, latex)
			blocks = append(blocks, mdBlock{kind: "list", start: i, end: j})
			i = j
		default:
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if blankRe.MatchString(next) || isFenceOpen(next) || headingRe.MatchString(next) || tableRowRe.MatchString(next) || quoteRe.MatchString(next) || listLineRe.MatchString(next) || hrRe.MatchString(next) || mathFenceRe.MatchString(next) || mathInlineOpen.MatchString(next) || latex && texBlockEnd(lines, j) > 0 {
					break
				}
				j++
			}
			blocks = append(blocks, mdBlock{kind: "para", start: i, end: j})
			i = j
		}
	}
	markListChildren(blocks, lines)
	return blocks
}

// The note's own token brackets and hard spaces are hidden while a block goes
// through Glamour, so only zn's tokens are styled or removed.
const (
	literalTokenOpen  = "\U0010FFF0"
	literalTokenClose = "\U0010FFF1"
	literalHardSpace  = "\U0010FFF2"
)

var (
	tokenHider    = strings.NewReplacer("⟦", literalTokenOpen, "⟧", literalTokenClose, "\u00a0", literalHardSpace)
	tokenFinisher = strings.NewReplacer("⟦", "", "⟧", "", literalTokenOpen, "⟦", literalTokenClose, "⟧", literalHardSpace, "\u00a0")
	tableRuleRe   = regexp.MustCompile(`[│|┼]`)
)

// prepareMarkdown rewrites the pieces Glamour does not know: wikilinks and
// embeds become tokens that are styled after rendering, callout markers
// become bold labels. Code is guarded first, so it stays as written; the
// result still carries guards that postStyleRow removes.
func prepareMarkdown(text string) string {
	text = guardCode(text)
	text = tokenHider.Replace(text)
	// A placeholder is a styled token that shows its brackets, as in the zen style:
	// the token's own go, and these are read as the note's own.
	placeholder := "⟦" + literalTokenOpen + "image: ${1}" + literalTokenClose + "⟧"
	text = embedWikiRe.ReplaceAllString(text, placeholder)
	text = embedImgRe.ReplaceAllString(text, placeholder)
	text = wikilinkAtRe.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "[["), "]]")
		if wikilinkTargetOf(strings.TrimPrefix(inner, "!")).target == "" {
			return m
		}
		target, _, alias := splitWiki(inner)
		if alias == "" {
			alias = target
		}
		// Hard spaces keep the token on one row where it fits; postStyleRow
		// puts the spaces back.
		return "⟦" + strings.ReplaceAll(alias, " ", "\u00a0") + "⟧"
	})
	// Glamour only knows [ ] and [x]; the app's other states become glyphs.
	text = taskStateRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := taskStateRe.FindStringSubmatch(m)
		switch sub[3] {
		case "/":
			return sub[1] + sub[2] + " ◩ " + sub[4]
		case ">":
			return sub[1] + sub[2] + " → " + sub[4]
		default:
			return sub[1] + sub[2] + " ✕ ~~" + strings.TrimSpace(sub[4]) + "~~"
		}
	})
	text = calloutLineRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := calloutLineRe.FindStringSubmatch(m)
		title := strings.TrimSpace(sub[2])
		label := "**" + strings.ToUpper(sub[1]) + "**"
		if title != "" {
			label += " " + title
		}
		return "> " + label
	})
	return unguardBracket.Replace(text)
}

// activeSGR returns the color and attribute codes in force at the end of
// an ANSI string, so a styled insertion can restore them afterwards.
func activeSGR(prefix string) string {
	fg, bg := "", ""
	attrs := map[string]bool{}
	for _, code := range sgrRe.FindAllString(prefix, -1) {
		params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(code, "\x1b["), "m"), ";")
		for i := 0; i < len(params); i++ {
			p := params[i]
			switch {
			case p == "" || p == "0":
				fg, bg = "", ""
				attrs = map[string]bool{}
			case p == "1" || p == "3" || p == "4" || p == "9":
				attrs[p] = true
			case p == "22":
				delete(attrs, "1")
			case p == "23":
				delete(attrs, "3")
			case p == "24":
				delete(attrs, "4")
			case p == "29":
				delete(attrs, "9")
			case p == "39":
				fg = ""
			case p == "49":
				bg = ""
			case p == "38" || p == "48":
				n := 0
				if i+1 < len(params) && params[i+1] == "5" {
					n = 2
				} else if i+1 < len(params) && params[i+1] == "2" {
					n = 4
				}
				if i+n < len(params) {
					seq := strings.Join(params[i:i+n+1], ";")
					if p == "38" {
						fg = seq
					} else {
						bg = seq
					}
					i += n
				}
			default:
				if len(p) == 2 && (p[0] == '3' || p[0] == '9') {
					fg = p
				} else if len(p) == 2 && (p[0] == '4' || p[0] == '1') {
					bg = p
				}
			}
		}
	}
	parts := []string{}
	if fg != "" {
		parts = append(parts, fg)
	}
	if bg != "" {
		parts = append(parts, bg)
	}
	for _, k := range []string{"1", "3", "4", "9"} {
		if attrs[k] {
			parts = append(parts, k)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// styleInline replaces a token inside a rendered row with a styled version
// and restores the surrounding style after it.
func styleInline(row string, re *regexp.Regexp, group int, style lipgloss.Style, keepMatch bool) string {
	out := strings.Builder{}
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(row, -1) {
		s, e := m[2*group], m[2*group+1]
		if s < 0 {
			continue
		}
		out.WriteString(row[last:s])
		text := row[s:e]
		if !keepMatch {
			// Drop the token brackets, keep the inner text.
			text = row[m[2*group+2]:m[2*group+3]]
		}
		// Glamour styles word by word, so a multi-word token carries escape
		// codes; the style renders rune by rune and would split them.
		out.WriteString("\x1b[0m" + style.Render(xansi.Strip(text)) + activeSGR(row[:s]))
		last = e
	}
	out.WriteString(row[last:])
	return out.String()
}

// postStyleRow colors wikilink tokens, tags and task metadata in a row.
func (a *App) postStyleRow(row string) string {
	th := a.theme
	// In-progress, forwarded and cancelled tasks were rewritten as plain
	// list items carrying a glyph; drop the bullet so they line up with
	// the real checkboxes.
	for _, g := range []string{"◩", "→", "✕"} {
		row = strings.Replace(row, "• "+g+" ", g+" ", 1)
	}
	if strings.Contains(row, "⟦") {
		row = styleInline(row, regexp.MustCompile(`(⟦([^⟧]*)⟧)`), 1, th.Link, false)
	}
	if strings.Contains(row, "#") {
		row = styleInline(row, regexp.MustCompile(`(?:^|[\s(])(#[\p{L}\p{N}_][\p{L}\p{N}_/-]*)`), 1, th.Tag, true)
	}
	if strings.Contains(row, "due:") || strings.Contains(row, "!") || strings.Contains(row, "@") || strings.Contains(row, "scheduled:") {
		row = styleInline(row, regexp.MustCompile(`(?:^|\s)(due:\S+|!(?:high|med|low)\b|@[\w-]+(?::\S+)?|scheduled:\S+)`), 1, lipgloss.NewStyle().Foreground(th.Purple), true)
	}
	return strings.ReplaceAll(row, "\u00a0", " ")
}

// isGlamourTable says a block renders as a table: one with outer pipes, or a
// paragraph whose second line is a separator row, which is one without them.
func isGlamourTable(b mdBlock, lines []string) bool {
	return b.kind == "table" || b.kind == "para" && b.end-b.start >= 2 && strings.Contains(lines[b.start+1], "|") && tableSepRe.MatchString(lines[b.start+1])
}

// postStyleTableRow is postStyleRow for a table row, where a column rule must
// not move. Glamour measured each cell with the token's brackets in it, so a
// cell gets those cells back at its end. A token too long for its cell wraps
// over rows and its brackets go the same way.
func (a *App) postStyleTableRow(row string) string {
	var b strings.Builder
	pad := func(cell string) {
		b.WriteString(cell)
		b.WriteString(strings.Repeat(" ", strings.Count(cell, "⟦")+strings.Count(cell, "⟧")))
	}
	tokens := wikiTokenRe.FindAllStringIndex(row, -1)
	inToken := func(i int) bool {
		for _, t := range tokens {
			if t[0] <= i && i < t[1] {
				return true // an alias may hold a pipe of its own
			}
		}
		return false
	}
	last := 0
	for _, rule := range tableRuleRe.FindAllStringIndex(row, -1) {
		if inToken(rule[0]) {
			continue
		}
		pad(row[last:rule[0]])
		b.WriteString(row[rule[0]:rule[1]])
		last = rule[1]
	}
	pad(row[last:])
	return finishTokens(a.postStyleRow(b.String()))
}

// finishTokens removes the brackets of a token that wrapped over rows and
// gives back what the note itself held.
func finishTokens(row string) string {
	return tokenFinisher.Replace(row)
}

// breakLongWords puts a space into every word over 2 KB. Glamour's word wrap is
// quadratic in a word's length, and a very long word would stall the reading
// view. No such word fits a pane, so it wraps either way, and lines stay lines.
func breakLongWords(text string) string {
	const limit, chunk = 2048, 1024
	run := 0
	for i := 0; i < len(text); i++ {
		if text[i] == ' ' || text[i] == '\n' || text[i] == '\t' {
			run = 0
		} else if run++; run > limit {
			break
		}
	}
	if run <= limit {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/chunk)
	run = 0
	for i, r := range text {
		if r == ' ' || r == '\n' || r == '\t' {
			run = 0
		} else if run >= chunk && len(text)-i > limit-chunk {
			b.WriteByte(' ')
			run = 0
		}
		b.WriteRune(r)
		run += utf8.RuneLen(r)
	}
	return b.String()
}

// renderGlamourLines renders a note block by block.
func (a *App) renderGlamourLines(text string, width int) ([]previewLine, error) {
	return a.renderGlamourLinesFrom(text, width, a.previewPath)
}

// renderGlamourLinesFrom renders with the note path known, so relative
// embeds resolve and a note does not transclude itself.
func (a *App) renderGlamourLinesFrom(text string, width int, fromPath string) ([]previewLine, error) {
	r, err := a.glamourRenderer(width)
	if err != nil {
		return nil, err
	}
	th := a.theme
	lines := strings.Split(breakLongWords(termsafe.Visible(text)), "\n")
	out := []previewLine{}
	for _, b := range splitBlocks(lines, a.prefs.TerminalMath == "latex") {
		embedded, ok := a.embedBlockLines(b, lines, width, fromPath)
		out = append(out, embedded...)
		if ok {
			continue
		}
		switch b.kind {
		case "front":
			for i := b.start + 1; i < b.end-1; i++ {
				out = append(out, previewLine{text: th.Frontmatter.Render(truncateCells(lines[i], width)), srcLine: i})
			}
			out = append(out, previewLine{text: th.Muted.Render(strings.Repeat("╌", min(width, 24))), srcLine: b.end - 1})
		case "blank":
			out = append(out, previewLine{text: "", srcLine: b.start})
		default:
			src := prepareMarkdown(blockSource(b, lines))
			if isGlamourTable(b, lines) {
				// A narrow column has to wrap a long alias between its words.
				src = strings.ReplaceAll(src, "\u00a0", " ")
			}
			rendered, err := r.Render(src)
			if err != nil {
				return nil, err
			}
			rows := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
			for len(rows) > 0 && strings.TrimSpace(xansi.Strip(rows[0])) == "" {
				rows = rows[1:]
			}
			for len(rows) > 0 && strings.TrimSpace(xansi.Strip(rows[len(rows)-1])) == "" {
				rows = rows[:len(rows)-1]
			}
			out = append(out, a.mapRows(b, lines, rows)...)
		}
	}
	return out, nil
}

// mapRows assigns source lines to the rendered rows of a block. Lists get
// per-item mapping by spotting item markers in the output; everything else
// maps to the block's first line. Code holds no items or links, and its guards
// come off only once every step has passed over a row.
func (a *App) mapRows(b mdBlock, lines []string, rows []string) []previewLine {
	out := make([]previewLine, 0, len(rows))
	source := blockSource(b, lines)
	mask, codeOnly := markdownCode(source)
	srcLines := strings.Split(source, "\n")
	offsets := make([]int, len(srcLines))
	for i, off := 0, 0; i < len(srcLines); i++ {
		offsets[i] = off
		off += len(srcLines[i]) + 1
	}
	linksOn := func(i int) []linkTarget {
		off := offsets[i-b.start]
		return linksOnLineWhere(srcLines[i-b.start], func(s, e int) bool {
			return mask[off+s] == notCode && mask[off+e-1] == notCode
		})
	}
	finish := func(row string) string {
		switch {
		case codeOnly:
			row = finishTokens(row)
		case isGlamourTable(b, lines):
			row = a.postStyleTableRow(row)
		default:
			row = finishTokens(a.postStyleRow(row))
		}
		return unguardRow.Replace(row)
	}
	if b.kind != "list" {
		links := []linkTarget{}
		for i := b.start; i < b.end; i++ {
			links = append(links, linksOn(i)...)
		}
		for i, row := range rows {
			pl := previewLine{text: finish(row), srcLine: b.start}
			if i == 0 {
				pl.links = links
			}
			out = append(out, pl)
		}
		return out
	}
	items := []int{}
	for i := b.start; i < b.end; i++ {
		line := srcLines[i-b.start]
		first := offsets[i-b.start] + len(line) - len(strings.TrimLeft(line, " \t"))
		if listLineRe.MatchString(line) && (first >= len(mask) || mask[first] != blockCode) {
			items = append(items, i)
		}
	}
	// A row belongs to the last item marker or paragraph line it reached. Its links
	// run up to the next line that gets a row of its own.
	paragraphs := []int{}
	starts := map[int]bool{}
	for _, i := range items {
		starts[i] = true
	}
	for i := b.start; i < b.end; i++ {
		if strings.Contains(srcLines[i-b.start], guardLine) {
			paragraphs = append(paragraphs, i)
			starts[i] = true
		}
	}
	k, p, src := -1, 0, b.start
	for _, row := range rows {
		if itemMarkerRe.MatchString(xansi.Strip(row)) && k+1 < len(items) {
			k++
			src = items[k]
		}
		for n := strings.Count(row, guardLine); n > 0 && p < len(paragraphs); n-- {
			src = paragraphs[p]
			p++
		}
		row = strings.ReplaceAll(row, guardLine, "")
		pl := previewLine{text: finish(row), srcLine: src}
		if k >= 0 {
			pl.task = taskLineRe.MatchString(lines[items[k]])
			pl.taskBack = src - items[k]
			if len(out) == 0 || out[len(out)-1].srcLine != src {
				for i := src; i < b.end && (i == src || !starts[i]); i++ {
					pl.links = append(pl.links, linksOn(i)...)
				}
			}
		}
		out = append(out, pl)
	}
	return out
}

// setPreviewStyle switches the reading view's style for this session.
func (a *App) setPreviewStyle(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "auto"
	}
	if !strings.EqualFold(name, "zen") {
		if _, err := resolveGlamourStyle(name, a.theme); err != nil {
			return err
		}
	}
	a.prefs.PreviewStyle = name
	a.glamour = nil
	a.invalidatePreviews()
	return nil
}

// invalidatePreviews drops every cached reading view.
func (a *App) invalidatePreviews() {
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if t.preview != nil {
				t.preview.cache = nil
			}
		}
	}
}

// zennotesStyle is the reading-view style that matches the interface
// palette: accent headings, dim quotes, the task and tag colors of the
// editor, code in the theme's own syntax colors. It starts from Glamour's
// dark or light style for everything the palette has no opinion on.
func zennotesStyle(th Theme) ansi.StyleConfig {
	cfg := styles.LightStyleConfig
	if th.Dark {
		cfg = styles.DarkStyleConfig
	}
	str := func(v string) *string { return &v }
	b := func(v bool) *bool { return &v }
	u := func(v uint) *uint { return &v }
	fg := string(th.Fg)
	cfg.Document.StylePrimitive.Color = str(fg)
	cfg.Text.Color = str(fg)
	cfg.Heading.StylePrimitive.Color = str(string(th.Accent))
	cfg.Heading.StylePrimitive.Bold = b(true)
	cfg.H1.StylePrimitive = ansi.StylePrimitive{Prefix: "▌ ", Color: str(string(th.Accent)), Bold: b(true)}
	cfg.H2.StylePrimitive = ansi.StylePrimitive{Prefix: "▌ ", Color: str(string(th.Accent)), Bold: b(true)}
	cfg.H3.StylePrimitive = ansi.StylePrimitive{Prefix: "▎ ", Color: str(string(th.AccentSoft)), Bold: b(true)}
	cfg.H4.StylePrimitive = ansi.StylePrimitive{Prefix: "▎ ", Color: str(string(th.AccentSoft)), Bold: b(true)}
	cfg.H5.StylePrimitive = ansi.StylePrimitive{Prefix: "▎ ", Color: str(string(th.AccentSoft))}
	cfg.H6.StylePrimitive = ansi.StylePrimitive{Prefix: "▎ ", Color: str(string(th.FgDim))}
	cfg.BlockQuote.StylePrimitive.Color = str(string(th.FgDim))
	cfg.BlockQuote.StylePrimitive.Italic = b(true)
	cfg.BlockQuote.Indent = u(1)
	cfg.BlockQuote.IndentToken = str("▍ ")
	cfg.Item.BlockPrefix = "• "
	cfg.Item.Color = str(fg)
	cfg.Enumeration.BlockPrefix = ". "
	cfg.Enumeration.Color = str(string(th.Yellow))
	cfg.Task.Ticked = "☑ "
	cfg.Task.Unticked = "☐ "
	cfg.Task.StylePrimitive.Color = str(string(th.Purple))
	cfg.Link.Color = str(string(th.Blue))
	cfg.Link.Underline = b(true)
	cfg.LinkText.Color = str(string(th.Blue))
	cfg.LinkText.Bold = b(false)
	cfg.Image.Color = str(string(th.FgDim))
	cfg.ImageText.Color = str(string(th.FgDim))
	cfg.Code.StylePrimitive.Color = str(string(th.Green))
	cfg.Code.StylePrimitive.BackgroundColor = nil
	// The base style pads inline code to fill out its background; with none
	// the padding is only stray spaces.
	cfg.Code.StylePrimitive.Prefix = ""
	cfg.Code.StylePrimitive.Suffix = ""
	cfg.CodeBlock.Theme = th.ChromaStyle
	cfg.CodeBlock.Chroma = nil
	cfg.CodeBlock.Margin = u(2)
	cfg.CodeBlock.StyleBlock.StylePrimitive.Color = str(string(th.Green))
	cfg.HorizontalRule.Color = str(string(th.FgMuted))
	cfg.HorizontalRule.Format = "\n──────────\n"
	cfg.Strong.Bold = b(true)
	cfg.Strong.Color = str(fg)
	cfg.Emph.Italic = b(true)
	cfg.Strikethrough.CrossedOut = b(true)
	cfg.Table.StylePrimitive.Color = str(fg)
	cfg.Table.CenterSeparator = str("┼")
	cfg.Table.ColumnSeparator = str("│")
	cfg.Table.RowSeparator = str("─")
	return cfg
}
