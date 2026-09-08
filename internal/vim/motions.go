package vim

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// motion is a parsed cursor movement.
type motion struct {
	kind string
	arg  rune
}

// motionResult is where a motion lands and how an operator treats it.
type motionResult struct {
	pos       Pos
	linewise  bool
	inclusive bool
	keepCol   bool
	stickyEnd bool
	jump      bool
	// exclusiveLinewise applies Vim's rule: an exclusive motion ending in
	// column 0 pulls back to the end of the previous line, and becomes
	// linewise when it also started at or before the first non-blank.
	failed bool
}

var charArgMotions = map[rune]bool{'f': true, 'F': true, 't': true, 'T': true, '`': true, '\'': true}

// parseMotion reads a motion off the front of keys.
func parseMotion(keys []Key) (motion, parseStatus) {
	if len(keys) == 0 {
		return motion{}, parseIncomplete
	}
	k := keys[0]
	switch {
	case k.Is("left") || k.Is("backspace") || k.IsCtrl('h'):
		return motion{kind: "h"}, parseComplete
	case k.Is("right") || (k.IsRune(' ')):
		return motion{kind: "l"}, parseComplete
	case k.Is("down") || k.IsCtrl('j') || k.IsCtrl('n'):
		return motion{kind: "j"}, parseComplete
	case k.Is("up") || k.IsCtrl('p'):
		return motion{kind: "k"}, parseComplete
	case k.Is("enter") || k.IsCtrl('m'):
		return motion{kind: "+"}, parseComplete
	case k.Is("home"):
		return motion{kind: "0"}, parseComplete
	case k.Is("end"):
		return motion{kind: "$"}, parseComplete
	}
	if !k.Printable() {
		return motion{}, parseInvalid
	}
	switch k.Rune {
	case 'h', 'l', 'j', 'k', '0', '^', '$', '|', 'w', 'W', 'b', 'B', 'e', 'E', 'G', 'H', 'M', 'L',
		'{', '}', '(', ')', ';', ',', '%', 'n', 'N', '*', '#', '_', '-', '+':
		return motion{kind: string(k.Rune)}, parseComplete
	case 'f', 'F', 't', 'T', '`', '\'':
		if len(keys) < 2 {
			return motion{}, parseIncomplete
		}
		if !keys[1].Printable() {
			if keys[1].Is("esc") {
				return motion{}, parseInvalid
			}
			return motion{}, parseInvalid
		}
		return motion{kind: string(k.Rune), arg: keys[1].Rune}, parseComplete
	case 'g':
		if len(keys) < 2 {
			return motion{}, parseIncomplete
		}
		if !keys[1].Printable() {
			return motion{}, parseInvalid
		}
		switch keys[1].Rune {
		case 'g', 'e', 'E', 'j', 'k', '0', '^', '$', 'm', '_', '*', '#':
			return motion{kind: "g" + string(keys[1].Rune)}, parseComplete
		}
		return motion{}, parseInvalid
	case ']', '[':
		if len(keys) < 2 {
			return motion{}, parseIncomplete
		}
		if !keys[1].Printable() {
			return motion{}, parseInvalid
		}
		second := keys[1].Rune
		switch second {
		case ']', '[', '(', ')', '{', '}', 's':
			return motion{kind: string(k.Rune) + string(second)}, parseComplete
		}
		return motion{}, parseInvalid
	}
	return motion{}, parseInvalid
}

