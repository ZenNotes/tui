package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	xansi "github.com/charmbracelet/x/ansi"
)

// The reading view can render through Glamour, the renderer behind Glow.
// Every Markdown block is rendered on its own so each screen row still
// knows the source line it came from, which the split mode, task toggling
// and link following depend on.

// glamourState caches one renderer per style and width.
type glamourState struct {
	style string
	width int
	dark  bool
	r     *glamour.TermRenderer
	err   error
}

// glamourStyleNames lists the built-in styles, for completion and help.
var glamourStyleNames = []string{"auto", "zennotes", "dark", "light", "dracula", "tokyo-night", "pink", "ascii", "notty"}

// resolveGlamourStyle turns a name or JSON path into a style config with
// the document margin removed, since the pane already pads the text.
func resolveGlamourStyle(name string, dark bool) (ansi.StyleConfig, error) {
	var cfg ansi.StyleConfig
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "", "auto", "system", "zennotes":
		cfg = zennotesStyle(dark)
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
	if a.glamour != nil && a.glamour.style == style && a.glamour.width == width && a.glamour.dark == a.theme.Dark {
		return a.glamour.r, a.glamour.err
	}
	st := &glamourState{style: style, width: width, dark: a.theme.Dark}
	cfg, err := resolveGlamourStyle(style, a.theme.Dark)
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
	kind  string // front, fence, table, list, quote, heading, hr, para, blank
	start int    // first source line
	end   int    // one past the last source line
}

var (
	listLineRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	blankRe       = regexp.MustCompile(`^\s*$`)
	wikiTokenRe   = regexp.MustCompile(`⟦([^⟧]*)⟧`)
	renderedTagRe = regexp.MustCompile(`(^|[\s(])(#[\p{L}\p{N}_][\p{L}\p{N}_/-]*)`)
	renderedMeta  = regexp.MustCompile(`(^|\s)(due:\S+|!(?:high|med|low)\b|@[\w-]+(?::\S+)?|scheduled:\S+)`)
	sgrRe         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	taskStateRe   = regexp.MustCompile(`(?m)^(\s*)([-*+]|\d+[.)])\s+\[([/>\-])\]\s?(.*)$`)
	calloutLineRe = regexp.MustCompile(`(?m)^\s{0,3}>\s*\[!(\w+)\]\s*(.*)$`)
	itemMarkerRe  = regexp.MustCompile(`^\s*(?:[•◦▪\-*+]|\d+\.|\[[ xX✓✔]\]|☐|☑)\s`)
)

// splitBlocks walks the source lines and groups them into blocks.
func splitBlocks(lines []string) []mdBlock {
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
		case fenceRe.MatchString(line):
			j := i + 1
			for j < len(lines) && !fenceRe.MatchString(lines[j]) {
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
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if listLineRe.MatchString(next) || (strings.HasPrefix(next, "  ") || strings.HasPrefix(next, "\t")) && !blankRe.MatchString(next) {
					j++
					continue
				}
				break
			}
			blocks = append(blocks, mdBlock{kind: "list", start: i, end: j})
			i = j
		default:
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				if blankRe.MatchString(next) || fenceRe.MatchString(next) || headingRe.MatchString(next) || tableRowRe.MatchString(next) || quoteRe.MatchString(next) || listLineRe.MatchString(next) || hrRe.MatchString(next) || mathFenceRe.MatchString(next) || mathInlineOpen.MatchString(next) {
					break
				}
				j++
			}
			blocks = append(blocks, mdBlock{kind: "para", start: i, end: j})
			i = j
		}
	}
	return blocks
}

// prepareMarkdown rewrites the pieces Glamour does not know: wikilinks and
// embeds become tokens that are styled after rendering, callout markers
// become bold labels.
func prepareMarkdown(text string) string {
	text = embedWikiRe.ReplaceAllString(text, "⟦image: $1⟧")
	text = embedImgRe.ReplaceAllString(text, "⟦image: $1⟧")
	text = wikilinkAtRe.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "[["), "]]")
		target, _, alias := splitWiki(inner)
		if alias != "" {
			return "⟦" + alias + "⟧"
		}
		return "⟦" + target + "⟧"
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
	return text
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
		out.WriteString("\x1b[0m" + style.Render(text) + activeSGR(row[:s]))
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
	return row
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
	lines := strings.Split(text, "\n")
	out := []previewLine{}
	for _, b := range splitBlocks(lines) {
		if embedded, ok := a.embedBlockLines(b, lines, width, fromPath); ok {
			out = append(out, embedded...)
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
			src := strings.Join(lines[b.start:b.end], "\n")
			rendered, err := r.Render(prepareMarkdown(src))
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
// maps to the block's first line.
func (a *App) mapRows(b mdBlock, lines []string, rows []string) []previewLine {
	out := make([]previewLine, 0, len(rows))
	if b.kind != "list" {
		links := []linkTarget{}
		for i := b.start; i < b.end; i++ {
			links = append(links, linksOnLine(lines[i])...)
		}
		for i, row := range rows {
			pl := previewLine{text: a.postStyleRow(row), srcLine: b.start}
			if i == 0 {
				pl.links = links
			}
			out = append(out, pl)
		}
		return out
	}
	items := []int{}
	for i := b.start; i < b.end; i++ {
		if listLineRe.MatchString(lines[i]) {
			items = append(items, i)
		}
	}
	k := -1
	for _, row := range rows {
		plain := xansi.Strip(row)
		if itemMarkerRe.MatchString(plain) && k+1 < len(items) {
			k++
		}
		src := b.start
		if k >= 0 {
			src = items[k]
		}
		pl := previewLine{text: a.postStyleRow(row), srcLine: src}
		if k >= 0 && (len(out) == 0 || out[len(out)-1].srcLine != src) {
			pl.links = linksOnLine(lines[src])
			pl.task = taskLineRe.MatchString(lines[src])
		} else if k >= 0 {
			pl.task = taskLineRe.MatchString(lines[src])
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
		if _, err := resolveGlamourStyle(name, a.theme.Dark); err != nil {
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
// editor. It starts from Glamour's dark or light style so code blocks
// keep a sensible chroma theme.
func zennotesStyle(dark bool) ansi.StyleConfig {
	cfg := styles.LightStyleConfig
	th := buildTheme(dark)
	chroma := "gruvbox-light"
	if dark {
		cfg = styles.DarkStyleConfig
		chroma = "gruvbox"
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
	cfg.CodeBlock.Theme = chroma
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
