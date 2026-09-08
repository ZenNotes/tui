package vim

import (
	"math"
	"strings"
	"unicode"
)

func (e *Editor) enterVisual(mode Mode) {
	if e.inVisual() {
		if e.mode == mode {
			e.exitVisual()
			return
		}
		e.mode = mode
		return
	}
	e.visualAnchor = e.cursor
	e.mode = mode
	e.pending = nil
}

func (e *Editor) exitVisual() {
	start, end, mode, _ := e.VisualRange()
	e.lastVisual.start, e.lastVisual.end, e.lastVisual.mode, e.lastVisual.set = start, end, mode, true
	e.marks['<'] = start
	e.marks['>'] = end
	e.mode = ModeNormal
	e.cursor = e.buf.clampNormal(e.cursor)
	e.pending = nil
}

func (e *Editor) reselectVisual() {
	if !e.lastVisual.set {
		return
	}
	e.mode = e.lastVisual.mode
	e.visualAnchor = e.buf.clampNormal(e.lastVisual.start)
	e.cursor = e.buf.clampNormal(e.lastVisual.end)
}

// VisualRange is the normalized selection: start..end inclusive, the mode,
// and ok when a selection is active. For block mode the columns come from
// BlockColumns.
func (e *Editor) VisualRange() (start, end Pos, mode Mode, ok bool) {
	if !e.inVisual() {
		return Pos{}, Pos{}, e.mode, false
	}
	start, end = e.visualAnchor, e.cursor
	if end.Less(start) {
		start, end = end, start
	}
	if e.mode == ModeVisualLine {
		start.Col = 0
		end.Col = max(0, e.buf.LineLen(end.Line)-1)
	}
	return start, end, e.mode, true
}

// BlockColumns is the column span of a block selection, plus whether `$`
// extends every line to its end.
func (e *Editor) BlockColumns() (left, right int, toEnd bool) {
	left = min(e.visualAnchor.Col, e.cursor.Col)
	right = max(e.visualAnchor.Col, e.cursor.Col)
	toEnd = e.desiredCol == math.MaxInt32
	return
}

// visualSpan converts the selection into an operator span.
func (e *Editor) visualSpan() span {
	start, end, mode, _ := e.VisualRange()
	switch mode {
	case ModeVisualLine:
		return span{start: Pos{start.Line, 0}, end: Pos{end.Line, 0}, linewise: true}
	case ModeVisualBlock:
		left, right, toEnd := e.BlockColumns()
		return span{start: Pos{start.Line, left}, end: Pos{end.Line, right}, blockwise: true, blockLeft: left, blockRight: right, blockToEnd: toEnd}
	}
	return span{start: start, end: e.stepInsertPos(end)}
}

func (e *Editor) visualKey(k Key) {
	e.ClearMessage()
	e.pending = append(e.pending, k)
	status := e.runVisual(e.pending)
	if status != parseIncomplete {
		e.pending = nil
	}
}

