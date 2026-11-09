package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestListNavPagesWithCtrlFAndCtrlB(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	c := listCursor{}
	if !a.listNav(&c, vim.Ctrl('f'), 100, 10) || c.cursor != 10 {
		t.Errorf("Ctrl+F: cursor %d", c.cursor)
	}
	a.keymap = keymaps.NewResolver(map[string]string{"global.toggleSidebar": "Alt+B", "global.toggleConnections": "Alt+C"})
	if !a.listNav(&c, vim.Ctrl('b'), 100, 10) || c.cursor != 0 {
		t.Errorf("Ctrl+B: cursor %d", c.cursor)
	}
}

func TestCtrlFPagesTheReadingView(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	pressAndDraw(a, vim.Ctrl('f'))
	if pv.scroll != pv.rows || pv.cursor != pv.rows {
		t.Errorf("Ctrl+F: scroll %d cursor %d, want both %d", pv.scroll, pv.cursor, pv.rows)
	}
	pressAndDraw(a, vim.KeyPgUp)
	if pv.scroll != 0 || pv.cursor != 0 {
		t.Errorf("PgUp: scroll %d cursor %d", pv.scroll, pv.cursor)
	}
}

// Ctrl+B is the documented sidebar toggle, and that holds in every surface. A list
// gets it as page up only once the toggle moves to another key.
func TestCtrlBLeavesTheSidebarToggleAloneUntilItIsRebound(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	pressAndDraw(a, vim.KeyPgDn)
	at, open := pv.scroll, a.sidebarOpen
	pressAndDraw(a, vim.Ctrl('b'))
	if pv.scroll != at || a.sidebarOpen == open {
		t.Errorf("default keymap: scroll %d (was %d), sidebar %v (was %v)", pv.scroll, at, a.sidebarOpen, open)
	}
	a.keymap = keymaps.NewResolver(map[string]string{"global.toggleSidebar": "Alt+B", "global.toggleConnections": "Alt+C"})
	pressAndDraw(a, vim.Ctrl('b'))
	if pv.scroll != at-pv.rows && pv.scroll != 0 {
		t.Errorf("rebound: scroll %d, want a page up from %d", pv.scroll, at)
	}
}

// A chord a keymap gives to another action stays with it.
func TestCtrlFStaysWithAnActionItIsBoundTo(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.keymap = keymaps.NewResolver(map[string]string{"nav.moveDown": "Ctrl+F"})
	c := listCursor{}
	if !a.listNav(&c, vim.Ctrl('f'), 100, 10) || c.cursor != 1 {
		t.Errorf("cursor %d, want the action's one row", c.cursor)
	}
}
