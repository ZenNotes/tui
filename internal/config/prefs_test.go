package config

import (
	"os"
	"path/filepath"
	"strings"
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

// writeTerminalConfig points ZENNOTES_CONFIG_DIR at a fresh dir whose
// config.toml holds body under [terminal].
func writeTerminalConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[terminal]\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The starter file carries an active `math = "typst"` under [terminal]
// and a comment naming the tools latex needs.
func TestStarterConfigNamesMathAndTools(t *testing.T) {
	section := DefaultConfigTOML
	start := strings.Index(section, "[terminal]")
	if start < 0 {
		t.Fatal("starter config has no [terminal] table")
	}
	section = section[start+len("[terminal]"):]
	if end := strings.Index(section, "\n["); end >= 0 {
		section = section[:end]
	}
	active, comments := false, ""
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			comments += line + "\n"
			continue
		}
		if strings.ReplaceAll(line, " ", "") == `math="typst"` {
			active = true
		}
	}
	if !active {
		t.Fatalf("[terminal] has no active math = \"typst\" line:\n%s", section)
	}
	for _, want := range []string{"latex", "pdftex", "pdftocairo"} {
		if !strings.Contains(comments, want) {
			t.Errorf("[terminal] comments do not name %s:\n%s", want, comments)
		}
	}
}

// No math key means typst.
func TestPrefsMathDefaultsToTypst(t *testing.T) {
	if got := DefaultPrefs().TerminalMath; got != "typst" {
		t.Fatalf("default: %q", got)
	}
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.TerminalMath != "typst" {
		t.Fatalf("no file: math %q, err %v", prefs.TerminalMath, err)
	}
	writeTerminalConfig(t, `images = "auto"`)
	prefs, _, err = LoadPrefs()
	if err != nil || prefs.TerminalMath != "typst" {
		t.Fatalf("no key: math %q, err %v", prefs.TerminalMath, err)
	}
	writeTerminalConfig(t, `math = "latex"`)
	prefs, _, err = LoadPrefs()
	if err != nil || prefs.TerminalMath != "latex" {
		t.Fatalf("math = \"latex\" must load as latex: math %q, err %v", prefs.TerminalMath, err)
	}
}

// An unknown engine is not an error when zn loads; it behaves as typst.
func TestPrefsUnknownMathLoadsAsTypst(t *testing.T) {
	writeTerminalConfig(t, `math = "foo"`)
	prefs, _, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs must not fail on math = \"foo\": %v", err)
	}
	if prefs.TerminalMath != "typst" {
		t.Fatalf("math = \"foo\" loaded as %q, want typst", prefs.TerminalMath)
	}
}

// The configuration check of zn doctor names the key and the allowed values.
func TestValidateConfigRejectsUnknownMath(t *testing.T) {
	writeTerminalConfig(t, `math = "foo"`)
	err := ValidateConfig()
	if err == nil {
		t.Fatal("ValidateConfig accepted math = \"foo\"")
	}
	for _, want := range []string{"terminal.math", "typst, latex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	for _, ok := range []string{`math = "typst"`, `math = "latex"`} {
		writeTerminalConfig(t, ok)
		if err := ValidateConfig(); err != nil {
			t.Errorf("%s must validate: %v", ok, err)
		}
	}
}

// No key, zero and negative all give a 10 second timeout.
func TestPrefsRenderTimeoutDefaultsAndNonPositive(t *testing.T) {
	for _, body := range []string{``, `render_timeout = 0`, `render_timeout = -3`} {
		writeTerminalConfig(t, body)
		prefs, _, err := LoadPrefs()
		if err != nil || prefs.RenderTimeout != 10 {
			t.Errorf("%q: timeout %v, err %v", body, prefs.RenderTimeout, err)
		}
	}
	writeTerminalConfig(t, `render_timeout = 2`)
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.RenderTimeout != 2 {
		t.Fatalf("render_timeout = 2: timeout %v, err %v", prefs.RenderTimeout, err)
	}
}

// A non-integer timeout is a wrong-type error in doctor; zn uses 10.
func TestPrefsRenderTimeoutWrongType(t *testing.T) {
	writeTerminalConfig(t, `render_timeout = "ten"`)
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.RenderTimeout != 10 {
		t.Fatalf("load: timeout %v, err %v", prefs.RenderTimeout, err)
	}
	err = ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), "terminal.render_timeout") || !strings.Contains(err.Error(), "wrong type") {
		t.Fatalf("doctor: %v", err)
	}
}

// No key, zero and negative all give a cap of 2.
func TestPrefsRenderConcurrencyDefaultsAndNonPositive(t *testing.T) {
	for _, body := range []string{``, `render_concurrency = 0`, `render_concurrency = -1`} {
		writeTerminalConfig(t, body)
		prefs, _, err := LoadPrefs()
		if err != nil || prefs.RenderConcurrency != 2 {
			t.Errorf("%q: cap %v, err %v", body, prefs.RenderConcurrency, err)
		}
	}
	writeTerminalConfig(t, `render_concurrency = 4`)
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.RenderConcurrency != 4 {
		t.Fatalf("render_concurrency = 4: cap %v, err %v", prefs.RenderConcurrency, err)
	}
}

// A non-integer cap is a wrong-type error in doctor; zn uses 2.
func TestPrefsRenderConcurrencyWrongType(t *testing.T) {
	writeTerminalConfig(t, `render_concurrency = "two"`)
	prefs, _, err := LoadPrefs()
	if err != nil || prefs.RenderConcurrency != 2 {
		t.Fatalf("load: cap %v, err %v", prefs.RenderConcurrency, err)
	}
	err = ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), "terminal.render_concurrency") || !strings.Contains(err.Error(), "wrong type") {
		t.Fatalf("doctor: %v", err)
	}
}
