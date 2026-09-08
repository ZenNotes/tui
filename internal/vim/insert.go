package vim

import (
	"strings"
	"unicode"
)

var autoPairOpeners = map[rune]rune{'(': ')', '[': ']', '{': '}', '`': '`'}
var autoPairQuotes = map[rune]rune{'"': '"', '\'': '\''}

func (e *Editor) insertKey(k Key) {
	e.ClearMessage()
	if e.ins.ctrlO {
		e.ctrlOKey(k)
		return
	}
	if e.ins.ctrlR {
		e.ins.ctrlR = false
		if k.Printable() {
			if reg, ok := e.readRegister(k.Rune); ok {
				e.insertRaw(reg.Text)
			}
		}
		return
	}
	if e.ins.ctrlV {
		e.ins.ctrlV = false
		switch {
		case k.Printable():
			e.insertRaw(string(k.Rune))
		case k.Is("tab"):
			e.insertRaw("\t")
		case k.Is("enter"):
			e.insertRaw("\n")
		}
		return
	}
	if e.ins.completion != nil && !k.IsCtrl('n') && !k.IsCtrl('p') {
		e.ins.completion = nil
	}
	// Insert-escape sequence such as `jk`.
	if seq := []rune(e.opts.InsertEscape); len(seq) == 2 {
		if e.ins.escPending {
			e.ins.escPending = false
			if k.IsRune(seq[1]) {
				e.deleteBefore(1)
				e.finishInsert()
				return
			}
		} else if k.IsRune(seq[0]) {
			e.ins.escPending = true
		}
	}
	switch {
	case k.Is("esc"):
		e.finishInsert()
	case k.IsCtrl('c'):
		e.finishInsert()
	case k.IsCtrl('['):
		e.finishInsert()
	case k.Is("enter") || k.IsCtrl('j') || k.IsCtrl('m'):
		e.insertNewline()
	case k.Is("backspace") || k.IsCtrl('h'):
		e.insertBackspace()
	case k.Is("delete"):
		e.insertDelete()
	case k.Is("shift+tab"):
		before := e.buf.LineLen(e.cursor.Line)
		e.shiftLines(e.cursor.Line, e.cursor.Line, false, 1)
		e.cursor.Col = max(0, e.cursor.Col-(before-e.buf.LineLen(e.cursor.Line)))
	case k.Is("tab"):
		e.insertTab()
	case k.Is("left"):
		if e.cursor.Col > 0 {
			e.cursor.Col--
		}
		e.desiredCol = -1
	case k.Is("right"):
		if e.cursor.Col < e.buf.LineLen(e.cursor.Line) {
			e.cursor.Col++
		}
		e.desiredCol = -1
	case k.Is("up"):
		if e.cursor.Line > 0 {
			if e.desiredCol < 0 {
				e.desiredCol = e.cursor.Col
			}
			e.cursor = Pos{e.cursor.Line - 1, e.colForDesired(e.cursor.Line - 1)}
		}
	case k.Is("down"):
		if e.cursor.Line+1 < e.buf.LineCount() {
			if e.desiredCol < 0 {
				e.desiredCol = e.cursor.Col
			}
			e.cursor = Pos{e.cursor.Line + 1, e.colForDesired(e.cursor.Line + 1)}
		}
	case k.Is("home"):
		e.cursor.Col = 0
	case k.Is("end"):
		e.cursor.Col = e.buf.LineLen(e.cursor.Line)
	case k.Is("pgup"):
		e.scrollPage(-1)
		e.cursor = e.buf.clampInsert(e.cursor)
	case k.Is("pgdn"):
		e.scrollPage(1)
		e.cursor = e.buf.clampInsert(e.cursor)
	case k.IsCtrl('w'):
		e.deleteWordBefore()
	case k.IsCtrl('u'):
		e.deleteBefore(e.cursor.Col)
	case k.IsCtrl('o'):
		e.ins.ctrlO = true
	case k.IsCtrl('r'):
		e.ins.ctrlR = true
	case k.IsCtrl('v') || k.IsCtrl('q'):
		e.ins.ctrlV = true
	case k.IsCtrl('t'):
		e.shiftLines(e.cursor.Line, e.cursor.Line, true, 1)
		e.cursor.Col += e.opts.TabSize
	case k.IsCtrl('d'):
		before := e.buf.LineLen(e.cursor.Line)
		e.shiftLines(e.cursor.Line, e.cursor.Line, false, 1)
		e.cursor.Col = max(0, e.cursor.Col-(before-e.buf.LineLen(e.cursor.Line)))
	case k.IsCtrl('n'):
		e.completeWord(1)
	case k.IsCtrl('p'):
		e.completeWord(-1)
	case k.IsCtrl('e'):
		e.copyFromLine(1)
	case k.IsCtrl('y'):
		e.copyFromLine(-1)
	case k.IsCtrl('a'):
		e.insertRaw(KeysString(e.lastInsertKey))
	case k.Printable():
		e.insertPrintable(k.Rune)
	}
	e.ensureCursorVisible()
}

