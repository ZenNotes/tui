package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestVersionOpensPersistentReportForBugReports(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.opts.Version = "0.6.0-test"
	suppressAsyncTestTimers(a)
	if handled, err := a.runEx(nil, vim.ExCommand{Name: "version"}); err != nil || !handled {
		t.Fatalf("version command: %t %v", handled, err)
	}
	if a.overlay == nil {
		t.Fatal(":version should open a persistent report, not a transient toast")
	}
	for _, cmd := range a.pendingCmds {
		a.Update(cmd())
	}
	report := stripAnsi(a.overlay.render(a, 120, 40))
	for _, expected := range []string{"ZenNotes TUI 0.6.0-test", "OS:", "Architecture: " + runtime.GOARCH, "Go: " + runtime.Version(), "Installation:", "Workspace: local", "Workspace source: terminal", "copy"} {
		if !strings.Contains(report, expected) {
			t.Errorf("missing %q in report:\n%s", expected, report)
		}
	}
	if strings.Contains(report, root) {
		t.Fatal("copyable report includes the user's private vault path")
	}
	a.overlay.handleKey(a, vim.KeyEsc)
	if a.overlay != nil {
		t.Fatal("Esc did not dismiss the report")
	}
}

func TestVersionReportsConnectedServerWithoutCredentials(t *testing.T) {
	const token = "bearer-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notes/api/version" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected version request path/authentication")
		}
		json.NewEncoder(w).Encode(map[string]string{"version": "2.56.0"})
	}))
	defer srv.Close()
	a, _ := newFeatureTestApp(t)
	u, _ := url.Parse(srv.URL + "/notes?token=query-secret#fragment-secret")
	u.User = url.UserPassword("private-user", "password-secret")
	a.opts.Target = backend.Target{Kind: backend.KindRemote, BaseURL: u.String(), AuthToken: token}
	suppressAsyncTestTimers(a)
	a.runEx(nil, vim.ExCommand{Name: "version"})
	if a.overlay == nil {
		t.Fatal(":version did not open a report")
	}
	for _, cmd := range a.pendingCmds {
		a.Update(cmd())
	}
	report := stripAnsi(a.overlay.render(a, 120, 40))
	if !strings.Contains(report, "Server: 2.56.0") || !strings.Contains(report, srv.URL+"/notes") {
		t.Fatalf("connected server details missing:\n%s", report)
	}
	for _, secret := range []string{token, "private-user", "password-secret", "query-secret", "fragment-secret"} {
		if strings.Contains(report, secret) {
			t.Fatalf("report exposed %q", secret)
		}
	}
}

func TestVersionCopyVariantsAndKeyboardCopy(t *testing.T) {
	for _, command := range []vim.ExCommand{{Name: "version", Args: "copy"}, {Name: "version", Bang: true}, {Name: "ve", Args: "copy"}} {
		t.Run(command.Name+command.Args+map[bool]string{true: "!"}[command.Bang], func(t *testing.T) {
			a, _ := newFeatureTestApp(t)
			suppressAsyncTestTimers(a)
			a.openNote("note.md", true)
			line := ":" + command.Name
			if command.Bang {
				line += "!"
			}
			if command.Args != "" {
				line += " " + command.Args
			}
			a.activeBuffer().ed.Feed(line + "<CR>")
			view, ok := a.overlay.(*versionView)
			if !ok {
				t.Fatal("version command did not open the version view")
			}
			copied := []string{}
			view.writeClipboard = func(text string) error { copied = append(copied, text); return nil }
			for _, cmd := range a.pendingCmds {
				a.Update(cmd())
			}
			if len(copied) != 1 || copied[0] != view.body || strings.Contains(copied[0], "\x1b") {
				t.Fatalf("copy did not use the completed plain-text report: %v", copied)
			}
			view.handleKey(a, vim.R('c'))
			if len(copied) != 2 || copied[1] != copied[0] {
				t.Fatal("c did not copy the report")
			}
		})
	}
}

func TestClosingVersionCancelsCollectionAndDoesNotStealFocusOrClipboard(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	suppressAsyncTestTimers(a)
	a.runEx(nil, vim.ExCommand{Name: "version", Args: "copy"})
	view := a.overlay.(*versionView)
	view.writeClipboard = func(string) error { t.Error("closed report wrote to clipboard"); return nil }
	commands := a.pendingCmds
	view.handleKey(a, vim.KeyEsc)
	next := &textReader{title: "Another operation"}
	a.overlay = next
	for _, cmd := range commands {
		a.Update(cmd())
	}
	if a.overlay != next {
		t.Fatal("late version result replaced a new overlay")
	}
}

func TestSettingsOffersVersionDetailsWithoutVim(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.VimMode = false
	suppressAsyncTestTimers(a)
	v := &settingsView{}
	if !v.handleKey(a, vim.R('v')) {
		t.Fatal("settings did not handle v")
	}
	report, ok := a.overlay.(*versionView)
	if !ok {
		t.Fatal("settings did not expose version details")
	}
	report.handleKey(a, vim.KeyEsc)
}

func TestConnectedServerVersionTimeoutAndErrorBodiesAreSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/slow/") {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("private server exception with bearer-secret and /private/notes"))
	}))
	defer srv.Close()
	if got := connectedServerVersion(context.Background(), srv.URL, "bearer-secret"); got != "unavailable (HTTP 500)" {
		t.Fatalf("server failure was not summarized safely: %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if got := connectedServerVersion(ctx, srv.URL+"/slow", ""); !strings.HasPrefix(got, "unavailable") {
		t.Fatalf("timeout: %s", got)
	}
	if ctx.Err() == nil {
		t.Fatal("slow request did not honor the context deadline")
	}
}

func TestVersionValuesAndLinuxDistributionArePlainText(t *testing.T) {
	if got := versionValue("\x1b[31mGhostty\x1b[0m\nINJECTED\x00"); got != "Ghostty INJECTED" {
		t.Fatalf("terminal value was not sanitized: %q", got)
	}
	for _, input := range []string{`PRETTY_NAME="Ubuntu 24.04.1 LTS"`, `PRETTY_NAME='Ubuntu 24.04.1 LTS'`, `PRETTY_NAME=Ubuntu 24.04.1 LTS`} {
		if got := linuxDistribution("NAME=Ubuntu\n" + input); got != "Ubuntu 24.04.1 LTS" {
			t.Fatalf("distribution parsing: %q", got)
		}
	}
	if got := versionServerURL("https://username:password@notes.example.com/base?secret=1#private"); got != "https://notes.example.com/base" {
		t.Fatalf("URL was not redacted: %s", got)
	}
}
