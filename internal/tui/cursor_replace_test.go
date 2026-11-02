package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// While a normal-mode r waits for its character the cursor is an underline at
// the cursor cell and no block is drawn.
func TestReplaceCharCursorUnderlineWhileWaiting(t *testing.T) {
	for _, keys := range []string{"r", "3r"} {
		t.Run("keys "+keys, func(t *testing.T) {
			a, buf := cursorApp(t, "hello world\n", 80)
			buf.ed.Feed("<Esc>")
			buf.ed.SetCursor(vim.Pos{Line: 0, Col: 4})
			buf.ed.Feed(keys)
			if buf.ed.Mode() != vim.ModeNormal {
				t.Fatalf("mode = %v, want normal", buf.ed.Mode())
			}
			view := a.View()
			if !a.cursor.visible || a.cursor.shape != cursorShapeUnderline {
				t.Fatalf("cursor = %+v, want a visible underline", a.cursor)
			}
			if got := cellText(view, a.cursor.x, a.cursor.y); got != "o" {
				t.Fatalf("underline on %q, want the cursor character", got)
			}
			if cells := reverseCells(view); len(cells) != 0 {
				t.Fatalf("reverse-video cells drawn while r waits: %v", cells)
			}
		})
	}
}

// The character, or Esc, ends the wait: the cursor hides and the block returns.
func TestReplaceCharCursorEndsWithCharOrEsc(t *testing.T) {
	for _, tc := range []struct{ name, keys string }{
		{"character", "rx"},
		{"escape", "r<Esc>"},
		{"counted character", "2rx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, buf := cursorApp(t, "hello world\n", 80)
			buf.ed.Feed("<Esc>")
			buf.ed.SetCursor(vim.Pos{Line: 0, Col: 4})
			buf.ed.Feed(tc.keys)
			view := a.View()
			if a.cursor.visible {
				t.Fatalf("cursor = %+v, want hidden", a.cursor)
			}
			if cells := reverseCells(view); len(cells) != 1 {
				t.Fatalf("block cells = %v, want exactly one", cells)
			}
		})
	}
}

// r typed in visual mode keeps the block, as every visual-mode key does.
func TestReplaceCharCursorVisualKeepsTheBlock(t *testing.T) {
	a, buf := cursorApp(t, "hello world\n", 80)
	buf.ed.Feed("<Esc>v")
	buf.ed.Feed("r")
	view := a.View()
	if a.cursor.visible {
		t.Fatalf("cursor = %+v, want hidden", a.cursor)
	}
	if len(reverseCells(view)) == 0 {
		t.Fatal("no block drawn in visual mode")
	}
}
