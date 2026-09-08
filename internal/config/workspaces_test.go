package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkspacesRoundTripAndTokens(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	ws := LoadWorkspaces()
	if len(ws.Vaults)+len(ws.Servers) != 0 || ws.Default != "" {
		t.Fatal("a missing file is an empty list")
	}
	v := ws.AddVault("", filepath.Join(dir, "Notes"))
	s := ws.AddServer("", "https://notes.example.com/")
	ws.Default = s.Name
	if v.Name != "Notes" || s.Name != "notes.example.com" || s.URL != "https://notes.example.com" {
		t.Fatalf("names: %+v %+v", v, s)
	}
	if again := ws.AddServer("home", "https://notes.example.com"); again.Name != "home" || len(ws.Servers) != 1 {
		t.Fatalf("same URL renames instead of duplicating: %+v", ws.Servers)
	}
	if err := SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	back := LoadWorkspaces()
	if back.Default != "home" || back.FindServer("HOME") == nil || back.FindServer("notes.example.com") == nil || back.FindVault("notes") == nil || back.FindVault(filepath.Join(dir, "Notes")) == nil {
		t.Fatalf("round trip: %+v", back)
	}
	if err := SaveToken("https://notes.example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	if LoadToken("https://notes.example.com/") != "secret" {
		t.Fatal("token by URL, slash-insensitive")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(CredentialsPath())
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("credentials must be private: %v %v", info.Mode(), err)
		}
	}
	if kind, ok := back.Remove("home"); !ok || kind != "remote" || back.Default != "" {
		t.Fatalf("remove clears the default: %s %v %q", kind, ok, back.Default)
	}
	if err := DeleteToken("https://notes.example.com"); err != nil || LoadToken("https://notes.example.com") != "" {
		t.Fatal("token deleted")
	}
}
