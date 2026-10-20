package helix

import (
	"math"
	"strings"
)

// The selection model: in ModeSelect the selection runs from anchor to
// cursor (either order) and kind says what it covers — characters, whole
// lines, or a rectangular block (a ZenNotes extension; Helix has no block
// mode). In ModeNormal the selection is collapsed onto the cursor and acts
// as the one character under it, the way Helix models its block cursor.

// selRange is the current selection as ordered endpoints. Outside select
// mode it is the collapsed selection on the cursor.
func (e *Editor) selRange() (from, to Pos) {
	if e.mode != ModeSelect && e.anchor == e.cursor {
		return e.cursor, e.cursor
	}
	from, to = e.anchor, e.cursor
	if to.Less(from) {
		from, to = to, from
	}
	return from, to
}

// enterSelect enters select mode with the given kind; the same kind toggles
// back to normal mode, a different kind switches the selection shape.
func (e *Editor) enterSelect(kind selKind) {
	if e.mode == ModeSelect {
		if e.kind == kind {
			e.exitSelect()
			return
		}
		e.kind = kind
		return
	}
	// A selector motion may already have left a spanning selection; keep it.
	e.kind = kind
	e.mode = ModeSelect
	e.selExcl = false
	e.pending = nil
}

// exitSelect collapses to normal mode, remembering the selection for `gv`.
func (e *Editor) exitSelect() {
	start, end := e.selRange()
	e.lastSelect.start, e.lastSelect.end, e.lastSelect.kind, e.lastSelect.set = start, end, e.kind, true
	e.marks['<'] = start
	e.marks['>'] = end
	e.anchor = e.cursor
	e.selExcl = false
	e.mode = ModeNormal
	e.cursor = e.buf.clampNormal(e.cursor)
	e.pending = nil
}

// collapseToCursor shrinks the selection onto the cursor (Helix's `;`),
// keeping the mode.
func (e *Editor) collapseToCursor() {
	e.anchor = e.cursor
	e.selExcl = false
	e.desiredCol = -1
}

// reselect restores the last selection (`gv`).
func (e *Editor) reselect() {
	if !e.lastSelect.set {
		return
	}
	e.mode = ModeSelect
	e.kind = e.lastSelect.kind
	e.selExcl = false
	e.anchor = e.buf.clampNormal(e.lastSelect.start)
	e.cursor = e.buf.clampNormal(e.lastSelect.end)
}

// SelectionKind reports what the active selection covers: "v", "V" or
// "^V". Outside select mode it is "v" (the collapsed selection on the
// cursor).
func (e *Editor) SelectionKind() string {
	if e.mode != ModeSelect {
		return "v"
	}
	switch e.kind {
	case selLine:
		return "V"
	case selBlock:
		return "^V"
	}
	return "v"
}

// VisualRange is the normalized selection: start..end inclusive, the mode,
// and ok when a selection is active. Selector motions leave spanning
// selections in normal mode, so any non-collapsed anchor counts as active —
// insert mode never renders one. A word-start selection's end is exclusive;
// it is reported one character short so the highlight stops before the next
// word's first char. For block mode the columns come from BlockColumns.
func (e *Editor) VisualRange() (start, end Pos, mode Mode, ok bool) {
	if e.mode == ModeInsert || (e.mode != ModeSelect && e.anchor == e.cursor) {
		return Pos{}, Pos{}, e.mode, false
	}
	start, end = e.selRange()
	if e.selExcl && start.Less(end) {
		end = e.prevCharPos(end)
	}
	if e.kind == selLine {
		start.Col = 0
		end.Col = max(0, e.buf.LineLen(end.Line)-1)
	}
	return start, end, e.mode, true
}

// prevCharPos is the character before p, walking back over line breaks.
func (e *Editor) prevCharPos(p Pos) Pos {
	if p.Col > 0 {
		return Pos{p.Line, p.Col - 1}
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, max(0, e.buf.LineLen(p.Line-1)-1)}
	}
	return p
}

// BlockColumns is the column span of a block selection, plus whether `$`
// extends every line to its end.
func (e *Editor) BlockColumns() (left, right int, toEnd bool) {
	left = min(e.anchor.Col, e.cursor.Col)
	right = max(e.anchor.Col, e.cursor.Col)
	toEnd = e.desiredCol == math.MaxInt32
	return
}

