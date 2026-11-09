package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// A colour code no theme uses, in a note's and a folder's name.
const nameColor = "\x1b[38;5;123m"

func colorNameApp(t *testing.T) (*App, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no file name with a control character in it")
	}
	dir := "inbox/" + nameColor + "fold"
	note := dir + "/" + nameColor + "name.md"
	a, root := linkTestApp(t, map[string]string{note: "# Note\n\n[[" + nameColor + "name|al" + nameColor + "ias]]\n\n[[other]]\n"})
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := a.loadIndexCmd()().(indexLoadedMsg)
	a.idx = m.idx
	a.openNote(note, true)
	return a, note
}

func assertNoNameColor(t *testing.T, what string, a *App) {
	t.Helper()
	if out := a.View(); strings.Contains(out, nameColor) {
		t.Errorf("%s: the frame holds the name's colour code", what)
	}
}

func TestANamesColorCodesStayOutOfTheFrame(t *testing.T) {
	a, note := colorNameApp(t)
	meta, ok := a.noteMeta(note)
	if !ok {
		t.Fatal("the note is not in the index")
	}
	assertNoNameColor(t, "note open", a)
	a.sidebar.rebuild(a)
	shown := false
	for _, r := range a.sidebar.rows {
		shown = shown || r.kind == "folder" && strings.Contains(r.label, "fold")
		if strings.Contains(r.label, nameColor) {
			t.Errorf("sidebar row %q holds the name's colour code", r.label)
		}
	}
	if !shown {
		t.Error("the sidebar does not show the folder")
	}
	a.setPaneMode(modePreview)
	assertNoNameColor(t, "reading view", a)

	for name, open := range map[string]func(){
		"confirm": func() { a.confirm("Move \""+meta.Title+"\" to trash?", func() {}) },
		"menu":    func() { a.showMenu(meta.Title, []menuItem{{key: "a", label: meta.Title, run: func(*App) {}}}) },
		"prompt":  func() { a.promptFor("Rename "+meta.Title, meta.Title, meta.Title, func(*App, string) {}) },
		"palette": func() {
			a.overlay = &palette{title: meta.Title, items: []paletteItem{{label: meta.Title}}, filtered: []paletteItem{{label: meta.Title}}}
		},
		"moved status": func() { a.notify("Moved to " + meta.Title) },
		"error status": func() { a.notifyError("open " + note + ": no such file") },
	} {
		open()
		assertNoNameColor(t, name, a)
		if a.overlay != nil {
			a.handleKey(vim.KeyDown)
			assertNoNameColor(t, name+" after a key", a)
		}
		a.overlay = nil
	}
}

// A load error names the path, in the status bar and in the pane.
func TestAPathErrorStaysClean(t *testing.T) {
	a, note := colorNameApp(t)
	a.activeBuffer().loadErr = errors.New("open " + note + ": no such file")
	assertNoNameColor(t, "load error", a)
	if out := stripAnsi(a.View()); !strings.Contains(out, "no such file") {
		t.Error("the error is not on screen")
	}
	delete(a.buffers, note)
	assertNoNameColor(t, "unloaded note", a)
}
