package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vim"
)

// previewState is the reading-mode state of a tab.
type previewState struct {
	scroll     int
	cursor     int
	followLine int
	cache      *previewCache
}

type previewCache struct {
	version int
	width   int
	style   string
	dark    bool
	lines   []previewLine
}

// previewLine is one rendered reading row and the source line it came from.
type previewLine struct {
	text    string
	srcLine int
	links   []linkTarget
	task    bool
}

type styledRune struct {
	r     rune
	class int
	link  int // index into links, -1 when none
}

var (
	tableRowRe   = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableSepRe   = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	calloutRe    = regexp.MustCompile(`^\s{0,3}>\s*\[!(\w+)\]\s*(.*)$`)
	embedWikiRe  = regexp.MustCompile(`!\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)
	embedImgRe   = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)[^)]*\)`)
	highlightRe  = regexp.MustCompile(`==[^=\n]+==`)
	mathInlineRe = regexp.MustCompile(`\$[^$\n]+\$`)
	orderedRe    = regexp.MustCompile(`^(\s*)(\d+)[.)]\s+(.*)$`)
	bulletRe     = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	taskLineRe   = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+\[([ xX>/\-])\]\s?(.*)$`)
)

// renderPreviewLines converts markdown to reading rows at a width.
func (a *App) renderPreviewLines(text string, width int) []previewLine {
	th := a.theme
	lines := strings.Split(text, "\n")
	out := []previewLine{}
	inFence := false
	inFront := len(lines) > 0 && lines[0] == "---"
	frontDone := false
	i := 0
	for i < len(lines) {
		line := lines[i]
		if inFront && !frontDone {
			if i > 0 && (line == "---" || line == "...") {
				frontDone = true
				inFront = false
				out = append(out, previewLine{text: th.Muted.Render(strings.Repeat("╌", min(width, 24))), srcLine: i})
				i++
				continue
			}
			if i > 0 {
				out = append(out, previewLine{text: th.Frontmatter.Render(truncateCells(line, width)), srcLine: i})
			}
			i++
			continue
		}
		if fenceRe.MatchString(line) {
			inFence = !inFence
			lang := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "`~"))
			if inFence && lang != "" {
				out = append(out, previewLine{text: th.Muted.Render("┌ " + lang), srcLine: i})
			} else {
				out = append(out, previewLine{text: th.Muted.Render(map[bool]string{true: "┌", false: "└"}[inFence]), srcLine: i})
			}
			i++
			continue
		}
		if inFence {
			for _, seg := range wrapLine([]rune(strings.ReplaceAll(line, "\t", "    ")), max(1, width-2)) {
				out = append(out, previewLine{text: th.Muted.Render("│ ") + th.Code.Render(string([]rune(strings.ReplaceAll(line, "\t", "    "))[seg.start:seg.end])), srcLine: i})
			}
			i++
			continue
		}
		if strings.TrimSpace(line) == "" {
			out = append(out, previewLine{text: "", srcLine: i})
			i++
			continue
		}
		if hrRe.MatchString(line) {
			out = append(out, previewLine{text: th.Muted.Render(strings.Repeat("─", width)), srcLine: i})
			i++
			continue
		}
		if m := headingRe.FindStringSubmatchIndex(line); m != nil {
			level := m[5] - m[4]
			content := strings.TrimSpace(line[m[5]:])
			prefix := ""
			style := th.Heading
			switch level {
			case 1:
				style = th.Heading.Underline(true)
			case 2:
				style = th.Heading
			default:
				style = th.Heading.Foreground(th.AccentSoft)
				prefix = strings.Repeat("·", level-2) + " "
			}
			rows := a.paragraph(content, width-cellWidth(prefix), func(class int) lipgloss.Style { return style }, prefix, "", i)
			out = append(out, rows...)
			i++
			continue
		}
		if tableRowRe.MatchString(line) {
			j := i
			block := []string{}
			for j < len(lines) && tableRowRe.MatchString(lines[j]) {
				block = append(block, lines[j])
				j++
			}
			out = append(out, a.renderTable(block, i, width)...)
			i = j
			continue
		}
		if m := calloutRe.FindStringSubmatch(line); m != nil {
			kind := strings.ToUpper(m[1])
			title := m[2]
			if title == "" {
				title = kind
			}
			out = append(out, previewLine{text: th.Quote.Foreground(th.Blue).Render("▍ ") + th.Bold.Foreground(th.Blue).Render(kind+" · "+title), srcLine: i})
			i++
			continue
		}
		if quoteRe.MatchString(line) {
			content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
			rows := a.paragraph(content, width-2, func(class int) lipgloss.Style { return th.Quote }, th.Quote.Render("▍ "), th.Quote.Render("▍ "), i)
			out = append(out, rows...)
			i++
			continue
		}
		if m := taskLineRe.FindStringSubmatch(line); m != nil {
			indent := strings.ReplaceAll(m[1], "\t", "  ")
			state := m[2]
			content := m[3]
			glyph := "☐"
			style := func(class int) lipgloss.Style { return a.styleFor(styleKey{class: class}) }
			switch state {
			case "x", "X":
				glyph = "☑"
				style = func(class int) lipgloss.Style { return th.Done }
			case "/":
				glyph = "◩"
			case ">":
				glyph = "→"
				style = func(class int) lipgloss.Style { return th.Dim }
			case "-":
				glyph = "✕"
				style = func(class int) lipgloss.Style { return th.Done }
			}
			prefix := indent + th.Checkbox.Render(glyph) + " "
			cont := indent + "  "
			rows := a.paragraph(content, width-cellWidth(indent)-2, style, prefix, cont, i)
			for r := range rows {
				rows[r].task = true
			}
			out = append(out, rows...)
			i++
			continue
		}
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			indent := strings.ReplaceAll(m[1], "\t", "  ")
			prefix := indent + th.Marker.Render("•") + " "
			rows := a.paragraph(m[2], width-cellWidth(indent)-2, nil, prefix, indent+"  ", i)
			out = append(out, rows...)
			i++
			continue
		}
		if m := orderedRe.FindStringSubmatch(line); m != nil {
			indent := strings.ReplaceAll(m[1], "\t", "  ")
			num := m[2] + "."
			prefix := indent + th.Marker.Render(num) + " "
			rows := a.paragraph(m[3], width-cellWidth(indent)-len(num)-1, nil, prefix, indent+strings.Repeat(" ", len(num)+1), i)
			out = append(out, rows...)
			i++
			continue
		}
		rows := a.paragraph(line, width, nil, "", "", i)
		out = append(out, rows...)
		i++
	}
	return out
}

