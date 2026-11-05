package tui

import (
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