// applyMotion computes where a motion lands from `from`. forOperator marks
// operator-pending use, where a few motions behave differently (cw, e).
func (e *Editor) applyMotion(m motion, count int, hasCount bool, from Pos, forOperator bool) (motionResult, bool) {
	n := max(1, count)
	line := e.buf.Line(from.Line)
	lineCount := e.buf.LineCount()
	res := motionResult{pos: from}
	switch m.kind {
	case "h":
		if from.Col == 0 {
			return res, false
		}
		res.pos.Col = max(0, from.Col-n)
	case "l":
		limit := len(line) - 1
		if forOperator || e.inVisual() {
			limit = len(line)
		}
		if from.Col >= limit && !(forOperator && from.Col < len(line)) {
			if from.Col >= len(line) {
				return res, false
			}
		}
		res.pos.Col = min(from.Col+n, max(0, limit))
		if !forOperator && !e.inVisual() && res.pos.Col == from.Col {
			return res, false
		}
	case "j":
		target := from.Line
		for i := 0; i < n; i++ {
			next := e.nextVisibleLine(target)
			if next >= lineCount {
				break
			}
			target = next
		}
		if target == from.Line && !forOperator {
			return res, false
		}
		res.pos = Pos{target, e.colForDesired(target)}
		res.linewise, res.keepCol = true, true
	case "k":
		target := from.Line
		for i := 0; i < n; i++ {
			prev := e.prevVisibleLine(target)
			if prev < 0 {
				break
			}
			target = prev
		}
		if target == from.Line && !forOperator {
			return res, false
		}
		res.pos = Pos{target, e.colForDesired(target)}
		res.linewise, res.keepCol = true, true
	case "gj", "gk":
		// Display-line motions: the host wraps, so without a wrap width
		// they are j/k.
		dir := 1
		if m.kind == "gk" {
			dir = -1
		}
		target := from.Line + dir*n
		target = min(max(0, target), lineCount-1)
		res.pos = Pos{target, e.colForDesired(target)}
		res.keepCol = true
	case "0", "g0":
		res.pos.Col = 0
	case "^", "g^":
		res.pos.Col = firstNonBlankInsert(line)
		if !forOperator {
			res.pos.Col = firstNonBlank(line)
		}
	case "$", "g$":
		target := min(from.Line+n-1, lineCount-1)
		res.pos = Pos{target, max(0, e.buf.LineLen(target)-1)}
		if forOperator {
			res.pos.Col = e.buf.LineLen(target)
		}
		res.inclusive, res.stickyEnd = true, true
		if forOperator {
			res.inclusive = false
		}
	case "g_":
		target := min(from.Line+n-1, lineCount-1)
		l := e.buf.Line(target)
		col := len(l) - 1
		for col > 0 && isBlank(l[col]) {
			col--
		}
		res.pos = Pos{target, max(0, col)}
		res.inclusive = true
	case "gm":
		res.pos.Col = min(len(line)/2, max(0, len(line)-1))
	case "|":
		col := 0
		if hasCount {
			col = count - 1
		}
		res.pos.Col = min(max(0, col), max(0, len(line)-1))
	case "w", "W":
		big := m.kind == "W"
		p := from
		for i := 0; i < n; i++ {
			next, ok := e.wordForward(p, big, forOperator && i == n-1)
			if !ok {
				break
			}
			p = next
		}
		res.pos = p
		if forOperator && p.Line > from.Line && p.Col == 0 {
			// `dw` on the last word of a line stops at the line end.
			prevLine := p.Line - 1
			res.pos = Pos{prevLine, e.buf.LineLen(prevLine)}
			if res.pos.Line == from.Line && res.pos.Col <= from.Col {
				res.pos = p
			}
		}
	case "b", "B":
		big := m.kind == "B"
		p := from
		for i := 0; i < n; i++ {
			next, ok := e.wordBackward(p, big)
			if !ok {
				break
			}
			p = next
		}
		res.pos = p
	case "e", "E":
		big := m.kind == "E"
		p := from
		for i := 0; i < n; i++ {
			next, ok := e.wordEnd(p, big)
			if !ok {
				break
			}
			p = next
		}
		res.pos = p
		res.inclusive = true
	case "ge", "gE":
		big := m.kind == "gE"
		p := from
		for i := 0; i < n; i++ {
			next, ok := e.wordEndBackward(p, big)
			if !ok {
				break
			}
			p = next
		}
		res.pos = p
		res.inclusive = true
	case "gg":
		target := 0
		if hasCount {
			target = min(max(0, count-1), lineCount-1)
		}
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise, res.jump = true, true
	case "G":
		target := lineCount - 1
		if hasCount {
			target = min(max(0, count-1), lineCount-1)
		}
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise, res.jump = true, true
	case "H", "M", "L":
		top, bottom := e.scrollTop, e.lastVisibleLine()
		var target int
		switch m.kind {
		case "H":
			target = min(top+n-1, bottom)
		case "L":
			target = max(bottom-n+1, top)
		default:
			target = (top + bottom) / 2
		}
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise, res.jump = true, true
	case "_":
		target := min(from.Line+n-1, lineCount-1)
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise = true
	case "+":
		target := min(from.Line+n, lineCount-1)
		if target == from.Line {
			return res, false
		}
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise = true
	case "-":
		target := max(from.Line-n, 0)
		if target == from.Line {
			return res, false
		}
		res.pos = Pos{target, firstNonBlank(e.buf.Line(target))}
		res.linewise = true
	case "}", "{":
		dir := 1
		if m.kind == "{" {
			dir = -1
		}
		p := from
		for i := 0; i < n; i++ {
			p = e.paragraphMove(p, dir)
		}
		res.pos = p
		res.jump = true
	case ")", "(":
		dir := 1
		if m.kind == "(" {
			dir = -1
		}
		p := from
		for i := 0; i < n; i++ {
			p = e.sentenceMove(p, dir)
		}
		res.pos = p
		res.jump = true
	case "f", "F", "t", "T":
		p, ok := e.findChar(from, m.kind[0], m.arg, n)
		if !ok {
			return res, false
		}
		res.pos = p
		res.inclusive = m.kind == "f" || m.kind == "t"
		e.lastFind.ch, e.lastFind.kind, e.lastFind.set = m.arg, rune(m.kind[0]), true
	case ";", ",":
		if !e.lastFind.set {
			return res, false
		}
		kind := e.lastFind.kind
		if m.kind == "," {
			kind = map[rune]rune{'f': 'F', 'F': 'f', 't': 'T', 'T': 't'}[kind]
		}
		p, ok := e.findChar(from, byte(kind), e.lastFind.ch, n)
		if !ok {
			return res, false
		}
		res.pos = p
		res.inclusive = kind == 'f' || kind == 't'
	case "%":
		p, ok := e.matchPair(from)
		if !ok {
			return res, false
		}
		res.pos = p
		res.inclusive, res.jump = true, true
	case "n", "N":
		dir := e.searchDir
		if m.kind == "N" {
			dir = -dir
		}
		p, ok := e.searchNext(from, dir, n, true)
		if !ok {
			return res, false
		}
		res.pos, res.jump = p, true
	case "*", "#", "g*", "g#":
		word := e.WordUnderCursor()
		if word == "" {
			e.setError("E348: No string under cursor")
			return res, false
		}
		pattern := regexp.QuoteMeta(word)
		if !strings.HasPrefix(m.kind, "g") {
			pattern = `\b` + pattern + `\b`
		}
		e.lastSearch = pattern
		e.highlight = true
		dir := 1
		if strings.HasSuffix(m.kind, "#") {
			dir = -1
		}
		e.searchDir = dir
		p, ok := e.searchNext(from, dir, n, true)
		if !ok {
			return res, false
		}
		res.pos, res.jump = p, true
	case "`", "'":
		p, ok := e.markPos(m.arg)
		if !ok {
			e.setError("E20: Mark not set")
			return res, false
		}
		res.pos = e.buf.clampNormal(p)
		if m.kind == "'" {
			res.pos.Col = firstNonBlank(e.buf.Line(res.pos.Line))
			res.linewise = true
		}
		res.jump = true
	case "]]", "[[":
		dir := 1
		if m.kind == "[[" {
			dir = -1
		}
		res.pos = e.headingMove(from, dir, n)
		res.jump = true
		res.linewise = false
	case "](", "[(", "]{", "[{", "])", "[}":
		open, close := '(', ')'
		if strings.ContainsAny(m.kind, "{}") {
			open, close = '{', '}'
		}
		var p Pos
		var ok bool
		if m.kind[0] == '[' {
			p, ok = e.findUnmatched(from, open, close, -1, n)
		} else {
			p, ok = e.findUnmatched(from, open, close, 1, n)
		}
		if !ok {
			return res, false
		}
		res.pos, res.jump, res.inclusive = p, true, true
	case "]s", "[s":
		return res, false
	default:
		return res, false
	}
	return res, true
}

