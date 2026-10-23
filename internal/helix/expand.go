package helix

import (
	"regexp"
	"strings"
)

// Selection growth. Helix grows a selection through the syntax tree:
// Alt+o expands to the parent node, Alt+i shrinks back to the child. There
// is no syntax tree here, so the contexts a note is built from take its
// place: the character, the word, the paragraph, the function inside a code
// block, the block, the heading section and the note itself. The ladder is
// a chain — every context holds the one before it — so growing and shrinking
// walk the same rungs.

// selState is a selection the growth commands can restore.
type selState struct {
	anchor  Pos
	cursor  Pos
	kind    selKind
	selExcl bool
	from    Pos // the selection's first character
	to      Pos // its last character
}

// holds reports whether the saved selection sits inside the given range.
func (s selState) holds(from, to Pos) bool {
	return !s.from.Less(from) && !to.Less(s.to)
}

// context is one rung of the ladder: the characters it covers, from start
// to end inclusive, and whether it selects whole lines.
type context struct {
	start, end Pos
	linewise   bool
	label      string
}

// covers reports whether the context holds the whole range.
func (c context) covers(from, to Pos) bool {
	return !from.Less(c.start) && !c.end.Less(to)
}

// inside reports whether the context sits inside the range.
func (c context) inside(from, to Pos) bool {
	return !from.Less(c.start) && !to.Less(c.end)
}

// sameAs reports whether the context is exactly the range.
func (c context) sameAs(from, to Pos) bool {
	return c.start == from && c.end == to
}

// expandSelection grows the selection to the next context around it
// (Helix's expand_selection). The smallest context holding the selection is
// the one to grow to; when the selection already is that context, the next
// one up is.
func (e *Editor) expandSelection() {
	e.markSelection()
	from, to := e.selectionChars()
	ladder := e.contextLadder(from)
	for i, c := range ladder {
		if !c.covers(from, to) {
			continue
		}
		if c.sameAs(from, to) {
			// The selection already is that context: grow to the next one.
			if i+1 < len(ladder) {
				e.applyContext(ladder[i+1], true)
			}
			return
		}
		e.applyContext(c, true)
		return
	}
}

// shrinkSelection shrinks the selection to the previous context (Helix's
// shrink_selection). It restores the selection the growth pushed when there
// is one, and otherwise falls to the largest context inside the selection.
func (e *Editor) shrinkSelection() {
	e.markSelection()
	from, to := e.selectionChars()
	if n := len(e.expandStack); n > 0 {
		st := e.expandStack[n-1]
		if st.holds(from, to) {
			e.expandStack = e.expandStack[:n-1]
			e.restoreSelState(st)
			return
		}
		e.expandStack = nil
	}
	ladder := e.contextLadder(from)
	for i := len(ladder) - 1; i >= 0; i-- {
		c := ladder[i]
		if c.sameAs(from, to) || !c.inside(from, to) {
			continue
		}
		e.applyContext(c, false)
		return
	}
}

// markSelection keeps the selection alive when the command leaves it
// untouched: normal mode collapses a selection that is not a motion's.
func (e *Editor) markSelection() {
	if e.mode != ModeSelect {
		e.motionSelected = true
	}
}

// applyContext selects a context. The head keeps the side of the selection
// it was on, so a backward selection grows to the left. When remember is
// set the selection it replaces is kept for the next shrink.
func (e *Editor) applyContext(c context, remember bool) {
	from, to := e.selectionChars()
	// The head keeps the side of the selection it was on: a backward
	// selection grows to the left.
	forward := !e.cursor.Less(e.anchor)
	if remember {
		e.expandStack = append(e.expandStack, selState{
			anchor:  e.anchor,
			cursor:  e.cursor,
			kind:    e.kind,
			selExcl: e.selExcl,
			from:    from,
			to:      to,
		})
	}
	e.selExcl = false
	if c.linewise {
		e.kind = selLine
	} else {
		e.kind = selChar
	}
	if forward {
		e.anchor = e.buf.clampNormal(c.start)
		e.cursor = e.buf.clampNormal(c.end)
	} else {
		e.anchor = e.buf.clampNormal(c.end)
		e.cursor = e.buf.clampNormal(c.start)
	}
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
	e.setMsg("selection: " + c.label)
}

