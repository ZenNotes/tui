package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// A workspaces.toml zn cannot parse must not be mistaken for an empty list
// and overwritten by the next command that saves: that would delete every
// vault and server the user connected.
func TestCorruptWorkspacesFileIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	ws := Workspaces{}
	ws.AddVault("alpha", filepath.Join(dir, "a"))
	ws.AddServer("home", "https://notes.example.com")
	if err := SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(WorkspacesPath())
	broken := append(original, []byte("\n[server]\nname = \"x\"\n")...) // a table where an array of tables is expected
	if err := os.WriteFile(WorkspacesPath(), broken, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadWorkspacesFile()
	if err == nil || len(loaded.Vaults) != 0 || len(loaded.Servers) != 0 {
		t.Fatalf("a broken file must report its problem and list nothing: %+v %v", loaded, err)
	}
	loaded.AddVault("beta", filepath.Join(dir, "b"))
	if err := SaveWorkspaces(loaded); err == nil || !strings.Contains(err.Error(), "not saving") {
		t.Fatalf("save must refuse: %v", err)
	}
	after, _ := os.ReadFile(WorkspacesPath())
	if string(after) != string(broken) {
		t.Fatal("the broken file was replaced")
	}

	// Repaired, everything is back and saving works again.
	if err := os.WriteFile(WorkspacesPath(), original, 0o644); err != nil {
		t.Fatal(err)
	}
	repaired, err := LoadWorkspacesFile()
	if err != nil || len(repaired.Vaults) != 1 || len(repaired.Servers) != 1 {
		t.Fatalf("repaired: %+v %v", repaired, err)
	}
	if err := SaveWorkspaces(repaired); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptCredentialsFileIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	if err := SaveToken("https://a.example.com", "token-a"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CredentialsPath(), []byte("[tokens]\n\"https://a.example.com\" = \"token-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if CredentialsProblem() == nil {
		t.Fatal("problem not reported")
	}
	if LoadToken("https://a.example.com") != "" {
		t.Fatal("a broken store must not yield tokens")
	}
	if err := SaveToken("https://b.example.com", "token-b"); err == nil || !strings.Contains(err.Error(), "not saving") {
		t.Fatalf("save must refuse: %v", err)
	}
	raw, _ := os.ReadFile(CredentialsPath())
	if string(raw) != "[tokens]\n\"https://a.example.com\" = \"token-a\n" {
		t.Fatal("the broken store was replaced")
	}
	if err := DeleteToken("https://a.example.com"); err == nil {
		t.Fatal("deleting from a broken store must report that the token could not be removed")
	}
}

func TestCredentialErrorsDoNotExposeFileContents(t *testing.T) {
	const secret = "testsecrettoken"
	for name, broken := range map[string]string{
		"unquoted value": "[tokens]\n\"https://notes.example.com\" = " + secret + "\n",
		"token as key":   "[tokens]\n" + secret + " = \"unterminated\n",
		"wrong type":     "tokens = \"" + secret + "\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
			if err := os.WriteFile(CredentialsPath(), []byte(broken), 0o600); err != nil {
				t.Fatal(err)
			}
			for operation, err := range map[string]error{
				"diagnose": CredentialsProblem(),
				"save":     SaveToken("https://other.example.com", "other"),
				"delete":   DeleteToken("https://notes.example.com"),
			} {
				if err == nil || !strings.Contains(err.Error(), "credentials.toml") {
					t.Errorf("%s: expected an actionable credentials error, got %v", operation, err)
				} else if strings.Contains(err.Error(), secret) {
					t.Errorf("%s exposed a credential in the diagnostic: %v", operation, err)
				}
			}
			if raw, err := os.ReadFile(CredentialsPath()); err != nil || string(raw) != broken {
				t.Fatalf("invalid credentials were overwritten: %q %v", raw, err)
			}
		})
	}
}
