package vim

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// span is a resolved operator target.
type span struct {
	start, end Pos // end exclusive for charwise; for linewise, lines start.Line..end.Line
	linewise   bool
	blockwise  bool
	blockLeft  int
	blockRight int // inclusive column
	blockToEnd bool
}

// applyOperatorToMotion resolves the motion under Vim's operator rules and
// applies the operator. Returns true when it entered insert mode.
func (e *Editor) applyOperatorToMotion(op string, m motion, count int, hasCount bool, register rune) bool {
	from := e.cursor
	// `cw` on a word acts like `ce`.
	if op == "c" && (m.kind == "w" || m.kind == "W") {
		if r, ok := e.runeAt(from); ok && !isBlank(r) {
			m.kind = map[string]string{"w": "e", "W": "E"}[m.kind]
		}
	}
	res, ok := e.applyMotion(m, count, hasCount, from, true)
	if !ok {
		return false
	}
	return e.applyOperatorResult(op, from, res, register, m.kind == "j")
}

// applyOperatorResult applies op between the cursor and a motion result,
// honoring Vim's exclusive and linewise adjustments.
func (e *Editor) applyOperatorResult(op string, from Pos, res motionResult, register rune, isJ bool) bool {
	start, end := from, res.pos
	if end.Less(start) {
		start, end = end, start
	}
	sp := span{start: start, end: end, linewise: res.linewise}
	if !res.linewise {
		if res.inclusive {
			sp.end = e.stepInsertPos(sp.end)
		} else if sp.end.Col == 0 && sp.end.Line > sp.start.Line && !isJ {
			// Exclusive motion ending in column 0: pull back to the end of the
			// previous line; linewise when it started at or before the first
			// non-blank.
			if sp.start.Col <= firstNonBlankInsert(e.buf.Line(sp.start.Line)) {
				sp.linewise = true
				sp.end = Pos{sp.end.Line - 1, 0}
			} else {
				sp.end = Pos{sp.end.Line - 1, e.buf.LineLen(sp.end.Line - 1)}
			}
		}
	}
	if res.jump {
		e.pushJump()
	}
	return e.runOperator(op, sp, register, res.keepCol)
}

// stepInsertPos advances an inclusive end to its exclusive form.
func (e *Editor) stepInsertPos(p Pos) Pos {
	if p.Col < e.buf.LineLen(p.Line) {
		return Pos{p.Line, p.Col + 1}
	}
	return p
}

func (e *Editor) applyOperatorToObject(op string, obj textObject, count int, register rune) bool {
	rng := e.textObjectRange(obj, count)
	if !rng.ok {
		return false
	}
	sp := span{start: rng.start, end: rng.end, linewise: rng.linewise}
	if !rng.linewise {
		if rng.end.Less(rng.start) {
			// Empty object (e.g. `di"` on `""`): nothing to do but c enters insert.
			sp.end = sp.start
		} else {
			sp.end = e.stepInsertPos(rng.end)
		}
	}
	return e.runOperator(op, sp, register, false)
}

func (e *Editor) applyLinewiseOperator(op string, count int, register rune) bool {
	start := e.cursor.Line
	end := min(e.buf.LineCount()-1, start+max(1, count)-1)
	if end, ok := e.folds[start]; ok {
		_ = end
	}
	sp := span{start: Pos{start, 0}, end: Pos{end, 0}, linewise: true}
	return e.runOperator(op, sp, register, false)
}