// restoreSelState puts back a selection the growth replaced.
func (e *Editor) restoreSelState(s selState) {
	e.anchor = e.buf.clampNormal(s.anchor)
	e.cursor = e.buf.clampNormal(s.cursor)
	e.kind = s.kind
	e.selExcl = s.selExcl
	e.desiredCol = -1
	e.openFoldAt(e.cursor.Line)
	e.ensureCursorVisible()
	e.ClearMessage()
}

// selectionChars is the selection as an inclusive range of characters. A
// word-start motion leaves out the word it landed on: the character under the
// head is not part of the selection. The head is the upper bound for a
// forward motion and the lower bound for a backward one.
func (e *Editor) selectionChars() (from, to Pos) {
	from, to = e.selRange()
	if !e.selExcl || !from.Less(to) {
		return from, to
	}
	if e.cursor.Less(e.anchor) {
		// The head is the lower bound: the word it landed on starts
		// after the selection.
		next := e.nextCharPos(from)
		if !next.Less(to) {
			return from, to
		}
		return next, to
	}
	prev := e.prevCharPos(to)
	if prev.Less(from) {
		// The head sits before the selection: there is nothing to leave
		// out of it.
		return from, to
	}
	return from, prev
}

// contextLadder lists the contexts around p, smallest first. Every context
// holds the one before it.
func (e *Editor) contextLadder(p Pos) []context {
	out := []context{}
	if c, ok := e.charContext(p); ok {
		out = append(out, c)
	}
	if c, ok := e.wordContext(p); ok {
		out = append(out, c)
	}
	if fenceStart, fenceEnd, inFence := e.fencedBlockAt(p.Line); inFence {
		if fnStart, fnEnd, ok := e.codeFunctionAt(p.Line, fenceStart, fenceEnd); ok {
			out = append(out, e.lineContext(fnStart, fnEnd, "function"))
		}
		out = append(out, e.lineContext(fenceStart, fenceEnd, "code block"))
	} else {
		start, end := e.paragraphRangeAt(p.Line)
		out = append(out, e.lineContext(start, end, "paragraph"))
	}
	if c, ok := e.sectionContext(p); ok {
		out = append(out, c)
	}
	out = append(out, e.noteContext())
	return out
}

// charContext is the character under p.
func (e *Editor) charContext(p Pos) (context, bool) {
	if p.Col >= e.buf.LineLen(p.Line) {
		return context{}, false
	}
	return context{start: p, end: p, label: "character"}, true
}

// wordContext is the word under p: the run of characters of the same class.
func (e *Editor) wordContext(p Pos) (context, bool) {
	line := e.buf.Line(p.Line)
	if p.Col >= len(line) {
		return context{}, false
	}
	cls := charClass(line[p.Col], false)
	start, end := p.Col, p.Col
	for start > 0 && charClass(line[start-1], false) == cls {
		start--
	}
	for end+1 < len(line) && charClass(line[end+1], false) == cls {
		end++
	}
	return context{start: Pos{p.Line, start}, end: Pos{p.Line, end}, label: "word"}, true
}

// paragraphRangeAt is the block of text around line: the run of non-blank
// lines, stopped by a blank line, a heading or a fence.
func (e *Editor) paragraphRangeAt(line int) (int, int) {
	start := line
	for start > 0 && e.inParagraphLine(start-1) {
		start--
	}
	end := line
	for end+1 < e.buf.LineCount() && e.inParagraphLine(end+1) {
		end++
	}
	return start, end
}

// inParagraphLine reports whether the line belongs to a paragraph.
func (e *Editor) inParagraphLine(i int) bool {
	if isBlankLine(e.buf.Line(i)) {
		return false
	}
	trimmed := strings.TrimLeft(e.buf.LineString(i), " \t")
	return !strings.HasPrefix(trimmed, "#") &&
		!strings.HasPrefix(trimmed, "```") &&
		!strings.HasPrefix(trimmed, "~~~")
}

