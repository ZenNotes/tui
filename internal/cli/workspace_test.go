package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func captureOutput(t *testing.T, run func()) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevErr := stdout, stderr
	stdout, stderr = &buf, &buf
	defer func() { stdout, stderr = prevOut, prevErr }()
	run()
	return buf.String()
}

func TestConnectInitUseAndList(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv("ZENNOTES_REMOTE_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer right" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/vault":
			_, _ = w.Write([]byte(`{"root":"/srv/notes","name":"notes"}`))
		case "/api/notes":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	out := captureOutput(t, func() {
		if code := Main([]string{"connect", server.URL, "--token", "wrong"}); code == 0 {
			t.Error("a rejected token must fail")
		}
	})
	if !strings.Contains(out, "rejected the token") {
		t.Fatalf("message: %s", out)
	}
	out = captureOutput(t, func() {
		if code := Main([]string{"connect", server.URL, "--token", "right", "--name", "home"}); code != 0 {
			t.Errorf("connect failed: %s", out)
		}
	})
	if !strings.Contains(out, "Connected to home") {
		t.Fatalf("connect output: %s", out)
	}
	ws := config.LoadWorkspaces()
	if ws.Default != "home" || config.LoadToken(server.URL) != "right" {
		t.Fatalf("saved: %+v token %q", ws, config.LoadToken(server.URL))
	}
	root := filepath.Join(dir, "Notes")
	out = captureOutput(t, func() {
		if code := Main([]string{"init", root}); code != 0 {
			t.Errorf("init failed: %s", out)
		}
	})
	if _, err := os.Stat(filepath.Join(root, ".zennotes", "vault.json")); err != nil {
		t.Fatal("init writes vault.json")
	}
	if _, err := os.Stat(filepath.Join(root, "Welcome.md")); err != nil {
		t.Fatal("init writes a welcome note")
	}
	if config.LoadWorkspaces().Default != "Notes" {
		t.Fatal("init makes the vault the default")
	}
	out = captureOutput(t, func() { _ = Main([]string{"vault", "list"}) })
	if !strings.Contains(out, "* Notes") || !strings.Contains(out, "home") {
		t.Fatalf("list: %s", out)
	}
	out = captureOutput(t, func() {
		if code := Main([]string{"use", "home"}); code != 0 {
			t.Errorf("use failed: %s", out)
		}
	})
	if config.LoadWorkspaces().Default != "home" {
		t.Fatal("use switches the default")
	}
	out = captureOutput(t, func() {
		if code := Main([]string{"list", "--json"}); code != 0 {
			t.Errorf("a command against the saved server exited %d", code)
		}
	})
	captureOutput(t, func() { _ = Main([]string{"disconnect", "home"}) })
	if config.LoadWorkspaces().FindServer("home") != nil || config.LoadToken(server.URL) != "" {
		t.Fatal("disconnect forgets the server and its token")
	}
}

func TestConnectWithBrokenCredentialsKeepsTheSavedDefault(t *testing.T) {
	dir := isolatedCLI(t)
	ws := config.Workspaces{Default: "notes"}
	ws.AddVault("notes", dir)
	if err := config.SaveWorkspaces(ws); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(config.WorkspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.CredentialsPath(), []byte("[tokens]\n\"https://other.example.com\" = \"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"root":"/srv/notes","name":"notes"}`))
	}))
	defer server.Close()
	code := 0
	out := captureOutput(t, func() { code = Main([]string{"connect", server.URL, "--token", "right", "--json"}) })
	if code == 0 || !strings.Contains(out, "credentials.toml") {
		t.Fatalf("must report the broken store: exit %d %s", code, out)
	}
	if raw, err := os.ReadFile(config.WorkspacesPath()); err != nil || string(raw) != string(original) {
		t.Fatalf("failed connection changed saved workspaces: %s %v", raw, err)
	}
}

// A desktop-managed zn follows the app, so `zn connect` must not claim zn
// now uses the server by default; what it does is give zn (and zn mcp) the
// token for the server whenever the app has it open.
func TestConnectUnderTheDesktopLauncherAuthenticatesTheAppsServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	t.Setenv("ZENNOTES_VAULT", "")
	t.Setenv("ZENNOTES_SERVER", "")
	t.Setenv("ZENNOTES_REMOTE_TOKEN", "")
	t.Setenv("ZENNOTES_WORKSPACE_SOURCE", "app")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer right" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/vault":
			_, _ = w.Write([]byte(`{"root":"/srv/notes","name":"notes"}`))
		case "/api/notes":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	app := `{"workspaceMode":"remote","remoteWorkspace":{"baseUrl":"` + server.URL + `"},"remoteWorkspaceProfiles":[{"name":"home","baseUrl":"` + server.URL + `"}]}`
	if err := os.WriteFile(filepath.Join(dir, config.AppConfigFile), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}

	code := 0
	out := captureOutput(t, func() { code = Main([]string{"list", "--json"}) })
	if code == 0 || !strings.Contains(out, `"error"`) {
		t.Fatalf("without a token the app's server must refuse: %s", out)
	}
	out = captureOutput(t, func() {
		if code := Main([]string{"connect", server.URL, "--token", "right"}); code != 0 {
			t.Errorf("connect failed")
		}
	})
	if !strings.Contains(out, "zn tui opens it by default now") || !strings.Contains(out, "keep following the ZenNotes app") || strings.Contains(out, "zn and zn tui use it") {
		t.Fatalf("connect output under the desktop launcher: %s", out)
	}
	out = captureOutput(t, func() { code = Main([]string{"list", "--json"}) })
	if code != 0 {
		t.Fatalf("the saved token must reach the app's server: %s", out)
	}
	out = captureOutput(t, func() { _ = Main([]string{"connect", server.URL, "--token", "right", "--no-default"}) })
	if !strings.Contains(out, "zn commands and zn mcp use this token whenever the ZenNotes app is connected to "+server.URL) {
		t.Fatalf("--no-default output under the desktop launcher: %s", out)
	}
}
