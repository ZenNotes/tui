package tui

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// fakeTerm is an in-memory term.File that records every Write call.
type fakeTerm struct {
	mu     sync.Mutex
	writes []string
	fd     uintptr
}

func (f *fakeTerm) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, string(p))
	return len(p), nil
}
func (f *fakeTerm) Read(p []byte) (int, error) { return 0, nil }
func (f *fakeTerm) Close() error               { return nil }
func (f *fakeTerm) Fd() uintptr                { return f.fd }

func (f *fakeTerm) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.writes, "")
}

func (f *fakeTerm) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func (f *fakeTerm) clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = nil
}

const (
	steadyBar       = "\x1b[6 q"
	blinkingBar     = "\x1b[5 q"
	steadyUnderline = "\x1b[4 q"
	blinkUnderline  = "\x1b[3 q"
	defaultShape    = "\x1b[0 q"
)

func TestCursorOutBarIsSteadyBar(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 3, y: 2, shape: cursorShapeBar})
	f.clear()
	_, _ = w.Write([]byte("frame text"))
	got := f.all()
	if !strings.Contains(got, steadyBar) {
		t.Fatalf("bar frame lacks CSI 6 SP q: %q", got)
	}
	if strings.Contains(got, blinkingBar) {
		t.Fatalf("bar frame contains blinking CSI 5 SP q: %q", got)
	}
}

func TestCursorOutUnderlineIsSteadyUnderline(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 3, y: 2, shape: cursorShapeUnderline})
	f.clear()
	_, _ = w.Write([]byte("frame text"))
	got := f.all()
	if !strings.Contains(got, steadyUnderline) {
		t.Fatalf("underline frame lacks CSI 4 SP q: %q", got)
	}
	if strings.Contains(got, blinkUnderline) {
		t.Fatalf("underline frame contains blinking CSI 3 SP q: %q", got)
	}
}

func TestCursorOutFrameWriteIsFollowedByPositionShapeShow(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 7, y: 4, shape: cursorShapeBar})
	f.clear()
	_, _ = w.Write([]byte("frame text"))
	want := "frame text" + ansi.CursorPosition(8, 5) + steadyBar + ansi.ShowCursor
	if got := f.all(); got != want {
		t.Fatalf("frame output = %q, want %q", got, want)
	}
}

func TestCursorOutHiddenAppendsHideCursor(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{})
	f.clear()
	_, _ = w.Write([]byte("frame text"))
	want := "frame text" + ansi.HideCursor
	if got := f.all(); got != want {
		t.Fatalf("frame output = %q, want %q", got, want)
	}
}

func TestCursorOutLoneControlSequenceGetsNothingAppended(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 1, y: 1, shape: cursorShapeBar})
	f.clear()
	_, _ = w.Write([]byte(ansi.HideCursor))
	if got := f.all(); got != ansi.HideCursor {
		t.Fatalf("lone control sequence output = %q, want %q", got, ansi.HideCursor)
	}
}

func TestCursorOutSetChangedWritesAtOnce(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 1, y: 1, shape: cursorShapeBar})
	f.clear()
	w.Set(cursorState{visible: true, x: 5, y: 1, shape: cursorShapeBar})
	got := f.all()
	if !strings.Contains(got, ansi.CursorPosition(6, 2)) || !strings.Contains(got, steadyBar) {
		t.Fatalf("changed Set wrote %q, want position and shape", got)
	}
}

func TestCursorOutSetUnchangedWritesNothing(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	c := cursorState{visible: true, x: 1, y: 1, shape: cursorShapeBar}
	w.Set(c)
	f.clear()
	w.Set(c)
	if got := f.all(); got != "" {
		t.Fatalf("unchanged Set wrote %q, want nothing", got)
	}
}

func TestCursorOutReleasedAppendsNothing(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 1, y: 1, shape: cursorShapeBar})
	w.Release()
	f.clear()
	_, _ = w.Write([]byte("frame text"))
	if got := f.all(); got != "frame text" {
		t.Fatalf("released frame output = %q, want %q", got, "frame text")
	}
	w.Set(cursorState{visible: true, x: 9, y: 9, shape: cursorShapeBar})
	if got := f.all(); got != "frame text" {
		t.Fatalf("released Set wrote %q", got)
	}
}

func TestCursorOutResetWritesDefaultShapeAndShowCursor(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	w.Set(cursorState{visible: true, x: 1, y: 1, shape: cursorShapeBar})
	f.clear()
	w.Reset()
	want := defaultShape + ansi.ShowCursor
	if got := f.all(); got != want {
		t.Fatalf("Reset wrote %q, want %q", got, want)
	}
}

func TestCursorOutConcurrentWritesInterleaveOnlyAtSequenceBoundaries(t *testing.T) {
	f := &fakeTerm{}
	w := newCursorOut(f)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = w.Write([]byte("frame text"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			w.Set(cursorState{visible: true, x: i % 20, y: i % 5, shape: cursorShapeBar})
		}
	}()
	wg.Wait()
	// Each recorded write is a whole unit: a frame followed by its cursor
	// sequence, or a complete cursor sequence. Never two frames in one
	// write, and a frame always starts a write.
	for i, c := range f.calls() {
		if strings.Count(c, "frame text") > 1 {
			t.Fatalf("write %d holds two frames: %q", i, c)
		}
		if idx := strings.Index(c, "frame text"); idx > 0 {
			t.Fatalf("write %d has a cursor sequence before the frame: %q", i, c)
		}
		if strings.HasSuffix(c, "\x1b") || strings.HasSuffix(c, "\x1b[") {
			t.Fatalf("write %d ends inside an escape sequence: %q", i, c)
		}
	}
}

func TestCursorOutFdIsForwarded(t *testing.T) {
	f := &fakeTerm{fd: 42}
	w := newCursorOut(f)
	if got := w.Fd(); got != 42 {
		t.Fatalf("Fd() = %d, want 42", got)
	}
}
