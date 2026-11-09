package helix

import "strings"

// Match mode (m): bracket matching, text-object selection, and surround
// editing — Helix's take on what Vim does with `i`/`a` objects and its
// surround plugin.

func (e *Editor) matchMode(rest []Key) (parseStatus, bool) {
	if len(rest) < 2 {
		return parseIncomplete, false
	}
	k := rest[1]
	if !k.Printable() {
		return parseInvalid, false
	}
	switch k.Rune {
	case 'm':
		// Match the bracket under the cursor (Helix's m m).
		p, ok := e.bracketMatch(e.cursor)
		if !ok {
			e.setError("No matching pair")
			return parseComplete, false
		}
		e.pushJump()
		e.cursor = e.buf.clampNormal(p)
		e.desiredCol = -1
		e.ensureCursorVisible()
		return parseComplete, false
	case 'i', 'a':
		inner := k.Rune == 'i'
		if len(rest) < 3 {
			return parseIncomplete, false
		}
		obj, st := parseTextObject(rest[1:])
		if st != parseComplete {
			if st == parseIncomplete {
				return parseIncomplete, false
			}
			// `t` and friends need one more key; anything else is invalid.
			return parseInvalid, false
		}
		if e.mode == ModeSelect {
			e.extendByObject(obj, 1)
			return parseComplete, false
		}
		rng := e.textObjectRange(obj, 1)
		if !rng.ok {
			e.setError("No matching object")
			return parseComplete, false
		}
		start, end := rng.start, rng.end
		if rng.linewise {
			start = Pos{start.Line, 0}
			end = Pos{end.Line, max(0, e.buf.LineLen(end.Line)-1)}
		}
		e.mode = ModeSelect
		if rng.linewise {
			e.kind = selLine
		} else {
			e.kind = selChar
		}
		e.selExcl = false
		e.anchor = e.buf.clampNormal(start)
		e.cursor = e.buf.clampNormal(end)
		e.desiredCol = -1
		e.pending = nil
		_ = inner
		return parseComplete, false
	case 's', 'd', 'r':
		if len(rest) < 3 {
			return parseIncomplete, false
		}
		if !rest[2].Printable() {
			return parseInvalid, false
		}
		return e.surroundCommand(k.Rune, rest[2].Rune, rest)
	}
	return parseInvalid, false
}

// --- surround editing ---

var surroundPairs = map[rune][2]rune{
	'(': {'(', ')'}, ')': {'(', ')'},
	'[': {'[', ']'}, ']': {'[', ']'},
	'{': {'{', '}'}, '}': {'{', '}'},
	'<': {'<', '>'}, '>': {'<', '>'},
	'"': {'"', '"'}, '\'': {'\'', '\''},
	'`': {'`', '`'},
}

// surroundCommand edits the delimiters around the selection: add (s),
// delete (d) or replace (r) them. `r` wants two keys (old, new).
func (e *Editor) surroundCommand(action, ch rune, rest []Key) (parseStatus, bool) {
	oldPair, ok := surroundPairs[ch]
	if !ok {
		return parseInvalid, false
	}
	pair := oldPair
	if action == 'r' {
		if len(rest) < 4 || !rest[3].Printable() {
			return parseIncomplete, false
		}
		newPair, ok := surroundPairs[rest[3].Rune]
		if !ok {
			return parseInvalid, false
		}
		pair = newPair
	}
	from, to := e.selRange()
	// For whole lines the delimiters sit at the line edges.
	left, right := from, to
	if e.mode == ModeSelect && e.kind == selLine {
		left = Pos{from.Line, 0}
		right = Pos{to.Line, e.buf.LineLen(to.Line)}
	} else {
		right = e.stepInsertPos(to)
	}
	openPos, closePos, ok := e.enclosingPair(oldPair[0], oldPair[1], left, right)
	e.beginChange()
	switch action {
	case 's':
		// Add: wrap the selection. Later edits shift columns, so insert the
		// closing delimiter first.
		e.buf.InsertText(right, string(pair[1]))
		e.buf.InsertText(left, string(pair[0]))
		e.cursor = e.buf.clampNormal(left)
	case 'd':
		if !ok {
			e.setError("No surrounding pair")
			return parseComplete, false
		}
		e.buf.DeleteRange(closePos, e.stepInsertPos(closePos))
		e.buf.DeleteRange(openPos, e.stepInsertPos(openPos))
		e.cursor = e.buf.clampNormal(openPos)
	case 'r':
		if !ok {
			e.setError("No surrounding pair")
			return parseComplete, false
		}
		e.buf.DeleteRange(closePos, e.stepInsertPos(closePos))
		e.buf.InsertText(closePos, string(pair[1]))
		e.buf.DeleteRange(openPos, e.stepInsertPos(openPos))
		e.buf.InsertText(openPos, string(pair[0]))
		e.cursor = e.buf.clampNormal(openPos)
	}
	e.cursor = e.buf.clampNormal(e.cursor)
	e.anchor = e.cursor
	if e.mode == ModeSelect {
		e.exitSelect()
	}
	e.commitChange()
	return parseComplete, false
}