// inlineRunes tokenizes inline markdown into styled runes, hiding syntax.
func (a *App) inlineRunes(text string) ([]styledRune, []linkTarget) {
	links := []linkTarget{}
	out := []styledRune{}
	emit := func(s string, class int, link int) {
		for _, r := range s {
			out = append(out, styledRune{r: r, class: class, link: link})
		}
	}
	// Embeds become placeholders first.
	text = embedWikiRe.ReplaceAllString(text, "⟦image: $1⟧")
	text = embedImgRe.ReplaceAllString(text, "⟦image: $1⟧")
	spans := []inlineSpan{}
	taken := make([]bool, len(text))
	add := func(m []int, class int, display string, link int) {
		for i := m[0]; i < m[1]; i++ {
			if taken[i] {
				return
			}
		}
		for i := m[0]; i < m[1]; i++ {
			taken[i] = true
		}
		spans = append(spans, inlineSpan{m[0], m[1], class, display, link})
	}
	for _, m := range inlineCodeRe.FindAllStringIndex(text, -1) {
		add(m, stCode, text[m[0]+1:m[1]-1], -1)
	}
	for _, m := range wikilinkAtRe.FindAllStringSubmatchIndex(text, -1) {
		content := text[m[2]:m[3]]
		target, _, alias := splitWiki(content)
		display := target
		if alias != "" {
			display = alias
		}
		links = append(links, linkTarget{kind: "wikilink", target: target, alias: alias})
		add(m[0:2], stLink, display, len(links)-1)
	}
	for _, m := range mdLinkAtRe.FindAllStringSubmatchIndex(text, -1) {
		label := text[m[2]:m[3]]
		target := text[m[4]:m[5]]
		kind := "path"
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			kind = "url"
		}
		links = append(links, linkTarget{kind: kind, target: target, alias: label})
		if label == "" {
			label = target
		}
		add(m[0:2], stLink, label, len(links)-1)
	}
	for _, m := range urlAtRe.FindAllStringIndex(text, -1) {
		links = append(links, linkTarget{kind: "url", target: text[m[0]:m[1]]})
		add(m, stLink, text[m[0]:m[1]], len(links)-1)
	}
	for _, m := range strongRe.FindAllStringIndex(text, -1) {
		add(m, stStrong, text[m[0]+2:m[1]-2], -1)
	}
	for _, m := range strikeRe.FindAllStringIndex(text, -1) {
		add(m, stStrike, text[m[0]+2:m[1]-2], -1)
	}
	for _, m := range highlightRe.FindAllStringIndex(text, -1) {
		add(m, stMeta, text[m[0]+2:m[1]-2], -1)
	}
	for _, m := range mathInlineRe.FindAllStringIndex(text, -1) {
		add(m, stCode, text[m[0]:m[1]], -1)
	}
	for _, m := range emphRe.FindAllStringIndex(text, -1) {
		start := m[0]
		if start < len(text) && text[start] != '*' && text[start] != '_' {
			start++
		}
		add([]int{start, m[1]}, stEmph, text[start+1:m[1]-1], -1)
	}
	for _, m := range tagRe.FindAllStringIndex(text, -1) {
		start := m[0]
		if start < len(text) && text[start] != '#' {
			start++
		}
		add([]int{start, m[1]}, stTag, text[start:m[1]], -1)
	}
	for _, m := range taskMetaRe.FindAllStringSubmatchIndex(text, -1) {
		add([]int{m[4], m[5]}, stMeta, text[m[4]:m[5]], -1)
	}
	sortSpans(spans)
	pos := 0
	for _, sp := range spans {
		if sp.start > pos {
			emit(text[pos:sp.start], stBase, -1)
		}
		emit(sp.text, sp.class, sp.link)
		pos = sp.end
	}
	if pos < len(text) {
		emit(text[pos:], stBase, -1)
	}
	return out, links
}

