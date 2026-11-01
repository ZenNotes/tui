package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"

	"github.com/ZenNotes/tui/internal/vim"
)

// cursorApp opens one note holding text in a single pane, with true colour so
// the drawn block cursor can be told apart from other cells.
func cursorApp(t *testing.T, text string, width int) (*App, *noteBuffer) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	a := newSessionParityApp(t, newSessionParityVault(t, "Alpha.md"))
	a.openNote("Alpha.md", true)
	a.sidebarOpen = false
	a.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	buf := a.activeBuffer()
	if buf == nil {
		t.Fatal("no active buffer")
	}
	buf.ed.SetText(text)
	buf.ed.Feed("i")
	return a, buf
}

type screenCell struct{ x, y int }

// reverseCells lists the cells the view draws in reverse video.
func reverseCells(view string) []screenCell {
	var out []screenCell
	for y, row := range strings.Split(view, "\n") {
		var style cellbuf.Style
		parser := ansi.NewParser()
		state, x := byte(0), 0
		for len(row) > 0 {
			seq, width, n, next := ansi.DecodeSequence(row, state, parser)
			if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
				cellbuf.ReadStyle(parser.Params(), &style)
			}
			if width > 0 && style.Attrs&cellbuf.ReverseAttr != 0 {
				out = append(out, screenCell{x, y})
			}
			x += width
			row, state = row[n:], next
		}
	}
	return out
}

// cellText is the text the view draws in the cell at (x, y).
func cellText(view string, x, y int) string {
	rows := strings.Split(ansi.Strip(view), "\n")
	return ansi.Cut(rows[y], x, x+1)
}

// cellText2 is the text of the n cells starting at (x, y), for wide runes.
func cellText2(view string, x, y, n int) string {
	rows := strings.Split(ansi.Strip(view), "\n")
	return ansi.Cut(rows[y], x, x+n)
}

func TestInsertCursorBarInInsertMode(t *testing.T) {
	a, buf := cursorApp(t, "hello world\n", 80)
	buf.ed.Feed("<Esc>i")
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 4})
	view := a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("cursor = %+v, want a visible bar", a.cursor)
	}
	if got := cellText(view, a.cursor.x, a.cursor.y); got != "o" {
		t.Fatalf("bar sits on %q, want the cell of %q", got, "o")
	}
	if cells := reverseCells(view); len(cells) != 0 {
		t.Fatalf("reverse-video cells drawn in insert mode: %v", cells)
	}
}

func TestInsertCursorWrappedLine(t *testing.T) {
	text := strings.Repeat("abcdefghij ", 12) + "\n"
	a, buf := cursorApp(t, text, 40)
	a.View()
	if len(buf.rows) < 3 {
		t.Fatalf("line did not wrap: %d rows", len(buf.rows))
	}
	second := buf.rows[1]
	col := second.segStart + 3
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: col})
	view := a.View()
	rc := a.contentRect(a.activePane)
	if a.cursor.y != rc.y+second.y {
		t.Fatalf("bar row = %d, want the second wrapped row %d", a.cursor.y, rc.y+second.y)
	}
	if want := string([]rune(text)[col]); cellText(view, a.cursor.x, a.cursor.y) != want {
		t.Fatalf("bar sits on %q, want %q", cellText(view, a.cursor.x, a.cursor.y), want)
	}
}

func TestInsertCursorWideRune(t *testing.T) {
	a, buf := cursorApp(t, "ab日本cd\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	view := a.View()
	if got := cellText2(view, a.cursor.x, a.cursor.y, 2); got != "本" {
		t.Fatalf("bar sits on %q, want the first cell of 本", got)
	}
	if prev := cellText2(view, a.cursor.x-2, a.cursor.y, 2); prev != "日" {
		t.Fatalf("two cells left of the bar is %q, want 日", prev)
	}
}

func TestInsertCursorTab(t *testing.T) {
	a, buf := cursorApp(t, "a\tb\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 1})
	a.View()
	start := a.cursor.x
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	view := a.View()
	if a.cursor.x-start != 4 {
		t.Fatalf("tab is %d cells wide, want 4", a.cursor.x-start)
	}
	if got := cellText(view, a.cursor.x, a.cursor.y); got != "b" {
		t.Fatalf("bar after the tab sits on %q, want b", got)
	}
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
	a.View()
	if start-a.cursor.x != 1 {
		t.Fatalf("a is %d cells wide, want 1", start-a.cursor.x)
	}
}

func TestInsertCursorPastEndOfLine(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 4})
	a.View()
	last := a.cursor
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 5})
	view := a.View()
	if !a.cursor.visible || a.cursor.x != last.x+1 || a.cursor.y != last.y {
		t.Fatalf("cursor = %+v, want the cell after the last character %+v", a.cursor, last)
	}
	if got := cellText(view, last.x, last.y); got != "o" {
		t.Fatalf("last character cell shows %q", got)
	}
	if cells := reverseCells(view); len(cells) != 0 {
		t.Fatalf("reverse-video cells drawn past the end of the line: %v", cells)
	}
}

