package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// A server that does not say it marks links shares nothing with a client that
// sees part of a vault, and neither does one that cannot be asked. This needs no
// real server, so it runs everywhere.
func TestPathScopeSharesNothingWithoutLinkMarks(t *testing.T) {
	for name, capabilities := range map[string]func(http.ResponseWriter){
		"the mark is absent":  func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"version":"2.82.0"}`)) },
		"the mark is false":   func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"supportsSymlinkMarks":false}`)) },
		"the request fails":   func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
		"the route is absent": func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) },
	} {
		t.Run(name, func(t *testing.T) {
			listed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/capabilities" {
					capabilities(w)
					return
				}
				if r.URL.Path == "/api/vault" {
					_, _ = w.Write([]byte(`{"root":"/srv/notes","name":"notes","linkedSystemFolders":[]}`))
					return
				}
				if r.URL.Path == "/api/notes" {
					listed = true
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			target := backend.Target{Kind: backend.KindRemote, BaseURL: server.URL, AuthToken: scopeServerToken}
			cs := serveScoped(t, target, testScope(t, "write", []string{"Projects"}, nil))
			for tool, args := range map[string]map[string]any{
				"list_notes":  {},
				"read_note":   {"path": "Projects/Plan.md"},
				"create_note": {"folder": "inbox", "subpath": "Projects", "title": "New"},
			} {
				text, failed := contractCall(cs, tool, args)
				if !failed {
					t.Errorf("%s answered: %s", tool, text)
				}
				if name != "the request fails" && !strings.Contains(text, "cannot share part of a vault") {
					t.Errorf("%s: got %q", tool, text)
				}
			}
			if listed {
				t.Error("the listings were read")
			}
		})
	}
}

