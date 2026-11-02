package tui

import (
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/vim"
)

// bufferCursorShape is the terminal cursor shape the buffer wants while its
// text is being edited, or a normal-mode r waits for its character. ok is false
// when the buffer keeps the drawn block instead: in vim normal and visual mode,
// and while the note is still loading. It does not know about the command
// line; computeCursor handles that.
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
	if buf.ed.ReplacePending() {
		return cursorShapeUnderline, true
	}
	return 0, false
}

// terminalCursorOwns reports whether the terminal's own cursor stands in for
// the drawn block in buf. Only the active tab's buffer is placed that way;
// a standalone editor, such as quick capture's, is placed by its host.
func (a *App) terminalCursorOwns(buf *noteBuffer) bool {
	if buf == nil || (!buf.standalone && buf != a.activeBuffer()) {
		return false
	}
	_, ok := a.bufferCursorShape(buf)
	return ok
}

// insertionCell is the screen cell where the next typed character in buf
// lands: the pane's content origin, the gutter, and the width of the text
// before the cursor on its wrapped row, all taken from the focused pane's own
// layout. rc is the origin of the area buf was drawn in.
func (a *App) insertionCell(buf *noteBuffer, rc rect) (x, y int, ok bool) {
	cur := buf.ed.Cursor()
	line := buf.ed.LineRunes(cur.Line)
	for _, row := range buf.focusRows {
		if row.line != cur.Line || cur.Col < row.segStart || cur.Col > row.segEnd {
			continue
		}
		if cur.Col == row.segEnd && row.segEnd < len(line) {
			continue // the cursor starts the next wrapped row
		}
		x = rc.x + row.x + cellWidth(string(line[row.segStart:cur.Col]))
		if buf.focusW > 0 {
			x = min(x, rc.x+buf.focusW-1) // keep the bar inside the editor
		}
		return x, rc.y + row.y, true
	}
	return 0, 0, false
}

// computeCursor picks the cursor for the frame just drawn. A toast hides it
// only when it is drawn over the cursor's cell: toasts take no keys, and the
// user keeps typing while one shows.
func (a *App) computeCursor() cursorState {
	c := a.uncoveredCursor()
	if c.visible && len(a.toasts) > 0 {
		for _, r := range a.toastRects {
			if c.x >= r.x && c.x < r.x+r.w && c.y >= r.y && c.y < r.y+r.h {
				return cursorState{}
			}
		}
	}
	return c
}

// uncoveredCursor is the cursor for the frame, toasts aside.
func (a *App) uncoveredCursor() cursorState {
	if a.leader != nil && a.leader.showHints {
		return cursorState{} // the which-key popup covers the pane
	}
	if a.overlay != nil {
		f, ok := a.overlay.(cursorField)
		if !ok {
			return cursorState{}
		}
		c := f.fieldCursor()
		if c.visible {
			c.x += a.overlayRect.x
			c.y += a.overlayRect.y
		}
		return c
	}
	if a.focus != focusPane {
		return cursorState{}
	}
	if a.cmdlineOpen {
		return cursorState{visible: true, x: a.cmdlineX, y: a.height - 1, shape: cursorShapeBar}
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
	x, y, ok := a.insertionCell(buf, a.contentRect(a.activePane))
	if !ok {
		return cursorState{}
	}
	return cursorState{visible: true, x: x, y: y, shape: shape}
}

// suspendCursor hands the terminal to something else (the shell or an
// external program): the cursor goes back to the terminal's default shape
// and the wrapper stops adding sequences until resumeCursor.
func (a *App) suspendCursor() {
	if a.cursorOut != nil {
		a.cursorOut.Reset()
		a.cursorOut.Release()
	}
}

// resumeCursor takes the terminal back and writes the cursor again.
func (a *App) resumeCursor() {
	if a.cursorOut != nil {
		a.cursorOut.Hold()
	}
}

// canSuspend is false where Bubble Tea cannot suspend: on Windows Suspend
// does nothing and no ResumeMsg follows, so the cursor must stay zn's.
var canSuspend = runtime.GOOS != "windows"

// suspend hands the terminal to the shell until `fg`.
func (a *App) suspend() {
	if canSuspend {
		a.suspendCursor()
	}
	a.queue(tea.Suspend)
}

// execOnTerminal hands the terminal to cmd until it exits. Bubble Tea gives
// the program it runs the program's own output, which is the cursor wrapper,
// and a writer that is not a file reaches the child as a pipe: Vim then warns
// that its output is not a terminal and Neovim draws nothing. So cmd writes
// to the terminal itself.
func (a *App) execOnTerminal(cmd *exec.Cmd, done tea.ExecCallback) {
	if cmd.Stdout == nil && a.cursorOut != nil {
		cmd.Stdout = a.cursorOut.terminal()
	}
	a.suspendCursor()
	a.queue(tea.ExecProcess(cmd, done))
}
