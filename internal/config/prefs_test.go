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

func TestPrefsReadTheTasksListOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.TasksListOrder != "note" {
		t.Fatalf("default order = %q, %v", prefs.TasksListOrder, err)
	}
	for _, tc := range []struct{ value, want string }{{"priority", "priority"}, {"note", "note"}, {"due", "note"}} {
		toml := "[view]\ntasks_list_order = \"" + tc.value + "\"  # note | priority\n"
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		prefs, _, err = LoadPrefs()
		if err != nil || prefs.TasksListOrder != tc.want {
			t.Fatalf("tasks_list_order = %q read as %q, want %q (%v)", tc.value, prefs.TasksListOrder, tc.want, err)
		}
	}
}
