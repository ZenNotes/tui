package config

import (
	"os"
	"testing"
)

func TestHelixPrefs(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	toml := "[vim]\nmotions = \"helix\"\n\n[keys.normal]\nC-s = \":w\"\n\n[keys.normal.g]\na = \"goto_last_line\"\n"
	if err := os.WriteFile(ConfigTomlPath(), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.Motions != "helix" {
		t.Fatalf("motions = %q", p.Motions)
	}
	normal, _ := p.HelixKeys["normal"].(map[string]any)
	if normal["C-s"] != ":w" {
		t.Fatalf("keys.normal = %v", normal)
	}
	// changing a setting from the app keeps the [keys] tables
	if err := SetValue("vim", "motions", "vim"); err != nil {
		t.Fatal(err)
	}
	p, _, err = LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	g, _ := p.HelixKeys["normal"].(map[string]any)["g"].(map[string]any)
	if p.Motions != "vim" || g["a"] != "goto_last_line" {
		t.Fatalf("after SetValue: motions=%q keys=%v", p.Motions, p.HelixKeys)
	}
}

func TestHelixPrefsLegacySection(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(ConfigTomlPath(), []byte("[helix]\nmotions = \"helix\"\ninsert_escape = \"kj\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.Motions != "helix" || p.VimInsertEscape != "kj" {
		t.Fatalf("legacy [helix]: motions=%q insert_escape=%q", p.Motions, p.VimInsertEscape)
	}
}

func TestMotionsValidated(t *testing.T) {
	err := validateKnownValues(map[string]any{"vim": map[string]any{"motions": "emacs"}}, false)
	if err == nil {
		t.Fatal("motions = emacs was accepted")
	}
}
