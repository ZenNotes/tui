package vim

// textObject is a parsed `iw`, `a(`, `it` ...
type textObject struct {
	inner bool
	kind  rune
}

func parseTextObject(keys []Key) (textObject, parseStatus) {
	if len(keys) == 0 {
		return textObject{}, parseIncomplete
	}
	if !keys[0].Printable() || (keys[0].Rune != 'i' && keys[0].Rune != 'a') {
		return textObject{}, parseInvalid
	}
	if len(keys) < 2 {
		return textObject{}, parseIncomplete
	}
	if !keys[1].Printable() {
		return textObject{}, parseInvalid
	}
	switch keys[1].Rune {
	case 'w', 'W', 's', 'p', '(', ')', 'b', '[', ']', '{', '}', 'B', '<', '>', '"', '\'', '`', 't':
		return textObject{inner: keys[0].Rune == 'i', kind: keys[1].Rune}, parseComplete
	}
	return textObject{}, parseInvalid
}

// objectRange is a selected span: start..end inclusive, or whole lines.
type objectRange struct {
	start, end Pos
	linewise   bool
	ok         bool
}

// textObjectRange resolves a text object around the cursor (or the visual
// selection when one is active).
func (e *Editor) textObjectRange(obj textObject, count int) objectRange {
	n := max(1, count)
	switch obj.kind {
	case 'w', 'W':
		return e.wordObject(obj.inner, obj.kind == 'W', n)
	case 's':
		return e.sentenceObject(obj.inner, n)
	case 'p':
		return e.paragraphObject(obj.inner, n)
	case '(', ')', 'b':
		return e.bracketObject(obj.inner, '(', ')', n)
	case '[', ']':
		return e.bracketObject(obj.inner, '[', ']', n)
	case '{', '}', 'B':
		return e.bracketObject(obj.inner, '{', '}', n)
	case '<', '>':
		return e.bracketObject(obj.inner, '<', '>', n)
	case '"', '\'', '`':
		return e.quoteObject(obj.inner, obj.kind)
	case 't':
		return e.tagObject(obj.inner, n)
	}
	return objectRange{}
}

func (e *Editor) wordObject(inner, big bool, n int) objectRange {
	line := e.buf.Line(e.cursor.Line)
	if len(line) == 0 {
		return objectRange{start: e.cursor, end: e.cursor, ok: true}
	}
	col := min(e.cursor.Col, len(line)-1)
	start, end := col, col
	for i := 0; i < n; i++ {
		if i > 0 {
			if end+1 >= len(line) {
				break
			}
			end++
		}
		cls := charClass(line[end], big)
		for end+1 < len(line) && charClass(line[end+1], big) == cls {
			end++
		}
		if i == 0 {
			for start > 0 && charClass(line[start-1], big) == cls {
				start--
			}
		}
		if !inner {
			if cls == 0 {
				// On whitespace: `aw` takes the whitespace plus the word after.
				if end+1 < len(line) {
					end++
					wc := charClass(line[end], big)
					for end+1 < len(line) && charClass(line[end+1], big) == wc {
						end++
					}
				}
			} else if end+1 < len(line) && isBlank(line[end+1]) {
				for end+1 < len(line) && isBlank(line[end+1]) {
					end++
				}
			} else if i == 0 {
				for start > 0 && isBlank(line[start-1]) {
					start--
				}
			}
		}
	}
	return objectRange{start: Pos{e.cursor.Line, start}, end: Pos{e.cursor.Line, end}, ok: true}
}

func (e *Editor) sentenceObject(inner bool, n int) objectRange {
	starts := e.sentenceStarts()
	if len(starts) == 0 {
		return objectRange{}
	}
	idx := 0
	for i, s := range starts {
		if !e.cursor.Less(s) {
			idx = i
		}
	}
	start := starts[idx]
	endIdx := min(idx+n, len(starts))
	var end Pos
	if endIdx < len(starts) {
		end, _ = e.step(starts[endIdx], -1)
	} else {
		last := e.buf.LineCount() - 1
		end = Pos{last, max(0, e.buf.LineLen(last)-1)}
	}
	if inner {
		for end.Line > start.Line || (end.Line == start.Line && end.Col > start.Col) {
			r, ok := e.runeAt(end)
			if ok && !isBlank(r) {
				break
			}
			prev, ok := e.step(end, -1)
			if !ok {
				break
			}
			end = prev
		}
	}
	return objectRange{start: start, end: end, ok: true}
}

