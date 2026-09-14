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
