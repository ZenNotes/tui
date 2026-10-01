package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

// The desktop app's vaults and servers appear in `zn vault list` next to
// zn's own, but only zn's own can be forgotten here. A name copied from the
// list must be told who owns it and what zn would accept instead.
func TestVaultRemoveExplainsDesktopEntriesAndListsZnsOwn(t *testing.T) {
	dir := isolatedCLI(t)
	root := filepath.Join(dir, "notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	app, _ := json.Marshal(map[string]any{
		"workspaceMode": "remote",
		"localVaults":   []map[string]any{{"name": "vault", "root": root, "lastOpenedAt": 1700000000000}},
		"remoteWorkspaceProfiles": []map[string]any{{
			"name": "workspace (zennotes.mydomain.com)", "baseUrl": "https://zennotes.mydomain.com", "lastConnectedAt": 1700000000000,
		}},
	})
	if err := os.WriteFile(filepath.Join(config.UserDataDir(), config.AppConfigFile), app, 0o600); err != nil {
		t.Fatal(err)
	}
	ws := config.LoadWorkspaces()
	ws.AddVault("alpha", filepath.Join(dir, "alpha"))
	ws.AddServer("", "https://zennotes.mydomain.com")
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveToken("https://zennotes.mydomain.com", "secret"); err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() { _ = Main([]string{"vault", "list"}) })
	for _, want := range []string{"alpha", "terminal", "vault", "app", "Entries marked app"} {
		if !strings.Contains(out, want) {
			t.Errorf("vault list should show %q:\n%s", want, out)
		}
	}
	out = captureOutput(t, func() { _ = Main([]string{"vault", "list", "--json"}) })
	var entries []vaultListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, e := range entries {
		sources[e.Name] = e.Source
	}
	if sources["alpha"] != "terminal" || sources["zennotes.mydomain.com"] != "terminal" || sources["vault"] != "app" {
		t.Fatalf("sources: %v", sources)
	}
	if _, listed := sources["workspace (zennotes.mydomain.com)"]; listed {
		t.Fatal("the desktop profile for a URL zn saved must not be listed twice")
	}

	// Kevin's attempts, in order.
	code := 0
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "workspace"}) })
	if code != 1 || !strings.Contains(out, `Did you mean "workspace (zennotes.mydomain.com)"`) || !strings.Contains(out, "zn's saved entries are: alpha, zennotes.mydomain.com") {
		t.Fatalf("partial name: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "vault"}) })
	if code != 1 || !strings.Contains(out, "saved by the ZenNotes desktop app") {
		t.Fatalf("desktop vault: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "zennotes.mydomain.com", "--json"}) })
	if code != 0 || !strings.Contains(out, `"kind": "remote"`) {
		t.Fatalf("remove by host: exit %d %s", code, out)
	}
	if config.LoadWorkspaces().FindServer("zennotes.mydomain.com") != nil {
		t.Fatal("server still saved")
	}
	if config.LoadToken("https://zennotes.mydomain.com") != "" {
		t.Fatal("vault remove must drop the server's token like disconnect does")
	}
	// Now only the desktop profile knows that URL.
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "zennotes.mydomain.com"}) })
	if code != 1 || !strings.Contains(out, "server saved by the ZenNotes desktop app") {
		t.Fatalf("desktop server by host: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "alpha"}) })
	if code != 0 || !strings.Contains(out, "Forgot vault alpha") {
		t.Fatalf("remove vault: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"vault", "remove", "alpha"}) })
	if code != 1 || !strings.Contains(out, "zn has no saved vaults or servers of its own") {
		t.Fatalf("nothing left: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"disconnect", "workspace (zennotes.mydomain.com)"}) })
	if code != 1 || !strings.Contains(out, "saved by the ZenNotes desktop app") {
		t.Fatalf("disconnect desktop profile: exit %d %s", code, out)
	}
}

// `zn use <desktop vault name>` adopts the folder the app knows under that
// name instead of failing with "neither a saved name nor a folder".
func TestUseAdoptsADesktopVaultByName(t *testing.T) {
	dir := isolatedCLI(t)
	root := filepath.Join(dir, "desktop-notes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	app, _ := json.Marshal(map[string]any{"localVaults": []map[string]any{{"name": "Desk", "root": root}}})
	if err := os.WriteFile(filepath.Join(config.UserDataDir(), config.AppConfigFile), app, 0o600); err != nil {
		t.Fatal(err)
	}
	code := 0
	out := captureOutput(t, func() { code = Main([]string{"use", "desk", "--json"}) })
	if code != 0 || !strings.Contains(out, `"default": "Desk"`) {
		t.Fatalf("use: exit %d %s", code, out)
	}
	if v := config.LoadWorkspaces().FindVault("Desk"); v == nil || v.Root != root {
		t.Fatalf("not adopted: %+v", config.LoadWorkspaces())
	}
	out = captureOutput(t, func() { code = Main([]string{"use", "nothing-like-it"}) })
	if code != 1 || !strings.Contains(out, "zn's saved entries are: Desk") || !strings.Contains(out, "folder path or a server URL") {
		t.Fatalf("unknown: exit %d %s", code, out)
	}
}

func TestVaultRemoveCompletesZnsOwnNames(t *testing.T) {
	dir := isolatedCLI(t)
	ws := config.LoadWorkspaces()
	ws.AddVault("alpha", filepath.Join(dir, "a"))
	ws.AddServer("home", "http://127.0.0.1:7878")
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"remove", "rm"} {
		out := captureOutput(t, func() { _ = Main([]string{"__complete", "--", "vault", verb, ""}) })
		if strings.TrimSpace(out) != "alpha\nhome" {
			t.Fatalf("vault %s completion: %q", verb, out)
		}
	}
}

func TestVaultRemovePrefersSavedNamesOverPathsAndHosts(t *testing.T) {
	for _, kind := range []string{"local", "remote", "remote host"} {
		t.Run(kind, func(t *testing.T) {
			dir := isolatedCLI(t)
			t.Chdir(dir)
			ws := config.Workspaces{}
			if kind == "remote host" {
				ws.AddServer("keep", "https://home")
			} else {
				ws.AddVault("keep", filepath.Join(dir, "home"))
			}
			if kind == "local" {
				ws.AddVault("home", filepath.Join(dir, "other"))
			} else {
				ws.AddServer("home", "https://notes.example.com")
				if err := config.SaveToken("https://notes.example.com", "token"); err != nil {
					t.Fatal(err)
				}
			}
			ws.Default = "keep"
			if err := config.SaveWorkspaces(ws); err != nil {
				t.Fatal(err)
			}
			code := 0
			out := captureOutput(t, func() { code = Main([]string{"vault", "remove", "home", "--json"}) })
			if code != 0 {
				t.Fatalf("remove: exit %d %s", code, out)
			}
			remaining := config.LoadWorkspaces()
			if names := remaining.Names(); len(names) != 1 || names[0] != "keep" || remaining.Default != "keep" {
				t.Fatalf("removed the wrong entry: %+v; output: %s", remaining, out)
			}
			if kind != "local" && config.LoadToken("https://notes.example.com") != "" {
				t.Fatal("the selected server's token was not deleted")
			}
		})
	}
}

func TestForgetServerReportsStorageFailuresAndCanBeRetried(t *testing.T) {
	for _, command := range [][]string{{"vault", "remove", "home"}, {"disconnect", "home"}} {
		for _, problem := range []string{"corrupt", "write failure", "workspace write failure"} {
			t.Run(strings.Join(command, " ")+"/"+problem, func(t *testing.T) {
				isolatedCLI(t)
				ws := config.Workspaces{Default: "home"}
				ws.AddServer("home", "https://notes.example.com")
				if err := config.SaveWorkspaces(ws); err != nil {
					t.Fatal(err)
				}
				if err := config.SaveToken("https://notes.example.com", "token"); err != nil {
					t.Fatal(err)
				}
				original, err := os.ReadFile(config.CredentialsPath())
				if err != nil {
					t.Fatal(err)
				}
				broken := []byte("[tokens]\n\"https://notes.example.com\" = \"token\n")
				failedPath := config.CredentialsPath()
				if problem == "workspace write failure" {
					failedPath = config.WorkspacesPath()
				}
				if problem == "corrupt" {
					err = os.WriteFile(config.CredentialsPath(), broken, 0o600)
				} else {
					err = os.Mkdir(failedPath+".tmp", 0o700)
				}
				if err != nil {
					t.Fatal(err)
				}
				code := 0
				out := captureOutput(t, func() { code = Main(append(command, "--json")) })
				if code == 0 || !strings.Contains(out, filepath.Base(failedPath)) {
					t.Errorf("must report failed removal: exit %d %s", code, out)
				}
				if saved := config.LoadWorkspaces(); saved.FindServer("home") == nil || saved.Default != "home" {
					t.Fatalf("must keep the entry so removal can be retried: %+v", saved)
				}
				want := original
				if problem == "corrupt" {
					want = broken
				}
				if raw, err := os.ReadFile(config.CredentialsPath()); err != nil || string(raw) != string(want) {
					t.Fatalf("credentials changed on failure: %q %v", raw, err)
				}
				if problem == "corrupt" {
					err = os.WriteFile(config.CredentialsPath(), original, 0o600)
				} else {
					err = os.Remove(failedPath + ".tmp")
				}
				if err != nil {
					t.Fatal(err)
				}
				out = captureOutput(t, func() { code = Main(command) })
				if code != 0 || config.LoadWorkspaces().FindServer("home") != nil || config.LoadToken("https://notes.example.com") != "" {
					t.Fatalf("retry did not remove the server and token: exit %d %s", code, out)
				}
			})
		}
	}
}

// With an unparseable workspaces.toml, every command that would save must
// refuse rather than write an empty list over the user's saved entries.
func TestCommandsRefuseToOverwriteABrokenWorkspacesFile(t *testing.T) {
	dir := isolatedCLI(t)
	ws := config.LoadWorkspaces()
	ws.AddVault("alpha", filepath.Join(dir, "alpha"))
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	broken := []byte("default = \"alpha\"\n[[vault]]\nname = \"alpha\"\nroot = \"/a\"\n[vault]\nname = \"oops\"\n")
	if err := os.WriteFile(config.WorkspacesPath(), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := 0
	out := captureOutput(t, func() { code = Main([]string{"vault", "add", filepath.Join(dir, "beta"), "--name", "beta"}) })
	if code == 0 || !strings.Contains(out, "not saving") || !strings.Contains(out, "workspaces.toml") {
		t.Fatalf("vault add: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"init", filepath.Join(dir, "gamma")}) })
	if code == 0 || !strings.Contains(out, "not saving") {
		t.Fatalf("init: exit %d %s", code, out)
	}
	if raw, _ := os.ReadFile(config.WorkspacesPath()); string(raw) != string(broken) {
		t.Fatal("workspaces.toml was overwritten")
	}
	out = captureOutput(t, func() { code = Main([]string{"doctor", "--json"}) })
	var report struct {
		Checks []diagnosticCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(out, "\n{\"error\"", 2)[0]), &report); err != nil {
		t.Fatalf("doctor json: %v %s", err, out)
	}
	found := false
	for _, c := range report.Checks {
		if c.Name == "saved vaults" {
			found = true
			if c.OK || !strings.Contains(c.Message, "workspaces.toml") {
				t.Fatalf("saved vaults check: %+v", c)
			}
		}
	}
	if !found || code == 0 {
		t.Fatalf("doctor must fail on a broken workspaces file: exit %d %s", code, out)
	}
	out = captureOutput(t, func() { code = Main([]string{"status"}) })
	if !strings.Contains(out, "Warning: saved vaults and servers are unavailable") {
		t.Fatalf("status warning: %s", out)
	}
	out = captureOutput(t, func() { _ = Main([]string{"vault", "list"}) })
	if !strings.Contains(out, "zn's own vaults and servers are not shown") {
		t.Fatalf("vault list warning: %s", out)
	}
}