// selectionSpan converts the current selection into an operator span; the
// collapsed selection becomes the single character under the cursor.
func (e *Editor) selectionSpan() span {
	if e.mode != ModeSelect && (e.anchor == e.cursor || e.mode == ModeInsert) {
		return span{start: e.cursor, end: e.stepInsertPos(e.cursor)}
	}
	from, to := e.selRange()
	switch e.kind {
	case selLine:
		return span{start: Pos{from.Line, 0}, end: Pos{to.Line, 0}, linewise: true}
	case selBlock:
		left, right, toEnd := e.BlockColumns()
		return span{start: Pos{from.Line, left}, end: Pos{to.Line, right}, blockwise: true, blockLeft: left, blockRight: right, blockToEnd: toEnd}
	}
	if e.selExcl {
		// The upper-bound character sits past the selection: `w` stops on
		// the next word's first char without selecting it.
		return span{start: from, end: to}
	}
	return span{start: from, end: e.stepInsertPos(to)}
}

// --- select mode dispatch ---

func (e *Editor) selectKey(k Key) {
	e.ClearMessage()
	e.pending = append(e.pending, k)
	status := e.runSelect(e.pending)
	if status != parseIncomplete {
		e.pending = nil
	}
}

func (e *Editor) runSelect(keys []Key) parseStatus {
	register, count, hasCount, rest, status := splitPrefix(keys)
	if status != parseComplete {
		return status
	}
	if len(rest) == 0 {
		return parseIncomplete
	}
	k := rest[0]
	n := max(1, count)
	versionBefore := e.version
	finish := func(entered bool) parseStatus {
		if entered {
			if !e.replaying {
				e.recordingIns = true
				e.changeKeys = append([]Key(nil), keys...)
			}
			return parseComplete
		}
		e.commitChange()
		if e.version != versionBefore && !e.replaying {
			e.last = lastChange{keys: stripCount(keys), count: count, register: register}
		}
		e.anchor = e.cursor
		e.ensureCursorVisible()
		return parseComplete
	}
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsCtrl('['):
		e.exitSelect()
		return parseComplete
	case k.IsRune('v'):
		e.enterSelect(selChar)
		return parseComplete
	case k.IsRune('V'):
		e.enterSelect(selLine)
		return parseComplete
	case k.IsCtrl('v'):
		e.enterSelect(selBlock)
		return parseComplete
	case k.IsRune('o'):
		e.anchor, e.cursor = e.cursor, e.anchor
		e.desiredCol = -1
		return parseComplete
	case k.IsRune('O'):
		if e.kind == selBlock {
			e.anchor.Col, e.cursor.Col = e.cursor.Col, e.anchor.Col
		} else {
			e.anchor, e.cursor = e.cursor, e.anchor
		}
		e.desiredCol = -1
		return parseComplete
	case k.IsRune(';'):
		e.collapseToCursor()
		return parseComplete
	case k.IsRune(':'):
		e.exitSelect()
		e.openCmdline(':', 0, false)
		e.cmd.text = []rune("'<,'>")
		e.cmd.pos = len(e.cmd.text)
		return parseComplete
	case k.IsRune('d') || k.Is("delete"):
		e.beginChange()
		sp := e.selectionSpan()
		e.exitSelect()
		e.deleteSpan(sp, register)
		return finish(false)
	case k.IsRune('y'):
		sp := e.selectionSpan()
		e.exitSelect()
		e.yankSpan(sp, register, true)
		e.cursor = e.buf.clampNormal(sp.start)
		return finish(false)
	case k.IsRune('c') || k.IsRune('s'):
		e.beginChange()
		sp := e.selectionSpan()
		e.exitSelect()
		entered := e.runOperator("c", sp, register, false)
		return finish(entered)
	case k.IsRune('D'):
		e.beginChange()
		sp := e.selectionSpan()
		from, to := e.selRange()
		if e.kind == selBlock {
			sp.blockToEnd = true
			e.exitSelect()
			e.deleteSpan(sp, register)
			return finish(false)
		}
		sp = span{start: from, end: Pos{to.Line, 0}, linewise: to.Line > from.Line}
		if to.Line == from.Line {
			sp = span{start: from, end: Pos{from.Line, e.buf.LineLen(from.Line)}}
		}
		e.exitSelect()
		e.deleteSpan(sp, register)
		return finish(false)
	case k.IsRune('C'):
		if e.kind == selBlock {
			e.beginChange()
			sp := e.selectionSpan()
			left, right, _ := e.BlockColumns()
			e.exitSelect()
			sp.blockToEnd = true
			e.deleteSpan(sp, register)
			e.startBlockInsert(sp.start.Line, sp.end.Line, left, false)
			_ = right
			return finish(true)
		}
		e.beginChange()
		sp := e.selectionSpan()
		e.exitSelect()
		entered := e.runOperator("c", span{start: Pos{sp.start.Line, 0}, end: Pos{sp.end.Line, 0}, linewise: true}, register, false)
		return finish(entered)
	case k.IsRune('p') || k.IsRune('P'):
		e.beginChange()
		e.putSelection(register, n, k.IsRune('p'))
		return finish(false)
	case k.IsRune('R'):
		e.beginChange()
		sp := e.selectionSpan()
		e.putOverSpan(sp, register, n, false)
		return finish(false)
	case k.IsRune('J') || (k.IsRune('g') && len(rest) > 1 && rest[1].IsRune('J')):
		e.beginChange()
		start, end := e.selRange()
		trim := k.IsRune('J')
		e.exitSelect()
		e.joinLines(start.Line, max(2, end.Line-start.Line+1), trim)
		return finish(false)
	case k.IsRune('<') || k.IsRune('>'):
		e.beginChange()
		start, end := e.selRange()
		e.exitSelect()
		e.shiftLines(start.Line, end.Line, k.IsRune('>'), n)
		e.cursor = Pos{start.Line, firstNonBlank(e.buf.Line(start.Line))}
		return finish(false)
	case k.IsRune('='):
		start, _ := e.selRange()
		e.exitSelect()
		e.cursor = Pos{start.Line, firstNonBlank(e.buf.Line(start.Line))}
		return parseComplete
	case k.IsRune('~') || k.IsRune('`'):
		e.beginChange()
		sp := e.selectionSpan()
		e.exitSelect()
		op := "g~"
		if k.IsRune('`') {
			op = "gu"
		}
		e.transformSpan(sp, op)
		e.cursor = e.buf.clampNormal(sp.start)
		return finish(false)
	case k.IsRune('r'):
		if len(rest) < 2 {
			return parseIncomplete
		}
		if !rest[1].Printable() && !rest[1].Is("enter") {
			e.exitSelect()
			return parseComplete
		}
		e.beginChange()
		sp := e.selectionSpan()
		e.exitSelect()
		e.replaceSpanWith(sp, rest[1])
		return finish(false)
	case k.IsRune('I') || k.IsRune('A'):
		if e.kind == selBlock {
			e.beginChange()
			sp := e.selectionSpan()
			left, right, toEnd := e.BlockColumns()
			e.exitSelect()
			col := left
			appendMode := k.IsRune('A')
			if appendMode {
				col = right + 1
				if toEnd {
					col = -1
				}
			}
			e.startBlockInsert(sp.start.Line, sp.end.Line, col, appendMode)
			return finish(true)
		}
		start, end := e.selRange()
		e.exitSelect()
		if k.IsRune('I') {
			e.startInsert(Pos{start.Line, firstNonBlankInsert(e.buf.Line(start.Line))})
		} else {
			e.startInsert(Pos{end.Line, e.buf.LineLen(end.Line)})
		}
		return finish(true)
	case k.IsRune('g'):
		if len(rest) < 2 {
			return parseIncomplete
		}
		second := rest[1]
		if !second.Printable() {
			return parseInvalid
		}
		switch second.Rune {
		case 'u', 'U', '~', '?':
			e.beginChange()
			sp := e.selectionSpan()
			e.exitSelect()
			e.transformSpan(sp, "g"+string(second.Rune))
			e.cursor = e.buf.clampNormal(sp.start)
			return finish(false)
		case 'q', 'w':
			e.beginChange()
			sp := e.selectionSpan()
			e.exitSelect()
			e.reflowLines(sp.start.Line, sp.end.Line)
			e.cursor = e.buf.clampNormal(Pos{sp.start.Line, 0})
			return finish(false)
		case 'v':
			e.reselect()
			return parseComplete
		case 'd':
			e.exitSelect()
			if e.hooks.FollowLink != nil {
				e.hooks.FollowLink()
			}
			return parseComplete
		}
		m, st := parseMotion(rest)
		if st != parseComplete {
			return st
		}
		e.extendByMotion(m, count, hasCount)
		return parseComplete
	case k.IsRune('m'):
		st, _ := e.matchMode(rest)
		return st
	case k.IsRune('x'):
		e.extendLineBelow()
		return parseComplete
	case k.IsRune('X'):
		e.extendToLineBounds()
		return parseComplete
	case k.IsRune('%'):
		e.selectAll()
		return parseComplete
	case k.IsCtrl('d'):
		e.scrollHalfPage(1, count, hasCount)
		return parseComplete
	case k.IsCtrl('u'):
		e.scrollHalfPage(-1, count, hasCount)
		return parseComplete
	case k.IsCtrl('f'):
		e.scrollPage(n)
		return parseComplete
	case k.IsCtrl('b'):
		e.scrollPage(-n)
		return parseComplete
	case k.IsCtrl('e'):
		e.scrollLines(n)
		return parseComplete
	case k.IsCtrl('y'):
		e.scrollLines(-n)
		return parseComplete
	case k.IsCtrl('a') || k.IsCtrl('x'):
		e.beginChange()
		delta := n
		if k.IsCtrl('x') {
			delta = -n
		}
		start, end := e.selRange()
		for l := start.Line; l <= end.Line; l++ {
			saved := e.cursor
			e.cursor = Pos{l, 0}
			e.incrementNumber(delta)
			e.cursor = saved
		}
		return finish(false)
	case k.IsAlt('.'):
		e.repeatLastMotion(n)
		return parseComplete
	case k.IsRune('z'):
		if len(rest) < 2 {
			return parseIncomplete
		}
		if rest[1].IsRune('f') {
			start, end := e.selRange()
			e.exitSelect()
			e.folds[start.Line] = end.Line
			e.cursor = Pos{start.Line, e.cursor.Col}
			return parseComplete
		}
		return parseInvalid
	}
	m, st := parseMotion(rest)
	if st != parseComplete {
		return st
	}
	switch {
	case m.kind == "n" || m.kind == "N":
		e.selectMatch(m, count, hasCount)
	case selectorMotion(m):
		e.selectByMotion(m, count, hasCount)
	default:
		e.extendByMotion(m, count, hasCount)
	}
	return parseComplete
}

