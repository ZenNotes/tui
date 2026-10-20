package vim

import "strings"

// Heading folds: a fold hides the section beneath a markdown heading, up to
// the next heading of the same or a higher level.

type section struct {
	start, end, level int
}

func (e *Editor) sections() []section {
	headings := e.headingLines()
	out := []section{}
	for i, h := range headings {
		level := headingLevel(e.buf.LineString(h))
		end := e.buf.LineCount() - 1
		for _, next := range headings[i+1:] {
			if headingLevel(e.buf.LineString(next)) <= level {
				end = next - 1
				break
			}
		}
		out = append(out, section{start: h, end: end, level: level})
	}
	return out
}

func headingLevel(line string) int {
	trimmed := strings.TrimLeft(line, " ")
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	return n
}

// enclosingSection is the innermost section containing line.
func (e *Editor) enclosingSection(line int) (section, bool) {
	var best section
	found := false
	for _, s := range e.sections() {
		if s.start <= line && line <= s.end {
			if !found || s.start >= best.start {
				best, found = s, true
			}
		}
	}
	return best, found
}

func (e *Editor) foldAtCursor() {
	s, ok := e.enclosingSection(e.cursor.Line)
	if !ok || s.end <= s.start {
		return
	}
	e.folds[s.start] = s.end
	e.cursor = e.buf.clampNormal(Pos{s.start, e.cursor.Col})
}

func (e *Editor) unfoldAtCursor() {
	if start, ok := e.foldContaining(e.cursor.Line); ok {
		delete(e.folds, start)
		return
	}
	delete(e.folds, e.cursor.Line)
}

func (e *Editor) toggleFoldAtCursor() {
	if _, ok := e.foldContaining(e.cursor.Line); ok {
		e.unfoldAtCursor()
		return
	}
	e.foldAtCursor()
}

func (e *Editor) foldAll() {
	for _, s := range e.sections() {
		if s.end > s.start {
			e.folds[s.start] = s.end
		}
	}
	if start, ok := e.foldContaining(e.cursor.Line); ok {
		e.cursor = e.buf.clampNormal(Pos{start, e.cursor.Col})
	}
}

func (e *Editor) unfoldAll() {
	e.folds = map[int]int{}
}

// foldContaining is the start of the outermost closed fold covering line.
func (e *Editor) foldContaining(line int) (int, bool) {
	best, found := -1, false
	for start, end := range e.folds {
		if start <= line && line <= end && (!found || start < best) {
			best, found = start, true
		}
	}
	return best, found
}

// IsFoldStart reports whether line starts a closed fold, and where it ends.
func (e *Editor) IsFoldStart(line int) (int, bool) {
	end, ok := e.folds[line]
	if !ok {
		return 0, false
	}
	if start, covered := e.foldContaining(line); covered && start != line {
		return 0, false
	}
	return end, true
}

// Hidden reports whether a line is inside a closed fold (not its first line).
func (e *Editor) Hidden(line int) bool {
	start, ok := e.foldContaining(line)
	return ok && start != line
}

// openFoldAt opens the fold covering line when the cursor lands inside it.
func (e *Editor) openFoldAt(line int) {
	for {
		start, ok := e.foldContaining(line)
		if !ok || start == line {
			return
		}
		delete(e.folds, start)
	}
}

func (e *Editor) moveToFold(dir, n int) {
	starts := []int{}
	for s := range e.folds {
		starts = append(starts, s)
	}
	for i := 0; i < len(starts); i++ {
		for j := i + 1; j < len(starts); j++ {
			if starts[j] < starts[i] {
				starts[i], starts[j] = starts[j], starts[i]
			}
		}
	}
	target := -1
	if dir > 0 {
		count := 0
		for _, s := range starts {
			if s > e.cursor.Line {
				count++
				target = s
				if count == n {
					break
				}
			}
		}
	} else {
		count := 0
		for i := len(starts) - 1; i >= 0; i-- {
			if starts[i] < e.cursor.Line {
				count++
				target = starts[i]
				if count == n {
					break
				}
			}
		}
	}
	if target >= 0 {
		e.cursor = e.buf.clampNormal(Pos{target, 0})
	}
}

// repairFolds drops folds the text no longer supports.
func (e *Editor) repairFolds() {
	if len(e.folds) == 0 {
		return
	}
	sections := e.sections()
	byStart := map[int]section{}
	for _, s := range sections {
		byStart[s.start] = s
	}
	for start := range e.folds {
		s, ok := byStart[start]
		if !ok || s.end <= s.start {
			delete(e.folds, start)
			continue
		}
		e.folds[start] = s.end
	}
}

// Folds lists closed folds as start..end pairs.
func (e *Editor) Folds() map[int]int {
	out := map[int]int{}
	for k, v := range e.folds {
		out[k] = v
	}
	return out
}
