package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/themes"
	"github.com/ZenNotes/tui/internal/vim"
)

func themedApp(t *testing.T, prefs config.Prefs) *App {
	t.Helper()
	prefs.VimMode = true
	return newApp(context.Background(), Options{}, prefs, true)
}

func runEx(t *testing.T, a *App, name, args string) error {
	t.Helper()
	for _, c := range a.commandTable() {
		for _, n := range c.names {
			if n == name {
				return c.run(a, nil, vim.ExCommand{Name: name, Args: args})
			}
		}
	}
	t.Fatalf("no :%s command", name)
	return nil
}

func TestTheDesktopsThemeSelectionDrivesThePalette(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	a := themedApp(t, config.Prefs{ThemeFamily: "catppuccin", ThemeMode: "dark", ThemeID: "catppuccin-frappe"})
	if a.theme.ID != "catppuccin-frappe" || !a.theme.Dark || a.theme.Bg != lipgloss.Color("#303446") {
		t.Fatalf("theme %s dark=%v bg=%s", a.theme.ID, a.theme.Dark, a.theme.Bg)
	}
	// auto follows the background the terminal reported (dark here) and
	// keeps the saved variant.
	a = themedApp(t, config.Prefs{ThemeFamily: "gruvbox", ThemeMode: "auto", ThemeID: "light-soft"})
	if a.theme.ID != "dark-soft" {
		t.Fatalf("auto resolved to %s", a.theme.ID)
	}
	// Quick tweaks recolor the accent.
	a = themedApp(t, config.Prefs{ThemeFamily: "nord", ThemeMode: "dark", ThemeTweaks: map[string]string{"accent": "#ff3b30"}})
	if a.theme.ID != "nord-dark" || a.theme.Accent != lipgloss.Color("#ff3b30") {
		t.Fatalf("tweaked theme %s accent %s", a.theme.ID, a.theme.Accent)
	}
	// [terminal] theme gives zn its own look.
	a = themedApp(t, config.Prefs{ThemeFamily: "apple", ThemeMode: "light", ThemeID: "apple-light", TerminalTheme: "kanagawa-dragon"})
	if a.theme.ID != "kanagawa-dragon" {
		t.Fatalf("[terminal] theme resolved to %s", a.theme.ID)
	}
	// A theme that cannot be found says so and leaves a usable palette.
	a = themedApp(t, config.Prefs{ThemeFamily: "custom", ThemeMode: "dark", ThemeID: "custom-gone"})
	if a.theme.ID != "dark-medium" || !a.messageErr {
		t.Fatalf("missing custom theme: %s err=%v %q", a.theme.ID, a.messageErr, a.message)
	}
}

func TestThemeCommandSwitchesForTheSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	css := ":root { --z-bg: 238 230 221; --z-fg-1: 87 82 121; --z-accent: 26 125 164; }\n" +
		":root[data-theme-mode=\"dark\"] { --z-bg: 48 52 70; --z-fg-1: 198 206 239; --z-accent: 140 170 238; }\n"
	if err := os.MkdirAll(filepath.Join(dir, "themes", "soft-paper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", "soft-paper", "theme.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	a := themedApp(t, config.DefaultPrefs())
	if a.theme.ID != "dark-hard" {
		t.Fatalf("default theme %s", a.theme.ID)
	}
	steps := []struct{ cmd, args, want string }{
		{"theme", "", "light-hard"}, // the toggle keeps the variant
		{"theme", "", "dark-hard"},
		{"theme", "rose-pine", "rose-pine-main"},
		{"theme", "light", "rose-pine-dawn"},
		{"colorscheme", "github-dark-dimmed", "github-dark-dimmed"},
		{"theme", "soft-paper", "custom-soft-paper"},
	}
	for _, s := range steps {
		if err := runEx(t, a, s.cmd, s.args); err != nil || a.theme.ID != s.want {
			t.Fatalf(":%s %s gave %s %v, want %s", s.cmd, s.args, a.theme.ID, err, s.want)
		}
	}
	if !a.theme.Dark || a.theme.Bg != lipgloss.Color("#303446") {
		t.Fatalf("custom theme in dark mode: dark=%v bg=%s", a.theme.Dark, a.theme.Bg)
	}
	if err := runEx(t, a, "theme", "no-such-theme"); err == nil || a.theme.ID != "custom-soft-paper" {
		t.Fatalf("unknown theme: %v, now %s", err, a.theme.ID)
	}
	// The ex line completes modes, families, variants and custom themes.
	_, cands := a.completeEx("theme ")
	for _, want := range []string{"auto", "tokyo-night", "kanagawa-lotus", "custom-soft-paper"} {
		found := false
		for _, c := range cands {
			found = found || c == want
		}
		if !found {
			t.Fatalf(":theme completion lacks %q: %v", want, cands)
		}
	}
}

func TestThemePickerPreviewsAndRestores(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	a := themedApp(t, config.DefaultPrefs())
	if err := runEx(t, a, "themes", ""); err != nil {
		t.Fatal(err)
	}
	p, ok := a.overlay.(*palette)
	if !ok || p.currentID() != "dark-hard" {
		t.Fatalf("the picker should open on the current theme: %v", a.overlay)
	}
	a.overlay.handleKey(a, vim.KeyDown)
	if a.theme.ID != "dark-medium" {
		t.Fatalf("moving should preview: %s", a.theme.ID)
	}
	a.overlay.handleKey(a, vim.KeyEsc)
	if a.theme.ID != "dark-hard" || a.overlay != nil {
		t.Fatalf("Esc should restore: %s", a.theme.ID)
	}
	_ = runEx(t, a, "themes", "")
	typeText(a, "mocha")
	a.overlay.handleKey(a, enterKey())
	if a.theme.ID != "catppuccin-mocha" || a.prefs.ThemeFamily != "catppuccin" {
		t.Fatalf("picked %s (%s)", a.theme.ID, a.prefs.ThemeFamily)
	}
}

// Every desktop palette has to survive the move to thin monospace text on
// a terminal: muted text, the accent and the syntax hues all read against
// the canvas, and text set on an accent block reads against the block.
func TestEveryThemeReadsInATerminal(t *testing.T) {
	rgb := func(c lipgloss.Color) themes.RGB {
		v, ok := themes.ParseColor(string(c))
		if !ok {
			t.Fatalf("not a color: %q", c)
		}
		return v
	}
	for _, opt := range themes.Builtin {
		p, err := themes.BuiltinPalette(opt.ID)
		if err != nil {
			t.Fatal(err)
		}
		th := themeFromPalette(p)
		bg := rgb(th.Bg)
		dim, muted, hue := contrastFloors(p.Tokens)
		if muted < 2.5 {
			t.Errorf("%s: body text is too faint to scale the floors from (muted floor %.2f)", opt.ID, muted)
		}
		floors := map[string]struct {
			c     lipgloss.Color
			ratio float64
		}{
			"dim": {th.FgDim, dim}, "muted": {th.FgMuted, muted},
			"accent": {th.Accent, hue}, "red": {th.Red, hue}, "green": {th.Green, hue},
			"yellow": {th.Yellow, hue}, "blue": {th.Blue, hue}, "purple": {th.Purple, hue}, "aqua": {th.Aqua, hue},
		}
		for name, f := range floors {
			if got := themes.Contrast(rgb(f.c), bg); got < f.ratio-0.01 {
				t.Errorf("%s: %s %s on %s is %.2f:1, want %.1f", opt.ID, name, f.c, th.Bg, got, f.ratio)
			}
		}
		fg := rgb(th.Fg)
		for name, c := range map[string]lipgloss.Color{"panel": th.BgPanel, "selection": th.BgSelected, "status bar": th.BgStatus} {
			if got, want := themes.Contrast(fg, rgb(c)), min(contrastMuted, themes.Contrast(fg, bg)*surfaceShare); got < want-0.01 {
				t.Errorf("%s: text on the %s %s is %.2f:1, want %.2f", opt.ID, name, c, got, want)
			}
		}
		for name, s := range map[string]lipgloss.Style{"selected row": th.SelectedFocus, "active tab": th.TabActive, "mode badge": th.StatusMode} {
			fg, _ := s.GetForeground().(lipgloss.Color)
			blockBg, _ := s.GetBackground().(lipgloss.Color)
			if got := themes.Contrast(rgb(fg), rgb(blockBg)); got < contrastBlock-0.05 {
				t.Errorf("%s: %s text %s on %s is %.2f:1", opt.ID, name, fg, blockBg, got)
			}
		}
		if styles.Registry[th.ChromaStyle] == nil || th.ChromaStyle != "zennotes-"+opt.ID {
			t.Errorf("%s: code style %q is not registered", opt.ID, th.ChromaStyle)
		}
	}
}