func splitWiki(content string) (target, heading, alias string) {
	if i := strings.Index(content, "|"); i >= 0 {
		alias = strings.TrimSpace(content[i+1:])
		content = content[:i]
	}
	if i := strings.Index(content, "#"); i >= 0 {
		heading = content[i+1:]
		content = content[:i]
	}
	return strings.TrimSpace(content), heading, alias
}

func sortSpans(spans []inlineSpan) {
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
}

// wrapStyled wraps styled runes to width and renders rows with prefixes.
func (a *App) wrapStyled(sr []styledRune, links []linkTarget, width int, override func(class int) lipgloss.Style, firstPrefix, contPrefix string, srcLine int) []previewLine {
	width = max(4, width)
	runes := make([]rune, len(sr))
	for i, s := range sr {
		runes[i] = s.r
	}
	segs := wrapLine(runes, width)
	out := []previewLine{}
	for si, seg := range segs {
		var b strings.Builder
		if si == 0 {
			b.WriteString(firstPrefix)
		} else {
			b.WriteString(contPrefix)
		}
		i := seg.start
		for i < seg.end {
			j := i
			for j < seg.end && sr[j].class == sr[i].class && sr[j].link == sr[i].link {
				j++
			}
			var style lipgloss.Style
			if override != nil {
				style = override(sr[i].class)
				if sr[i].class == stLink {
					style = style.Underline(true)
				}
			} else {
				style = a.styleFor(styleKey{class: sr[i].class})
			}
			b.WriteString(style.Render(string(runes[i:j])))
			i = j
		}
		rowLinks := []linkTarget{}
		seen := map[int]bool{}
		for i := seg.start; i < seg.end; i++ {
			if sr[i].link >= 0 && !seen[sr[i].link] {
				seen[sr[i].link] = true
				rowLinks = append(rowLinks, links[sr[i].link])
			}
		}
		out = append(out, previewLine{text: b.String(), srcLine: srcLine, links: rowLinks})
	}
	if len(segs) == 0 {
		out = append(out, previewLine{text: firstPrefix, srcLine: srcLine})
	}
	return out
}

