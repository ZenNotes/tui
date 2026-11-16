package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ZenNotes/tui/internal/vim"
)

const outlineJumpNote = "# Plan\n\nintro\n\n## Second\n\nbody\n"

// outlineJumpApp opens a note with two headings, the outline panel open and
// the editor cursor parked on the last line.
func outlineJumpApp(t *testing.T) (*App, *noteBuffer) {
	t.Helper()
	a, buf := cursorApp(t, outlineJumpNote, 120)
	buf.ed.HandleKey(vim.KeyEsc)
	buf.ed.GotoLine(buf.ed.LineCount())
	a.toggleSidePanel(&a.outlineOpen, focusOutline)
	a.View()
	if len(a.outline.items) != 2 {
		t.Fatalf("outline has %d items, want 2", len(a.outline.items))
	}
	return a, buf
}

// The outline's lines are 1-based, as GotoLine takes them, so opening a
// heading lands on the heading and not on the line above it.
func TestOutlineEnterLandsOnHeading(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune("l")},
		{Type: tea.KeyRunes, Runes: []rune("o")},
	} {
		t.Run(key.String(), func(t *testing.T) {
			a, buf := outlineJumpApp(t)
			a.focus = focusOutline
			a.outline.list.cursor = 1
			a.Update(key)
			if got := buf.ed.Cursor().Line; got != 4 {
				t.Fatalf("cursor on line %d (%q), want 4 (## Second)", got, buf.ed.Line(got))
			}
			if a.focus != focusPane {
				t.Fatalf("focus = %v after opening a heading, want the pane", a.focus)
			}
		})
	}
}

func TestOutlineClickLandsOnHeading(t *testing.T) {
	a, buf := outlineJumpApp(t)
	main := a.mainRect()
	rows := strings.Split(ansi.Strip(a.View()), "\n")
	x, y := -1, -1
	for i, row := range rows {
		col := strings.Index(row, "Second")
		if col < 0 {
			continue
		}
		if cell := ansi.StringWidth(row[:col]); cell > main.x+main.w {
			x, y = cell, i
			break
		}
	}
	if y < 0 {
		t.Fatalf("no outline row for ## Second in the view:\n%s", strings.Join(rows, "\n"))
	}
	a.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if got := buf.ed.Cursor().Line; got != 4 {
		t.Fatalf("cursor on line %d (%q), want 4 (## Second)", got, buf.ed.Line(got))
	}
}
