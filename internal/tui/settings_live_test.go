package tui

import (
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
	"os"
	"testing"
)

func TestPreferenceCommandPersistsAndReloads(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.runEx(nil, vim.ExCommand{Name: "wrap"})
	a.afterKey()
	p, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.WordWrap {
		t.Fatal("wrap command was not persisted")
	}
	if err := config.SetValue("editor", "tabs_enabled", false); err != nil {
		t.Fatal(err)
	}
	a.runEx(nil, vim.ExCommand{Name: "reloadconfig"})
	if a.prefs.TabsEnabled() {
		t.Fatal("reload did not apply tab preference")
	}
}
func TestMalformedConfigIsNotOverwrittenByPreferenceCommand(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	if err := os.WriteFile(config.ConfigTomlPath(), []byte("broken=["), 0600); err != nil {
		t.Fatal(err)
	}
	a.runEx(nil, vim.ExCommand{Name: "wrap"})
	a.afterKey()
	got, _ := os.ReadFile(config.ConfigTomlPath())
	if string(got) != "broken=[" {
		t.Fatal("malformed config overwritten")
	}
	if !a.prefs.WordWrap {
		t.Fatal("failed preference change was not rolled back")
	}
}

func TestThemePreviewDoesNotChangePortablePreferences(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	before := a.rawPref
	a.openThemePicker()
	typeText(a, "github-light")
	p, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.ThemeFamily != before.ThemeFamily || p.ThemeMode != before.ThemeMode || p.ThemeID != before.ThemeID {
		t.Fatalf("preview changed saved theme: %+v", p)
	}
	a.handleKey(vim.KeyEsc)
}

func TestSessionThemeSurvivesUnrelatedPreferenceChange(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	if err := a.selectTheme("github-light"); err != nil {
		t.Fatal(err)
	}
	a.prefs.WordWrap = !a.prefs.WordWrap
	a.afterKey()
	if a.theme.ID != "github-light" {
		t.Fatalf("saving word wrap reset session theme: %s", a.theme.ID)
	}
	p, _, err := config.LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.ThemeMode != config.DefaultPrefs().ThemeMode {
		t.Fatal("session theme changed desktop preferences")
	}
	if err := config.SetValue("terminal", "theme", "nord-dark"); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadPreferences(); err != nil {
		t.Fatal(err)
	}
	if a.theme.ID != "nord-dark" {
		t.Fatalf("explicit theme preference not applied: %s", a.theme.ID)
	}
}