// runOperator applies op to a span. Returns true when insert mode begins.
func (e *Editor) runOperator(op string, sp span, register rune, keepCol bool) bool {
	switch op {
	case "y":
		e.yankSpan(sp, register, true)
		if sp.linewise {
			e.cursor = Pos{sp.start.Line, e.cursor.Col}
			if !keepCol {
				e.cursor = e.buf.clampNormal(Pos{sp.start.Line, min(e.cursor.Col, sp.start.Col)})
				if sp.start.Line != e.cursor.Line || true {
					e.cursor = e.buf.clampNormal(Pos{sp.start.Line, e.cursor.Col})
				}
			}
		} else {
			e.cursor = e.buf.clampNormal(sp.start)
		}
		return false
	case "d":
		e.deleteSpan(sp, register)
		return false
	case "c":
		if sp.linewise {
			indent := leadingWhitespace(e.buf.Line(sp.start.Line))
			e.yankSpan(sp, register, false)
			e.buf.ReplaceLines(sp.start.Line, sp.end.Line, []string{string(indent)})
			e.startInsert(Pos{sp.start.Line, len(indent)})
			return true
		}
		if sp.blockwise {
			e.deleteSpan(sp, register)
			e.startBlockInsert(sp.start.Line, sp.end.Line, sp.blockLeft, false)
			return true
		}
		e.deleteSpan(sp, register)
		e.startInsert(sp.start)
		return true
	case "<", ">":
		e.shiftLines(sp.start.Line, sp.end.Line, op == ">", 1)
		e.cursor = Pos{sp.start.Line, firstNonBlank(e.buf.Line(sp.start.Line))}
		return false
	case "=":
		e.cursor = Pos{sp.start.Line, firstNonBlank(e.buf.Line(sp.start.Line))}
		return false
	case "gu", "gU", "g~", "g?":
		e.transformSpan(sp, op)
		e.cursor = e.buf.clampNormal(sp.start)
		return false
	case "gq", "gw":
		e.reflowLines(sp.start.Line, sp.end.Line)
		if op == "gq" {
			e.cursor = e.buf.clampNormal(Pos{sp.start.Line, 0})
			e.cursor.Col = firstNonBlank(e.buf.Line(e.cursor.Line))
		}
		return false
	}
	return false
}

// spanText reads a span as register content.
func (e *Editor) spanText(sp span) Register {
	if sp.linewise {
		var sb strings.Builder
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			sb.WriteString(e.buf.LineString(l))
			sb.WriteByte('\n')
		}
		return Register{Text: sb.String(), Linewise: true}
	}
	if sp.blockwise {
		lines := []string{}
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			line := e.buf.Line(l)
			right := sp.blockRight
			if sp.blockToEnd {
				right = len(line) - 1
			}
			from := min(sp.blockLeft, len(line))
			to := min(right+1, len(line))
			if to < from {
				to = from
			}
			lines = append(lines, string(line[from:to]))
		}
		return Register{Text: strings.Join(lines, "\n"), Blockwise: true}
	}
	return Register{Text: e.buf.Range(sp.start, sp.end)}
}

func (e *Editor) yankSpan(sp span, register rune, isYank bool) {
	reg := e.spanText(sp)
	e.storeRegister(register, reg, isYank)
	if isYank {
		if sp.linewise {
			n := sp.end.Line - sp.start.Line + 1
			if n > 2 {
				e.setMsg(strconv.Itoa(n) + " lines yanked")
			}
		} else if sp.blockwise {
			e.setMsg("block of " + strconv.Itoa(sp.end.Line-sp.start.Line+1) + " lines yanked")
		}
	}
	e.marks['['] = sp.start
	e.marks[']'] = sp.end
}

func (e *Editor) deleteSpan(sp span, register rune) {
	e.yankSpan(sp, register, false)
	switch {
	case sp.linewise:
		e.buf.DeleteLines(sp.start.Line, sp.end.Line)
		line := min(sp.start.Line, e.buf.LineCount()-1)
		e.cursor = Pos{line, firstNonBlank(e.buf.Line(line))}
		n := sp.end.Line - sp.start.Line + 1
		if n > 2 {
			e.setMsg(strconv.Itoa(n) + " fewer lines")
		}
	case sp.blockwise:
		for l := sp.start.Line; l <= sp.end.Line; l++ {
			line := e.buf.Line(l)
			right := sp.blockRight
			if sp.blockToEnd {
				right = len(line) - 1
			}
			from := min(sp.blockLeft, len(line))
			to := min(right+1, len(line))
			if to <= from {
				continue
			}
			next := append(cloneRunes(line[:from]), line[to:]...)
			e.buf.ReplaceLine(l, next)
		}
		e.cursor = e.buf.clampNormal(Pos{sp.start.Line, sp.blockLeft})
	default:
		e.buf.DeleteRange(sp.start, sp.end)
		e.cursor = e.buf.clampNormal(sp.start)
	}
}

