package tui

import (
	"io"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// cursorShape is the terminal cursor shape zn asks for.
type cursorShape int

const (
	// cursorShapeBar is a steady vertical bar (DECSCUSR 6).
	cursorShapeBar cursorShape = iota
	// cursorShapeUnderline is a steady underline (DECSCUSR 4).
	cursorShapeUnderline
)

// cursorState is the cursor the terminal should show for one frame: hidden,
// or visible at a screen cell (zero-based) with a shape.
type cursorState struct {
	visible bool
	x, y    int
	shape   cursorShape
}

// cursorFile is the terminal handle the program reads from and writes to. It
// has the same method set as Bubble Tea's term.File, so window-size and
// raw-mode detection keep working through the wrapper.
type cursorFile interface {
	io.ReadWriteCloser
	Fd() uintptr
}

// cursorOut wraps the terminal output. Bubble Tea parks the cursor at the
// bottom after each frame and skips frames that did not change, so the
// wrapper appends the wanted cursor to every frame and writes it on its own
// when only the cursor changed. One mutex serialises every write.
type cursorOut struct {
	mu       sync.Mutex
	dst      cursorFile
	want     cursorState
	released bool
}

func newCursorOut(dst cursorFile) *cursorOut {
	return &cursorOut{dst: dst}
}

// sequence returns the bytes that put the terminal's cursor into state c.
func (c cursorState) sequence() string {
	if !c.visible {
		return ansi.HideCursor
	}
	style := 6
	if c.shape == cursorShapeUnderline {
		style = 4
	}
	return ansi.CursorPosition(c.x+1, c.y+1) + ansi.SetCursorStyle(style) + ansi.ShowCursor
}

// loneControlSequence reports whether p is exactly one CSI sequence, such as
// a lone cursor hide or show, rather than a rendered frame.
func loneControlSequence(p []byte) bool {
	if len(p) < 3 || p[0] != 0x1b || p[1] != '[' {
		return false
	}
	for i := 2; i < len(p); i++ {
		if p[i] >= 0x40 && p[i] <= 0x7e {
			return i == len(p)-1
		}
	}
	return false
}

// Write forwards p and, when p is a frame, appends the wanted cursor in the
// same write so nothing can land between them.
func (w *cursorOut) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.released || loneControlSequence(p) {
		return w.dst.Write(p)
	}
	out := make([]byte, 0, len(p)+32)
	out = append(out, p...)
	out = append(out, w.want.sequence()...)
	n, err := w.dst.Write(out)
	if n > len(p) {
		n = len(p)
	}
	return n, err
}

// Set records the wanted cursor and writes it at once if it changed.
func (w *cursorOut) Set(c cursorState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c == w.want {
		return
	}
	w.want = c
	if w.released {
		return
	}
	_, _ = w.dst.Write([]byte(c.sequence()))
}

// Reset puts the terminal back to its default cursor shape and shows it.
func (w *cursorOut) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.dst.Write([]byte(ansi.SetCursorStyle(0) + ansi.ShowCursor))
}

// Release stops the wrapper from adding cursor sequences, for suspend and
// external programs that own the terminal.
func (w *cursorOut) Release() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.released = true
}

// Hold resumes cursor control and writes the wanted cursor again.
func (w *cursorOut) Hold() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.released = false
	_, _ = w.dst.Write([]byte(w.want.sequence()))
}

func (w *cursorOut) Read(p []byte) (int, error) { return w.dst.Read(p) }
func (w *cursorOut) Close() error               { return w.dst.Close() }
func (w *cursorOut) Fd() uintptr                { return w.dst.Fd() }
