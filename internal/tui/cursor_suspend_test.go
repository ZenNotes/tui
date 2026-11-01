package tui

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/vim"
)

// suspendApp is an insert-mode app whose wrapper sits over a fake terminal,
// with the bar already written at the insertion point.
func suspendApp(t *testing.T) (*App, *noteBuffer, *fakeTerm) {
	t.Helper()
	a, buf := cursorApp(t, "hello world\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	term := &fakeTerm{}
	a.cursorOut = newCursorOut(term)
	a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("cursor = %+v, want a visible bar", a.cursor)
	}
	term.clear()
	return a, buf, term
}

func cursorReleased(a *App) bool {
	a.cursorOut.mu.Lock()
	defer a.cursorOut.mu.Unlock()
	return a.cursorOut.released
}

func assertReleasedWithReset(t *testing.T, a *App, term *fakeTerm, what string) {
	t.Helper()
	if !strings.Contains(term.all(), defaultShape) {
		t.Fatalf("%s: wrote %q, want %q", what, term.all(), defaultShape)
	}
	if !cursorReleased(a) {
		t.Fatalf("%s: the wrapper is not released", what)
	}
}

func TestCursorSuspendCtrlZ(t *testing.T) {
	a, _, term := suspendApp(t)
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	assertReleasedWithReset(t, a, term, "Ctrl+Z")
}

func TestCursorSuspendExCommand(t *testing.T) {
	a, buf, term := suspendApp(t)
	if _, err := a.runEx(buf, vim.ExCommand{Name: "suspend"}); err != nil {
		t.Fatal(err)
	}
	assertReleasedWithReset(t, a, term, ":suspend")
}

func TestCursorSuspendResumeWritesBar(t *testing.T) {
	a, _, term := suspendApp(t)
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	term.clear()
	a.Update(tea.ResumeMsg{})
	want := cursorState{visible: true, x: a.cursor.x, y: a.cursor.y, shape: cursorShapeBar}.sequence()
	if !strings.HasSuffix(term.all(), want) {
		t.Fatalf("after resume wrote %q, want it to end with %q", term.all(), want)
	}
	if cursorReleased(a) {
		t.Fatal("the wrapper is still released after resume")
	}
}

func TestCursorSuspendExternalEditor(t *testing.T) {
	t.Setenv("EDITOR", "true")
	t.Setenv("VISUAL", "")
	a, _, term := suspendApp(t)
	a.editActiveInEditor()
	assertReleasedWithReset(t, a, term, "the external editor")
}

func TestCursorSuspendOpenConfig(t *testing.T) {
	t.Setenv("EDITOR", "true")
	t.Setenv("VISUAL", "")
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	a, _, term := suspendApp(t)
	if err := a.editConfig(); err != nil {
		t.Fatal(err)
	}
	assertReleasedWithReset(t, a, term, "open config")
}

func TestCursorExternalProgramWritesToTheTerminal(t *testing.T) {
	a, _, term := suspendApp(t)
	cmd := exec.Command("true")
	a.execOnTerminal(cmd, nil)
	if cmd.Stdout != io.Writer(term) {
		t.Fatalf("the program writes to %T, want the terminal itself", cmd.Stdout)
	}
	assertReleasedWithReset(t, a, term, "an external program")

	own := &strings.Builder{}
	cmd = exec.Command("true")
	cmd.Stdout = own
	a.execOnTerminal(cmd, nil)
	if cmd.Stdout != io.Writer(own) {
		t.Fatal("a stdout the command already had was replaced")
	}
}

func returnMsgs() map[string]tea.Msg {
	return map[string]tea.Msg{
		"editor": editorDoneMsg{path: "Alpha.md"},
		"config": configEditedMsg{},
	}
}

func TestCursorSuspendReturnInInsertShowsBar(t *testing.T) {
	for name, msg := range returnMsgs() {
		t.Run(name, func(t *testing.T) {
			a, _, term := suspendApp(t)
			a.suspendCursor()
			term.clear()
			a.Update(msg)
			a.toasts = nil
			a.View()
			if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
				t.Fatalf("cursor = %+v, want a visible bar", a.cursor)
			}
			if want := a.cursor.sequence(); !strings.HasSuffix(term.all(), want) {
				t.Fatalf("wrote %q, want it to end with %q", term.all(), want)
			}
		})
	}
}

func TestCursorSuspendReturnInNormalHidesCursor(t *testing.T) {
	for name, msg := range returnMsgs() {
		t.Run(name, func(t *testing.T) {
			a, buf, term := suspendApp(t)
			a.suspendCursor()
			buf.ed.Feed("<Esc>")
			term.clear()
			a.Update(msg)
			a.toasts = nil
			a.View()
			if a.cursor.visible {
				t.Fatalf("cursor = %+v in normal mode, want hidden", a.cursor)
			}
			if !strings.HasSuffix(term.all(), "\x1b[?25l") {
				t.Fatalf("wrote %q, want it to end with the hide sequence", term.all())
			}
		})
	}
}