func (e *Editor) transformSpan(sp span, op string) {
	transform := func(r rune) rune {
		switch op {
		case "gu":
			return unicode.ToLower(r)
		case "gU":
			return unicode.ToUpper(r)
		case "g~":
			if unicode.IsUpper(r) {
				return unicode.ToLower(r)
			}
			return unicode.ToUpper(r)
		case "g?":
			return rot13(r)
		}
		return r
	}
	apply := func(l, from, to int) {
		line := cloneRunes(e.buf.Line(l))
		to = min(to, len(line))
		for i := max(0, from); i < to; i++ {
			line[i] = transform(line[i])
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
}

func rot13(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z':
		return 'a' + (r-'a'+13)%26
	case r >= 'A' && r <= 'Z':
		return 'A' + (r-'A'+13)%26
	}
	return r
}

// shiftLines indents or outdents whole lines by tabsize columns.
func (e *Editor) shiftLines(from, to int, right bool, times int) {
	width := e.opts.TabSize * times
	for l := from; l <= to; l++ {
		line := e.buf.Line(l)
		if len(line) == 0 {
			continue
		}
		if right {
			e.buf.ReplaceLine(l, append([]rune(strings.Repeat(" ", width)), line...))
			continue
		}
		remove := 0
		for remove < len(line) && remove < width && line[remove] == ' ' {
			remove++
		}
		if remove == 0 && len(line) > 0 && line[0] == '\t' {
			remove = 1
		}
		e.buf.ReplaceLine(l, line[remove:])
	}
}

// --- simple edits ---

func (e *Editor) deleteChars(n int, register rune) {
	line := e.buf.Line(e.cursor.Line)
	if len(line) == 0 {
		return
	}
	end := min(e.cursor.Col+n, len(line))
	e.deleteSpan(span{start: e.cursor, end: Pos{e.cursor.Line, end}}, register)
}

func (e *Editor) deleteCharsBack(n int, register rune) {
	if e.cursor.Col == 0 {
		return
	}
	start := max(0, e.cursor.Col-n)
	e.deleteSpan(span{start: Pos{e.cursor.Line, start}, end: e.cursor}, register)
	e.cursor = e.buf.clampNormal(Pos{e.cursor.Line, start})
}

func (e *Editor) deleteToEnd(n int, register rune) {
	endLine := min(e.buf.LineCount()-1, e.cursor.Line+n-1)
	sp := span{start: e.cursor, end: Pos{endLine, e.buf.LineLen(endLine)}}
	e.deleteSpan(sp, register)
	e.cursor = e.buf.clampNormal(Pos{e.cursor.Line, e.cursor.Col})
	if e.cursor.Col > 0 && e.cursor.Col >= e.buf.LineLen(e.cursor.Line) {
		e.cursor.Col = max(0, e.buf.LineLen(e.cursor.Line)-1)
	}
}

func (e *Editor) changeLines(n int, register rune) {
	end := min(e.buf.LineCount()-1, e.cursor.Line+n-1)
	e.runOperator("c", span{start: Pos{e.cursor.Line, 0}, end: Pos{end, 0}, linewise: true}, register, false)
}

func (e *Editor) yankLines(from, to int, register rune) {
	e.yankSpan(span{start: Pos{from, 0}, end: Pos{to, 0}, linewise: true}, register, true)
}

func (e *Editor) toggleCaseChars(n int) {
	line := cloneRunes(e.buf.Line(e.cursor.Line))
	if len(line) == 0 {
		return
	}
	end := min(e.cursor.Col+n, len(line))
	for i := e.cursor.Col; i < end; i++ {
		if unicode.IsUpper(line[i]) {
			line[i] = unicode.ToLower(line[i])
		} else {
			line[i] = unicode.ToUpper(line[i])
		}
	}
	e.buf.ReplaceLine(e.cursor.Line, line)
	e.cursor = e.buf.clampNormal(Pos{e.cursor.Line, end})
}

func (e *Editor) replaceChars(k Key, n int) {
	line := e.buf.Line(e.cursor.Line)
	if e.cursor.Col+n > len(line) {
		return
	}
	if k.Is("enter") {
		head := cloneRunes(line[:e.cursor.Col])
		tail := cloneRunes(line[e.cursor.Col+n:])
		e.buf.ReplaceLine(e.cursor.Line, head)
		e.buf.InsertLines(e.cursor.Line+1, []string{string(tail)})
		e.cursor = Pos{e.cursor.Line + 1, 0}
		return
	}
	if !k.Printable() {
		return
	}
	next := cloneRunes(line)
	for i := 0; i < n; i++ {
		next[e.cursor.Col+i] = k.Rune
	}
	e.buf.ReplaceLine(e.cursor.Line, next)
	e.cursor.Col += n - 1
}

// joinLines joins count lines starting at line; spaces controls the single
// space Vim inserts (J) versus none (gJ).
func (e *Editor) joinLines(line, count int, spaces bool) {
	last := min(e.buf.LineCount()-1, line+count-1)
	if last == line {
		return
	}
	result := cloneRunes(e.buf.Line(line))
	joinCol := 0
	for l := line + 1; l <= last; l++ {
		next := e.buf.Line(l)
		if spaces {
			trimmed := next[firstNonBlankInsert(next):]
			result = trimRightBlanks(result)
			joinCol = len(result)
			if len(trimmed) > 0 && len(result) > 0 && trimmed[0] != ')' {
				result = append(result, ' ')
				joinCol = len(result) - 1
			}
			result = append(result, trimmed...)
		} else {
			joinCol = len(result)
			result = append(result, next...)
		}
	}
	e.buf.ReplaceLine(line, result)
	e.buf.DeleteLines(line+1, last)
	e.cursor = e.buf.clampNormal(Pos{line, joinCol})
}

func trimRightBlanks(r []rune) []rune {
	n := len(r)
	for n > 0 && isBlank(r[n-1]) {
		n--
	}
	return r[:n]
}

var numberRe = regexp.MustCompile(`-?\d+`)

// incrementNumber adds delta to the number at or after the cursor.
func (e *Editor) incrementNumber(delta int) {
	line := e.buf.LineString(e.cursor.Line)
	runes := e.buf.Line(e.cursor.Line)
	byteCol := len(string(runes[:min(e.cursor.Col, len(runes))]))
	locs := numberRe.FindAllStringIndex(line, -1)
	for _, loc := range locs {
		if loc[1] <= byteCol {
			continue
		}
		value, err := strconv.Atoi(line[loc[0]:loc[1]])
		if err != nil {
			continue
		}
		next := strconv.Itoa(value + delta)
		updated := line[:loc[0]] + next + line[loc[1]:]
		e.buf.ReplaceLine(e.cursor.Line, []rune(updated))
		e.cursor.Col = len([]rune(updated[:loc[0]+len(next)])) - 1
		return
	}
}

// --- put ---

func (e *Editor) put(register rune, n int, after bool, cursorAfter bool) {
	reg, ok := e.readRegister(register)
	if !ok || reg.Text == "" {
		if register == 0 || register == '"' {
			e.setError("E353: Nothing in register \"")
		} else {
			e.setError("E353: Nothing in register " + string(register))
		}
		return
	}
	switch {
	case reg.Linewise:
		text := strings.TrimSuffix(reg.Text, "\n")
		lines := strings.Split(text, "\n")
		all := []string{}
		for i := 0; i < n; i++ {
			all = append(all, lines...)
		}
		at := e.cursor.Line
		if after {
			at = e.nextVisibleLine(e.cursor.Line)
		}
		e.buf.InsertLines(at, all)
		if cursorAfter {
			e.cursor = e.buf.clampNormal(Pos{min(at+len(all), e.buf.LineCount()-1), 0})
		} else {
			e.cursor = Pos{at, firstNonBlank(e.buf.Line(at))}
		}
		if len(all) > 2 {
			e.setMsg(strconv.Itoa(len(all)) + " more lines")
		}
	case reg.Blockwise:
		lines := strings.Split(reg.Text, "\n")
		col := e.cursor.Col
		if after && e.buf.LineLen(e.cursor.Line) > 0 {
			col++
		}
		width := 0
		for _, l := range lines {
			width = max(width, len([]rune(l)))
		}
		for i, l := range lines {
			lineIdx := e.cursor.Line + i
			if lineIdx >= e.buf.LineCount() {
				e.buf.InsertLines(e.buf.LineCount(), []string{""})
			}
			cur := cloneRunes(e.buf.Line(lineIdx))
			for len(cur) < col {
				cur = append(cur, ' ')
			}
			chunk := []rune(strings.Repeat(l, n))
			if n > 1 {
				chunk = []rune{}
				for k := 0; k < n; k++ {
					pad := []rune(l)
					for len(pad) < width {
						pad = append(pad, ' ')
					}
					chunk = append(chunk, pad...)
				}
			}
			next := append(cloneRunes(cur[:col]), chunk...)
			next = append(next, cur[col:]...)
			e.buf.ReplaceLine(lineIdx, next)
		}
		e.cursor = e.buf.clampNormal(Pos{e.cursor.Line, col})
	default:
		text := strings.Repeat(reg.Text, n)
		p := e.cursor
		if after && e.buf.LineLen(p.Line) > 0 {
			p.Col++
		}
		end := e.buf.InsertText(p, text)
		if cursorAfter {
			e.cursor = e.buf.clampNormal(end)
			if end.Col >= e.buf.LineLen(end.Line) && end.Line+1 < e.buf.LineCount() && strings.HasSuffix(text, "\n") {
				e.cursor = Pos{end.Line, 0}
			} else {
				e.cursor = e.buf.clampInsert(end)
				e.cursor = e.buf.clampNormal(e.cursor)
				if end.Col < e.buf.LineLen(end.Line) {
					e.cursor = end
				}
			}
		} else if strings.Contains(text, "\n") {
			e.cursor = p
		} else {
			e.cursor = e.buf.clampNormal(Pos{end.Line, end.Col - 1})
		}
	}
	e.marks['['] = e.cursor
}

// --- insert entry helpers ---

func (e *Editor) startInsert(p Pos) {
	e.beginChange()
	e.mode = ModeInsert
	e.cursor = e.buf.clampInsert(p)
	e.ins = insertState{start: e.cursor}
	e.desiredCol = -1
}

func (e *Editor) openLine(above bool) {
	cur := e.buf.Line(e.cursor.Line)
	indent := leadingWhitespace(cur)
	prefix := string(indent)
	if e.opts.MarkdownLists && !above {
		if cont, ok := listContinuation(cur); ok {
			prefix = cont
		}
	} else if e.opts.MarkdownLists && above {
		if cont, ok := listContinuation(cur); ok {
			prefix = cont
		}
	}
	at := e.cursor.Line
	if !above {
		at = e.nextVisibleLine(e.cursor.Line)
		if end, ok := e.folds[e.cursor.Line]; ok {
			at = end + 1
		}
	}
	e.buf.InsertLines(at, []string{prefix})
	if !above && e.opts.MarkdownLists {
		e.renumberList(at)
	}
	e.mode = ModeInsert
	e.cursor = Pos{at, len([]rune(prefix))}
	e.ins = insertState{start: e.cursor}
	e.desiredCol = -1
}

var listMarkerRe = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])(\s+)(\[[ xX/>-]\]\s+)?`)
var quoteMarkerRe = regexp.MustCompile(`^(\s*>\s?)+`)

// listContinuation is the marker a new line below a list item should start
// with: the same bullet, the next number, a fresh unchecked box.
func listContinuation(line []rune) (string, bool) {
	s := string(line)
	m := listMarkerRe.FindStringSubmatch(s)
	if m == nil {
		if q := quoteMarkerRe.FindString(s); q != "" && strings.TrimSpace(s) != strings.TrimSpace(q) {
			return q, true
		}
		return "", false
	}
	indent, marker, space, box := m[1], m[2], m[3], m[4]
	if strings.TrimSpace(s[len(m[0]):]) == "" {
		// Empty item: the continuation is nothing (the caller ends the list).
		return "", false
	}
	if marker[len(marker)-1] == '.' || marker[len(marker)-1] == ')' {
		n, _ := strconv.Atoi(marker[:len(marker)-1])
		marker = strconv.Itoa(n+1) + marker[len(marker)-1:]
	}
	if box != "" {
		box = "[ ] "
	}
	return indent + marker + space + box, true
}

// isEmptyListItem is true for a bare marker like `- ` or `1. [ ] `.
func isEmptyListItem(line []rune) bool {
	s := string(line)
	m := listMarkerRe.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	return strings.TrimSpace(s[len(m[0]):]) == ""
}

var orderedMarkerRe = regexp.MustCompile(`^(\s*)(\d+)([.)])(\s+)`)

// renumberList renumbers an ordered list around line so numbers stay in
// sequence after an insertion.
func (e *Editor) renumberList(line int) {
	m := orderedMarkerRe.FindStringSubmatch(e.buf.LineString(line))
	if m == nil {
		return
	}
	indent := m[1]
	start := line
	for start > 0 {
		pm := orderedMarkerRe.FindStringSubmatch(e.buf.LineString(start - 1))
		if pm == nil || pm[1] != indent {
			break
		}
		start--
	}
	num := 0
	if sm := orderedMarkerRe.FindStringSubmatch(e.buf.LineString(start)); sm != nil {
		num, _ = strconv.Atoi(sm[2])
	}
	for l := start; l < e.buf.LineCount(); l++ {
		lm := orderedMarkerRe.FindStringSubmatch(e.buf.LineString(l))
		if lm == nil || lm[1] != indent {
			break
		}
		want := strconv.Itoa(num)
		if lm[2] != want {
			s := e.buf.LineString(l)
			e.buf.ReplaceLine(l, []rune(lm[1]+want+lm[3]+lm[4]+s[len(lm[0]):]))
		}
		num++
	}
}

// --- reflow (gq / gw) ---

var reflowSkipRe = regexp.MustCompile(`^\s*(#{1,6}\s|\||>|[-*+]\s|\d+[.)]\s|` + "```" + `|~~~|\$\$|---\s*$|\*\*\*\s*$)`)

// reflowLines joins the hard-wrapped lines of every paragraph in the range
// into one line. Headings, lists, tables, quotes, code, math and explicit
// line breaks are left alone.
func (e *Editor) reflowLines(from, to int) {
	to = min(to, e.buf.LineCount()-1)
	l := from
	for l <= to {
		s := e.buf.LineString(l)
		if strings.TrimSpace(s) == "" || reflowSkipRe.MatchString(s) {
			l++
			continue
		}
		end := l
		for end+1 <= to {
			next := e.buf.LineString(end + 1)
			cur := e.buf.LineString(end)
			if strings.TrimSpace(next) == "" || reflowSkipRe.MatchString(next) {
				break
			}
			if strings.HasSuffix(cur, "  ") || strings.HasSuffix(cur, "\\") || strings.HasSuffix(strings.TrimSpace(cur), "<br>") {
				break
			}
			end++
		}
		if end > l {
			parts := []string{strings.TrimRight(s, " \t")}
			for k := l + 1; k <= end; k++ {
				parts = append(parts, strings.TrimSpace(e.buf.LineString(k)))
			}
			e.buf.ReplaceLine(l, []rune(strings.Join(parts, " ")))
			e.buf.DeleteLines(l+1, end)
			to -= end - l
		}
		l++
	}
}

// ReflowParagraph joins the paragraph under the cursor (Alt+Q).
func (e *Editor) ReflowParagraph() {
	e.beginChange()
	rng := e.paragraphObject(true, 1)
	if rng.ok {
		e.reflowLines(rng.start.Line, rng.end.Line)
	}
	e.cursor = e.buf.clampNormal(e.cursor)
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(e.cursor)
	} else {
		e.commitChange()
	}
}