func (e *Editor) paragraphObject(inner bool, n int) objectRange {
	l := e.cursor.Line
	lineCount := e.buf.LineCount()
	blank := isBlankLine(e.buf.Line(l))
	start := l
	for start > 0 && isBlankLine(e.buf.Line(start-1)) == blank {
		start--
	}
	end := l
	for i := 0; i < n; i++ {
		if i > 0 {
			if end+1 >= lineCount {
				break
			}
			end++
			blank = isBlankLine(e.buf.Line(end))
		}
		for end+1 < lineCount && isBlankLine(e.buf.Line(end+1)) == blank {
			end++
		}
		if !inner {
			// `ap` takes the following blank lines too (or preceding when at end).
			if end+1 < lineCount {
				nextBlank := isBlankLine(e.buf.Line(end + 1))
				if nextBlank != blank {
					end++
					for end+1 < lineCount && isBlankLine(e.buf.Line(end+1)) == nextBlank {
						end++
					}
				}
			} else {
				for start > 0 && isBlankLine(e.buf.Line(start-1)) != blank {
					start--
				}
			}
		}
	}
	return objectRange{start: Pos{start, 0}, end: Pos{end, 0}, linewise: true, ok: true}
}

func (e *Editor) bracketObject(inner bool, open, close rune, n int) objectRange {
	// Find the enclosing open bracket, n levels out.
	openPos := e.cursor
	if r, ok := e.runeAt(openPos); ok && r == open {
		n--
		if n == 0 {
			goto haveOpen
		}
	}
	if r, ok := e.runeAt(openPos); ok && r == close {
		openPos, ok = e.findUnmatched(openPos, open, close, -1, 1)
		if !ok {
			return objectRange{}
		}
		n--
	}
	if n > 0 {
		p, ok := e.findUnmatched(openPos, open, close, -1, n)
		if !ok {
			return objectRange{}
		}
		openPos = p
	}
haveOpen:
	closePos, ok := e.findUnmatched(openPos, open, close, 1, 1)
	if !ok {
		return objectRange{}
	}
	if !inner {
		return objectRange{start: openPos, end: closePos, ok: true}
	}
	start, ok1 := e.step(openPos, 1)
	end, ok2 := e.step(closePos, -1)
	if !ok1 || !ok2 || closePos.Less(start) || end.Less(openPos) {
		return objectRange{start: closePos, end: closePos, ok: true, linewise: false}
	}
	if start == closePos {
		// Empty brackets: nothing inside.
		return objectRange{start: closePos, end: openPos, ok: true}
	}
	// Brackets on their own lines select whole inner lines.
	if openPos.Col == e.buf.LineLen(openPos.Line)-1 && closePos.Col == firstNonBlankInsert(e.buf.Line(closePos.Line)) && closePos.Line > openPos.Line+1 {
		return objectRange{start: Pos{openPos.Line + 1, 0}, end: Pos{closePos.Line - 1, 0}, linewise: true, ok: true}
	}
	return objectRange{start: start, end: end, ok: true}
}

