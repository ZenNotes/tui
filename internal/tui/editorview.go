package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vim"
)

// Style classes for editor runes.
const (
	stBase = iota
	stHeading
	stMarker
	stCheckbox
	stDone
	stCode
	stLink
	stTag
	stQuote
	stStrong
	stEmph
	stFront
	stMeta
	stHR
	stFence
	stStrike
)

type styleKey struct {
	class          int
	visual, search bool
	cursor         bool
	dimDone        bool
	// fg, bold and italic come from syntax highlighting inside code fences.
	fg           string
	bold, italic bool
}

func (a *App) styleFor(k styleKey) lipgloss.Style {
	if a.styleCache == nil {
		a.styleCache = map[styleKey]lipgloss.Style{}
	}
	if s, ok := a.styleCache[k]; ok {
		return s
	}
	th := a.theme
	var s lipgloss.Style
	switch k.class {
	case stHeading:
		s = th.Heading
	case stMarker:
		s = th.Marker
	case stCheckbox:
		s = th.Checkbox
	case stDone:
		s = th.Done
	case stCode, stFence:
		s = th.Code
	case stLink:
		s = th.Link
	case stTag:
		s = th.Tag
	case stQuote:
		s = th.Quote
	case stStrong:
		s = th.Strong
	case stEmph:
		s = th.Emphasis
	case stFront:
		s = th.Frontmatter
	case stMeta:
		s = lipgloss.NewStyle().Foreground(th.Purple)
	case stHR:
		s = th.Muted
	case stStrike:
		s = lipgloss.NewStyle().Strikethrough(true).Foreground(th.FgDim)
	default:
		s = th.Base
	}
	if k.fg != "" {
		s = s.Foreground(lipgloss.Color(k.fg))
	}
	if k.bold {
		s = s.Bold(true)
	}
	if k.italic {
		s = s.Italic(true)
	}
	if k.dimDone {
		s = s.Foreground(th.FgMuted)
	}
	if k.visual {
		s = s.Background(th.BgSelected)
	}
	if k.search {
		s = s.Background(th.Yellow).Foreground(th.Bg)
	}
	if k.cursor {
		s = s.Reverse(true)
	}
	a.styleCache[k] = s
	return s
}

