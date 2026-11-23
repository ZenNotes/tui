package backend

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/remote/remotetest"
	"github.com/ZenNotes/tui/internal/vault"
)

const e2eToken = "isolated-conditional-write-token"

// startConditionalServer runs a real server over a vault holding one note and
// fronts it with a proxy, and says whether it has conditional writes.
func startConditionalServer(t *testing.T, body string) (string, *remotetest.Proxy, *remote.Client, bool) {
	t.Helper()
	binary := os.Getenv(remotetest.BinaryEnv)
	if binary == "" {
		t.Skip("set " + remotetest.BinaryEnv + " to a ZenNotes server binary to run against a real server")
	}
	root := t.TempDir()
	file := filepath.Join(root, "inbox", "Plan.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	base := remotetest.StartBinary(t, binary, root, e2eToken)
	other := remote.NewClient(base, e2eToken)
	caps, err := other.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return file, remotetest.NewProxy(t, base), other, caps["supportsConditionalNoteWrites"] == true
}

// Against a real server: a save another client makes between zn's read and
// its write is refused there, read again, and kept under zn's own change. A
// note that keeps changing ends in an error with nothing written over. A
// server without conditional writes edits exactly as before: one write, and
// the save in between is lost, as it always was there.
func TestARealServerKeepsAConcurrentSaveUnderAnEdit(t *testing.T) {
	ctx := context.Background()
	start := "# Plan\n\n- [ ] ship\n"
	theirs := start + "\nTheir line.\n"
	file, proxy, other, conditional := startConditionalServer(t, start)
	r := newRemote(Target{Kind: KindRemote, BaseURL: proxy.URL, AuthToken: e2eToken})

	if !conditional {
		proxy.SetBeforeWrite(func() {
			if _, err := other.WriteNote(ctx, "inbox/Plan.md", theirs); err != nil {
				t.Error(err)
			}
		})
		if _, err := r.AppendToNote(ctx, "inbox/Plan.md", "My line."); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(file)
		if string(data) != vault.AppendToBody(start, "My line.") {
			t.Fatalf("on disk: %q", data)
		}
		if got := proxy.WriteStatuses(); !reflect.DeepEqual(got, []int{http.StatusOK}) {
			t.Fatalf("write statuses: %v", got)
		}
		return
	}

	raced := false
	proxy.SetBeforeWrite(func() {
		if !raced {
			raced = true
			if _, err := other.WriteNote(ctx, "inbox/Plan.md", theirs); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := r.AppendToNote(ctx, "inbox/Plan.md", "My line."); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if string(data) != vault.AppendToBody(theirs, "My line.") {
		t.Fatalf("on disk: %q", data)
	}
	if got := proxy.WriteStatuses(); !reflect.DeepEqual(got, []int{http.StatusConflict, http.StatusOK}) {
		t.Fatalf("write statuses: %v", got)
	}

	// The toggle, on the text as it now is.
	raced = false
	theirs = string(data) + "Another line.\n"
	if _, err := r.ToggleTask(ctx, "inbox/Plan.md#0", vault.DialectCLI); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(file)
	if !strings.Contains(string(data), "- [x] ship") || !strings.HasSuffix(string(data), "Another line.\n") {
		t.Fatalf("on disk after toggle: %q", data)
	}

	// Changed before every write: three refusals, then an error.
	n := 0
	proxy.SetBeforeWrite(func() {
		n++
		if _, err := other.WriteNote(ctx, "inbox/Plan.md", strings.Repeat("theirs\n", n)); err != nil {
			t.Error(err)
		}
	})
	_, err := r.AppendToNote(ctx, "inbox/Plan.md", "Never written.")
	if !remote.IsNoteChanged(err) || !strings.Contains(err.Error(), "tried 3 times") {
		t.Fatalf("expected the give-up error, got %v", err)
	}
	data, _ = os.ReadFile(file)
	if string(data) != strings.Repeat("theirs\n", RewriteAttempts) {
		t.Fatalf("on disk after giving up: %q", data)
	}
}