// --- selection construction ---

// extendLineBelow selects the cursor's line, or grows an existing line
// selection by one more line (Helix's x).
func (e *Editor) extendLineBelow() {
	if e.mode != ModeSelect || e.kind != selLine {
		e.mode = ModeSelect
		e.kind = selLine
		e.selExcl = false
		e.anchor = Pos{e.cursor.Line, 0}
		e.cursor = Pos{e.cursor.Line, max(0, e.buf.LineLen(e.cursor.Line)-1)}
		e.desiredCol = -1
		e.pending = nil
		return
	}
	last := max(e.anchor.Line, e.cursor.Line)
	if last+1 < e.buf.LineCount() {
		if e.anchor.Less(e.cursor) {
			e.cursor = Pos{last + 1, max(0, e.buf.LineLen(last+1)-1)}
		} else {
			e.anchor = Pos{last + 1, 0}
		}
	}
}

// extendToLineBounds extends the selection to whole lines (Helix's X).
func (e *Editor) extendToLineBounds() {
	from, to := e.selRange()
	e.mode = ModeSelect
	e.selExcl = false
	e.anchor = Pos{from.Line, 0}
	e.cursor = Pos{to.Line, max(0, e.buf.LineLen(to.Line)-1)}
	e.desiredCol = -1
	e.pending = nil
}