func (e *Editor) ctrlOKey(k Key) {
	e.mode = ModeNormal
	atEnd := e.cursor.Col >= e.buf.LineLen(e.cursor.Line)
	e.cursor = e.buf.clampNormal(e.cursor)
	e.normalKey(k)
	if len(e.pending) > 0 {
		e.mode = ModeNormal
		return
	}
	e.ins.ctrlO = false
	if e.mode == ModeNormal {
		e.mode = ModeInsert
		// `<C-o>$` lands after the last character, and a cursor that was at
		// the end stays there when the command did not move it.
		if k.IsRune('$') || k.Is("end") || (atEnd && e.cursor.Col == max(0, e.buf.LineLen(e.cursor.Line)-1)) {
			e.cursor.Col = e.buf.LineLen(e.cursor.Line)
		}
		e.cursor = e.buf.clampInsert(e.cursor)
	}
}

// finishInsert leaves insert or replace mode: the cursor steps back one
// column like Vim's, the change commits, and dot-repeat records it.
func (e *Editor) finishInsert() {
	wasReplaying := e.replaying
	if e.blockInsert != nil {
		e.finishBlockInsert()
	}
	e.marks['^'] = e.cursor
	if e.cursor.Col > 0 {
		e.cursor.Col--
	}
	e.mode = ModeNormal
	e.cursor = e.buf.clampNormal(e.cursor)
	e.ins = insertState{}
	e.commitChange()
	if e.recordingIns && !wasReplaying {
		keys := e.changeKeys
		e.last = lastChange{keys: stripCount(keys)}
		_, count, hasCount, _, _ := splitPrefix(keys)
		if hasCount {
			e.last.count = count
		}
		for i := 0; i+1 < len(keys); i++ {
			if keys[i].IsRune('"') && keys[i+1].Printable() {
				e.last.register = keys[i+1].Rune
				break
			}
		}
	}
	e.recordingIns = false
	e.changeKeys = nil
	e.desiredCol = -1
	e.ensureCursorVisible()
}

func (e *Editor) insertRaw(text string) {
	e.cursor = e.buf.InsertText(e.cursor, text)
}

func (e *Editor) deleteBefore(n int) {
	if n <= 0 || e.cursor.Col == 0 {
		return
	}
	start := Pos{e.cursor.Line, max(0, e.cursor.Col-n)}
	e.buf.DeleteRange(start, e.cursor)
	e.cursor = start
}