func (e *Editor) inVisual() bool {
	return e.mode == ModeVisual || e.mode == ModeVisualLine || e.mode == ModeVisualBlock
}

func (e *Editor) colForDesired(line int) int {
	l := e.buf.Line(line)
	want := e.desiredCol
	if want < 0 {
		want = e.cursor.Col
	}
	if e.mode == ModeInsert || e.mode == ModeReplace {
		return min(want, len(l))
	}
	if want == math.MaxInt32 {
		return max(0, len(l)-1)
	}
	return min(want, max(0, len(l)-1))
}

// --- words ---

func (e *Editor) runeAt(p Pos) (rune, bool) {
	line := e.buf.Line(p.Line)
	if p.Col < 0 || p.Col >= len(line) {
		return 0, false
	}
	return line[p.Col], true
}

// wordForward moves to the start of the next word; a blank line counts as a
// word. lastForOperator stops an operator's final step at line end instead
// of crossing into the next line.
func (e *Editor) wordForward(p Pos, big bool, lastForOperator bool) (Pos, bool) {
	line := e.buf.Line(p.Line)
	lineCount := e.buf.LineCount()
	col := p.Col
	if col < len(line) {
		cls := charClass(line[col], big)
		if cls != 0 {
			for col < len(line) && charClass(line[col], big) == cls {
				col++
			}
		}
		for col < len(line) && isBlank(line[col]) {
			col++
		}
		if col < len(line) {
			return Pos{p.Line, col}, true
		}
	}
	if lastForOperator {
		return Pos{p.Line, len(line)}, true
	}
	// Next lines: the first non-blank, or a blank line.
	for l := p.Line + 1; l < lineCount; l++ {
		next := e.buf.Line(l)
		if len(next) == 0 {
			return Pos{l, 0}, true
		}
		c := 0
		for c < len(next) && isBlank(next[c]) {
			c++
		}
		if c < len(next) {
			return Pos{l, c}, true
		}
	}
	last := lineCount - 1
	end := Pos{last, max(0, e.buf.LineLen(last)-1)}
	if end == p {
		return p, false
	}
	return end, true
}