// selectAll selects the whole buffer (Helix's %).
func (e *Editor) selectAll() {
	last := e.buf.LineCount() - 1
	e.mode = ModeSelect
	e.selExcl = false
	e.anchor = Pos{0, 0}
	e.cursor = Pos{last, max(0, e.buf.LineLen(last)-1)}
	e.desiredCol = -1
	e.pending = nil
}

func (e *Editor) extendByMotion(m motion, count int, hasCount bool) {
	from := e.cursor
	e.lastMotion = m
	res, ok := e.applyMotion(m, count, hasCount, e.cursor, false)
	if !ok {
		return
	}
	e.selExcl = false
	if res.jump {
		e.pushJump()
	}
	e.cursor = e.buf.clampNormal(res.pos)
	if m.kind == "$" || m.kind == "g$" {
		e.desiredCol = math.MaxInt32
	} else if res.keepCol {
		if e.desiredCol < 0 {
			e.desiredCol = from.Col
		}
	} else {
		e.desiredCol = -1
	}
	e.ensureCursorVisible()
}

func (e *Editor) extendByObject(obj textObject, count int) {
	rng := e.textObjectRange(obj, count)
	if !rng.ok {
		return
	}
	e.selExcl = false
	if rng.linewise && e.kind == selChar {
		e.kind = selLine
	}
	// Extend rather than replace when the selection already spans more than
	// the object.
	if e.anchor.Less(rng.start) && e.anchor != e.cursor {
		e.cursor = rng.end
		return
	}
	e.anchor = rng.start
	e.cursor = rng.end
	e.desiredCol = -1
}