// A server with the marks that does not report linked system folders could still
// be hiding one, so it is treated like a server without the marks.
func TestPathScopeSharesNothingWithoutTheLinkedFolderList(t *testing.T) {
	for name, vaultInfo := range map[string]string{
		"the list is absent": `{"root":"/srv/notes","name":"notes"}`,
		"the list is null":   `{"root":"/srv/notes","name":"notes","linkedSystemFolders":null}`,
		"an entry is empty":  `{"root":"/srv/notes","name":"notes","linkedSystemFolders":[""]}`,
		"a trailing slash":   `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["quick/"]}`,
		"a leading slash":    `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["/quick"]}`,
		"a dot segment":      `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["./quick"]}`,
		"a parent segment":   `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["a/../quick"]}`,
		"a doubled slash":    `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["a//quick"]}`,
		"a leading space":    `{"root":"/srv/notes","name":"notes","linkedSystemFolders":[" quick"]}`,
		"a space inside":     `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["a /quick"]}`,
		"a byte order mark":  "{\"root\":\"/srv/notes\",\"name\":\"notes\",\"linkedSystemFolders\":[\"\ufeffquick\"]}",
		"a backslash":        `{"root":"/srv/notes","name":"notes","linkedSystemFolders":["a\\\\quick"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/capabilities":
					_, _ = w.Write([]byte(`{"supportsSymlinkMarks":true}`))
				case "/api/vault":
					_, _ = w.Write([]byte(vaultInfo))
				case "/api/vault/settings":
					_, _ = w.Write([]byte(`{"primaryNotesLocation":"root"}`))
				case "/api/notes", "/api/folders", "/api/assets":
					_, _ = w.Write([]byte(`[]`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			target := backend.Target{Kind: backend.KindRemote, BaseURL: server.URL, AuthToken: scopeServerToken}
			cs := serveScoped(t, target, testScope(t, "write", []string{"Projects"}, nil))
			text, failed := contractCall(cs, "list_notes", map[string]any{})
			if !failed || !strings.Contains(text, "cannot share part of a vault") {
				t.Errorf("got %q", text)
			}
		})
	}
}

// A server's answer about a lifecycle move's destination gets the usual refusal
// under a path scope, not the words that say a link is there. Other failures and
// other clients keep the server's answer.
func TestLifecycleAnswersFromAServer(t *testing.T) {
	notes := []vault.NoteMeta{
		{Path: "Projects/Plan.md", Title: "Plan", Folder: vault.FolderInbox},
		{Path: "archive/Projects/Old.md", Title: "Old", Folder: vault.FolderArchive},
		{Path: "trash/Projects/Thrown.md", Title: "Thrown", Folder: vault.FolderTrash},
	}
	for route, call := range map[string]struct {
		tool string
		path string
	}{
		"/api/notes/archive":   {"archive_note", "Projects/Plan.md"},
		"/api/notes/trash":     {"move_to_trash", "Projects/Plan.md"},
		"/api/notes/unarchive": {"unarchive_note", "archive/Projects/Old.md"},
		"/api/notes/restore":   {"restore_from_trash", "trash/Projects/Thrown.md"},
	} {
		for status, mapped := range map[int]bool{http.StatusBadRequest: true, http.StatusNotFound: true, http.StatusInternalServerError: false} {
			t.Run(fmt.Sprintf("%s %d", call.tool, status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/capabilities":
						_, _ = w.Write([]byte(`{"supportsSymlinkMarks":true}`))
					case "/api/vault":
						_, _ = w.Write([]byte(`{"root":"/srv/notes","name":"notes","linkedSystemFolders":[]}`))
					case "/api/vault/settings":
						_, _ = w.Write([]byte(`{"primaryNotesLocation":"root"}`))
					case "/api/notes":
						_ = json.NewEncoder(w).Encode(notes)
					case "/api/folders":
						_, _ = w.Write([]byte(`[{"folder":"inbox","subpath":"Projects"}]`))
					case "/api/assets":
						_, _ = w.Write([]byte(`[]`))
					case route:
						http.Error(w, "path goes through a symbolic link", status)
					default:
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				target := backend.Target{Kind: backend.KindRemote, BaseURL: server.URL, AuthToken: scopeServerToken}
				cs := serveScoped(t, target, testScope(t, "write", []string{"Projects", "archive", "trash"}, nil))
				text, failed := contractCall(cs, call.tool, map[string]any{"path": call.path})
				if !failed {
					t.Fatalf("answered: %s", text)
				}
				if mapped != strings.HasPrefix(text, "Error: This assistant can create and move notes only inside") {
					t.Errorf("got %q", text)
				}
				if mapped && strings.Contains(text, "symbolic") {
					t.Errorf("got %q", text)
				}
			})
		}
	}
}

// reportsLinkedFolders is a vault that says a system folder is a link, in the
// spelling it chooses.
type reportsLinkedFolders struct {
	Backend
	linked []string
}

func (r reportsLinkedFolders) LinkedSystemFolders(context.Context) ([]string, error) {
	return r.linked, nil
}

// A vault may spell a linked system folder differently from its settings.
func TestLifecycleExitMatchesALinkedFolderAcrossSpellings(t *testing.T) {
	root := seedLinkedVault(t, nil)
	local, err := backend.New(backend.Target{Kind: backend.KindLocal, Root: root}, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scoped := newScopedBackend(reportsLinkedFolders{local, []string{"ARCHIVE"}}, *testScope(t, "write", []string{"Projects"}, nil))
	if _, err := scoped.ArchiveNote(context.Background(), "Projects/Plan.md"); err == nil {
		t.Fatal("archived into a folder reported as a link")
	}
	if _, err := os.Stat(filepath.Join(root, "Projects", "Plan.md")); err != nil {
		t.Error(err)
	}
}

// The asset routes trim whitespace after the scope check, so these paths would
// reach another file than the one checked.
func TestAssetPathsThatTrimDifferentlyAreRefused(t *testing.T) {
	for _, space := range []string{" ", "\u00a0", "\u2003"} {
		root := seedLinkedVault(t, nil)
		for rel, body := range map[string]string{
			"Journal/secret.png":         "secret",
			space + "Journal/secret.png": "twin",
			"assets/pic.png":             "png",
		} {
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cs := runScoped(t, root, testScope(t, "write", nil, []string{"Journal"}))
		if text, failed := contractCall(cs, "move_asset", map[string]any{"path": "assets/pic.png", "targetDir": "Journal" + space}); !failed {
			t.Errorf("move into the denied folder: %s", text)
		}
		twin := space + "Journal/secret.png"
		for tool, args := range map[string]map[string]any{
			"read_asset":      {"path": twin},
			"rename_asset":    {"path": twin, "name": "out.png"},
			"move_asset":      {"path": twin, "targetDir": "assets"},
			"duplicate_asset": {"path": twin},
			"delete_asset":    {"path": twin, "confirm": true},
		} {
			if text, failed := contractCall(cs, tool, args); !failed || strings.Contains(text, "secret\"") {
				t.Errorf("%s: %s", tool, text)
			}
		}
		got, err := os.ReadFile(filepath.Join(root, "Journal", "secret.png"))
		if err != nil || string(got) != "secret" {
			t.Errorf("the denied file: %q (%v)", got, err)
		}
		if _, err := os.Stat(filepath.Join(root, "Journal", "pic.png")); err == nil {
			t.Error("the file landed in the denied folder")
		}
	}
}

func TestAScopedClientIsNotToldWhereAMissingVaultIs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "secret-home", "gone")
	cs := runScoped(t, root, testScope(t, "read", []string{"Projects"}, nil))
	text, failed := contractCall(cs, "vault_info", map[string]any{})
	if !failed || strings.Contains(text, "secret-home") {
		t.Errorf("got %q", text)
	}
}