// renderTable lays a pipe table out with aligned columns.
func (a *App) renderTable(block []string, srcLine, width int) []previewLine {
	th := a.theme
	rows := [][]string{}
	sepIdx := -1
	for i, line := range block {
		if tableSepRe.MatchString(line) {
			sepIdx = i
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for c := range cells {
			cells[c] = strings.TrimSpace(cells[c])
		}
		rows = append(rows, cells)
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			widths[c] = max(widths[c], cellWidth(stripInline(cell)))
		}
	}
	total := 0
	for _, w := range widths {
		total += w + 3
	}
	if total > width && cols > 0 {
		over := total - width
		for over > 0 {
			widest := 0
			for c := range widths {
				if widths[c] > widths[widest] {
					widest = c
				}
			}
			if widths[widest] <= 4 {
				break
			}
			widths[widest]--
			over--
		}
	}
	out := []previewLine{}
	for ri, r := range rows {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			sr, _ := a.inlineRunes(cell)
			runes := make([]rune, len(sr))
			for i, s := range sr {
				runes[i] = s.r
			}
			text := truncateCells(string(runes), widths[c])
			style := th.Base
			if ri == 0 && sepIdx >= 0 {
				style = th.Bold
			}
			b.WriteString(th.Muted.Render("│ "))
			b.WriteString(style.Render(padRight(text, widths[c])))
			b.WriteString(" ")
		}
		b.WriteString(th.Muted.Render("│"))
		line := srcLine + ri
		if sepIdx >= 0 && ri >= sepIdx {
			line++
		}
		out = append(out, previewLine{text: b.String(), srcLine: line})
		if ri == 0 && sepIdx >= 0 {
			segs := make([]string, cols)
			for c := 0; c < cols; c++ {
				segs[c] = strings.Repeat("─", widths[c]+2)
			}
			var s strings.Builder
			s.WriteString(th.Muted.Render("├" + strings.Join(segs, "┼") + "┤"))
			out = append(out, previewLine{text: s.String(), srcLine: srcLine + sepIdx})
		}
	}
	return out
}

func stripInline(s string) string {
	s = inlineCodeRe.ReplaceAllStringFunc(s, func(m string) string { return m[1 : len(m)-1] })
	s = strongRe.ReplaceAllStringFunc(s, func(m string) string { return m[2 : len(m)-2] })
	return s
}

// previewLinesFor returns the cached rows for a buffer at a width.
func (a *App) previewLinesFor(t *tab, buf *noteBuffer, width int) []previewLine {
	if t.preview == nil {
		t.preview = &previewState{}
	}
	pv := t.preview
	style := a.prefs.PreviewStyle
	if pv.cache == nil || pv.cache.version != buf.ed.Version() || pv.cache.width != width || pv.cache.style != style || pv.cache.dark != a.theme.Dark {
		var lines []previewLine
		a.previewPath = buf.path
		if strings.EqualFold(style, "zen") {
			lines = a.renderPreviewLines(buf.ed.Text(), width)
		} else {
			var err error
			lines, err = a.renderGlamourLinesFrom(buf.ed.Text(), width, buf.path)
			if err != nil {
				if !a.glamourWarned {
					a.glamourWarned = true
					a.notifyError(err.Error() + "; using the built-in renderer")
				}
				lines = a.renderPreviewLines(buf.ed.Text(), width)
			}
		}
		pv.cache = &previewCache{version: buf.ed.Version(), width: width, style: style, dark: a.theme.Dark, lines: lines}
	}
	return pv.cache.lines
}