// --- selection operations ---

// putSelection pastes after (p) or before (P) the selection. With a
// collapsed selection it behaves the Helix/Vim way: after the character
// under the cursor, or below its line for a linewise register.
func (e *Editor) putSelection(register rune, n int, after bool) {
	if e.mode != ModeSelect {
		e.put(register, n, after, false)
		return
	}
	reg, ok := e.readRegister(register)
	if !ok {
		return
	}
	sp := e.selectionSpan()
	if sp.linewise {
		line := sp.end.Line
		if !after {
			line = sp.start.Line - 1
		}
		text := strings.TrimSuffix(reg.Text, "\n")
		lines := []string{}
		for i := 0; i < n; i++ {
			lines = append(lines, strings.Split(text, "\n")...)
		}
		e.buf.InsertLines(line+1, lines)
		e.cursor = Pos{line + 1, firstNonBlank(e.buf.Line(line + 1))}
		e.exitSelect()
		return
	}
	at := sp.end
	if !after {
		at = sp.start
	}
	text := reg.Text
	if !reg.Linewise {
		text = strings.Repeat(strings.TrimSuffix(reg.Text, "\n"), n)
	}
	end := e.buf.InsertText(at, text)
	e.cursor = e.buf.clampNormal(end)
}

// indentSelection shifts the selected lines one stop in or out.
func (e *Editor) indentSelection(dir int) {
	from, to := e.selRange()
	e.shiftLines(from.Line, to.Line, dir > 0, 1)
	e.cursor = Pos{from.Line, firstNonBlank(e.buf.Line(from.Line))}
	e.anchor = e.cursor
}

// transformSelection applies a case operator to the selection.
func (e *Editor) transformSelection(op string) {
	sp := e.selectionSpan()
	e.transformSpan(sp, op)
	e.cursor = e.buf.clampNormal(sp.start)
	e.anchor = e.cursor
}

// replaceSelectionWith swaps every character of the selection for one rune.
func (e *Editor) replaceSelectionWith(r rune) {
	e.replaceSpanWith(e.selectionSpan(), R(r))
	e.anchor = e.cursor
}

// joinSelection joins the selection's lines; with a collapsed selection it
// joins the cursor line with the next ones.
func (e *Editor) joinSelection(n int) {
	from, to := e.selRange()
	e.joinLines(from.Line, max(2, to.Line-from.Line+1), true)
	_ = n
}

