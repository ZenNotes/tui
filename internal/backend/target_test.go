package backend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func TestDefaultTargetPrefersZnWorkspaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv(RemoteTokenEnv, "")
	if _, err := ResolveDefaultTarget(""); err == nil {
		t.Fatal("nothing configured must fail")
	}
	ws := config.LoadWorkspaces()
	ws.AddVault("work", filepath.Join(dir, "Work"))
	ws.AddServer("home", "https://notes.example.com")
	ws.Default = "home"
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveToken("https://notes.example.com", "tok"); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveDefaultTarget("")
	if err != nil || target.Kind != KindRemote || target.BaseURL != "https://notes.example.com" || target.AuthToken != "tok" || target.Name != "home" {
		t.Fatalf("default: %+v %v", target, err)
	}
	local, err := ResolveVaultTarget("work", "")
	if err != nil || local.Kind != KindLocal || local.Root != filepath.Join(dir, "Work") {
		t.Fatalf("saved vault by name: %+v %v", local, err)
	}
	server, err := ResolveServerTarget("home", "flag")
	if err != nil || server.AuthToken != "flag" {
		t.Fatalf("--token wins over the stored token: %+v %v", server, err)
	}
	if err := RememberTarget(Target{Kind: KindLocal, Root: filepath.Join(dir, "Other")}); err != nil {
		t.Fatal(err)
	}
	if ws := config.LoadWorkspaces(); ws.Default != "Other" || ws.FindVault("Other") == nil {
		t.Fatalf("remember: %+v", ws)
	}
}

func TestSameVaultComparesTheVaultNotTheCredentials(t *testing.T) {
	dir := t.TempDir()
	work, home := filepath.Join(dir, "Work"), filepath.Join(dir, "Home")
	for _, root := range []string{work, home} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Windows only allows symlinks with developer mode or elevation.
	link := filepath.Join(dir, "WorkLink")
	if err := os.Symlink(work, link); err != nil {
		link = work
	}
	local := func(root string) Target { return Target{Kind: KindLocal, Root: root} }
	server := func(url, name, token string) Target {
		return Target{Kind: KindRemote, BaseURL: url, Name: name, AuthToken: token}
	}
	for _, tc := range []struct {
		name string
		a, b Target
		want bool
	}{
		{"same directory", local(work), local(work), true},
		{"unclean spelling", local(work), local(filepath.Join(home, "..", "Work") + "/"), true},
		{"symlink", local(work), local(link), true},
		{"other directory", local(work), local(home), false},
		{"missing directories", local(filepath.Join(dir, "a")), local(filepath.Join(dir, "b")), false},
		{"token and name differ", server("https://notes.example.com", "home", "old"), server("https://notes.example.com", "", "new"), true},
		{"URL spelling", server("https://Notes.Example.com/", "", ""), server("https://notes.example.com", "", ""), true},
		{"bare host:port", server("notes.example.com:7878", "", ""), server("http://notes.example.com:7878", "", ""), true},
		{"other port", server("http://notes.example.com:7878", "", ""), server("http://notes.example.com:7879", "", ""), false},
		{"local and remote", local(work), server("https://notes.example.com", "", ""), false},
	} {
		if got := SameVault(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: SameVault = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := server("https://notes.example.com", "home", "secret").Label(); got != "home (https://notes.example.com)" {
		t.Errorf("named server label %q", got)
	}
	if got := server("https://notes.example.com", "", "secret").Label(); got != "https://notes.example.com" {
		t.Errorf("bare server label %q", got)
	}
	if got := local(work).Label(); got != work {
		t.Errorf("local label %q", got)
	}
}