// enclosingPair finds the innermost open..close pair of the given kind that
// surrounds the range start..end (end exclusive).
func (e *Editor) enclosingPair(open, close rune, start, end Pos) (openPos, closePos Pos, ok bool) {
	// Walk backwards from start for an opener whose match lands after end.
	p := start
	nesting := 0
	for p.Line >= 0 {
		for p.Col >= 0 {
			p.Col--
			if p.Col < 0 {
				break
			}
			r := e.buf.Line(p.Line)[p.Col]
			if r == close && open != close {
				nesting++
			} else if r == open {
				if nesting == 0 {
					match, found := e.bracketMatchPos(p, open, close)
					if found && (match.Line > end.Line || (match.Line == end.Line && match.Col >= end.Col)) {
						return p, match, true
					}
					// This opener does not enclose the range; it is part of
					// the selection's own text. Keep looking outward.
				} else {
					nesting--
				}
			}
		}
		p.Line--
		if p.Line < 0 {
			break
		}
		p.Col = e.buf.LineLen(p.Line)
	}
	// Quote-like pairs are their own opener; scan for the nearest one
	// before the selection whose twin lands after it.
	if open == close {
		p = start
		for p.Line >= 0 {
			for p.Col >= 0 {
				p.Col--
				if p.Col < 0 {
					break
				}
				if e.buf.Line(p.Line)[p.Col] == open {
					match, found := e.quoteMatchPos(p, open)
					if found && (match.Line > end.Line || (match.Line == end.Line && match.Col >= end.Col)) {
						return p, match, true
					}
				}
			}
			p.Line--
			if p.Line < 0 {
				break
			}
			p.Col = e.buf.LineLen(p.Line)
		}
	}
	return Pos{}, Pos{}, false
}

// bracketMatchPos matches one opener forward through the buffer.
func (e *Editor) bracketMatchPos(from Pos, open, close rune) (Pos, bool) {
	depth := 0
	p := from
	for p.Line < e.buf.LineCount() {
		line := e.buf.Line(p.Line)
		for p.Col < len(line) {
			r := line[p.Col]
			if r == open {
				depth++
			} else if r == close {
				depth--
				if depth == 0 {
					return p, true
				}
			}
			p.Col++
		}
		p.Line++
		p.Col = 0
	}
	return Pos{}, false
}

// quoteMatchPos matches a quote-like delimiter forward, skipping escaped
// occurrences.
func (e *Editor) quoteMatchPos(from Pos, q rune) (Pos, bool) {
	p := from
	first := true
	for p.Line < e.buf.LineCount() {
		line := e.buf.Line(p.Line)
		for p.Col < len(line) {
			r := line[p.Col]
			if r == '\\' && q != '\\' {
				p.Col += 2
				continue
			}
			if r == q {
				if first {
					first = false
				} else {
					return p, true
				}
			}
			p.Col++
		}
		p.Line++
		p.Col = 0
	}
	return Pos{}, false
}

// bracketMatch returns the position of the bracket matching the one at or
// after the cursor on the current line, scanning brackets anywhere in the
// buffer when the cursor sits on one.
func (e *Editor) bracketMatch(from Pos) (Pos, bool) {
	line := e.buf.Line(from.Line)
	opens := "([{<"
	closes := ")]}>"
	c := from.Col
	if c < len(line) {
		if i := strings.IndexRune(opens, line[c]); i >= 0 {
			if p, ok := e.bracketMatchPos(from, rune(opens[i]), rune(closes[i])); ok {
				return p, true
			}
		} else if i := strings.IndexRune(closes, line[c]); i >= 0 {
			// Walk back for this closer's opener.
			if p, ok := e.bracketMatchReverse(from, rune(opens[i]), rune(closes[i])); ok {
				return p, true
			}
		}
	}
	// Not on a bracket: try the next one on the line, then the previous.
	for i := c + 1; i < len(line); i++ {
		if j := strings.IndexRune(opens, line[i]); j >= 0 {
			p := Pos{from.Line, i}
			if m, ok := e.bracketMatchPos(p, rune(opens[j]), rune(closes[j])); ok {
				return m, true
			}
		}
	}
	for i := c - 1; i >= 0; i-- {
		if j := strings.IndexRune(closes, line[i]); j >= 0 {
			p := Pos{from.Line, i}
			if m, ok := e.bracketMatchReverse(p, rune(opens[j]), rune(closes[j])); ok {
				return m, true
			}
		}
	}
	return Pos{}, false
}

// bracketMatchReverse matches one closer backwards through the buffer.
func (e *Editor) bracketMatchReverse(from Pos, open, close rune) (Pos, bool) {
	depth := 0
	p := from
	for p.Line >= 0 {
		line := e.buf.Line(p.Line)
		for p.Col >= 0 && p.Col < len(line) {
			r := line[p.Col]
			if r == close {
				depth++
			} else if r == open {
				depth--
				if depth == 0 {
					return p, true
				}
			}
			p.Col--
		}
		p.Line--
		if p.Line >= 0 {
			p.Col = e.buf.LineLen(p.Line) - 1
		}
	}
	return Pos{}, false
}