func (e *Editor) replaceSpanWith(sp span, k Key) {
	if k.Is("enter") {
		return
	}
	r := k.Rune
	apply := func(l, from, to int) {
		line := cloneRunes(e.buf.Line(l))
		to = min(to, len(line))
		for i := max(0, from); i < to; i++ {
			line[i] = r
		}
		e.buf.ReplaceLine(l, line)
	}
	switch {
	case sp.linewise:
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			apply(l, 0, e.buf.LineLen(l))
		}
	case sp.blockwise:
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			right := sp.blockRight + 1
			if sp.blockToEnd {
				right = e.buf.LineLen(l)
			}
			apply(l, sp.blockLeft, right)
		}
	default:
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			from, to := 0, e.buf.LineLen(l)
			if l == sp.start.Line {
				from = sp.start.Col
			}
			if l == sp.end.Line {
				to = sp.end.Col
			}
			apply(l, from, to)
		}
	}
	e.cursor = e.buf.clampNormal(sp.start)
}

// putOverSpan replaces the selection with a register; the replaced text
// lands in the unnamed register when swapRegister is set.
func (e *Editor) putOverSpan(sp span, register rune, n int, swapRegister bool) {
	reg, ok := e.readRegister(register)
	if !ok {
		return
	}
	removed := e.spanText(sp)
	switch {
	case sp.linewise:
		at := sp.start.Line
		text := strings.TrimSuffix(reg.Text, "\n")
		lines := []string{}
		for i := 0; i < n; i++ {
			lines = append(lines, strings.Split(text, "\n")...)
		}
		e.buf.ReplaceLines(sp.start.Line, sp.end.Line, lines)
		e.cursor = Pos{at, firstNonBlank(e.buf.Line(at))}
	case sp.blockwise:
		e.deleteSpan(sp, '_')
		saved := e.cursor
		e.cursor = Pos{sp.start.Line, sp.blockLeft}
		e.put(register, n, false, false)
		_ = saved
	default:
		e.buf.DeleteRange(sp.start, sp.end)
		text := strings.Repeat(strings.TrimSuffix(reg.Text, "\n"), n)
		if reg.Linewise {
			text = "\n" + strings.TrimSuffix(reg.Text, "\n") + "\n"
		}
		end := e.buf.InsertText(sp.start, text)
		e.cursor = e.buf.clampNormal(Pos{end.Line, end.Col - 1})
		if reg.Linewise {
			e.cursor = Pos{sp.start.Line + 1, 0}
		}
	}
	e.anchor = e.cursor
	if swapRegister {
		e.storeRegister(0, removed, false)
	}
}

// startBlockInsert begins insert on the first line of a block; the typed
// text is replicated onto the other lines when insert ends. col -1 means
// each line's own end.
func (e *Editor) startBlockInsert(startLine, endLine, col int, appendMode bool) {
	e.blockInsert = &blockInsertState{startLine: startLine, endLine: endLine, col: col, appendMode: appendMode}
	first := Pos{startLine, col}
	if col < 0 {
		first.Col = e.buf.LineLen(startLine)
	} else if appendMode {
		line := cloneRunes(e.buf.Line(startLine))
		for len(line) < col {
			line = append(line, ' ')
		}
		e.buf.ReplaceLine(startLine, line)
	}
	e.mode = ModeInsert
	e.cursor = e.buf.clampInsert(first)
	e.ins = insertState{start: e.cursor}
	e.blockInsert.startCol = e.cursor.Col
}

type blockInsertState struct {
	startLine, endLine int
	col                int
	startCol           int
	appendMode         bool
}

// finishBlockInsert copies what was typed on the first line onto the rest
// of the block.
func (e *Editor) finishBlockInsert() {
	bi := e.blockInsert
	e.blockInsert = nil
	if bi == nil || e.cursor.Line != bi.startLine {
		return
	}
	line := e.buf.Line(bi.startLine)
	endCol := e.cursor.Col
	if endCol <= bi.startCol {
		return
	}
	typed := string(line[bi.startCol:endCol])
	if strings.Contains(typed, "\n") {
		return
	}
	for l := bi.startLine + 1; l <= bi.endLine && l < e.buf.LineCount(); l++ {
		cur := cloneRunes(e.buf.Line(l))
		col := bi.col
		if col < 0 {
			col = len(cur)
		}
		if col > len(cur) {
			if !bi.appendMode {
				continue
			}
			for len(cur) < col {
				cur = append(cur, ' ')
			}
		}
		next := append(cloneRunes(cur[:col]), []rune(typed)...)
		next = append(next, cur[col:]...)
		e.buf.ReplaceLine(l, next)
	}
}