var checkboxLineRe = regexp.MustCompile(`^(\s*(?:>\s*)*(?:[-+*]|\d+[.)])\s+\[)( |x|X|/|>|-)(\].*)$`)
var bulletOnlyRe = regexp.MustCompile(`^(\s*(?:>\s*)*(?:[-+*]|\d+[.)])\s+)(.*)$`)

// ToggleCheckboxAtCursor turns the current line into a checkbox and toggles
// it on repeat: plain text becomes `- [ ] text`, a bullet keeps its marker,
// `[ ]` flips to `[x]` and back, `[/]` checks off, `[>]` and `[-]` stay.
func (e *Editor) ToggleCheckboxAtCursor() {
	e.beginChange()
	lines := []int{e.cursor.Line}
	if e.inVisual() {
		start, end, _, _ := e.VisualRange()
		lines = nil
		for l := start.Line; l <= end.Line; l++ {
			lines = append(lines, l)
		}
	}
	for _, l := range lines {
		s := e.buf.LineString(l)
		switch {
		case checkboxLineRe.MatchString(s):
			m := checkboxLineRe.FindStringSubmatch(s)
			switch m[2] {
			case ">", "-":
				continue
			case "x", "X":
				e.buf.ReplaceLine(l, []rune(m[1]+" "+m[3]))
			default:
				e.buf.ReplaceLine(l, []rune(m[1]+"x"+m[3]))
			}
		case bulletOnlyRe.MatchString(s):
			m := bulletOnlyRe.FindStringSubmatch(s)
			e.buf.ReplaceLine(l, []rune(m[1]+"[ ] "+m[2]))
		default:
			indent := leadingWhitespace([]rune(s))
			e.buf.ReplaceLine(l, []rune(string(indent)+"- [ ] "+strings.TrimLeft(s, " \t")))
		}
	}
	if e.inVisual() {
		e.exitVisual()
	}
	if e.mode == ModeInsert {
		e.cursor = e.buf.clampInsert(e.cursor)
	} else {
		e.cursor = e.buf.clampNormal(e.cursor)
		e.commitChange()
	}
}