var (
	headingRe    = regexp.MustCompile(`^(\s{0,3})(#{1,6})\s`)
	listRe       = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s`)
	checkboxRe   = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+\[([ xX>/\-])\]`)
	quoteRe      = regexp.MustCompile(`^\s{0,3}>`)
	hrRe         = regexp.MustCompile(`^\s{0,3}(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	inlineCodeRe = regexp.MustCompile("`[^`\n]+`")
	strongRe     = regexp.MustCompile(`\*\*[^*\n]+\*\*|__[^_\n]+__`)
	emphRe       = regexp.MustCompile(`(^|[^*\w])\*[^*\n]+\*|(^|[^_\w])_[^_\n]+_`)
	strikeRe     = regexp.MustCompile(`~~[^~\n]+~~`)
	linkRe       = regexp.MustCompile(`!?\[\[[^\]\n]+\]\]|!?\[[^\]\n]*\]\([^)\n]*\)|<?(?:https?://|mailto:)[^\s<>()]+>?`)
	tagRe        = regexp.MustCompile(`(^|\s)#[\p{L}\p{N}_][\p{L}\p{N}_/-]*`)
	taskMetaRe   = regexp.MustCompile(`(^|\s)(due:\S+|!(?:high|med|low)\b|@[\w-]+(?::\S+)?|✅\s?\d{4}-\d{2}-\d{2}|scheduled:\S+)`)
)

// lineClasses computes a style class for every rune of a line.
func lineClasses(line string, inFence, inFront bool) []int {
	runes := []rune(line)
	classes := make([]int, len(runes))
	if inFront {
		for i := range classes {
			classes[i] = stFront
		}
		return classes
	}
	if inFence || fenceRe.MatchString(line) {
		for i := range classes {
			classes[i] = stFence
		}
		return classes
	}
	byteToRune := byteRuneMap(line)
	mark := func(b0, b1, class int) {
		r0, r1 := byteToRune[b0], byteToRune[b1]
		for i := r0; i < r1 && i < len(classes); i++ {
			classes[i] = class
		}
	}
	if hrRe.MatchString(line) {
		mark(0, len(line), stHR)
		return classes
	}
	if m := headingRe.FindStringSubmatchIndex(line); m != nil {
		mark(0, len(line), stHeading)
	}
	if quoteRe.MatchString(line) {
		mark(0, len(line), stQuote)
	}
	done := false
	if m := checkboxRe.FindStringSubmatchIndex(line); m != nil {
		state := line[m[4]:m[5]]
		mark(m[4]-1, m[5]+1, stCheckbox)
		if state == "x" || state == "X" || state == "-" {
			done = true
			for i := byteToRune[m[5]+1]; i < len(classes); i++ {
				classes[i] = stDone
			}
		}
	} else if m := listRe.FindStringSubmatchIndex(line); m != nil {
		mark(m[4], m[5], stMarker)
	}
	if done {
		return classes
	}
	for _, m := range inlineCodeRe.FindAllStringIndex(line, -1) {
		mark(m[0], m[1], stCode)
	}
	for _, m := range strongRe.FindAllStringIndex(line, -1) {
		if classes[byteToRune[m[0]]] == stCode {
			continue
		}
		mark(m[0], m[1], stStrong)
	}
	for _, m := range emphRe.FindAllStringIndex(line, -1) {
		start := m[0]
		if start < len(line) && line[start] != '*' && line[start] != '_' {
			start++
		}
		if classes[byteToRune[start]] == stCode || classes[byteToRune[start]] == stStrong {
			continue
		}
		mark(start, m[1], stEmph)
	}
	for _, m := range strikeRe.FindAllStringIndex(line, -1) {
		mark(m[0], m[1], stStrike)
	}
	for _, m := range linkRe.FindAllStringIndex(line, -1) {
		if classes[byteToRune[m[0]]] == stCode {
			continue
		}
		mark(m[0], m[1], stLink)
	}
	for _, m := range tagRe.FindAllStringSubmatchIndex(line, -1) {
		start := m[0]
		if start < len(line) && line[start] != '#' {
			start++
		}
		if classes[byteToRune[start]] == stCode || classes[byteToRune[start]] == stLink {
			continue
		}
		mark(start, m[1], stTag)
	}
	for _, m := range taskMetaRe.FindAllStringSubmatchIndex(line, -1) {
		if classes[byteToRune[m[4]]] == stCode {
			continue
		}
		mark(m[4], m[5], stMeta)
	}
	return classes
}

// byteRuneMap maps every byte offset (inclusive of len) to a rune index.
func byteRuneMap(s string) []int {
	m := make([]int, len(s)+1)
	ri := 0
	for bi := range s {
		m[bi] = ri
		ri++
	}
	// Fill continuation bytes and the end.
	last := 0
	for bi := 0; bi <= len(s); bi++ {
		if bi < len(s) && !isRuneStart(s[bi]) {
			m[bi] = last
			continue
		}
		if bi < len(s) {
			last = m[bi]
		}
	}
	m[len(s)] = ri
	return m
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// blockState tracks fences and frontmatter from the top of the buffer.
type blockState struct {
	fence []bool
	front []bool
}

func computeBlockState(lines []string) blockState {
	st := blockState{fence: make([]bool, len(lines)), front: make([]bool, len(lines))}
	inFence := false
	inFront := len(lines) > 0 && lines[0] == "---"
	for i, line := range lines {
		if inFront {
			st.front[i] = true
			if i > 0 && (line == "---" || line == "...") {
				inFront = false
			}
			continue
		}
		if fenceRe.MatchString(line) {
			st.fence[i] = true
			inFence = !inFence
			continue
		}
		st.fence[i] = inFence
	}
	return st
}

// rowInfo is one rendered editor row, kept for hint mode.
type rowInfo struct {
	line     int
	segStart int
	segEnd   int
	x        int
	y        int
}

// renderEditor draws a note buffer into w x h cells.
func (a *App) renderEditor(buf *noteBuffer, w, h int, focused bool) string {
	ed := buf.ed
	ed.SetViewport(h)
	ed.EnsureCursorVisible()
	gutter := a.gutterWidth(buf)
	textW := max(1, w-gutter)
	lines := ed.Lines()
	blocks := computeBlockState(lines)
	cur := ed.Cursor()
	vStart, vEnd, vMode, inVisual := ed.VisualRange()
	blockLeft, blockRight, blockToEnd := 0, 0, false
	if inVisual && vMode == vim.ModeVisualBlock {
		blockLeft, blockRight, blockToEnd = ed.BlockColumns()
	}
	out := make([]string, 0, h)
	buf.rows = buf.rows[:0]
	th := a.theme
	line := ed.ScrollTop()
	// Relative numbers count visible lines, so folds collapse the distance.
	visibleIdx := map[int]int{}
	cursorVisible := 0
	for i, n := line, 0; i < len(lines) && n < h+len(lines); i++ {
		if ed.Hidden(i) {
			continue
		}
		visibleIdx[i] = n
		if i == cur.Line {
			cursorVisible = n
		}
		n++
	}
	relOf := func(i int) int { return abs(visibleIdx[i] - cursorVisible) }
	style := a.prefs.CompletedTaskStyle
	dimDone := style == "dim" || style == "muted" || style == "gray" || style == "gray-strikethrough"
	strikeDone := style == "strikethrough" || style == "strike" || style == "gray-strikethrough"
	codeSpans := a.codeSpansFor(buf, lines)
	for len(out) < h && line < len(lines) {
		if ed.Hidden(line) {
			line++
			continue
		}
		text := lines[line]
		runes := []rune(text)
		classes := lineClasses(text, blocks.fence[line], blocks.front[line])
		if !strikeDone && !dimDone {
			for i, c := range classes {
				if c == stDone {
					classes[i] = stBase
				}
			}
		}
		if dimDone {
			for i, c := range classes {
				if c == stDone {
					classes[i] = stBase
				}
			}
		}
		lineDone := checkboxDone(text)
		lineSpans := codeSpans[line]
		segs := []segment{{0, len(runes)}}
		if a.prefs.WordWrap {
			segs = wrapLine(runes, textW)
		}
		searchHits := ed.SearchHighlights(line)
		if hits := ed.IncrementalHighlights(line); len(hits) > 0 {
			searchHits = append(searchHits, hits...)
		}
		foldCount, isFold := ed.IsFoldStart(line)
		for si, seg := range segs {
			if len(out) >= h {
				break
			}
			var b strings.Builder
			b.WriteString(a.renderGutter(buf, line, cur.Line, relOf(line), si == 0, gutter))
			cells := 0
			col := seg.start
			for col < seg.end {
				r := runes[col]
				key := styleKey{class: classes[col]}
				if classes[col] == stFence {
					if sp, ok := codeSpanAt(lineSpans, col); ok {
						key.fg, key.bold, key.italic = sp.fg, sp.bold, sp.italic
					}
				}
				if dimDone && lineDone {
					key.dimDone = true
				}
				if inVisual && inVisualRange(line, col, vStart, vEnd, vMode, blockLeft, blockRight, blockToEnd) {
					key.visual = true
				}
				for _, hit := range searchHits {
					if col >= hit[0] && col < hit[1] {
						key.search = true
					}
				}
				if focused && line == cur.Line && col == cur.Col {
					key.cursor = true
				}
				// Merge runs of identical style.
				run := []rune{}
				j := col
				for j < seg.end {
					k2 := styleKey{class: classes[j], dimDone: key.dimDone}
					if classes[j] == stFence {
						if sp, ok := codeSpanAt(lineSpans, j); ok {
							k2.fg, k2.bold, k2.italic = sp.fg, sp.bold, sp.italic
						}
					}
					if inVisual && inVisualRange(line, j, vStart, vEnd, vMode, blockLeft, blockRight, blockToEnd) {
						k2.visual = true
					}
					for _, hit := range searchHits {
						if j >= hit[0] && j < hit[1] {
							k2.search = true
						}
					}
					if focused && line == cur.Line && j == cur.Col {
						k2.cursor = true
					}
					if k2 != key {
						break
					}
					rr := runes[j]
					if rr == '\t' {
						run = append(run, ' ', ' ', ' ', ' ')
					} else {
						run = append(run, rr)
					}
					j++
				}
				_ = r
				b.WriteString(a.styleFor(key).Render(string(run)))
				cells += cellWidth(string(run))
				col = j
			}
			if focused && line == cur.Line && cur.Col >= seg.end && si == len(segs)-1 {
				b.WriteString(a.styleFor(styleKey{cursor: true}).Render(" "))
				cells++
			}
			if isFold && si == len(segs)-1 {
				marker := fmt.Sprintf(" ▸ %d lines", foldCount)
				b.WriteString(th.Muted.Render(marker))
				cells += cellWidth(marker)
			}
			buf.rows = append(buf.rows, rowInfo{line: line, segStart: seg.start, segEnd: seg.end, x: gutter, y: len(out)})
			row := b.String()
			if cells < textW {
				row += strings.Repeat(" ", textW-cells)
			}
			out = append(out, padRight(row, w))
		}
		line++
	}
	for len(out) < h {
		out = append(out, padRight(th.Muted.Render("~"), w))
	}
	return strings.Join(out, "\n")
}

func checkboxDone(line string) bool {
	m := checkboxRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	return m[2] == "x" || m[2] == "X" || m[2] == "-"
}

func (a *App) renderGutter(buf *noteBuffer, line, curLine, rel int, first bool, width int) string {
	if width <= 1 {
		return strings.Repeat(" ", width)
	}
	if !first {
		return strings.Repeat(" ", width)
	}
	var num string
	switch a.prefs.LineNumberMode {
	case "relative":
		num = fmt.Sprint(rel)
	case "hybrid", "both":
		if line == curLine {
			num = fmt.Sprint(line + 1)
		} else {
			num = fmt.Sprint(rel)
		}
	default:
		num = fmt.Sprint(line + 1)
	}
	style := a.theme.LineNumber
	if line == curLine {
		style = a.theme.Dim
	}
	return style.Render(fmt.Sprintf("%*s ", width-1, num))
}

// inVisualRange reports whether a cell is inside the visual selection.
func inVisualRange(line, col int, start, end vim.Pos, mode vim.Mode, bl, br int, toEnd bool) bool {
	if line < start.Line || line > end.Line {
		return false
	}
	switch mode {
	case vim.ModeVisualLine:
		return true
	case vim.ModeVisualBlock:
		if toEnd {
			return col >= bl
		}
		return col >= bl && col <= br
	}
	if start.Line == end.Line {
		return col >= start.Col && col <= end.Col
	}
	if line == start.Line {
		return col >= start.Col
	}
	if line == end.Line {
		return col <= end.Col
	}
	return true
}

// renderPlainEditor draws a small editor (quick capture) without gutter.
func renderPlainEditor(a *App, ed *vim.Editor, w, h int, focused bool) string {
	tmp := &noteBuffer{ed: ed}
	saved := a.prefs.LineNumberMode
	a.prefs.LineNumberMode = "off"
	out := a.renderEditor(tmp, w, h, focused)
	a.prefs.LineNumberMode = saved
	return out
}

// editorHintTargets labels every link on the visible rows of a buffer.
func (a *App) editorHintTargets(p *pane, buf *noteBuffer) []hintTarget {
	out := []hintTarget{}
	y0 := a.contentRect(p).y
	for _, row := range buf.rows {
		text := buf.ed.Line(row.line)
		runes := []rune(text)
		for _, m := range linkRe.FindAllStringIndex(text, -1) {
			bm := byteRuneMap(text)
			rc := bm[m[0]]
			if rc < row.segStart || rc >= row.segEnd {
				continue
			}
			x := a.contentRect(p).x + row.x + cellWidth(string(runes[row.segStart:rc]))
			link, ok := linkAt(text, rc)
			if !ok {
				continue
			}
			l := link
			out = append(out, hintTarget{x: x, y: y0 + row.y, run: func(a *App) { a.followLink(l) }})
		}
	}
	return out
}

// hintTargets gathers the targets for the focused surface.
func (a *App) hintTargets() []hintTarget {
	main := a.mainRect()
	switch a.focus {
	case focusSidebar:
		return a.sidebar.hintTargets(a, 1, main.y, main.h)
	case focusPane:
		p := a.activePane
		t := p.activeTab()
		if t == nil {
			return nil
		}
		if t.view != nil {
			if hv, ok := t.view.(hintable); ok {
				return hv.hintTargets(a, p)
			}
			return nil
		}
		if buf := a.buffers[t.path]; buf != nil {
			if t.mode == modePreview {
				return a.previewHintTargets(p, t, buf)
			}
			return a.editorHintTargets(p, buf)
		}
	}
	return nil
}

// hintable views can label their rows for hint mode.
type hintable interface {
	hintTargets(a *App, p *pane) []hintTarget
}