// fencedBlockAt reports the fenced code block containing line.
func (e *Editor) fencedBlockAt(line int) (int, int, bool) {
	inFence := false
	marker := ""
	open := 0
	for i := 0; i < e.buf.LineCount(); i++ {
		trimmed := strings.TrimLeft(e.buf.LineString(i), " \t")
		if !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		m := trimmed[:3]
		if !inFence {
			inFence, marker, open = true, m, i
			continue
		}
		if m != marker {
			continue
		}
		inFence = false
		if line >= open && line <= i {
			return open, i, true
		}
	}
	return 0, 0, false
}

// codeFuncRe opens a function, a class or a block in code.
var codeFuncRe = regexp.MustCompile(`^\s*(?:pub |export |public |private |protected |static |async |export default )*(?:func|fn|def|function|class|impl|struct|trait|interface|type|enum|const|let|var|module|package)\b`)

// codeFunctionAt is the definition around line and the body under it, inside
// the fence that runs from fenceStart to fenceEnd.
func (e *Editor) codeFunctionAt(line, fenceStart, fenceEnd int) (int, int, bool) {
	for i := line; i >= fenceStart; i-- {
		if !codeFuncRe.MatchString(e.buf.LineString(i)) {
			continue
		}
		indent := indentWidth(e.buf.Line(i))
		end := i
		for j := i + 1; j <= fenceEnd; j++ {
			l := e.buf.Line(j)
			if isBlankLine(l) {
				continue
			}
			if indentWidth(l) > indent {
				end = j
				continue
			}
			// The body ends here: a closing brace belongs to it.
			if isClosingLine(l) {
				end = j
			}
			break
		}
		return i, end, true
	}
	return 0, 0, false
}

// isClosingLine reports whether the line closes the block above it.
func isClosingLine(line []rune) bool {
	trimmed := strings.TrimLeft(string(line), " \t")
	for _, close := range []string{"}", ")", "]", "end", "fi", "done", "EOF"} {
		if trimmed == close || strings.HasPrefix(trimmed, close+" ") || strings.HasPrefix(trimmed, close+",") {
			return true
		}
	}
	return false
}

// indentWidth is the width of the line's leading whitespace.
func indentWidth(line []rune) int {
	n := 0
	for _, r := range line {
		switch r {
		case '\t':
			n += 4
		case ' ':
			n++
		default:
			return n
		}
	}
	return n
}

// sectionContext is the heading section around p: the heading and the text
// under it, up to the next heading.
func (e *Editor) sectionContext(p Pos) (context, bool) {
	if s, found := e.enclosingOutlineSection(p.Line); found {
		return e.lineContext(s.start, s.end, "section"), true
	}
	headings := e.headingLines()
	if len(headings) > 0 && p.Line < headings[0] {
		return e.lineContext(0, headings[0]-1, "section"), true
	}
	return context{}, false
}

// outlineSections are the heading sections of the buffer: every heading and
// the text under it, up to the next heading of any level. The folds in
// folds.go nest the other way round — a heading folds its subsections too —
// but the ladder needs the smaller section to sit inside the bigger one.
func (e *Editor) outlineSections() []section {
	headings := e.headingLines()
	out := []section{}
	for i, h := range headings {
		end := e.buf.LineCount() - 1
		if i+1 < len(headings) {
			end = headings[i+1] - 1
		}
		out = append(out, section{start: h, end: end, level: headingLevel(e.buf.LineString(h))})
	}
	return out
}

// enclosingOutlineSection is the innermost heading section holding line.
func (e *Editor) enclosingOutlineSection(line int) (section, bool) {
	var best section
	found := false
	for _, s := range e.outlineSections() {
		if s.start <= line && line <= s.end {
			best, found = s, true
		}
	}
	return best, found
}

// noteContext is the whole buffer.
func (e *Editor) noteContext() context {
	return e.lineContext(0, e.buf.LineCount()-1, "note")
}

// lineContext is a range of whole lines.
func (e *Editor) lineContext(start, end int, label string) context {
	return context{
		start:    Pos{start, 0},
		end:      Pos{end, max(0, e.buf.LineLen(end)-1)},
		linewise: true,
		label:    label,
	}
}