func (e *Editor) wordBackward(p Pos, big bool) (Pos, bool) {
	l, col := p.Line, p.Col
	for {
		line := e.buf.Line(l)
		col = min(col, len(line))
		// Step back over blanks.
		c := col - 1
		for c >= 0 && isBlank(line[c]) {
			c--
		}
		if c < 0 {
			if l == 0 {
				if p.Col == 0 && p.Line == 0 {
					return p, false
				}
				return Pos{0, 0}, true
			}
			l--
			col = e.buf.LineLen(l)
			if col == 0 {
				return Pos{l, 0}, true
			}
			continue
		}
		cls := charClass(line[c], big)
		for c > 0 && charClass(line[c-1], big) == cls {
			c--
		}
		return Pos{l, c}, true
	}
}

func (e *Editor) wordEnd(p Pos, big bool) (Pos, bool) {
	l, col := p.Line, p.Col+1
	for l < e.buf.LineCount() {
		line := e.buf.Line(l)
		for col < len(line) && isBlank(line[col]) {
			col++
		}
		if col < len(line) {
			cls := charClass(line[col], big)
			for col+1 < len(line) && charClass(line[col+1], big) == cls {
				col++
			}
			return Pos{l, col}, true
		}
		l++
		col = 0
	}
	return p, false
}

func (e *Editor) wordEndBackward(p Pos, big bool) (Pos, bool) {
	l, col := p.Line, p.Col
	line := e.buf.Line(l)
	if col < len(line) {
		cls := charClass(line[col], big)
		if cls != 0 {
			for col > 0 && charClass(line[col-1], big) == cls {
				col--
			}
		}
	}
	col--
	for {
		if col < 0 {
			if l == 0 {
				return Pos{0, 0}, p != Pos{}
			}
			l--
			line = e.buf.Line(l)
			col = len(line) - 1
			if len(line) == 0 {
				return Pos{l, 0}, true
			}
			continue
		}
		line = e.buf.Line(l)
		for col >= 0 && isBlank(line[col]) {
			col--
		}
		if col < 0 {
			continue
		}
		return Pos{l, col}, true
	}
}

