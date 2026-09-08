package backend

import (
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
