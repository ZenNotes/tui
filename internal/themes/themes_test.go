package themes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseColorForms(t *testing.T) {
	cases := map[string]RGB{
		"29 32 33":             {29, 32, 33},
		"29, 32, 33":           {29, 32, 33},
		"#1d2021":              {29, 32, 33},
		"#FFF":                 {255, 255, 255},
		"rgb(231, 138, 78)":    {231, 138, 78},
		"rgb(231 138 78 / .5)": {231, 138, 78},
	}
	for in, want := range cases {
		if got, ok := ParseColor(in); !ok || got != want {
			t.Fatalf("%q: got %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "red", "1 2", "300 0 0", "var(--x)"} {
		if _, ok := ParseColor(in); ok {
			t.Fatalf("%q should not parse", in)
		}
	}
}

func TestEveryBuiltinThemeHasItsDesktopColors(t *testing.T) {
	for _, opt := range Builtin {
		p, err := BuiltinPalette(opt.ID)
		if err != nil {
			t.Fatalf("%s: %v", opt.ID, err)
		}
		if p.Dark != opt.Dark || p.Family != opt.Family {
			t.Fatalf("%s: palette says dark=%v family=%s", opt.ID, p.Dark, p.Family)
		}
		// A dark theme's canvas is darker than its text, and the reverse.
		if (Luminance(p.Tokens.Bg) < Luminance(p.Tokens.Fg)) != opt.Dark {
			t.Fatalf("%s: bg %s and fg %s contradict dark=%v", opt.ID, p.Tokens.Bg.Hex(), p.Tokens.Fg.Hex(), opt.Dark)
		}
	}
	if len(builtinTokens()) != len(Builtin) {
		t.Fatalf("builtin.css has %d themes, the registry %d", len(builtinTokens()), len(Builtin))
	}
	// Gruvbox cascades on the desktop: the shared dark block plus the
	// variant's own canvas must both have landed in the flattened block.
	p, _ := BuiltinPalette("dark-hard")
	if p.Tokens.Bg.Hex() != "#1d2021" || p.Tokens.Accent.Hex() != "#e78a4e" || p.Tokens.Grey2.Hex() != "#a89984" {
		t.Fatalf("dark-hard tokens: %+v", p.Tokens)
	}
	p, _ = BuiltinPalette("light-soft")
	if p.Tokens.Bg.Hex() != "#f2e5bc" || p.Tokens.Accent.Hex() != "#c35e0a" {
		t.Fatalf("light-soft tokens: %+v", p.Tokens)
	}
}

func TestResolveFollowsFamilyModeAndVariant(t *testing.T) {
	cases := []struct {
		sel        Selection
		systemDark bool
		want       string
	}{
		{Selection{"gruvbox", "dark", "dark-hard"}, false, "dark-hard"},
		// auto follows the background and keeps the variant.
		{Selection{"gruvbox", "auto", "dark-hard"}, false, "light-hard"},
		{Selection{"gruvbox", "system", "light-soft"}, true, "dark-soft"},
		// A family with no saved variant lands on its canonical default.
		{Selection{"catppuccin", "dark", ""}, false, "catppuccin-mocha"},
		{Selection{"kanagawa", "light", "kanagawa-dragon"}, true, "kanagawa-lotus"},
		// A stale id loses to the family the user picked.
		{Selection{"nord", "dark", "dark-hard"}, false, "nord-dark"},
		// No family: the id's own.
		{Selection{"", "dark", "rose-pine-moon"}, false, "rose-pine-moon"},
		{Selection{"", "", ""}, true, "dark-medium"},
	}
	for _, c := range cases {
		p, err := Resolve(c.sel, c.systemDark, t.TempDir())
		if err != nil || p.ID != c.want {
			t.Fatalf("%+v: got %s %v, want %s", c.sel, p.ID, err, c.want)
		}
	}
}

func writeCustom(t *testing.T, dir, slug, manifest, css string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, slug), 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, slug, "manifest.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, slug, "theme.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
}

const paperCSS = `/* Soft Paper; a { brace } in a comment must not matter */
@font-face { font-family: "My Font"; src: url(zen-theme://soft-paper/f.woff2) format("woff2"); }
:root {
  color-scheme: light;
  --z-bg: 238 230 221;
  --z-fg-1: #575279;
  --z-accent: rgb(26, 125, 164);
  --z-font-text: "My Font", sans-serif;
}
.cm-editor { --z-bg: 0 0 0; }
:root[data-theme-mode="dark"] {
  --z-bg: 48 52 70;
  --z-fg-1: 198 206 239;
  --z-accent: 140 170 238;
  --z-red: 231 130 132;
}
`

func TestCustomThemeReadsTheDesktopFolder(t *testing.T) {
	dir := t.TempDir()
	writeCustom(t, dir, "soft-paper", `{"name": "Soft Paper", "modes": "both"}`, paperCSS)
	writeCustom(t, dir, "night-only", `{"modes": ["dark"]}`, `:root { --z-bg: 10 10 10; --z-fg-1: 240 240 240; --z-accent: 255 0 128; }`)
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	light, err := Resolve(Selection{"custom", "light", "custom-soft-paper"}, true, dir)
	if err != nil || light.Dark || light.Name != "Soft Paper" || light.Tokens.Bg.Hex() != "#eee6dd" || light.Tokens.Accent.Hex() != "#1a7da4" {
		t.Fatalf("light: %+v %v", light, err)
	}
	dark, err := Resolve(Selection{"custom", "auto", "custom-soft-paper"}, true, dir)
	if err != nil || !dark.Dark || dark.Tokens.Bg.Hex() != "#303446" || dark.Tokens.Red.Hex() != "#e78284" {
		t.Fatalf("dark: %+v %v", dark, err)
	}
	// Tokens the theme leaves out derive from its background, text and accent.
	if dark.Tokens.Bg1 != Mix(dark.Tokens.Bg, dark.Tokens.Fg1, 0.05) || dark.Tokens.Green != semanticDefaults["green"][1] {
		t.Fatalf("derived tokens: %+v", dark.Tokens)
	}
	// A single-mode theme pins its mode whatever the preference says.
	pinned, err := Resolve(Selection{"custom", "light", "custom-night-only"}, false, dir)
	if err != nil || !pinned.Dark || pinned.Name != "night-only" {
		t.Fatalf("pinned: %+v %v", pinned, err)
	}
	// A theme that is gone falls back to the family default and says why.
	gone, err := Resolve(Selection{"custom", "dark", "custom-missing"}, false, dir)
	if err == nil || gone.ID != "dark-medium" {
		t.Fatalf("missing theme: %s %v", gone.ID, err)
	}
	if got := ListCustom(dir); len(got) != 2 || got[0].Slug != "night-only" || got[1].Slug != "soft-paper" {
		t.Fatalf("list: %+v", got)
	}
	if _, err := LoadCustom(dir, "../soft-paper"); err == nil {
		t.Fatal("a slug must not escape the themes folder")
	}
}

func TestSelectReadsModesFamiliesVariantsAndCustomThemes(t *testing.T) {
	dir := t.TempDir()
	writeCustom(t, dir, "soft-paper", "", paperCSS)
	cur := Selection{"gruvbox", "auto", "dark-hard"}
	check := func(name string, want Selection) {
		t.Helper()
		got, err := Select(name, cur, dir)
		if err != nil || got != want {
			t.Fatalf("%q: got %+v %v, want %+v", name, got, err, want)
		}
	}
	check("light", Selection{"gruvbox", "light", "dark-hard"})
	check("system", Selection{"gruvbox", "auto", "dark-hard"})
	check("Nord", Selection{"nord", "auto", "dark-hard"})
	check("catppuccin-mocha", Selection{"catppuccin", "dark", "catppuccin-mocha"})
	check("custom-soft-paper", Selection{"custom", "auto", "custom-soft-paper"})
	check("soft-paper", Selection{"custom", "auto", "custom-soft-paper"})
	if _, err := Select("no-such-theme", cur, dir); err == nil {
		t.Fatal("an unknown name should error")
	}
	// Leaving a custom theme for a family must drop the custom id.
	cur = Selection{"custom", "dark", "custom-soft-paper"}
	check("nord", Selection{"nord", "dark", ""})
}

func TestTweaksOverrideAccentAndHues(t *testing.T) {
	p, _ := BuiltinPalette("dark-hard")
	p.ApplyTweaks(map[string]string{"accent": "#ff3b30", "green": "not a color", "bg": "#ffffff"})
	if p.Tokens.Accent.Hex() != "#ff3b30" || p.Tokens.Green.Hex() != "#a9b665" || p.Tokens.Bg.Hex() != "#1d2021" {
		t.Fatalf("tweaks: %+v", p.Tokens)
	}
}

func TestEnsureContrastOnlyMovesFaintColors(t *testing.T) {
	bg, fg := RGB{255, 255, 255}, RGB{29, 29, 31}
	if got := EnsureContrast(RGB{0, 90, 200}, bg, fg, 3.5); got != (RGB{0, 90, 200}) {
		t.Fatalf("a readable color moved: %v", got)
	}
	faint := RGB{90, 200, 250}
	got := EnsureContrast(faint, bg, fg, 3.5)
	if Contrast(got, bg) < 3.5 || got == fg {
		t.Fatalf("faint color became %v (contrast %.2f)", got, Contrast(got, bg))
	}
}

func TestFlattenBuiltinRejectsAHalfDefinedTheme(t *testing.T) {
	if _, err := FlattenBuiltin(`:root { --z-bg: 1 2 3; }`, "test"); err == nil {
		t.Fatal("a stylesheet without the registry's themes must fail")
	}
}
