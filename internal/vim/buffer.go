package vim

import (
	"strings"
	"unicode"
)

// Pos is a position in the buffer: a zero-based line and a rune column.
type Pos struct {
	Line, Col int
}

// Less orders positions.
func (p Pos) Less(q Pos) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

func minPos(a, b Pos) Pos {
	if b.Less(a) {
		return b
	}
	return a
}

func maxPos(a, b Pos) Pos {
	if a.Less(b) {
		return b
	}
	return a
}

// Buffer is the text: a slice of lines without their newlines. Line slices
// are treated as immutable, so an undo snapshot is just a copy of the outer
// slice.
type Buffer struct {
	lines [][]rune
}

// NewBuffer splits text into lines; CRLF is normalized to LF.
func NewBuffer(text string) *Buffer {
	b := &Buffer{}
	b.SetText(text)
	return b
}

// SetText replaces the whole buffer.
func (b *Buffer) SetText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n")
	b.lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.lines[i] = []rune(p)
	}
}

// Text joins the lines with LF.
func (b *Buffer) Text() string {
	parts := make([]string, len(b.lines))
	for i, l := range b.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// Lines copies every line as a string.
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	for i, l := range b.lines {
		out[i] = string(l)
	}
	return out
}

// LineCount is the number of lines, at least one.
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line is a line's runes. Callers must not mutate the slice.
func (b *Buffer) Line(i int) []rune {
	if i < 0 || i >= len(b.lines) {
		return nil
	}
	return b.lines[i]
}

// LineString is a line as a string.
func (b *Buffer) LineString(i int) string { return string(b.Line(i)) }

// LineLen is a line's rune count.
func (b *Buffer) LineLen(i int) int { return len(b.Line(i)) }

// Snapshot copies the outer slice, enough for undo since lines are never
// mutated in place.
func (b *Buffer) Snapshot() [][]rune {
	return append([][]rune(nil), b.lines...)
}

// Restore puts a snapshot back.
func (b *Buffer) Restore(lines [][]rune) {
	b.lines = append([][]rune(nil), lines...)
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
}

func cloneRunes(r []rune) []rune {
	return append([]rune(nil), r...)
}

// ReplaceLine swaps a line's content.
func (b *Buffer) ReplaceLine(i int, text []rune) {
	if i < 0 || i >= len(b.lines) {
		return
	}
	b.lines[i] = cloneRunes(text)
}

// InsertLines inserts lines before index at (at may equal LineCount).
func (b *Buffer) InsertLines(at int, lines []string) {
	if at < 0 {
		at = 0
	}
	if at > len(b.lines) {
		at = len(b.lines)
	}
	inserted := make([][]rune, len(lines))
	for i, l := range lines {
		inserted[i] = []rune(l)
	}
	next := make([][]rune, 0, len(b.lines)+len(inserted))
	next = append(next, b.lines[:at]...)
	next = append(next, inserted...)
	next = append(next, b.lines[at:]...)
	b.lines = next
}

// DeleteLines removes lines from..to inclusive and returns them. The buffer
// always keeps one line.
func (b *Buffer) DeleteLines(from, to int) []string {
	if from < 0 {
		from = 0
	}
	if to >= len(b.lines) {
		to = len(b.lines) - 1
	}
	if from > to {
		return nil
	}
	removed := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		removed = append(removed, string(b.lines[i]))
	}
	next := make([][]rune, 0, len(b.lines)-(to-from+1))
	next = append(next, b.lines[:from]...)
	next = append(next, b.lines[to+1:]...)
	if len(next) == 0 {
		next = [][]rune{{}}
	}
	b.lines = next
	return removed
}

// ReplaceLines swaps lines from..to (inclusive) for new ones. Replacing
// every line replaces the buffer's mandatory single line too, so no empty
// line lingers.
func (b *Buffer) ReplaceLines(from, to int, lines []string) {
	if from <= 0 && to >= len(b.lines)-1 {
		if len(lines) == 0 {
			lines = []string{""}
		}
		next := make([][]rune, len(lines))
		for i, l := range lines {
			next[i] = []rune(l)
		}
		b.lines = next
		return
	}
	b.DeleteLines(from, to)
	b.InsertLines(from, lines)
}