// renderPreview draws reading mode.
func (a *App) renderPreview(t *tab, buf *noteBuffer, w, h int, focused bool) string {
	th := a.theme
	pad := 1
	width := max(10, w-2*pad)
	lines := a.previewLinesFor(t, buf, width)
	pv := t.preview
	if t.mode == modeSplit {
		// Follow the editor cursor.
		target := 0
		for i, l := range lines {
			if l.srcLine <= pv.followLine {
				target = i
			}
		}
		pv.cursor = target
		if target < pv.scroll || target >= pv.scroll+h {
			pv.scroll = max(0, target-h/3)
		}
	} else {
		if pv.cursor >= len(lines) {
			pv.cursor = max(0, len(lines)-1)
		}
		if pv.cursor < pv.scroll {
			pv.scroll = pv.cursor
		}
		if pv.cursor >= pv.scroll+h {
			pv.scroll = pv.cursor - h + 1
		}
	}
	if pv.scroll > max(0, len(lines)-1) {
		pv.scroll = max(0, len(lines)-1)
	}
	if pv.scroll < 0 {
		pv.scroll = 0
	}
	out := make([]string, 0, h)
	for i := pv.scroll; i < len(lines) && len(out) < h; i++ {
		row := strings.Repeat(" ", pad) + lines[i].text
		if focused && i == pv.cursor && t.mode != modeSplit {
			row = th.Selected.Render(padRight(strings.Repeat(" ", pad)+lines[i].text, w))
		}
		out = append(out, padRight(row, w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	if len(lines) > h {
		pct := fmt.Sprintf(" %d%% ", min(100, (pv.scroll+h)*100/max(1, len(lines))))
		out[h-1] = splice(out[h-1], max(0, w-len(pct)), th.Muted.Render(pct), w)
	}
	return strings.Join(out, "\n")
}

// previewKey handles reading-mode keys.
// previewHint is the help line for reading mode.
func (a *App) previewHint() string {
	return a.keysHint([]hintPair{{"key:Esc/i", "back to editor"}, {"nav.moveDown|nav.moveUp", "scroll"}, {"key:Enter", "follow link"}, {"nav.toggleTask", "toggle task"}, {"key:Space z s", "split"}, {"vim.leaderPrefix", "leader"}})
}

func (a *App) previewKey(t *tab, buf *noteBuffer, k vim.Key) {
	pv := t.preview
	if pv == nil {
		pv = &previewState{}
		t.preview = pv
	}
	lines := pv.cache
	n := 0
	if lines != nil {
		n = len(lines.lines)
	}
	rows := a.editorRows(buf)
	cur := listCursor{cursor: pv.cursor, scroll: pv.scroll}
	if a.listNav(&cur, k, n, rows) {
		pv.cursor, pv.scroll = cur.cursor, cur.scroll
		return
	}
	if k.Is("enter") {
		a.previewFollow(t, buf)
		return
	}
	if k.Is("esc") || k.IsCtrl('e') {
		if len(a.navKeys) > 0 {
			a.navKeys = nil
			return
		}
		a.setPaneMode(modeEdit)
		return
	}
	if !a.listKeyAllowed(k) {
		return
	}
	id, pending := a.resolveAction(k, "nav.toggleTask", "nav.openResult", "nav.localEx", "nav.filter", "vim.goToDefinition")
	if pending {
		return
	}
	switch id {
	case "nav.toggleTask":
		a.previewToggleTask(t, buf)
	case "nav.openResult", "vim.goToDefinition":
		a.previewFollow(t, buf)
	case "nav.localEx":
		if buf != nil {
			buf.ed.OpenCmdline(':', "")
			a.setPaneMode(modeEdit)
		}
	case "nav.filter":
		if buf != nil {
			a.setPaneMode(modeEdit)
			buf.ed.OpenCmdline('/', "")
		}
	default:
		switch {
		case k.IsRune('i') || k.IsRune('e') || k.IsRune('a') || k.IsRune('q'):
			if lines != nil && pv.cursor < n {
				buf.ed.GotoLine(lines.lines[pv.cursor].srcLine)
			}
			a.setPaneMode(modeEdit)
		case k.IsRune('y'):
			a.copyActiveNote()
		case k.IsRune('?'):
			a.openKeyHelp()
		}
	}
}

func (a *App) previewFollow(t *tab, buf *noteBuffer) {
	pv := t.preview
	if pv.cache == nil || pv.cursor >= len(pv.cache.lines) {
		return
	}
	row := pv.cache.lines[pv.cursor]
	if len(row.links) == 0 {
		a.notify("No link on this row")
		return
	}
	if len(row.links) == 1 {
		a.followLink(row.links[0])
		return
	}
	items := []menuItem{}
	for i, l := range row.links {
		link := l
		label := link.alias
		if label == "" {
			label = link.target
		}
		items = append(items, menuItem{key: fmt.Sprint(i + 1), label: label, run: func(a *App) { a.followLink(link) }})
	}
	a.showMenu("Follow link", items)
}

func (a *App) previewToggleTask(t *tab, buf *noteBuffer) {
	pv := t.preview
	if pv.cache == nil || pv.cursor >= len(pv.cache.lines) {
		return
	}
	row := pv.cache.lines[pv.cursor]
	if !row.task {
		return
	}
	cur := buf.ed.Cursor()
	buf.ed.SetCursor(vim.Pos{Line: row.srcLine, Col: 0})
	buf.ed.ToggleCheckboxAtCursor()
	buf.ed.SetCursor(cur)
}

func (a *App) previewHintTargets(p *pane, t *tab, buf *noteBuffer) []hintTarget {
	out := []hintTarget{}
	pv := t.preview
	if pv == nil || pv.cache == nil {
		return nil
	}
	y0 := a.contentRect(p).y
	for i := pv.scroll; i < len(pv.cache.lines) && i-pv.scroll < a.editorRows(buf); i++ {
		row := pv.cache.lines[i]
		if len(row.links) == 0 {
			continue
		}
		link := row.links[0]
		out = append(out, hintTarget{x: a.contentRect(p).x + 1, y: y0 + (i - pv.scroll), run: func(a *App) { a.followLink(link) }})
	}
	return out
}

// inlineSpan is a styled run found in a line, in byte offsets.
type inlineSpan struct {
	start, end int
	class      int
	text       string
	link       int
}

// paragraph tokenizes and wraps one block of inline markdown.
func (a *App) paragraph(content string, width int, override func(class int) lipgloss.Style, firstPrefix, contPrefix string, srcLine int) []previewLine {
	sr, links := a.inlineRunes(content)
	return a.wrapStyled(sr, links, width, override, firstPrefix, contPrefix, srcLine)
}

// renderPreviewLinesFrom renders a note body for embedding inside another
// preview, in the reading style the user chose.
func (a *App) renderPreviewLinesFrom(text string, width int, fromPath string) []previewLine {
	if strings.EqualFold(a.prefs.PreviewStyle, "zen") {
		return a.renderPreviewLines(text, width)
	}
	lines, err := a.renderGlamourLinesFrom(text, width, fromPath)
	if err != nil {
		return a.renderPreviewLines(text, width)
	}
	return lines
}