func (e *Editor) insertPrintable(r rune) {
	if e.mode == ModeReplace {
		line := cloneRunes(e.buf.Line(e.cursor.Line))
		if e.cursor.Col < len(line) {
			e.ins.replaced = append(e.ins.replaced, line[e.cursor.Col])
			line[e.cursor.Col] = r
			e.buf.ReplaceLine(e.cursor.Line, line)
		} else {
			e.ins.replaced = append(e.ins.replaced, -1)
			e.buf.ReplaceLine(e.cursor.Line, append(line, r))
		}
		e.cursor.Col++
		return
	}
	line := e.buf.Line(e.cursor.Line)
	next := rune(0)
	if e.cursor.Col < len(line) {
		next = line[e.cursor.Col]
	}
	// Typing a closer that is already there steps over it.
	if e.opts.AutoPairs && (r == ')' || r == ']' || r == '}' || r == '`') && next == r {
		e.cursor.Col++
		return
	}
	if (e.opts.AutoPairQuotes || e.opts.AutoPairs) && (r == '"' || r == '\'') && next == r && e.ins.pairedInsert.Line == e.cursor.Line {
		e.cursor.Col++
		return
	}
	e.insertRaw(string(r))
	if closer, ok := autoPairOpeners[r]; ok && e.opts.AutoPairs && (next == 0 || isBlank(next) || next == ')' || next == ']' || next == '}') {
		if r != '`' || next == 0 || isBlank(next) {
			e.buf.InsertText(e.cursor, string(closer))
			e.ins.pairedInsert = e.cursor
		}
	} else if closer, ok := autoPairQuotes[r]; ok && e.opts.AutoPairQuotes && (next == 0 || isBlank(next)) {
		prev := rune(0)
		if e.cursor.Col >= 2 {
			prev = line[e.cursor.Col-2]
		}
		if prev == 0 || isBlank(prev) || prev == '(' || prev == '[' || prev == '{' {
			e.buf.InsertText(e.cursor, string(closer))
			e.ins.pairedInsert = e.cursor
		}
	}
	e.applyTextReplacement()
}

// applyTextReplacement expands a configured trigger that just completed
// before the cursor; the longest matching trigger wins.
func (e *Editor) applyTextReplacement() {
	if !e.opts.TextReplacementsEnabled || len(e.opts.TextReplacements) == 0 {
		return
	}
	line := e.buf.Line(e.cursor.Line)
	before := string(line[:e.cursor.Col])
	best := ""
	for trigger := range e.opts.TextReplacements {
		if trigger != "" && strings.HasSuffix(before, trigger) && len(trigger) > len(best) {
			best = trigger
		}
	}
	if best == "" {
		return
	}
	replacement := e.opts.TextReplacements[best]
	start := Pos{e.cursor.Line, e.cursor.Col - len([]rune(best))}
	e.buf.DeleteRange(start, e.cursor)
	e.cursor = e.buf.InsertText(start, replacement)
}

func (e *Editor) insertNewline() {
	line := e.buf.Line(e.cursor.Line)
	if e.mode == ModeReplace {
		e.insertRaw("\n")
		return
	}
	if e.opts.MarkdownLists && e.cursor.Col >= len(line) && isEmptyListItem(line) {
		// Enter on an empty item ends the list.
		e.buf.ReplaceLine(e.cursor.Line, leadingWhitespace(line))
		e.cursor.Col = e.buf.LineLen(e.cursor.Line)
		return
	}
	prefix := string(leadingWhitespace(line))
	if e.opts.MarkdownLists && e.cursor.Col > 0 {
		if cont, ok := listContinuation(line); ok {
			prefix = cont
		}
	}
	// Auto-pair: Enter between brackets opens a block.
	if e.opts.AutoPairs && e.cursor.Col > 0 && e.cursor.Col < len(line) {
		open, close := line[e.cursor.Col-1], line[e.cursor.Col]
		if (open == '(' && close == ')') || (open == '[' && close == ']') || (open == '{' && close == '}') {
			indent := string(leadingWhitespace(line))
			e.insertRaw("\n" + indent + strings.Repeat(" ", e.opts.TabSize) + "\n" + indent)
			e.cursor = Pos{e.cursor.Line - 1, len([]rune(indent)) + e.opts.TabSize}
			return
		}
	}
	e.insertRaw("\n" + prefix)
	if e.opts.MarkdownLists {
		e.renumberList(e.cursor.Line)
	}
}

