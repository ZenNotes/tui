package tui

import (
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// With Vim off the arrows, PgDn and the chords stay, and a letter does nothing.
func TestReadingViewKeysWithVimOff(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	a.prefs.VimMode = false
	pressAndDraw(a, vim.KeyDown)
	if pv.scroll != 1 || pv.cursor != 1 {
		t.Errorf("down: scroll %d cursor %d, want both 1", pv.scroll, pv.cursor)
	}
	pressAndDraw(a, vim.Key{Rune: 'j'})
	if pv.scroll != 1 || pv.cursor != 1 {
		t.Errorf("j moved the view with Vim off: scroll %d cursor %d", pv.scroll, pv.cursor)
	}
	pressAndDraw(a, vim.Ctrl('f'))
	if pv.scroll != 1+pv.rows {
		t.Errorf("Ctrl+F: scroll %d", pv.scroll)
	}
	if got := a.activeBuffer().ed.Text(); got != tallNote(100) {
		t.Error("keys in the reading view edited the note")
	}
}

// A switch from insert mode leaves the editor in it, and the keys still belong to
// the view on screen.
func TestReadingViewKeysAfterLeavingInsertMode(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	a.setPaneMode(modeEdit)
	a.activeBuffer().ed.HandleKey(vim.Key{Rune: 'i'})
	a.setPaneMode(modePreview)
	a.View()
	pressAndDraw(a, vim.Key{Rune: 'j'})
	if pv.scroll != 1 {
		t.Errorf("scroll %d, want 1", pv.scroll)
	}
	if got := a.activeBuffer().ed.Text(); got != tallNote(100) {
		t.Error("j was typed into the note")
	}
}

// A command line being typed goes when the view changes, and Esc then does what the
// hint says.
func TestReadingViewClosesAnOpenCommandLine(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	a.setPaneMode(modeEdit)
	buf := a.activeBuffer()
	a.View()
	pressAndDraw(a, vim.Key{Rune: ':'}, vim.Key{Rune: 'w'}, vim.Key{Rune: 'q'})
	if buf.ed.Mode() != vim.ModeCmdline {
		t.Fatalf("no command line: mode %v", buf.ed.Mode())
	}
	pressAndDraw(a, vim.Alt('p'))
	if a.activeTab().mode != modePreview {
		t.Fatalf("not in the reading view: %v", a.activeTab().mode)
	}
	if buf.ed.Mode() == vim.ModeCmdline {
		t.Error("the command line stayed open over the reading view")
	}
	if strings.Contains(stripAnsi(a.View()), ":wq") {
		t.Error("the frame still shows the command")
	}
	pressAndDraw(a, vim.Key{Rune: 'j'})
	if pv.scroll != 1 {
		t.Errorf("j: scroll %d", pv.scroll)
	}
	pressAndDraw(a, vim.KeyEsc)
	if a.activeTab().mode != modeEdit || buf.ed.Mode() == vim.ModeCmdline {
		t.Errorf("Esc: mode %v, editor %v", a.activeTab().mode, buf.ed.Mode())
	}
	if a.quitting {
		t.Error("the command ran")
	}
}
