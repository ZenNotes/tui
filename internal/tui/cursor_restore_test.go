package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

// restoreRun runs app through runProgram on a fake terminal and a pipe for
// input. feed receives the pipe's writer once the first frame is out.
type restoreRun struct {
	term *fakeTerm
	in   *io.PipeWriter
	done chan error
}

func startRestoreRun(t *testing.T, app *App, opts ...tea.ProgramOption) *restoreRun {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	r := &restoreRun{term: &fakeTerm{}, in: pw, done: make(chan error, 1)}
	opts = append([]tea.ProgramOption{tea.WithoutSignals()}, opts...)
	go func() { r.done <- runProgram(app, r.term, pr, opts...) }()
	waitFor(t, func() bool { return strings.Contains(r.term.all(), "\x1b[") && app.program != nil })
	return r
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *restoreRun) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("program did not exit")
		return nil
	}
}

func tail(s string) string {
	if len(s) > 80 {
		return s[len(s)-80:]
	}
	return s
}

// assertRestored checks the output ends with the default shape and a shown
// cursor, after every earlier shape.
func assertRestored(t *testing.T, out string) {
	t.Helper()
	reset := strings.LastIndex(out, defaultShape)
	if reset < 0 {
		t.Fatalf("no CSI 0 SP q in output (tail %q)", tail(out))
	}
	for _, shape := range []string{steadyBar, steadyUnderline} {
		if i := strings.LastIndex(out, shape); i > reset {
			t.Fatalf("shape %q written after the reset", shape)
		}
	}
	if !strings.Contains(out[reset:], "\x1b[?25h") {
		t.Fatalf("cursor not shown after the reset (tail %q)", tail(out))
	}
}

func TestCursorRestoreQuit(t *testing.T) {
	a, _ := cursorApp(t, "hello", 80)
	r := startRestoreRun(t, a)
	waitFor(t, func() bool { return strings.Contains(r.term.all(), steadyBar) })
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune(":")},
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEnter},
	} {
		a.program.Send(k)
	}
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	assertRestored(t, r.term.all())
}

func TestCursorRestoreError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, _ := cursorApp(t, "hello", 80)
	a.ctx = ctx
	r := startRestoreRun(t, a, tea.WithContext(ctx))
	cancel()
	if err := r.wait(t); err == nil {
		t.Fatal("expected an error from the cancelled run")
	}
	assertRestored(t, r.term.all())
}

type panicOverlay struct{}

func (panicOverlay) handleKey(*App, vim.Key)      { panic("boom") }
func (panicOverlay) render(*App, int, int) string { return "" }

func TestCursorRestorePanic(t *testing.T) {
	a, _ := cursorApp(t, "hello", 80)
	a.overlay = panicOverlay{}
	r := startRestoreRun(t, a)
	a.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if err := r.wait(t); err == nil {
		t.Fatal("expected an error from the recovered panic")
	}
	assertRestored(t, r.term.all())
}

func TestCursorRestoreDefaultPrefsGiveBar(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	prefs, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	a, buf := cursorApp(t, "hello", 40)
	a.rawPref, a.prefs = prefs, prefsView{Prefs: prefs, TabsOff: !prefs.TabsEnabled}
	buf.ed.Feed("i")
	a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("insert mode with default prefs: cursor = %+v", a.cursor)
	}
}