// --- paragraphs, sentences, headings ---

func (e *Editor) paragraphMove(p Pos, dir int) Pos {
	l := p.Line
	lineCount := e.buf.LineCount()
	// Skip blank lines we are on, then find the next blank.
	for l+dir >= 0 && l+dir < lineCount && isBlankLine(e.buf.Line(l)) {
		l += dir
	}
	for l+dir >= 0 && l+dir < lineCount {
		l += dir
		if isBlankLine(e.buf.Line(l)) {
			return Pos{l, 0}
		}
	}
	if dir > 0 {
		return Pos{lineCount - 1, max(0, e.buf.LineLen(lineCount-1)-1)}
	}
	return Pos{0, 0}
}

func (e *Editor) sentenceMove(p Pos, dir int) Pos {
	starts := e.sentenceStarts()
	if dir > 0 {
		for _, s := range starts {
			if p.Less(s) {
				return s
			}
		}
		last := e.buf.LineCount() - 1
		return Pos{last, max(0, e.buf.LineLen(last)-1)}
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i].Less(p) {
			return starts[i]
		}
	}
	return Pos{0, 0}
}

// sentenceStarts lists every sentence start: the first non-blank after a
// paragraph boundary or after `.`, `!`, `?` followed by whitespace.
func (e *Editor) sentenceStarts() []Pos {
	out := []Pos{}
	prevBlank := true
	for l := 0; l < e.buf.LineCount(); l++ {
		line := e.buf.Line(l)
		if isBlankLine(line) {
			if !prevBlank {
				out = append(out, Pos{l, 0})
			}
			prevBlank = true
			continue
		}
		start := firstNonBlank(line)
		if prevBlank {
			out = append(out, Pos{l, start})
		}
		prevBlank = false
		for c := start; c < len(line); c++ {
			r := line[c]
			if r == '.' || r == '!' || r == '?' {
				k := c + 1
				for k < len(line) && (line[k] == ')' || line[k] == ']' || line[k] == '"' || line[k] == '\'') {
					k++
				}
				if k >= len(line) {
					if l+1 < e.buf.LineCount() && !isBlankLine(e.buf.Line(l+1)) {
						next := e.buf.Line(l + 1)
						out = append(out, Pos{l + 1, firstNonBlank(next)})
					}
					break
				}
				if isBlank(line[k]) {
					for k < len(line) && isBlank(line[k]) {
						k++
					}
					if k < len(line) {
						out = append(out, Pos{l, k})
					}
				}
			}
		}
	}
	return out
}

