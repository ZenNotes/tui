package tui

import "github.com/ZenNotes/tui/internal/vim"

// bufferCursorShape is the terminal cursor shape the buffer wants while its
// text is being edited. ok is false when the buffer keeps the drawn block
// instead: in normal and visual mode, in the command line, and while the
// note is still loading.
func (a *App) bufferCursorShape(buf *noteBuffer) (shape cursorShape, ok bool) {
	if buf == nil || buf.loading {
		return 0, false
	}
	if !a.prefs.VimMode {
		return cursorShapeBar, true
	}
	switch buf.ed.Mode() {
	case vim.ModeInsert:
		return cursorShapeBar, true
	case vim.ModeReplace:
		return cursorShapeUnderline, true
	}
	return 0, false
}

// terminalCursorOwns reports whether the terminal's own cursor stands in for
// the drawn block in buf. Only the active tab's buffer is placed that way;
// other editors, such as quick capture's, keep drawing their block.
func (a *App) terminalCursorOwns(buf *noteBuffer) bool {
	if buf == nil || buf != a.activeBuffer() {
		return false
	}
	_, ok := a.bufferCursorShape(buf)
	return ok
}

// insertionCell is the screen cell where the next typed character in buf
// lands: the pane's content origin, the gutter, and the width of the text
// before the cursor on its wrapped row, all taken from the focused pane's own
// layout.
func (a *App) insertionCell(buf *noteBuffer) (x, y int, ok bool) {
	cur := buf.ed.Cursor()
	line := buf.ed.LineRunes(cur.Line)
	rc := a.contentRect(a.activePane)
	for _, row := range buf.focusRows {
		if row.line != cur.Line || cur.Col < row.segStart || cur.Col > row.segEnd {
			continue
		}
		if cur.Col == row.segEnd && row.segEnd < len(line) {
			continue // the cursor starts the next wrapped row
		}
		return rc.x + row.x + cellWidth(string(line[row.segStart:cur.Col])), rc.y + row.y, true
	}
	return 0, 0, false
}

// computeCursor picks the cursor for the frame just drawn.
func (a *App) computeCursor() cursorState {
	if a.overlay != nil || a.focus != focusPane {
		return cursorState{}
	}
	t := a.activeTab()
	if t == nil || t.view != nil || t.mode == modePreview {
		return cursorState{}
	}
	buf := a.activeBuffer()
	shape, ok := a.bufferCursorShape(buf)
	if !ok {
		return cursorState{}
	}
	x, y, ok := a.insertionCell(buf)
	if !ok {
		return cursorState{}
	}
	return cursorState{visible: true, x: x, y: y, shape: shape}
}
