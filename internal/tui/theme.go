package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/themes"
)

// Theme is the palette the terminal UI draws with. It is built from one of
// the desktop app's color schemes (internal/themes), resolved to light or
// dark from theme_mode, or from the terminal background when the mode is
// auto.
type Theme struct {
	// ID and Name identify the color scheme: "dark-hard", "Gruvbox · Dark Hard".
	ID, Name string
	Dark     bool
	Mode     string
	// ChromaStyle is the registered chroma style that colors fenced code
	// with this palette, in the editor and in the preview.
	ChromaStyle string

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

// themeSelection is the theme the preferences ask for: the desktop's
// [appearance] keys, unless [terminal] theme names one for zn alone.
func themeSelection(p config.Prefs) (themes.Selection, error) {
	sel := themes.Selection{Family: p.ThemeFamily, Mode: p.ThemeMode, ID: p.ThemeID}
	if name := strings.TrimSpace(p.TerminalTheme); name != "" {
		own, err := themes.Select(name, sel, config.CustomThemesDir())
		if err != nil {
			return sel, fmt.Errorf("[terminal] theme: %w", err)
		}
		return own, nil
	}
	return sel, nil
}

// NewTheme resolves a selection against the detected background and builds
// the palette, the desktop's Quick tweaks applied. A selection that cannot
// be honored still yields a usable theme, along with the reason.
func NewTheme(sel themes.Selection, tweaks map[string]string, systemDark bool) (Theme, error) {
	p, err := themes.Resolve(sel, systemDark, config.CustomThemesDir())
	p.ApplyTweaks(tweaks)
	t := themeFromPalette(p)
	t.Mode = themes.NormalizeMode(sel.Mode)
	return t, err
}

// setTheme switches to a selection and drops everything painted with the
// old palette. The theme changes even when the selection could only be
// honored in part; the error says what fell back.
func (a *App) setTheme(sel themes.Selection) error {
	th, err := NewTheme(sel, a.rawPref.ThemeTweaks, a.systemDark)
	a.themeSel = sel
	a.prefs.ThemeFamily, a.prefs.ThemeMode, a.prefs.ThemeID = sel.Family, sel.Mode, sel.ID
	a.theme = th
	a.styleCache = nil
	a.glamour = nil
	for _, buf := range a.buffers {
		buf.codeHL = nil
	}
	a.invalidatePreviews()
	return err
}

// paint supplies terminal-independent colors for every row, including the
// whitespace and unstyled text after nested styles reset their colors.
func (t Theme) paint(screen string) string {
	fg := stylePrefix(t.Base)
	bg := backgroundSeq(t.Bg)
	if fg == "" && bg == "" {
		return screen
	}
	base := fg + bg
	reset := strings.NewReplacer(
		"\x1b[0m", "\x1b[0m"+base,
		"\x1b[m", "\x1b[m"+base,
		"\x1b[39m", fg,
		"\x1b[49m", bg,
	)
	rows := strings.Split(screen, "\n")
	for i, row := range rows {
		rows[i] = base + reset.Replace(row) + "\x1b[0m"
	}
	return strings.Join(rows, "\n")
}

// buildTheme is the stock Gruvbox palette, for tests and for callers that
// only know whether the background is dark.
func buildTheme(dark bool) Theme {
	id := "light-hard"
	if dark {
		id = themes.DefaultID
	}
	p, _ := themes.BuiltinPalette(id)
	return themeFromPalette(p)
}

// Contrast floors for a palette tuned for a backlit app window: what a
// terminal draws as thin monospace text needs more than the desktop's pills
// and labels do. Colors that already read are left alone. The floors assume
// body text at 7:1 and shrink with it, so a deliberately soft palette
// (Solarized) keeps its muted steps below its body text.
const (
	contrastBody  = 7.0
	contrastDim   = 5.0
	contrastMuted = 4.5
	contrastHue   = 3.5
	contrastBlock = 4.0
	// A raised surface keeps at least this share of the canvas's contrast
	// with body text, and never needs more than contrastMuted.
	surfaceShare = 0.6
)

// contrastFloors scales the floors to the palette's own body text.
func contrastFloors(tk themes.Tokens) (dim, muted, hue float64) {
	scale := min(1, themes.Contrast(tk.Fg, tk.Bg)/contrastBody)
	return contrastDim * scale, contrastMuted * scale, contrastHue * scale
}

// themeFromPalette maps the desktop's --z-* tokens onto the terminal's
// roles: the canvas and its three raised surfaces, body text and its two
// muted steps, the accent, and the six syntax hues.
func themeFromPalette(p themes.Palette) Theme {
	tk := p.Tokens
	t := Theme{ID: p.ID, Name: p.Name, Dark: p.Dark}
	color := func(c themes.RGB) lipgloss.Color { return lipgloss.Color(c.Hex()) }
	legible := func(c themes.RGB, ratio float64) lipgloss.Color {
		return color(themes.EnsureContrast(c, tk.Bg, tk.Fg, ratio))
	}
	dimFloor, mutedFloor, hueFloor := contrastFloors(tk)
	accent := themes.EnsureContrast(tk.Accent, tk.Bg, tk.Fg, hueFloor)
	t.Accent = color(accent)
	t.Fg = color(tk.Fg)
	t.FgDim = legible(tk.Grey2, dimFloor)
	t.FgMuted = legible(tk.Grey1, mutedFloor)
	// bg-1 to bg-3 double as border colors on the desktop (Solarized's and
	// GitHub High Contrast's bg-3 is nearly the text color); here they sit
	// under text, so each gives way toward the canvas until text reads on it.
	surfaceFloor := min(contrastMuted, themes.Contrast(tk.Fg, tk.Bg)*surfaceShare)
	surface := func(c themes.RGB) lipgloss.Color {
		return color(themes.EnsureContrast(c, tk.Fg, tk.Bg, surfaceFloor))
	}
	t.Bg = color(tk.Bg)
	t.BgPanel = surface(tk.Bg1)
	t.BgSelected = surface(tk.Bg2)
	t.BgStatus = surface(tk.Bg3)
	t.Red = legible(tk.Red, hueFloor)
	t.Green = legible(tk.Green, hueFloor)
	t.Yellow = legible(tk.Yellow, hueFloor)
	t.Blue = legible(tk.Blue, hueFloor)
	t.Purple = legible(tk.Purple, hueFloor)
	t.Aqua = legible(tk.Aqua, hueFloor)
	// A block of accent carries text in whichever end of the palette reads
	// better on it, and gives way toward the other end until it does.
	block := func(c themes.RGB) (bg, fg lipgloss.Color) {
		text, away := tk.Bg, tk.Fg
		if themes.Contrast(tk.Fg, c) > themes.Contrast(tk.Bg, c) {
			text, away = tk.Fg, tk.Bg
		}
		return color(themes.EnsureContrast(c, text, away, contrastBlock)), color(text)
	}
	accentBlock, onAccent := block(accent)
	// The desktop's accent-soft is a second hue (Apple pairs blue with
	// orange); selection needs a quieter shade of the accent itself, which
	// is how the desktop derives the token for a theme that names none.
	t.AccentSoft = color(themes.Mix(accent, tk.Bg, 0.3))
	softBlock, onSoft := block(themes.Mix(accent, tk.Bg, 0.3))

	t.Base = lipgloss.NewStyle().Foreground(t.Fg)
	t.Dim = lipgloss.NewStyle().Foreground(t.FgDim)
	t.Muted = lipgloss.NewStyle().Foreground(t.FgMuted)
	t.Bold = lipgloss.NewStyle().Bold(true).Foreground(t.Fg)
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Selected = lipgloss.NewStyle().Background(t.BgSelected).Foreground(t.Fg)
	t.SelectedFocus = lipgloss.NewStyle().Background(softBlock).Foreground(onSoft).Bold(true)
	t.CursorRow = lipgloss.NewStyle().Background(t.BgStatus).Foreground(t.Fg).Bold(true)
	t.Status = lipgloss.NewStyle().Background(t.BgStatus).Foreground(t.Fg)
	t.StatusMode = lipgloss.NewStyle().Background(accentBlock).Foreground(onAccent).Bold(true).Padding(0, 1)
	t.StatusError = lipgloss.NewStyle().Foreground(t.Red).Bold(true)
	t.Border = lipgloss.NewStyle().Foreground(t.FgMuted)
	// Focus brightens a border a step instead of coloring it: the accent
	// stays reserved for the active tab, the cursor and links.
	t.BorderFocus = lipgloss.NewStyle().Foreground(t.FgDim)
	t.TabActive = lipgloss.NewStyle().Background(accentBlock).Foreground(onAccent).Bold(true)
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
	t.ChromaStyle = registerChromaStyle(t)
	return t
}