func TestInsertCursorReplaceModeIsUnderline(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	buf.ed.Feed("<Esc>R")
	if buf.ed.Mode() != vim.ModeReplace {
		t.Fatalf("mode = %v, want replace", buf.ed.Mode())
	}
	view := a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeUnderline {
		t.Fatalf("cursor = %+v, want a visible underline", a.cursor)
	}
	if cells := reverseCells(view); len(cells) != 0 {
		t.Fatalf("reverse-video cells drawn in replace mode: %v", cells)
	}
}

func TestInsertCursorBarWithVimModeOff(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	a.prefs.VimMode = false
	buf.ed.SetOptions(a.editorOptions())
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	view := a.View()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("cursor = %+v, want a visible bar", a.cursor)
	}
	if cells := reverseCells(view); len(cells) != 0 {
		t.Fatalf("reverse-video cells drawn with vim mode off: %v", cells)
	}
}

// Normal and visual mode keep the drawn block and show no terminal cursor.
func TestInsertCursorNormalAndVisualKeepTheBlock(t *testing.T) {
	for _, keys := range []string{"", "v", "V", "<C-v>"} {
		t.Run("keys "+keys, func(t *testing.T) {
			a, buf := cursorApp(t, "hello world\n", 80)
			buf.ed.Feed("<Esc>")
			buf.ed.SetCursor(vim.Pos{Line: 0, Col: 4})
			buf.ed.Feed(keys)
			view := a.View()
			if a.cursor.visible {
				t.Fatalf("cursor = %+v, want hidden", a.cursor)
			}
			cells := reverseCells(view)
			found := false
			for _, c := range cells {
				if cellText(view, c.x, c.y) == "o" {
					found = true
				}
			}
			if !found {
				t.Fatalf("no reverse-video cell on the cursor character; reverse cells: %v", cells)
			}
			if keys == "" && len(cells) != 1 {
				t.Fatalf("normal mode draws %d reverse cells, want 1", len(cells))
			}
		})
	}
}

func TestInsertCursorEscRestoresTheBlock(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	a.View()
	if !a.cursor.visible {
		t.Fatal("no bar in insert mode")
	}
	buf.ed.Feed("<Esc>")
	view := a.View()
	if a.cursor.visible {
		t.Fatalf("cursor = %+v after Esc, want hidden", a.cursor)
	}
	if len(reverseCells(view)) != 1 {
		t.Fatalf("block cursor not drawn after Esc: %v", reverseCells(view))
	}
}

func TestInsertCursorWaitsForTheNoteToLoad(t *testing.T) {
	for _, vimMode := range []bool{true, false} {
		a, buf := cursorApp(t, "hello\n", 80)
		a.prefs.VimMode = vimMode
		buf.ed.SetOptions(a.editorOptions())
		buf.loading = true
		view := a.View()
		if a.cursor.visible {
			t.Fatalf("vim=%v: cursor = %+v while loading, want hidden", vimMode, a.cursor)
		}
		if len(reverseCells(view)) != 1 {
			t.Fatalf("vim=%v: block not drawn while loading: %v", vimMode, reverseCells(view))
		}
		buf.loading = false
		view = a.View()
		if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
			t.Fatalf("vim=%v: cursor = %+v once loaded, want a bar", vimMode, a.cursor)
		}
		if len(reverseCells(view)) != 0 {
			t.Fatalf("vim=%v: block still drawn once loaded", vimMode)
		}
	}
}
