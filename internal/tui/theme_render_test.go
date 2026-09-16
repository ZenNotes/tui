package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"

	"github.com/ZenNotes/tui/internal/config"
)

func TestViewPaintsEveryCellIndependentlyOfTerminalColors(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, mode := range []string{"light", "dark"} {
		t.Run(mode, func(t *testing.T) {
			a := newApp(context.Background(), Options{}, config.Prefs{ThemeMode: mode, VimMode: true}, true)
			a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			for y, row := range strings.Split(a.View(), "\n") {
				// Bubble Tea can redraw any row on its own after a resize or edit.
				var style cellbuf.Style
				parser := ansi.NewParser()
				state, x := byte(0), 0
				for len(row) > 0 {
					seq, width, n, next := ansi.DecodeSequence(row, state, parser)
					if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
						cellbuf.ReadStyle(parser.Params(), &style)
					}
					if width > 0 && (style.Bg == nil || style.Fg == nil) {
						t.Fatalf("cell (%d,%d) %q inherits terminal colors: %+v", x, y, seq, style)
					}
					x += width
					row, state = row[n:], next
				}
				if x != 80 {
					t.Fatalf("row %d has %d cells, want 80", y, x)
				}
			}
		})
	}
}

func TestThemePaintPreservesNestedColorsAndHonorsNoColor(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme := buildTheme(false)
	text := theme.Selected.Render("S") + theme.Link.Render("L") + "P\n "
	var pen cellbuf.Style
	parser := ansi.NewParser()
	var state byte
	for row := theme.paint(text); len(row) > 0; {
		seq, width, n, next := ansi.DecodeSequence(row, state, parser)
		if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
			cellbuf.ReadStyle(parser.Params(), &pen)
		}
		if width > 0 {
			fg, bg := theme.Fg, theme.Bg
			if seq == "S" {
				bg = theme.BgSelected
			} else if seq == "L" {
				fg = theme.Blue
			}
			if pen.Fg == nil || pen.Bg == nil {
				t.Fatalf("%q has unset colors: %+v", seq, pen)
			}
			r, g, b, _ := pen.Fg.RGBA()
			actualFg := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
			r, g, b, _ = pen.Bg.RGBA()
			actualBg := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
			if actualFg != string(fg) || actualBg != string(bg) {
				t.Fatalf("%q has %s on %s, want %s on %s", seq, actualFg, actualBg, fg, bg)
			}
		}
		row, state = row[n:], next
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	if got := theme.paint("plain\ntext"); got != "plain\ntext" {
		t.Fatalf("no-color output contains styling: %q", got)
	}
}
