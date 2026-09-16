package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestDesktopLauncherDoesNotChangeTUIVaultSwitches(t *testing.T) {
	for _, tc := range []struct{ command, source string }{
		{"vault", ""}, {"local", ""}, {"vault", "terminal"}, {"local", "terminal"}, {"vault", "app"}, {"local", "app"},
	} {
		t.Run(tc.command+"/"+tc.source, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("ZENNOTES_CONFIG_DIR", dir)
			t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "app")
			t.Setenv("ZENNOTES_VAULT", "")
			t.Setenv("ZENNOTES_SERVER", "")
			desktop, terminal, initial := filepath.Join(dir, "desktop"), filepath.Join(dir, "terminal"), filepath.Join(dir, "initial")
			for _, root := range []string{desktop, terminal, initial} {
				if err := os.MkdirAll(root, 0755); err != nil {
					t.Fatal(err)
				}
			}
			data, _ := json.Marshal(map[string]any{"vaultRoot": desktop, "localVaults": []map[string]string{{"name": "work", "root": desktop}}})
			if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), data, 0600); err != nil {
				t.Fatal(err)
			}
			ws := config.LoadWorkspaces()
			ws.AddVault("work", terminal)
			if err := config.SaveWorkspaces(ws); err != nil {
				t.Fatal(err)
			}
			target := backend.Target{Kind: backend.KindLocal, Root: initial}
			b, err := backend.New(target, backend.Options{})
			if err != nil {
				t.Fatal(err)
			}
			a := newApp(context.Background(), Options{Backend: b, Target: target, WorkspaceSource: tc.source}, config.Prefs{}, true)
			t.Cleanup(func() {
				if a.watcher != nil {
					a.watcher.close()
				}
			})
			for _, cmd := range a.parityCommands() {
				if cmd.names[0] == tc.command {
					if err := cmd.run(a, nil, vim.ExCommand{Args: "work"}); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			want := terminal
			if tc.source == "app" {
				want = desktop
			}
			if a.opts.Target.Root != want {
				t.Fatalf(":%s work selected %q, want %q (message: %s)", tc.command, a.opts.Target.Root, want, a.message)
			}
		})
	}
}

func TestTUIServerSwitchHonorsExplicitAppWorkspaceSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "terminal")
	t.Setenv("ZENNOTES_REMOTE_TOKEN", "fixture-token")
	respond := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"root":"/notes"}`))
	})
	desktop := httptest.NewServer(respond)
	defer desktop.Close()
	terminal := httptest.NewServer(respond)
	defer terminal.Close()
	data, _ := json.Marshal(map[string]any{"remoteWorkspaceProfiles": []map[string]string{{"name": "home", "baseUrl": desktop.URL}}})
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	ws := config.LoadWorkspaces()
	ws.AddServer("home", terminal.URL)
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	target := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"server command", "server picker", "vault picker"} {
		t.Run(action, func(t *testing.T) {
			a := newApp(context.Background(), Options{Backend: b, Target: target, WorkspaceSource: "app"}, config.Prefs{}, true)
			if action == "server command" {
				for _, cmd := range a.parityCommands() {
					if cmd.names[0] == "server" {
						if err := cmd.run(a, nil, vim.ExCommand{Args: "home"}); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
			} else {
				if action == "server picker" {
					a.openServerPicker()
				} else {
					a.openVaultSwitcher()
				}
				picker, ok := a.overlay.(*palette)
				if !ok {
					t.Fatal("expected a workspace picker")
				}
				for _, item := range picker.items {
					if item.label == "home" {
						picker.onSelect(a, item)
						break
					}
				}
			}
			if a.opts.Target.BaseURL != desktop.URL {
				t.Fatalf("selected %q, want desktop server %q (message: %s)", a.opts.Target.BaseURL, desktop.URL, a.message)
			}
		})
	}
}

func TestNewServerURLStillPromptsForToken(t *testing.T) {
	for _, source := range []string{"app", "terminal"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
			t.Setenv("ZENNOTES_REMOTE_TOKEN", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()
			target := backend.Target{Kind: backend.KindLocal, Root: t.TempDir()}
			b, err := backend.New(target, backend.Options{})
			if err != nil {
				t.Fatal(err)
			}
			a := newApp(context.Background(), Options{Backend: b, Target: target, WorkspaceSource: source}, config.Prefs{}, true)
			for _, cmd := range a.parityCommands() {
				if cmd.names[0] == "server" {
					if err := cmd.run(a, nil, vim.ExCommand{Args: server.URL}); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			entry, ok := a.overlay.(*prompt)
			if !ok || !entry.masked {
				t.Fatalf("new server needs a token prompt; overlay=%T, message=%s", a.overlay, a.message)
			}
			if a.opts.Target.Root != target.Root {
				t.Fatal("workspace changed before authentication")
			}
		})
	}
}
