package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/zennotescli/internal/config"
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
	out = captureOutput(t, func() { _ = Main([]string{"list", "--json"}) })
	if strings.Contains(out, "zn:") {
		t.Fatalf("a command against the saved server should reach it: %s", out)
	}
	captureOutput(t, func() { _ = Main([]string{"disconnect", "home"}) })
	if config.LoadWorkspaces().FindServer("home") != nil || config.LoadToken(server.URL) != "" {
		t.Fatal("disconnect forgets the server and its token")
	}
}