func (e *Editor) quoteObject(inner bool, q rune) objectRange {
	line := e.buf.Line(e.cursor.Line)
	col := e.cursor.Col
	positions := []int{}
	for i, r := range line {
		if r == q && (i == 0 || line[i-1] != '\\') {
			positions = append(positions, i)
		}
	}
	if len(positions) < 2 {
		return objectRange{}
	}
	start, end := -1, -1
	for i := 0; i+1 < len(positions); i += 2 {
		if positions[i] <= col && col <= positions[i+1] {
			start, end = positions[i], positions[i+1]
			break
		}
	}
	if start < 0 {
		// Cursor before the first quote: the first pair after it.
		for i := 0; i+1 < len(positions); i++ {
			if positions[i] > col {
				start, end = positions[i], positions[i+1]
				break
			}
		}
	}
	if start < 0 {
		return objectRange{}
	}
	if inner {
		if end == start+1 {
			return objectRange{start: Pos{e.cursor.Line, end}, end: Pos{e.cursor.Line, start}, ok: true}
		}
		return objectRange{start: Pos{e.cursor.Line, start + 1}, end: Pos{e.cursor.Line, end - 1}, ok: true}
	}
	s, en := start, end
	if en+1 < len(line) && isBlank(line[en+1]) {
		for en+1 < len(line) && isBlank(line[en+1]) {
			en++
		}
	} else {
		for s > 0 && isBlank(line[s-1]) {
			s--
		}
	}
	return objectRange{start: Pos{e.cursor.Line, s}, end: Pos{e.cursor.Line, en}, ok: true}
}

// tagObject selects an `<x>...</x>` element around the cursor.
func (e *Editor) tagObject(inner bool, n int) objectRange {
	text := []rune(e.buf.Text())
	offsets := e.lineOffsets()
	cur := offsets[e.cursor.Line] + e.cursor.Col
	type tag struct {
		start, end int
		name       string
		closing    bool
	}
	tags := []tag{}
	for i := 0; i < len(text); i++ {
		if text[i] != '<' {
			continue
		}
		j := i + 1
		closing := false
		if j < len(text) && text[j] == '/' {
			closing = true
			j++
		}
		nameStart := j
		for j < len(text) && (isWordRune(text[j]) || text[j] == '-') {
			j++
		}
		if j == nameStart {
			continue
		}
		name := string(text[nameStart:j])
		for j < len(text) && text[j] != '>' {
			j++
		}
		if j >= len(text) {
			break
		}
		if text[j-1] == '/' {
			i = j
			continue
		}
		tags = append(tags, tag{start: i, end: j, name: name, closing: closing})
		i = j
	}
	// Match pairs with a stack, collect elements containing the cursor.
	type element struct{ open, close tag }
	elements := []element{}
	stack := []tag{}
	for _, t := range tags {
		if !t.closing {
			stack = append(stack, t)
			continue
		}
		for k := len(stack) - 1; k >= 0; k-- {
			if stack[k].name == t.name {
				elements = append(elements, element{open: stack[k], close: t})
				stack = stack[:k]
				break
			}
		}
	}
	enclosing := []element{}
	for _, el := range elements {
		if el.open.start <= cur && cur <= el.close.end {
			enclosing = append(enclosing, el)
		}
	}
	if len(enclosing) == 0 {
		return objectRange{}
	}
	// Innermost first.
	for i := 0; i < len(enclosing); i++ {
		for j := i + 1; j < len(enclosing); j++ {
			if enclosing[j].open.start > enclosing[i].open.start {
				enclosing[i], enclosing[j] = enclosing[j], enclosing[i]
			}
		}
	}
	el := enclosing[min(n, len(enclosing))-1]
	var s, en int
	if inner {
		s, en = el.open.end+1, el.close.start-1
		if en < s {
			return objectRange{start: e.posFromOffset(offsets, el.close.start), end: e.posFromOffset(offsets, el.open.end), ok: true}
		}
	} else {
		s, en = el.open.start, el.close.end
	}
	return objectRange{start: e.posFromOffset(offsets, s), end: e.posFromOffset(offsets, en), ok: true}
}

func (e *Editor) lineOffsets() []int {
	offsets := make([]int, e.buf.LineCount()+1)
	total := 0
	for i := 0; i < e.buf.LineCount(); i++ {
		offsets[i] = total
		total += e.buf.LineLen(i) + 1
	}
	offsets[e.buf.LineCount()] = total
	return offsets
}

func (e *Editor) posFromOffset(offsets []int, off int) Pos {
	line := 0
	for line+1 < len(offsets)-1 && offsets[line+1] <= off {
		line++
	}
	return Pos{line, off - offsets[line]}
}
