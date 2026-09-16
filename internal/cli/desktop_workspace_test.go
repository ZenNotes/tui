package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func TestDesktopVaultListReturnsNamesDesktopCommandsCanResolve(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "app")
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	root := filepath.Join(dir, "notes")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{
		"vaultRoot": root, "localVaults": []map[string]string{{"name": "desktop-name", "root": root}},
		"remoteWorkspaceProfiles": []map[string]string{{"name": "desktop-server", "baseUrl": "https://notes.example.com"}},
	})
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	ws := config.LoadWorkspaces()
	ws.AddVault("terminal-name", root)
	ws.AddServer("terminal-server", "https://notes.example.com")
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"app", "terminal"} {
		t.Run(source, func(t *testing.T) {
			out := captureOutput(t, func() {
				if code := Main([]string{"vault", "list", "--workspace-source", source, "--json"}); code != 0 {
					t.Errorf("vault list exited %d", code)
				}
			})
			var entries []vaultListEntry
			if err := json.Unmarshal([]byte(out), &entries); err != nil {
				t.Fatal(err)
			}
			prefix := "desktop"
			if source == "terminal" {
				prefix = "terminal"
			}
			if len(entries) != 2 || entries[0].Name != prefix+"-name" || entries[1].Name != prefix+"-server" {
				t.Fatalf("unexpected list: %s", out)
			}
			for _, entry := range entries {
				if _, err := ResolveTargetFromArgs(Parse([]string{"--vault", entry.Name, "--workspace-source", source})); err != nil {
					t.Errorf("listed name %q does not resolve: %v", entry.Name, err)
				}
			}
		})
	}
}
