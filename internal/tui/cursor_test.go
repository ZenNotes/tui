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
			// The block is the character rendered with the cursor style (plus
			// the selection background inside a visual range), unchanged.
			want := a.styleFor(styleKey{cursor: true, visual: keys != ""}).Render("o")
			if !strings.Contains(view, want) {
				t.Fatalf("block cell is not drawn as %q", want)
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

// assertBarOnChar fails unless the bar is visible and sits on the cell where
// the view draws the character at the buffer's cursor.
func assertBarOnChar(t *testing.T, name string, a *App, buf *noteBuffer, view string) {
	t.Helper()
	cur := buf.ed.Cursor()
	want := string(buf.ed.LineRunes(cur.Line)[cur.Col])
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("%s: cursor = %+v, want a visible bar", name, a.cursor)
	}
	if got := cellText(view, a.cursor.x, a.cursor.y); got != want {
		t.Fatalf("%s: bar at (%d,%d) sits on %q, want %q", name, a.cursor.x, a.cursor.y, got, want)
	}
	if cells := reverseCells(view); len(cells) != 0 {
		t.Fatalf("%s: reverse-video cells drawn: %v", name, cells)
	}
}

func TestInsertCursorLayoutFollowsEditing(t *testing.T) {
	a, buf := cursorApp(t, "first line\nsecond line\nthird line\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
	a.View()
	start := a.cursor

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("xyz")})
	view := a.View()
	if a.cursor.x != start.x+3 || a.cursor.y != start.y {
		t.Fatalf("after typing the bar is %+v, want 3 cells right of %+v", a.cursor, start)
	}
	if got := cellText(view, a.cursor.x, a.cursor.y); got != "f" {
		t.Fatalf("after typing the bar sits on %q, want f", got)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	a.View()
	if a.cursor.y != start.y+1 {
		t.Fatalf("after Down the bar row is %d, want %d", a.cursor.y, start.y+1)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	view = a.View()
	assertBarOnChar(t, "after arrows", a, buf, view)

	a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	view = a.View()
	assertBarOnChar(t, "after a deletion", a, buf, view)

	target := screenCell{x: start.x + 5, y: start.y + 2}
	a.Update(tea.MouseMsg{X: target.x, Y: target.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	view = a.View()
	if a.cursor.x != target.x || a.cursor.y != target.y {
		t.Fatalf("after a click the bar is %+v, want %+v", a.cursor, target)
	}
	assertBarOnChar(t, "after a click", a, buf, view)
}

func TestInsertCursorLayoutAcrossLayouts(t *testing.T) {
	cases := []struct {
		name  string
		setup func(a *App)
	}{
		{"single pane", func(a *App) {}},
		{"vertical split", func(a *App) { a.splitPane(true) }},
		{"horizontal split", func(a *App) { a.splitPane(false) }},
		{"sidebar open", func(a *App) { a.sidebarOpen = true }},
		{"side panel open", func(a *App) { a.outlineOpen = true }},
		{"tabs hidden", func(a *App) { a.prefs.TabsOff = true }},
		{"zen mode", func(a *App) { a.zen = true }},
		{"split note mode", func(a *App) { a.setPaneMode(modeSplit) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, buf := cursorApp(t, "alpha beta\nline with Qcursor here\nomega\n", 100)
			c.setup(a)
			a.layout()
			buf.ed.SetCursor(vim.Pos{Line: 1, Col: 10})
			assertBarOnChar(t, c.name, a, buf, a.View())
		})
	}
}

func TestInsertCursorLayoutSecondPaneFocused(t *testing.T) {
	for _, vertical := range []bool{true, false} {
		a, buf := cursorApp(t, "alpha beta\nline with Qcursor here\nomega\n", 100)
		first := a.activePane
		a.splitPane(vertical)
		if a.activePane == first {
			t.Fatalf("vertical=%v: split did not move focus", vertical)
		}
		a.openNote("Alpha.md", true)
		a.layout()
		buf = a.activeBuffer()
		buf.ed.Feed("i")
		buf.ed.SetCursor(vim.Pos{Line: 1, Col: 10})
		assertBarOnChar(t, "second pane", a, buf, a.View())
		if rc := a.contentRect(a.activePane); a.cursor.x < rc.x || a.cursor.y < rc.y {
			t.Fatalf("vertical=%v: bar %+v outside the focused pane %+v", vertical, a.cursor, rc)
		}
	}
}

func TestInsertCursorLayoutFollowsResize(t *testing.T) {
	text := strings.Repeat("abcdefghi ", 10) + "\n"
	a, buf := cursorApp(t, text, 120)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 95})
	view := a.View()
	assertBarOnChar(t, "wide", a, buf, view)
	wide := a.cursor

	a.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	view = a.View()
	if a.cursor == wide {
		t.Fatalf("bar stayed at %+v after the line rewrapped", wide)
	}
	if a.cursor.y <= wide.y {
		t.Fatalf("bar row %d, want below %d once the line rewraps", a.cursor.y, wide.y)
	}
	assertBarOnChar(t, "narrow", a, buf, view)
}

// With the same note open in two panes of different sizes, the bar belongs to
// the focused pane's own wrapping whichever pane is painted first.
func TestInsertCursorLayoutFirstPaneFocusedUnequalPanes(t *testing.T) {
	text := strings.Repeat("abcdefghi ", 20) + "\n"
	for _, vertical := range []bool{true, false} {
		for _, focusFirst := range []bool{true, false} {
			a, _ := cursorApp(t, text, 100)
			first := a.activePane
			a.splitPane(vertical)
			if a.activePane == first {
				t.Fatalf("vertical=%v: split did not move focus", vertical)
			}
			a.openNote("Alpha.md", true)
			a.resizePane(0.3)
			a.layout()
			if focusFirst {
				a.activePane = first
			}
			buf := a.activeBuffer()
			buf.ed.Feed("i")
			buf.ed.SetCursor(vim.Pos{Line: 0, Col: 85})
			name := "first pane"
			if !focusFirst {
				name = "second pane"
			}
			assertBarOnChar(t, name, a, buf, a.View())
		}
	}
}
