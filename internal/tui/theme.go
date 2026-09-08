package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the palette the terminal UI draws with. Every color is defined
// as a light and dark pair; the pair is resolved once from theme_mode, or
// from the terminal background when the mode is "system".
type Theme struct {
	Dark bool
	Mode string

	Accent, AccentSoft, Fg, FgDim, FgMuted, Bg, BgPanel, BgSelected, BgStatus lipgloss.Color
	Red, Green, Yellow, Blue, Purple, Aqua                                    lipgloss.Color

	Base          lipgloss.Style
	Dim           lipgloss.Style
	Muted         lipgloss.Style
	Bold          lipgloss.Style
	Title         lipgloss.Style
	Selected      lipgloss.Style
	SelectedFocus lipgloss.Style
	// CursorRow is the quiet, full-width block under the cursor of a
	// navigation surface; focus is told by the border, not by the row.
	CursorRow    lipgloss.Style
	Status       lipgloss.Style
	StatusMode   lipgloss.Style
	StatusError  lipgloss.Style
	Border       lipgloss.Style
	BorderFocus  lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	Heading      lipgloss.Style
	Link         lipgloss.Style
	Tag          lipgloss.Style
	Code         lipgloss.Style
	Quote        lipgloss.Style
	Marker       lipgloss.Style
	Checkbox     lipgloss.Style
	Done         lipgloss.Style
	Cursor       lipgloss.Style
	Visual       lipgloss.Style
	SearchHit    lipgloss.Style
	Overlay      lipgloss.Style
	OverlayTitle lipgloss.Style
	Hint         lipgloss.Style
	Emphasis     lipgloss.Style
	Strong       lipgloss.Style
	Frontmatter  lipgloss.Style
	LineNumber   lipgloss.Style
	Overdue      lipgloss.Style
	KeyHint      lipgloss.Style
	Match        lipgloss.Style
}

// DetectDarkBackground asks the terminal for its background color. It must
// run before Bubble Tea takes over the terminal, so Run calls it once and
// the answer is reused for `:theme system` later.
func DetectDarkBackground() bool {
	return lipgloss.HasDarkBackground()
}

// NewTheme resolves a mode ("dark", "light", "system") against the detected
// background and builds the palette.
func NewTheme(mode string, systemDark bool) Theme {
	dark := systemDark
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	t := buildTheme(dark)
	t.Mode = mode
	return t
}

// pair is a light/dark color choice, the way Glow's styles are written.
type pair struct{ light, dark string }

func buildTheme(dark bool) Theme {
	t := Theme{Dark: dark}
	c := func(p pair) lipgloss.Color {
		if dark {
			return lipgloss.Color(p.dark)
		}
		return lipgloss.Color(p.light)
	}
	t.Accent = c(pair{"#b5651d", "#fe8019"})
	t.AccentSoft = c(pair{"#d9a066", "#d65d0e"})
	t.Fg = c(pair{"#3c3836", "#ebdbb2"})
	t.FgDim = c(pair{"#7c6f64", "#a89984"})
	t.FgMuted = c(pair{"#a89984", "#7c6f64"})
	t.Bg = c(pair{"#fbf1c7", "#1d2021"})
	t.BgPanel = c(pair{"#f2e5bc", "#282828"})
	t.BgSelected = c(pair{"#ebdbb2", "#3c3836"})
	t.BgStatus = c(pair{"#d5c4a1", "#504945"})
	t.Red = c(pair{"#9d0006", "#fb4934"})
	t.Green = c(pair{"#79740e", "#b8bb26"})
	t.Yellow = c(pair{"#b57614", "#fabd2f"})
	t.Blue = c(pair{"#076678", "#83a598"})
	t.Purple = c(pair{"#8f3f71", "#d3869b"})
	t.Aqua = c(pair{"#427b58", "#8ec07c"})

	t.Base = lipgloss.NewStyle().Foreground(t.Fg)
	t.Dim = lipgloss.NewStyle().Foreground(t.FgDim)
	t.Muted = lipgloss.NewStyle().Foreground(t.FgMuted)
	t.Bold = lipgloss.NewStyle().Bold(true).Foreground(t.Fg)
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Selected = lipgloss.NewStyle().Background(t.BgSelected).Foreground(t.Fg)
	t.SelectedFocus = lipgloss.NewStyle().Background(t.AccentSoft).Foreground(t.Bg).Bold(true)
	t.CursorRow = lipgloss.NewStyle().Background(t.BgStatus).Foreground(t.Fg).Bold(true)
	t.Status = lipgloss.NewStyle().Background(t.BgStatus).Foreground(t.Fg)
	t.StatusMode = lipgloss.NewStyle().Background(t.Accent).Foreground(t.Bg).Bold(true).Padding(0, 1)
	t.StatusError = lipgloss.NewStyle().Foreground(t.Red).Bold(true)
	t.Border = lipgloss.NewStyle().Foreground(t.FgMuted)
	// Focus brightens a border a step instead of coloring it: the accent
	// stays reserved for the active tab, the cursor and links.
	t.BorderFocus = lipgloss.NewStyle().Foreground(t.FgDim)
	t.TabActive = lipgloss.NewStyle().Background(t.Accent).Foreground(t.Bg).Bold(true)
	t.TabInactive = lipgloss.NewStyle().Foreground(t.FgDim)
	t.Heading = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Link = lipgloss.NewStyle().Foreground(t.Blue).Underline(true)
	t.Tag = lipgloss.NewStyle().Foreground(t.Aqua)
	t.Code = lipgloss.NewStyle().Foreground(t.Green)
	t.Quote = lipgloss.NewStyle().Foreground(t.FgDim).Italic(true)
	t.Marker = lipgloss.NewStyle().Foreground(t.Yellow)
	t.Checkbox = lipgloss.NewStyle().Foreground(t.Purple)
	t.Done = lipgloss.NewStyle().Foreground(t.FgMuted).Strikethrough(true)
	t.Cursor = lipgloss.NewStyle().Reverse(true)
	t.Visual = lipgloss.NewStyle().Background(t.BgSelected)
	t.SearchHit = lipgloss.NewStyle().Background(t.Yellow).Foreground(t.Bg)
	t.Overlay = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent).Foreground(t.Fg).Padding(0, 1)
	t.OverlayTitle = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Hint = lipgloss.NewStyle().Foreground(t.Yellow).Bold(true)
	t.Emphasis = lipgloss.NewStyle().Italic(true).Foreground(t.Fg)
	t.Strong = lipgloss.NewStyle().Bold(true).Foreground(t.Fg)
	t.Frontmatter = lipgloss.NewStyle().Foreground(t.FgMuted)
	t.LineNumber = lipgloss.NewStyle().Foreground(t.FgMuted)
	t.Overdue = lipgloss.NewStyle().Foreground(t.Red)
	t.KeyHint = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.Match = lipgloss.NewStyle().Foreground(t.Accent).Underline(true).Bold(true)
	return t
}
