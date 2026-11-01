package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// lastRow is the screen row of the command line.
func lastRow(a *App) int { return a.height - 1 }

func TestCmdlineCursorAfterText(t *testing.T) {
	for _, kind := range []string{":", "/", "?"} {
		for _, text := range []string{"", "wq"} {
			a, buf := cursorApp(t, "hello\n", 80)
			buf.ed.Feed("<Esc>" + kind + text)
			view := a.View()
			want := 1 + len(text)
			if !a.cursor.visible || a.cursor.shape != cursorShapeBar ||
				a.cursor.x != want || a.cursor.y != lastRow(a) {
				t.Fatalf("%q %q: cursor = %+v, want a bar at (%d,%d)", kind, text, a.cursor, want, lastRow(a))
			}
			for _, c := range reverseCells(view) {
				if c.y == lastRow(a) {
					t.Fatalf("%q %q: reverse cell in the command line at %v", kind, text, c)
				}
			}
			if got := ansi.Strip(strings.Split(view, "\n")[lastRow(a)]); !strings.HasPrefix(got, kind+text) {
				t.Fatalf("command line = %q", got)
			}
		}
	}
}

func TestCmdlineCursorMovedLeft(t *testing.T) {
	cases := []struct {
		name string
		keys string
		x    int
		on   string
	}{
		{"left once", "<Left>", 1 + 2 + 2, "c"},
		{"left twice, wide rune", "<Left><Left>", 1 + 2, "日"},
		{"ctrl+b to the start", "<C-b>", 1, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, buf := cursorApp(t, "hello\n", 80)
			buf.ed.Feed("<Esc>:ab日c" + c.keys)
			view := a.View()
			if !a.cursor.visible || a.cursor.x != c.x || a.cursor.y != lastRow(a) {
				t.Fatalf("cursor = %+v, want x %d on the last row", a.cursor, c.x)
			}
			if got := cellText2(view, a.cursor.x, a.cursor.y, cellWidth(c.on)); got != c.on {
				t.Fatalf("bar on %q, want %q", got, c.on)
			}
			for _, cell := range reverseCells(view) {
				if cell.y == lastRow(a) {
					t.Fatalf("reverse cell in the command line at %v", cell)
				}
			}
		})
	}
}

func TestCmdlineCursorClosed(t *testing.T) {
	for _, key := range []string{"<Esc>", "<CR>"} {
		a, buf := cursorApp(t, "hello\n", 80)
		buf.ed.Feed("<Esc>:" + key)
		view := a.View()
		if a.cursor.visible {
			t.Fatalf("%s: cursor = %+v, want hidden", key, a.cursor)
		}
		if cells := reverseCells(view); len(cells) == 0 {
			t.Fatalf("%s: no block cursor drawn in the buffer", key)
		}
	}
}

func TestCmdlineCursorWiderThanTerminal(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 40)
	buf.ed.Feed("<Esc>:" + strings.Repeat("a", 100))
	a.View()
	if a.cursor.visible && (a.cursor.x < 0 || a.cursor.x >= a.width) {
		t.Fatalf("cursor = %+v, want a column inside the %d-wide terminal", a.cursor, a.width)
	}
}