func (e *Editor) insertBackspace() {
	if e.mode == ModeReplace {
		if e.cursor.Col == 0 {
			return
		}
		if n := len(e.ins.replaced); n > 0 {
			orig := e.ins.replaced[n-1]
			e.ins.replaced = e.ins.replaced[:n-1]
			line := cloneRunes(e.buf.Line(e.cursor.Line))
			e.cursor.Col--
			if orig < 0 {
				line = line[:e.cursor.Col]
			} else if e.cursor.Col < len(line) {
				line[e.cursor.Col] = orig
			}
			e.buf.ReplaceLine(e.cursor.Line, line)
			return
		}
		e.cursor.Col--
		return
	}
	if e.cursor.Col == 0 {
		if e.cursor.Line == 0 {
			return
		}
		prevLen := e.buf.LineLen(e.cursor.Line - 1)
		e.buf.DeleteRange(Pos{e.cursor.Line - 1, prevLen}, Pos{e.cursor.Line, 0})
		e.cursor = Pos{e.cursor.Line - 1, prevLen}
		return
	}
	line := e.buf.Line(e.cursor.Line)
	if e.opts.AutoPairs && e.cursor.Col < len(line) {
		prev, next := line[e.cursor.Col-1], line[e.cursor.Col]
		if closer, ok := autoPairOpeners[prev]; ok && closer == next {
			e.buf.DeleteRange(Pos{e.cursor.Line, e.cursor.Col - 1}, Pos{e.cursor.Line, e.cursor.Col + 1})
			e.cursor.Col--
			return
		}
		if closer, ok := autoPairQuotes[prev]; ok && closer == next && e.ins.pairedInsert == e.cursor {
			e.buf.DeleteRange(Pos{e.cursor.Line, e.cursor.Col - 1}, Pos{e.cursor.Line, e.cursor.Col + 1})
			e.cursor.Col--
			return
		}
	}
	e.deleteBefore(1)
}

func (e *Editor) insertDelete() {
	line := e.buf.Line(e.cursor.Line)
	if e.cursor.Col < len(line) {
		e.buf.DeleteRange(e.cursor, Pos{e.cursor.Line, e.cursor.Col + 1})
		return
	}
	if e.cursor.Line+1 < e.buf.LineCount() {
		e.buf.DeleteRange(e.cursor, Pos{e.cursor.Line + 1, 0})
	}
}

func (e *Editor) insertTab() {
	line := e.buf.Line(e.cursor.Line)
	if e.opts.MarkdownLists && listMarkerRe.MatchString(string(line)) && e.cursor.Col <= firstNonBlankInsert(line)+len(listMarkerRe.FindString(string(line)))-len(string(leadingWhitespace(line))) {
		e.shiftLines(e.cursor.Line, e.cursor.Line, true, 1)
		e.cursor.Col += e.opts.TabSize
		return
	}
	width := e.opts.TabSize - (e.cursor.Col % e.opts.TabSize)
	e.insertRaw(strings.Repeat(" ", width))
}

func (e *Editor) deleteWordBefore() {
	line := e.buf.Line(e.cursor.Line)
	col := e.cursor.Col
	if col == 0 {
		e.insertBackspace()
		return
	}
	i := col
	for i > 0 && isBlank(line[i-1]) {
		i--
	}
	if i > 0 {
		cls := charClass(line[i-1], false)
		for i > 0 && charClass(line[i-1], false) == cls {
			i--
		}
	}
	e.buf.DeleteRange(Pos{e.cursor.Line, i}, e.cursor)
	e.cursor.Col = i
}

func (e *Editor) copyFromLine(dir int) {
	src := e.cursor.Line + dir
	if src < 0 || src >= e.buf.LineCount() {
		return
	}
	line := e.buf.Line(src)
	if e.cursor.Col < len(line) {
		e.insertRaw(string(line[e.cursor.Col]))
	}
}

