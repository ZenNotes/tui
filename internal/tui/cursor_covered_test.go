package tui

import (
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

// coveredModes runs fn once with vim mode on and once with it off.
func coveredModes(t *testing.T, fn func(t *testing.T, vimMode bool)) {
	t.Helper()
	for _, vimMode := range []bool{true, false} {
		name := "vim"
		if !vimMode {
			name = "plain"
		}
		t.Run(name, func(t *testing.T) { fn(t, vimMode) })
	}
}

// coveredApp is cursorApp in the requested editing style, with the bar
// confirmed at the insertion point before anything covers it.
func coveredApp(t *testing.T, vimMode bool) (*App, cursorState) {
	t.Helper()
	a, buf := cursorApp(t, "hello world\n", 80)
	a.prefs.VimMode = vimMode
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("cursor = %+v before covering, want a visible bar", a.cursor)
	}
	return a, a.cursor
}

func assertCursorHidden(t *testing.T, a *App, what string) {
	t.Helper()
	a.View()
	if a.cursor.visible {
		t.Fatalf("cursor = %+v under %s, want hidden", a.cursor, what)
	}
}

func assertCursorAt(t *testing.T, a *App, want cursorState, what string) {
	t.Helper()
	a.View()
	if a.cursor != want {
		t.Fatalf("cursor = %+v %s, want %+v", a.cursor, what, want)
	}
}

func TestCursorCoveredContextMenu(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, want := coveredApp(t, vimMode)
		a.noteContextMenu("Alpha.md")
		assertCursorHidden(t, a, "a context menu")
		a.overlay = nil
		assertCursorAt(t, a, want, "after the menu closed")
	})
}

func TestCursorCoveredCentredMenu(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, want := coveredApp(t, vimMode)
		a.overlay = &menu{title: "Centre", items: []menuItem{{key: "a", label: "A", run: func(*App) {}}}}
		assertCursorHidden(t, a, "a centred menu")
		a.overlay = nil
		assertCursorAt(t, a, want, "after the menu closed")
	})
}

func TestCursorCoveredHintsOverlay(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, want := coveredApp(t, vimMode)
		a.overlay = &hintOverlay{targets: []hintTarget{{label: "a", x: 1, y: 1, length: 2, run: func(*App) {}}}}
		assertCursorHidden(t, a, "the hints overlay")
		a.overlay = nil
		assertCursorAt(t, a, want, "after hints closed")
	})
}

func TestCursorCoveredWhichKey(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, want := coveredApp(t, vimMode)
		a.startLeader()
		a.leader.showHints = true
		assertCursorHidden(t, a, "the which-key popup")
		a.leader = nil
		assertCursorAt(t, a, want, "after the popup closed")
	})
}

func TestCursorCoveredToast(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, want := coveredApp(t, vimMode)
		a.notify("saved")
		if len(a.toasts) == 0 {
			t.Fatal("no toast")
		}
		assertCursorHidden(t, a, "a toast")
		for i := range a.toasts {
			a.toasts[i].until = time.Now().Add(-time.Second)
		}
		a.expireToasts()
		assertCursorAt(t, a, want, "after the toast expired")
	})
}

func TestCursorCoveredTagMenuKeepsBar(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		prefs := vimPrefs()
		prefs.VimMode = vimMode
		a, buf := newTagTestApp(t, prefs, "\n")
		a.sidebarOpen = false
		a.layout()
		if vimMode {
			feed(a, vim.R('i'))
		}
		typeKeys(a, "see #pro")
		if a.liveTagMenu() == nil {
			t.Fatal("the tag menu did not open")
		}
		a.View()
		x, y, ok := a.insertionCell(buf, a.contentRect(a.activePane))
		if !ok {
			t.Fatal("no insertion cell")
		}
		want := cursorState{visible: true, x: x, y: y, shape: cursorShapeBar}
		if a.cursor != want {
			t.Fatalf("cursor = %+v with the tag menu open, want %+v", a.cursor, want)
		}
	})
}

func TestCursorCoveredSidePanelFocus(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, _ := coveredApp(t, vimMode)
		a.sidebarOpen = true
		a.layout()
		a.View()
		want := a.cursor
		if !want.visible {
			t.Fatalf("cursor = %+v with the sidebar open, want a bar", want)
		}
		a.focus = focusSidebar
		assertCursorHidden(t, a, "the sidebar's focus")
		a.focus = focusPane
		assertCursorAt(t, a, want, "back in the pane")

		a.toggleSidePanel(&a.outlineOpen, focusOutline)
		a.focus = focusPane
		a.View()
		want = a.cursor
		a.focus = focusOutline
		assertCursorHidden(t, a, "a side panel's focus")
		a.focus = focusPane
		assertCursorAt(t, a, want, "back in the pane")
	})
}

func TestCursorCoveredPromptFromSidebar(t *testing.T) {
	coveredModes(t, func(t *testing.T, vimMode bool) {
		a, _ := coveredApp(t, vimMode)
		a.focus = focusSidebar
		a.promptFor("Name", "ab", "", func(*App, string) {})
		view := a.View()
		assertFieldBar(t, a, view, " ")
	})
}