// InsertText inserts text (which may contain newlines) at p and returns the
// position just after it.
func (b *Buffer) InsertText(p Pos, text string) Pos {
	p = b.clampInsert(p)
	line := b.lines[p.Line]
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		next := make([]rune, 0, len(line)+len([]rune(text)))
		next = append(next, line[:p.Col]...)
		next = append(next, []rune(text)...)
		next = append(next, line[p.Col:]...)
		b.lines[p.Line] = next
		return Pos{p.Line, p.Col + len([]rune(text))}
	}
	head := cloneRunes(line[:p.Col])
	tail := cloneRunes(line[p.Col:])
	first := append(head, []rune(parts[0])...)
	lastRunes := []rune(parts[len(parts)-1])
	last := append(cloneRunes(lastRunes), tail...)
	middle := make([][]rune, 0, len(parts))
	middle = append(middle, first)
	for _, m := range parts[1 : len(parts)-1] {
		middle = append(middle, []rune(m))
	}
	middle = append(middle, last)
	next := make([][]rune, 0, len(b.lines)+len(middle)-1)
	next = append(next, b.lines[:p.Line]...)
	next = append(next, middle...)
	next = append(next, b.lines[p.Line+1:]...)
	b.lines = next
	return Pos{p.Line + len(parts) - 1, len(lastRunes)}
}

// Range is the text between start (inclusive) and end (exclusive).
func (b *Buffer) Range(start, end Pos) string {
	start, end = b.clampInsert(start), b.clampInsert(end)
	if end.Less(start) {
		start, end = end, start
	}
	if start.Line == end.Line {
		return string(b.lines[start.Line][start.Col:end.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[start.Line][start.Col:]))
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[i]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[end.Line][:end.Col]))
	return sb.String()
}

// DeleteRange removes the text between start (inclusive) and end
// (exclusive) and returns it.
func (b *Buffer) DeleteRange(start, end Pos) string {
	start, end = b.clampInsert(start), b.clampInsert(end)
	if end.Less(start) {
		start, end = end, start
	}
	removed := b.Range(start, end)
	head := cloneRunes(b.lines[start.Line][:start.Col])
	tail := b.lines[end.Line][end.Col:]
	joined := append(head, tail...)
	next := make([][]rune, 0, len(b.lines))
	next = append(next, b.lines[:start.Line]...)
	next = append(next, joined)
	next = append(next, b.lines[end.Line+1:]...)
	b.lines = next
	return removed
}

// clampInsert keeps a position inside the buffer, allowing the column just
// past the last rune.
func (b *Buffer) clampInsert(p Pos) Pos {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	if p.Col < 0 {
		p.Col = 0
	}
	if p.Col > len(b.lines[p.Line]) {
		p.Col = len(b.lines[p.Line])
	}
	return p
}

// clampNormal keeps a position on a character, the way a normal-mode
// cursor sits.
func (b *Buffer) clampNormal(p Pos) Pos {
	p = b.clampInsert(p)
	if n := len(b.lines[p.Line]); p.Col >= n {
		p.Col = max(0, n-1)
	}
	return p
}

// --- character classes ---

// isWordRune is Vim's `iskeyword`: letters, digits and underscore.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

func isBlank(r rune) bool {
	return r == ' ' || r == '\t'
}

// charClass buckets a rune for word motions: 0 blank, 1 keyword, 2 other.
func charClass(r rune, bigWord bool) int {
	if isBlank(r) {
		return 0
	}
	if bigWord || isWordRune(r) {
		return 1
	}
	return 2
}

func firstNonBlank(line []rune) int {
	for i, r := range line {
		if !isBlank(r) {
			return i
		}
	}
	return max(0, len(line)-1)
}

func firstNonBlankInsert(line []rune) int {
	for i, r := range line {
		if !isBlank(r) {
			return i
		}
	}
	return len(line)
}

func leadingWhitespace(line []rune) []rune {
	i := 0
	for i < len(line) && isBlank(line[i]) {
		i++
	}
	return cloneRunes(line[:i])
}

func isBlankLine(line []rune) bool {
	for _, r := range line {
		if !isBlank(r) {
			return false
		}
	}
	return true
}