func (e *Editor) runVisual(keys []Key) parseStatus {
	register, count, hasCount, rest, status := splitPrefix(keys)
	if status != parseComplete {
		return status
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
		e.ensureCursorVisible()
		return parseComplete
	}
	switch {
	case k.Is("esc") || k.IsCtrl('c') || k.IsCtrl('['):
		e.exitVisual()
		return parseComplete
	case k.IsRune('v'):
		e.enterVisual(ModeVisual)
		return parseComplete
	case k.IsRune('V'):
		e.enterVisual(ModeVisualLine)
		return parseComplete
	case k.IsCtrl('v'):
		e.enterVisual(ModeVisualBlock)
		return parseComplete
	case k.IsRune('o'):
		e.visualAnchor, e.cursor = e.cursor, e.visualAnchor
		e.desiredCol = -1
		return parseComplete
	case k.IsRune('O'):
		if e.mode == ModeVisualBlock {
			e.visualAnchor.Col, e.cursor.Col = e.cursor.Col, e.visualAnchor.Col
		} else {
			e.visualAnchor, e.cursor = e.cursor, e.visualAnchor
		}
		e.desiredCol = -1
		return parseComplete
	case k.IsRune(':'):
		e.exitVisual()
		e.openCmdline(':', 0, false)
		e.cmd.text = []rune("'<,'>")
		e.cmd.pos = len(e.cmd.text)
		return parseComplete
	case k.IsRune('d') || k.IsRune('x') || k.Is("delete"):
		e.beginChange()
		sp := e.visualSpan()
		e.exitVisual()
		e.deleteSpan(sp, register)
		return finish(false)
	case k.IsRune('X') || k.IsRune('D'):
		e.beginChange()
		sp := e.visualSpan()
		if e.mode == ModeVisualBlock {
			sp.blockToEnd = true
		} else {
			sp = span{start: Pos{sp.start.Line, 0}, end: Pos{sp.end.Line, 0}, linewise: true}
		}
		e.exitVisual()
		e.deleteSpan(sp, register)
		return finish(false)
	case k.IsRune('y'):
		sp := e.visualSpan()
		e.exitVisual()
		e.yankSpan(sp, register, true)
		e.cursor = e.buf.clampNormal(sp.start)
		return finish(false)
	case k.IsRune('Y'):
		sp := e.visualSpan()
		sp = span{start: Pos{sp.start.Line, 0}, end: Pos{sp.end.Line, 0}, linewise: true}
		e.exitVisual()
		e.yankSpan(sp, register, true)
		e.cursor = e.buf.clampNormal(Pos{sp.start.Line, e.cursor.Col})
		return finish(false)
	case k.IsRune('c') || k.IsRune('s'):
		e.beginChange()
		sp := e.visualSpan()
		e.exitVisual()
		entered := e.runOperator("c", sp, register, false)
		return finish(entered)
	case k.IsRune('C') || k.IsRune('S') || k.IsRune('R'):
		e.beginChange()
		sp := e.visualSpan()
		if e.mode == ModeVisualBlock && k.IsRune('C') {
			sp.blockToEnd = true
			e.exitVisual()
			e.deleteSpan(sp, register)
			e.startBlockInsert(sp.start.Line, sp.end.Line, sp.blockLeft, false)
			return finish(true)
		}
		sp = span{start: Pos{sp.start.Line, 0}, end: Pos{sp.end.Line, 0}, linewise: true}
		e.exitVisual()
		entered := e.runOperator("c", sp, register, false)
		return finish(entered)
	case k.IsRune('J') || (k.IsRune('g') && len(rest) > 1 && rest[1].IsRune('J')):
		e.beginChange()
		start, end, _, _ := e.VisualRange()
		e.exitVisual()
		e.joinLines(start.Line, max(2, end.Line-start.Line+1), k.IsRune('J'))
		return finish(false)
	case k.IsRune('<') || k.IsRune('>'):
		e.beginChange()
		start, end, _, _ := e.VisualRange()
		e.exitVisual()
		e.shiftLines(start.Line, end.Line, k.IsRune('>'), n)
		e.cursor = Pos{start.Line, firstNonBlank(e.buf.Line(start.Line))}
		return finish(false)
	case k.IsRune('='):
		start, _, _, _ := e.VisualRange()
		e.exitVisual()
		e.cursor = Pos{start.Line, firstNonBlank(e.buf.Line(start.Line))}
		return parseComplete
	case k.IsRune('~') || k.IsRune('u') || k.IsRune('U'):
		e.beginChange()
		sp := e.visualSpan()
		e.exitVisual()
		op := map[rune]string{'~': "g~", 'u': "gu", 'U': "gU"}[k.Rune]
		e.transformSpan(sp, op)
		e.cursor = e.buf.clampNormal(sp.start)
		return finish(false)
	case k.IsRune('r'):
		if len(rest) < 2 {
			return parseIncomplete
		}
		if !rest[1].Printable() && !rest[1].Is("enter") {
			e.exitVisual()
			return parseComplete
		}
		e.beginChange()
		sp := e.visualSpan()
		e.exitVisual()
		e.replaceSpanWith(sp, rest[1])
		return finish(false)
	case k.IsRune('p') || k.IsRune('P'):
		e.beginChange()
		sp := e.visualSpan()
		e.exitVisual()
		e.putOverSpan(sp, register, n, k.IsRune('p'))
		return finish(false)
	case k.IsRune('I') || k.IsRune('A'):
		if e.mode == ModeVisualBlock {
			e.beginChange()
			sp := e.visualSpan()
			e.exitVisual()
			col := sp.blockLeft
			appendMode := k.IsRune('A')
			if appendMode {
				col = sp.blockRight + 1
				if sp.blockToEnd {
					col = -1
				}
			}
			e.startBlockInsert(sp.start.Line, sp.end.Line, col, appendMode)
			return finish(true)
		}
		start, end, _, _ := e.VisualRange()
		e.exitVisual()
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
			sp := e.visualSpan()
			e.exitVisual()
			e.transformSpan(sp, "g"+string(second.Rune))
			e.cursor = e.buf.clampNormal(sp.start)
			return finish(false)
		case 'q', 'w':
			e.beginChange()
			sp := e.visualSpan()
			e.exitVisual()
			e.reflowLines(sp.start.Line, sp.end.Line)
			e.cursor = e.buf.clampNormal(Pos{sp.start.Line, 0})
			return finish(false)
		case 'v':
			e.reselectVisual()
			return parseComplete
		case 'd':
			e.exitVisual()
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
	case k.IsRune('i') || k.IsRune('a'):
		obj, st := parseTextObject(rest)
		if st != parseComplete {
			return st
		}
		e.extendByObject(obj, n)
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
		start, end, _, _ := e.VisualRange()
		e.exitVisual()
		delta := n
		if k.IsCtrl('x') {
			delta = -n
		}
		for l := start.Line; l <= end.Line; l++ {
			saved := e.cursor
			e.cursor = Pos{l, 0}
			e.incrementNumber(delta)
			e.cursor = saved
		}
		e.cursor = e.buf.clampNormal(start)
		return finish(false)
	case k.IsRune('z'):
		if len(rest) < 2 {
			return parseIncomplete
		}
		if rest[1].IsRune('f') {
			start, end, _, _ := e.VisualRange()
			e.exitVisual()
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
	e.extendByMotion(m, count, hasCount)
	return parseComplete
}

func (e *Editor) extendByMotion(m motion, count int, hasCount bool) {
	from := e.cursor
	res, ok := e.applyMotion(m, count, hasCount, e.cursor, false)
	if !ok {
		return
	}
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
	if rng.linewise && e.mode == ModeVisual {
		e.mode = ModeVisualLine
	}
	// Extend rather than replace when the selection already spans more than
	// the object.
	if e.visualAnchor.Less(rng.start) && e.visualAnchor != e.cursor {
		e.cursor = rng.end
		return
	}
	e.visualAnchor = rng.start
	e.cursor = rng.end
	e.desiredCol = -1
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
// lands in the unnamed register unless P is used.
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

var _ = unicode.IsUpper
