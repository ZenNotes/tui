package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func TestDesktopLauncherKeepsDesktopWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "app")
	desktop, terminal := filepath.Join(dir, "Desktop"), filepath.Join(dir, "Terminal")
	for _, root := range []string{desktop, terminal} {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
	}
	app, _ := json.Marshal(map[string]any{"workspaceMode": "local", "vaultRoot": desktop, "localVaults": []map[string]string{{"name": "work", "root": desktop}}})
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), app, 0600); err != nil {
		t.Fatal(err)
	}
	ws := config.LoadWorkspaces()
	ws.AddVault("work", terminal)
	ws.Default = "work"
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"", "work"} {
		got, err := ResolveTarget(selector, "", "")
		if err != nil || got.Root != desktop {
			t.Errorf("selector %q: %+v, %v; want desktop", selector, got, err)
		}
	}
	if got := config.LoadWorkspaces().Default; got != "work" {
		t.Fatalf("terminal preference changed: %q", got)
	}
	t.Setenv("ZENNOTES_VAULT", terminal)
	if got, err := ResolveTarget("", "", ""); err != nil || got.Root != terminal {
		t.Fatalf("explicit environment: %+v %v", got, err)
	}
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "terminal")
	if got, err := ResolveDefaultTarget(""); err != nil || got.Root != terminal {
		t.Fatalf("standalone default: %+v %v", got, err)
	}
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "typo")
	if _, err := ResolveDefaultTarget(""); err == nil {
		t.Fatal("unknown workspace source must not select a vault")
	}
}

func TestDesktopWorkspaceSourceKeepsDesktopTokenPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv(RemoteTokenEnv, "")
	baseURL := "https://notes.example.com"
	data, _ := json.Marshal(map[string]any{
		"workspaceMode": "remote", "remoteWorkspace": map[string]string{"baseUrl": baseURL, "authToken": "app-token"},
		"remoteWorkspaceProfiles": []map[string]string{{"name": "home", "baseUrl": baseURL, "authToken": "app-token"}},
	})
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveToken(baseURL, "terminal-token"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, vault, server, source, want string }{
		{"app default", "", "", "app", "app-token"},
		{"app server", "", "home", "app", "app-token"},
		{"app vault", "home", "", "app", "app-token"},
		// No desktop token comes with a bare URL, so the one `zn connect`
		// saved for it is the fallback.
		{"app URL", "", baseURL, "app", "terminal-token"},
		{"terminal default", "", "", "terminal", "terminal-token"},
		{"terminal server", "", "home", "terminal", "terminal-token"},
		{"terminal URL", "", baseURL, "terminal", "terminal-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTargetWithSource(tc.vault, tc.server, "", tc.source)
			if err != nil || got.AuthToken != tc.want {
				t.Fatalf("token = %q, error = %v; want %q", got.AuthToken, err, tc.want)
			}
			t.Setenv(RemoteTokenEnv, "env-token")
			got, err = ResolveTargetWithSource(tc.vault, tc.server, "", tc.source)
			if err != nil || got.AuthToken != "env-token" {
				t.Fatalf("environment token did not win: %+v %v", got, err)
			}
			got, err = ResolveTargetWithSource(tc.vault, tc.server, "flag-token", tc.source)
			if err != nil || got.AuthToken != "flag-token" {
				t.Fatalf("flag token did not win: %+v %v", got, err)
			}
		})
	}
}

// The app keeps its server tokens in the OS secret store, so a current
// desktop config carries none. App mode then falls back to the token
// `zn connect` saved for the server the app has open, and to nothing else.
func TestDesktopWorkspaceSourceFallsBackToTheZnConnectToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv(RemoteTokenEnv, "")
	data, _ := json.Marshal(map[string]any{
		"workspaceMode": "remote", "remoteWorkspace": map[string]string{"baseUrl": "https://notes.example.com/"},
		"remoteWorkspaceProfileId": "p1",
		"remoteWorkspaceProfiles":  []map[string]string{{"id": "p1", "name": "home", "baseUrl": "https://Notes.Example.com"}},
	})
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, vault, server, want string }{
		{"default", "", "", ""},
		{"server", "", "home", ""},
		{"vault", "home", "", ""},
	} {
		if got, err := ResolveTargetWithSource(tc.vault, tc.server, "", "app"); err != nil || got.AuthToken != tc.want {
			t.Fatalf("%s before zn connect: token %q, error %v", tc.name, got.AuthToken, err)
		}
	}
	if err := config.SaveToken("https://notes.example.com", "zn-token"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveToken("https://other.example.com", "other-token"); err != nil {
		t.Fatal(err)
	}
	// A terminal default elsewhere must not redirect the app's workspace.
	ws := config.LoadWorkspaces()
	ws.AddServer("other", "https://other.example.com")
	ws.Default = "other"
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, vault, server, wantURL, want string }{
		{"default", "", "", "https://notes.example.com", "zn-token"},
		{"server", "", "home", "https://Notes.Example.com", "zn-token"},
		{"vault", "home", "", "https://Notes.Example.com", "zn-token"},
		{"other URL", "", "https://other.example.com", "https://other.example.com", "other-token"},
		{"unsaved URL", "", "https://third.example.com", "https://third.example.com", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTargetWithSource(tc.vault, tc.server, "", "app")
			if err != nil || got.Kind != KindRemote || got.BaseURL != tc.wantURL || got.AuthToken != tc.want {
				t.Fatalf("got %s token %q, error %v; want %s token %q", got.Label(), got.AuthToken, err, tc.wantURL, tc.want)
			}
			t.Setenv(RemoteTokenEnv, "env-token")
			if got, _ := ResolveTargetWithSource(tc.vault, tc.server, "", "app"); got.AuthToken != "env-token" {
				t.Fatalf("environment token did not win: %q", got.AuthToken)
			}
			if got, _ := ResolveTargetWithSource(tc.vault, tc.server, "flag-token", "app"); got.AuthToken != "flag-token" {
				t.Fatalf("flag token did not win: %q", got.AuthToken)
			}
		})
	}
}
