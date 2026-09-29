package config

import (
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"testing"
)

func TestSetValuePreservesUnknownTablesAndSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	target := filepath.Join(dir, "dotfiles.toml")
	before := "[editor]\nword_wrap=true\n[cloud]\nfuture_setting=\"keep\"\n[unrecognized.nested]\nlist=[1,2,3]\n"
	if err := os.WriteFile(target, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ConfigTomlPath()); err != nil {
		t.Skip(err)
	}
	if err := SetValue("editor", "word_wrap", false); err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadPrefs()
	if err != nil || p.WordWrap {
		t.Fatalf("persisted prefs: wrap=%v err=%v", p.WordWrap, err)
	}
	if info, _ := os.Lstat(ConfigTomlPath()); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config symlink replaced")
	}
	var doc map[string]any
	if _, err := toml.DecodeFile(target, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["cloud"].(map[string]any)["future_setting"] != "keep" || doc["unrecognized"] == nil {
		t.Fatal("unknown data lost")
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Fatal("permissions changed")
	}
}

func TestSetValueRemovesEntryAndRejectsBrokenConfig(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	if err := SetValue("saved_filters", "Work", "@project:alpha"); err != nil {
		t.Fatal(err)
	}
	if err := SetValue("saved_filters", "Work", nil); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.DecodeFile(ConfigTomlPath(), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["saved_filters"].(map[string]any)["Work"]; ok {
		t.Fatal("entry wasn't deleted")
	}
	broken := []byte("[unfinished")
	if err := os.WriteFile(ConfigTomlPath(), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetValue("editor", "word_wrap", true); err == nil {
		t.Fatal("broken config overwritten")
	}
	after, _ := os.ReadFile(ConfigTomlPath())
	if string(after) != string(broken) {
		t.Fatal("failed update modified config")
	}
}
