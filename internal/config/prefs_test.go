package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrefsReadTheDesktopsThemeSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	// A missing file keeps the desktop's defaults.
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.ThemeFamily != "gruvbox" || prefs.ThemeMode != "dark" || prefs.ThemeID != "dark-hard" || prefs.TerminalTheme != "" {
		t.Fatalf("defaults: %+v %v", prefs, err)
	}
	toml := `
[appearance]
theme_family = "catppuccin"  # apple | gruvbox | catppuccin | …
theme_mode = "auto"
theme_id = "catppuccin-mocha"

[tweaks]
"accent" = "#ff3b30"

[terminal]
theme = "nord"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	prefs, _, err = LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.ThemeFamily != "catppuccin" || prefs.ThemeMode != "auto" || prefs.ThemeID != "catppuccin-mocha" {
		t.Fatalf("appearance: %+v", prefs)
	}
	if prefs.ThemeTweaks["accent"] != "#ff3b30" || prefs.TerminalTheme != "nord" {
		t.Fatalf("tweaks %v, terminal theme %q", prefs.ThemeTweaks, prefs.TerminalTheme)
	}
	if got := CustomThemesDir(); got != filepath.Join(dir, "themes") {
		t.Fatalf("custom themes dir %q", got)
	}
}

func TestStarterConfigParses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(DefaultConfigTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	prefs, _, err := LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.ThemeFamily != "gruvbox" || prefs.ThemeMode != "auto" || prefs.ThemeID != "dark-hard" || prefs.TerminalTheme != "" {
		t.Fatalf("starter theme: %+v", prefs)
	}
}