// completeWord cycles buffer words starting with the prefix before the
// cursor (Ctrl-N / Ctrl-P).
func (e *Editor) completeWord(dir int) {
	if e.ins.completion == nil {
		line := e.buf.Line(e.cursor.Line)
		start := e.cursor.Col
		for start > 0 && isWordRune(line[start-1]) {
			start--
		}
		prefix := string(line[start:e.cursor.Col])
		if prefix == "" {
			return
		}
		seen := map[string]bool{prefix: true}
		candidates := []string{}
		for l := 0; l < e.buf.LineCount(); l++ {
			text := e.buf.Line(l)
			for i := 0; i < len(text); i++ {
				if !isWordRune(text[i]) || (i > 0 && isWordRune(text[i-1])) {
					continue
				}
				j := i
				for j < len(text) && isWordRune(text[j]) {
					j++
				}
				word := string(text[i:j])
				if strings.HasPrefix(word, prefix) && !seen[word] {
					seen[word] = true
					candidates = append(candidates, word)
				}
				i = j
			}
		}
		if len(candidates) == 0 {
			e.setError("Pattern not found")
			return
		}
		candidates = append(candidates, prefix)
		e.ins.completion = &completionState{prefix: prefix, candidates: candidates, idx: -1, start: Pos{e.cursor.Line, start}}
	}
	c := e.ins.completion
	c.idx = (c.idx + dir + len(c.candidates)) % len(c.candidates)
	e.buf.DeleteRange(c.start, e.cursor)
	e.cursor = e.buf.InsertText(c.start, c.candidates[c.idx])
	if c.idx == len(c.candidates)-1 {
		e.setMsg("Back at original")
	} else {
		e.setMsg("match " + itoa(c.idx+1) + " of " + itoa(len(c.candidates)-1))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// --- plain (non Vim) editing ---

func (e *Editor) plainKey(k Key) bool {
	e.ClearMessage()
	commitAfter := true
	e.beginChange()
	switch {
	case k.Printable():
		e.insertPrintable(k.Rune)
		commitAfter = false
	case k.Is("enter"):
		e.insertNewline()
	case k.Is("backspace") || k.IsCtrl('h'):
		e.insertBackspace()
		commitAfter = false
	case k.Is("delete"):
		e.insertDelete()
	case k.Is("shift+tab"):
		e.shiftLines(e.cursor.Line, e.cursor.Line, false, 1)
		e.cursor = e.buf.clampInsert(e.cursor)
	case k.Is("tab"):
		e.insertTab()
	case k.Is("left"):
		if e.cursor.Col > 0 {
			e.cursor.Col--
		} else if e.cursor.Line > 0 {
			e.cursor = Pos{e.cursor.Line - 1, e.buf.LineLen(e.cursor.Line - 1)}
		}
		e.desiredCol = -1
	case k.Is("right"):
		if e.cursor.Col < e.buf.LineLen(e.cursor.Line) {
			e.cursor.Col++
		} else if e.cursor.Line+1 < e.buf.LineCount() {
			e.cursor = Pos{e.cursor.Line + 1, 0}
		}
		e.desiredCol = -1
	case k.Is("up"):
		if e.cursor.Line > 0 {
			if e.desiredCol < 0 {
				e.desiredCol = e.cursor.Col
			}
			e.cursor = Pos{e.cursor.Line - 1, e.colForDesired(e.cursor.Line - 1)}
		}
	case k.Is("down"):
		if e.cursor.Line+1 < e.buf.LineCount() {
			if e.desiredCol < 0 {
				e.desiredCol = e.cursor.Col
			}
			e.cursor = Pos{e.cursor.Line + 1, e.colForDesired(e.cursor.Line + 1)}
		}
	case k.Is("home") || k.IsCtrl('a'):
		e.cursor.Col = 0
	case k.Is("end") || k.IsCtrl('e'):
		e.cursor.Col = e.buf.LineLen(e.cursor.Line)
	case k.Is("pgup"):
		e.scrollPage(-1)
		e.cursor = e.buf.clampInsert(e.cursor)
	case k.Is("pgdn"):
		e.scrollPage(1)
		e.cursor = e.buf.clampInsert(e.cursor)
	case k.IsCtrl('z'):
		e.commitChange()
		e.Undo()
		e.cursor = e.buf.clampInsert(e.cursor)
		return true
	case k.IsCtrl('y'):
		e.commitChange()
		e.Redo()
		e.cursor = e.buf.clampInsert(e.cursor)
		return true
	case k.IsCtrl('w'):
		e.deleteWordBefore()
	case k.IsCtrl('k'):
		line := e.buf.Line(e.cursor.Line)
		if e.cursor.Col < len(line) {
			e.buf.DeleteRange(e.cursor, Pos{e.cursor.Line, len(line)})
		} else {
			e.insertDelete()
		}
	default:
		e.commitChange()
		return false
	}
	if commitAfter {
		e.commitChange()
	}
	e.ensureCursorVisible()
	return true
}

var _ = unicode.IsLetter