var headingLineRe = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]`)

// headingLines lists heading lines outside fenced code and frontmatter.
func (e *Editor) headingLines() []int {
	out := []int{}
	inFence := false
	fenceMarker := ""
	start := 0
	if e.buf.LineCount() > 0 && e.buf.LineString(0) == "---" {
		for i := 1; i < e.buf.LineCount(); i++ {
			if e.buf.LineString(i) == "---" {
				start = i + 1
				break
			}
		}
	}
	for i := start; i < e.buf.LineCount(); i++ {
		line := strings.TrimLeft(e.buf.LineString(i), " \t")
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			marker := line[:3]
			if !inFence {
				inFence, fenceMarker = true, marker
			} else if marker == fenceMarker {
				inFence, fenceMarker = false, ""
			}
			continue
		}
		if inFence {
			continue
		}
		if headingLineRe.MatchString(e.buf.LineString(i)) {
			out = append(out, i)
		}
	}
	return out
}

func (e *Editor) headingMove(from Pos, dir, n int) Pos {
	headings := e.headingLines()
	var ahead []int
	if dir > 0 {
		for _, h := range headings {
			if h > from.Line {
				ahead = append(ahead, h)
			}
		}
	} else {
		for i := len(headings) - 1; i >= 0; i-- {
			if headings[i] < from.Line {
				ahead = append(ahead, headings[i])
			}
		}
	}
	if len(ahead) == 0 {
		if dir > 0 {
			return Pos{e.buf.LineCount() - 1, 0}
		}
		return Pos{0, 0}
	}
	return Pos{ahead[min(n, len(ahead))-1], 0}
}

// --- find char ---

func (e *Editor) findChar(from Pos, kind byte, ch rune, n int) (Pos, bool) {
	line := e.buf.Line(from.Line)
	col := from.Col
	switch kind {
	case 'f', 't':
		for i := 0; i < n; i++ {
			start := col + 1
			if kind == 't' && i == 0 && start < len(line) && line[start] == ch && n == 1 {
				// `t` on an adjacent char still needs to find the NEXT one
				// when repeated with ; in Vim; plain t stays put. Vim's cpo
				// default: t does not move. Match that by searching from
				// col+2 only for repeats.
			}
			found := -1
			for c := start; c < len(line); c++ {
				if line[c] == ch {
					found = c
					break
				}
			}
			if found < 0 {
				return from, false
			}
			col = found
		}
		if kind == 't' {
			col--
			if col <= from.Col && n == 1 {
				if col < from.Col {
					return from, false
				}
			}
		}
		return Pos{from.Line, col}, true
	default:
		for i := 0; i < n; i++ {
			found := -1
			for c := col - 1; c >= 0; c-- {
				if line[c] == ch {
					found = c
					break
				}
			}
			if found < 0 {
				return from, false
			}
			col = found
		}
		if kind == 'T' {
			col++
		}
		return Pos{from.Line, col}, true
	}
}

// --- brackets ---

var pairs = map[rune]rune{'(': ')', '[': ']', '{': '}', ')': '(', ']': '[', '}': '{'}

func (e *Editor) matchPair(from Pos) (Pos, bool) {
	line := e.buf.Line(from.Line)
	col := from.Col
	for col < len(line) {
		if _, ok := pairs[line[col]]; ok {
			break
		}
		col++
	}
	if col >= len(line) {
		return from, false
	}
	open := line[col]
	close := pairs[open]
	dir := 1
	if open == ')' || open == ']' || open == '}' {
		dir = -1
	}
	depth := 0
	p := Pos{from.Line, col}
	for {
		r, ok := e.runeAt(p)
		if ok {
			if r == open {
				depth++
			} else if r == close {
				depth--
				if depth == 0 {
					return p, true
				}
			}
		}
		next, ok := e.step(p, dir)
		if !ok {
			return from, false
		}
		p = next
	}
}

// step moves one character forward or backward across lines (an empty line
// counts as a position).
func (e *Editor) step(p Pos, dir int) (Pos, bool) {
	if dir > 0 {
		if p.Col+1 < e.buf.LineLen(p.Line) {
			return Pos{p.Line, p.Col + 1}, true
		}
		if p.Line+1 < e.buf.LineCount() {
			return Pos{p.Line + 1, 0}, true
		}
		return p, false
	}
	if p.Col > 0 {
		return Pos{p.Line, min(p.Col-1, max(0, e.buf.LineLen(p.Line)-1))}, true
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, max(0, e.buf.LineLen(p.Line-1)-1)}, true
	}
	return p, false
}

func (e *Editor) findUnmatched(from Pos, open, close rune, dir, n int) (Pos, bool) {
	p := from
	for i := 0; i < n; i++ {
		depth := 0
		found := false
		for {
			next, ok := e.step(p, dir)
			if !ok {
				return from, false
			}
			p = next
			r, ok := e.runeAt(p)
			if !ok {
				continue
			}
			if dir < 0 {
				if r == close {
					depth++
				} else if r == open {
					if depth == 0 {
						found = true
						break
					}
					depth--
				}
			} else {
				if r == open {
					depth++
				} else if r == close {
					if depth == 0 {
						found = true
						break
					}
					depth--
				}
			}
		}
		if !found {
			return from, false
		}
	}
	return p, true
}

func (e *Editor) markPos(r rune) (Pos, bool) {
	switch r {
	case '`', '\'':
		p, ok := e.marks['\'']
		if !ok {
			return Pos{}, false
		}
		return p, true
	}
	p, ok := e.marks[r]
	if !ok {
		return Pos{}, false
	}
	if p.Line >= e.buf.LineCount() {
		return Pos{}, false
	}
	return p, true
}

var _ = unicode.IsLetter
