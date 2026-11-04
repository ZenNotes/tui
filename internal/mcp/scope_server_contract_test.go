package mcp

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

const scopeServerToken = "tok-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// scopeServerBinary is the real server these tests run against, built from
// ZenNotes/znserver. CI always sets it; see docs/shared-contracts.md.
func scopeServerBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("ZENNOTES_SERVER_CONTRACT_BINARY")
	if binary == "" {
		t.Skip("set ZENNOTES_SERVER_CONTRACT_BINARY to an independently built server to run the scope contract against it")
	}
	return binary
}

// runScopedOnServer starts the server on a vault and serves it under a scope
// the way `zn mcp --server` does.
func runScopedOnServer(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	state := t.TempDir()
	log, err := os.Create(filepath.Join(state, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(scopeServerBinary(t))
	command.Dir = state
	command.Env = append(os.Environ(),
		"ZENNOTES_BIND="+address,
		"ZENNOTES_VAULT_PATH="+root,
		"ZENNOTES_CONFIG_PATH="+filepath.Join(state, "server.json"),
		"ZENNOTES_AUTH_TOKEN="+scopeServerToken,
	)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		log.Close()
	})

	base := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			logged, _ := os.ReadFile(log.Name())
			t.Fatalf("server did not become ready: %v\n%s", err, logged)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return serveScoped(t, backend.Target{Kind: backend.KindRemote, BaseURL: base, AuthToken: scopeServerToken}, scope)
}

const (
	scopeServerNoBytes = "Reading or copying attachments on a ZenNotes server is not available to an assistant that sees only part of the vault."
	scopeServerNoMoves = "Renaming or moving attachments on a ZenNotes server is not available to an assistant that sees only part of the vault: the server rewrites links in every note, including ones this assistant cannot see."
)

// What a scoped client may see differ on a server. Everything else in the
// fixture holds on both, the refusal bytes for a link included.
var scopeServerDifferences = []scopeContractDifference{
	{
		Case: "assets-answer-through-the-scope", Call: 2, Tool: "read_asset",
		Why:    "a server that does not mark links could hand over a hidden file, so a client that sees part of the vault gets no file contents",
		Expect: scopeContractExpect{Error: ptr(scopeServerNoBytes)},
	},
	{
		Case: "assets-answer-through-the-scope", Call: 3, Tool: "read_asset",
		Why:    "the same refusal, for another spelling of the path",
		Expect: scopeContractExpect{Error: ptr(scopeServerNoBytes)},
	},
	{
		Case: "asset-writes-stay-inside-the-scope", Call: 6, Tool: "rename_asset",
		Why:    "a server rewrites links to a file in every note itself, as it does for a note",
		Expect: scopeContractExpect{Error: ptr(scopeServerNoMoves)},
		Exists: []string{"assets/pic.png"},
		Absent: []string{"assets/photo.png"},
		Equals: map[string]string{"Projects/Plan.md": "# Plan\n\nIntro\n\n## Actions\n\n- [ ] ship it\n- [ ] write it due:2026-01-05 !low\n\n### Detail\n\nfine print\n\n## Later\n\n![[pic.png]]\n"},
	},
	{
		Case: "asset-writes-stay-inside-the-scope", Call: 15, Tool: "move_asset",
		Why:    "the same refusal, for a move",
		Expect: scopeContractExpect{Error: ptr(scopeServerNoMoves)},
		Exists: []string{"assets/words.txt"},
		Absent: []string{"Projects/Files/words.txt"},
	},
	{
		Case: "asset-writes-stay-inside-the-scope", Call: 16, Tool: "duplicate_asset",
		Why:    "the rename above was refused, so the file it would have named is not there",
		Expect: scopeContractExpect{Error: ptr("Asset not found: assets/photo.png")},
	},
	{
		Case: "asset-writes-stay-inside-the-scope", Call: 18, Tool: "delete_asset",
		Why:    "the copy above was not made",
		Expect: scopeContractExpect{Error: ptr("Asset not found: assets/photo copy.png")},
	},
	{
		Case: "writes-stay-inside-the-scope",
		Call: 15,
		Tool: "rename_note",
		Why:  "a server rewrites inbound links in every note itself, so a client that sees part of the vault cannot rename there",
		Expect: scopeContractExpect{
			Error: ptr("Renaming notes on a ZenNotes server is not available to an assistant that sees only part of the vault: the server rewrites links in every note, including ones this assistant cannot see."),
		},
		Exists: []string{"Projects/Plan.md"},
		Absent: []string{"Projects/Plan B.md"},
	},
	{
		Case: "vault-info-names-no-absolute-paths-or-deny-entries",
		Call: 0,
		Tool: "vault_info",
		Why:  "vault_info reports where the vault is, and this one is on a server",
		Expect: scopeContractExpect{
			Match:      []byte(`{"kind":"remote","primaryNotesLocation":"root","topFolders":["inbox","quick","archive","trash"],"access":{"level":"read","partial":true,"allow":["Areas/Gone","Projects"],"missingAllow":["Areas/Gone"]}}`),
			AbsentKeys: []string{"vaultRoot", "inboxAbsolutePath"},
		},
	},
}

func ptr[T any](value T) *T { return &value }

func TestSharedScopeContractOnServer(t *testing.T) {
	scopeServerBinary(t)
	runScopeContract(t, runScopedOnServer, scopeServerDifferences)
}

// A server lists a link to a note, and reads and writes follow it, so a link in
// a shared folder must not read as a note there. Links to a note are not in the
// shared fixture yet: the desktop's engine does not honor the server's mark.
func TestScopedServerHidesLinkedNotes(t *testing.T) {
	scopeServerBinary(t)
	root := filepath.Join(t.TempDir(), "vault")
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":     "# Plan\n",
		"Journal/Diary.md":     "# Diary\n\nprivate needle\n- [ ] call mum\n\nsee [[Plan]]\n",
		"Journal/secret.png":   "png",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"Projects/alias.md":  "../Journal/Diary.md",
		"Projects/alias.png": "../Journal/secret.png",
	} {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	scope := testScope(t, "write", []string{"Projects"}, nil)
	cs := runScopedOnServer(t, root, scope)
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"read_note", map[string]any{"path": "Projects/alias.md"}},
		{"write_note", map[string]any{"path": "Projects/alias.md", "body": "# Overwritten\n"}},
		{"append_to_note", map[string]any{"path": "Projects/alias.md", "text": "more"}},
		{"delete_note", map[string]any{"path": "Projects/alias.md", "confirm": true}},
	} {
		text, failed := contractCall(cs, call.tool, call.args)
		if want := "Error: Note not found: Projects/alias.md"; !failed || text != want {
			t.Errorf("%s: got %q, want %q", call.tool, text, want)
		}
	}
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"list_notes", map[string]any{}},
		{"list_assets", map[string]any{}},
		{"list_tasks", map[string]any{}},
		{"search_text", map[string]any{"query": "needle"}},
		{"search_by_title", map[string]any{"query": "alias"}},
		{"backlinks", map[string]any{"path": "Projects/Plan.md"}},
	} {
		text, failed := contractCall(cs, call.tool, call.args)
		if failed {
			t.Errorf("%s failed: %s", call.tool, text)
		}
		for _, leak := range []string{"alias", "needle", "call mum"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s shows %q: %s", call.tool, leak, text)
			}
		}
	}
	got, err := os.ReadFile(filepath.Join(root, "Journal", "Diary.md"))
	if err != nil || !strings.Contains(string(got), "private needle") {
		t.Errorf("the hidden note was changed: %q (%v)", got, err)
	}
}

// The fixture's refusal comes from zn, which looks before it writes, so it
// cannot show that the server also refuses. An unscoped client asks the server
// directly. That is what closes the race between looking and writing, and what
// protects a client that does not look.
func TestServerRefusesWritesThroughAFolderLink(t *testing.T) {
	scopeServerBinary(t)
	root := filepath.Join(t.TempDir(), "vault")
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":     "# Plan\n",
		"Journal/Diary.md":     "# Diary\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.FromSlash("../Journal"), filepath.Join(root, "Projects", "inner")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cs := runScopedOnServer(t, root, nil)
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"create_note", map[string]any{"folder": "inbox", "subpath": "Projects/inner", "title": "Via"}},
		{"create_folder", map[string]any{"folder": "inbox", "subpath": "Projects/inner/Made"}},
		{"move_note", map[string]any{"path": "Projects/Plan.md", "targetFolder": "inbox", "targetSubpath": "Projects/inner"}},
	} {
		text, failed := contractCall(cs, call.tool, call.args)
		if !failed || !strings.Contains(text, "symbolic link") {
			t.Errorf("%s: got %q", call.tool, text)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "Journal"))
	if err != nil || len(entries) != 1 {
		t.Errorf("Journal holds %v (%v)", entries, err)
	}
}

// Files on a server: a linked one is absent, and a file is not put through a
// link to a folder. The refusal is zn's, except for an attachment folder that is
// itself a link, where a read answers the same as for a real one and the server
// has to refuse.
func TestScopedServerKeepsFilesOutOfLinks(t *testing.T) {
	scopeServerBinary(t)
	root := filepath.Join(t.TempDir(), "vault")
	for rel, body := range map[string]string{
		".zennotes/vault.json":  `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":      "# Plan\n",
		"Projects/pic.png":      "png",
		"Projects/Held/old.png": "old",
		"assets/shared.png":     "png",
		"Journal/Diary.md":      "# Diary\n",
		"Journal/secret.png":    "secret",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"Projects/inner":     "../Journal",
		"Projects/alias.png": "../Journal/secret.png",
	} {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	journal := func() int {
		t.Helper()
		entries, err := os.ReadDir(filepath.Join(root, "Journal"))
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	before := journal()

	cs := runScopedOnServer(t, root, testScope(t, "write", []string{"Projects", "assets"}, nil))
	for _, tool := range []string{"read_asset", "rename_asset", "move_asset", "duplicate_asset", "delete_asset"} {
		args := map[string]any{"path": "Projects/alias.png", "name": "x.png", "targetDir": "Projects", "confirm": true}
		if text, failed := contractCall(cs, tool, args); !failed || strings.Contains(text, "secret") {
			t.Errorf("%s on a linked file: got %q", tool, text)
		}
	}
	if text, failed := contractCall(cs, "list_assets", map[string]any{}); failed || strings.Contains(text, "alias") {
		t.Errorf("list_assets: %q", text)
	}

	refusal := "Error: This assistant can put files only inside: Projects, assets."
	if text, _ := contractCall(cs, "move_asset", map[string]any{"path": "Projects/pic.png", "targetDir": "Projects/inner"}); text != refusal {
		t.Errorf("move_asset through a link: got %q, want %q", text, refusal)
	}
	var deleted struct {
		UndoToken string `json:"undoToken"`
	}
	if err := json.Unmarshal([]byte(mustCall(t, cs, "delete_asset", map[string]any{"path": "Projects/Held/old.png", "confirm": true})), &deleted); err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(root, "Projects", "Held")
	if err := os.RemoveAll(held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash("../Journal"), held); err != nil {
		t.Fatal(err)
	}
	want := "Error: Deleted asset not found: " + deleted.UndoToken
	if text, _ := contractCall(cs, "restore_asset", map[string]any{"undoToken": deleted.UndoToken}); text != want {
		t.Errorf("restore_asset through a link: got %q, want %q", text, want)
	}
	if got := journal(); got != before {
		t.Errorf("Journal went from %d entries to %d", before, got)
	}
	// The assets folder is real, and listings hide it. It stays usable.
	if text, failed := contractCall(cs, "upload_asset", map[string]any{"filename": "new.png", "content_base64": "cG5n"}); failed {
		t.Errorf("upload_asset to the assets folder: %q", text)
	}
}

func seedLinkedVault(t *testing.T, links map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":     "# Plan\n",
		"Projects/pic.png":     "png",
		"Journal/Diary.md":     "# Diary\n\nprivate needle\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range links {
		abs := filepath.Join(root, filepath.FromSlash(link))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.FromSlash(target), abs); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	return root
}

func journalNames(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "Journal"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return strings.Join(names, ",")
}

// A dangling link reads as a 404, the same as a free name, so only the server
// can keep a new file from landing on one.
func TestServerKeepsNewFilesOffADanglingLink(t *testing.T) {
	scopeServerBinary(t)
	root := seedLinkedVault(t, map[string]string{
		"Projects/Ghost.md":     "../Journal/New.md",
		"Projects/Plan copy.md": "../Journal/Copied.md",
		"assets/dropped.png":    "../Journal/dropped.png",
	})
	cs := runScopedOnServer(t, root, testScope(t, "write", []string{"Projects", "assets"}, nil))
	if text, failed := contractCall(cs, "create_note", map[string]any{"folder": "inbox", "subpath": "Projects", "title": "Ghost", "body": "written"}); failed || !strings.Contains(text, "Projects/Ghost 2.md") {
		t.Errorf("create_note: %s", text)
	}
	if text, failed := contractCall(cs, "duplicate_note", map[string]any{"path": "Projects/Plan.md"}); failed || !strings.Contains(text, "Projects/Plan copy 2.md") {
		t.Errorf("duplicate_note: %s", text)
	}
	if text, failed := contractCall(cs, "upload_asset", map[string]any{"filename": "dropped.png", "content_base64": "cG5n"}); failed || !strings.Contains(text, "assets/dropped 2.png") {
		t.Errorf("upload_asset: %s", text)
	}
	if got := journalNames(t, root); got != "Diary.md" {
		t.Errorf("Journal holds %s", got)
	}
}

func TestServerKeepsUploadsOutOfALinkedAssetsFolder(t *testing.T) {
	scopeServerBinary(t)
	root := seedLinkedVault(t, map[string]string{"assets": "Journal"})
	cs := runScopedOnServer(t, root, testScope(t, "write", []string{"Projects", "assets"}, nil))
	if text, failed := contractCall(cs, "upload_asset", map[string]any{"filename": "dropped.png", "content_base64": "cG5n"}); !failed {
		t.Errorf("upload_asset: %s", text)
	}
	if got := journalNames(t, root); got != "Diary.md" {
		t.Errorf("Journal holds %s", got)
	}
}

// A scope with no paths has nothing to keep from the client, so a linked note
// reads as it does unscoped.
func TestAccessOnlyScopeOnServerKeepsLinkedNotes(t *testing.T) {
	scopeServerBinary(t)
	root := seedLinkedVault(t, map[string]string{"Projects/alias.md": "Plan.md"})
	cs := runScopedOnServer(t, root, testScope(t, "read", nil, nil))
	if text, failed := contractCall(cs, "read_note", map[string]any{"path": "Projects/alias.md"}); failed || !strings.Contains(text, "# Plan") {
		t.Errorf("read_note: %s", text)
	}
}

// Someone who denied a link to be safe keeps the rest of the vault.
func TestDenyEntryOnServerMayNameALinkedNote(t *testing.T) {
	scopeServerBinary(t)
	root := seedLinkedVault(t, map[string]string{"Projects/alias.md": "../Journal/Diary.md"})
	cs := runScopedOnServer(t, root, testScope(t, "read", nil, []string{"Projects/alias.md"}))
	if text, failed := contractCall(cs, "read_note", map[string]any{"path": "Projects/Plan.md"}); failed {
		t.Errorf("read_note Plan: %s", text)
	}
	if text, failed := contractCall(cs, "read_note", map[string]any{"path": "Projects/alias.md"}); !failed {
		t.Errorf("read_note alias: %s", text)
	}
}

// A system folder that is a link shows another folder's notes under its own
// name on a local vault, and takes writes on both. A client that sees part of
// the vault gets none of that, and a client with no paths is not affected.
func TestLinkedSystemFolderStaysOutOfAScope(t *testing.T) {
	for name, open := range map[string]func(*testing.T, string, *Scope) *sdk.ClientSession{
		"on a local vault": runScoped,
		"on a server": func(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
			scopeServerBinary(t)
			return runScopedOnServer(t, root, scope)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Run("a denied folder through a linked one", func(t *testing.T) {
				root := seedLinkedVault(t, map[string]string{"quick": "Journal"})
				cs := open(t, root, testScope(t, "write", nil, []string{"Journal"}))
				for _, tool := range []string{"read_note", "write_note", "append_to_note"} {
					text, failed := contractCall(cs, tool, map[string]any{"path": "quick/Diary.md", "body": "# Overwritten\n", "text": "more"})
					if want := "Error: Note not found: quick/Diary.md"; !failed || text != want {
						t.Errorf("%s: got %q, want %q", tool, text, want)
					}
				}
				for _, call := range []struct {
					tool string
					args map[string]any
				}{{"list_notes", map[string]any{}}, {"search_text", map[string]any{"query": "needle"}}} {
					if text, failed := contractCall(cs, call.tool, call.args); failed || strings.Contains(text, "Diary") {
						t.Errorf("%s: %s", call.tool, text)
					}
				}
				if text, failed := contractCall(cs, "create_note", map[string]any{"folder": "quick", "title": "Planted"}); !failed {
					t.Errorf("create_note: %s", text)
				}
				if got := journalNames(t, root); got != "Diary.md" {
					t.Errorf("Journal holds %s", got)
				}
				got, err := os.ReadFile(filepath.Join(root, "Journal", "Diary.md"))
				if err != nil || !strings.Contains(string(got), "private needle") {
					t.Errorf("the denied note changed: %q (%v)", got, err)
				}
			})
			for tool, link := range map[string]string{"archive_note": "archive", "move_to_trash": "trash"} {
				t.Run(tool+" into a linked folder", func(t *testing.T) {
					root := seedLinkedVault(t, map[string]string{link: "Journal"})
					cs := open(t, root, testScope(t, "write", []string{"Projects"}, nil))
					text, _ := contractCall(cs, tool, map[string]any{"path": "Projects/Plan.md"})
					if want := "Error: This assistant can create and move notes only inside: Projects."; text != want {
						t.Errorf("got %q, want %q", text, want)
					}
					if got := journalNames(t, root); got != "Diary.md" {
						t.Errorf("Journal holds %s", got)
					}
					if _, err := os.Stat(filepath.Join(root, "Projects", "Plan.md")); err != nil {
						t.Errorf("the note moved: %v", err)
					}
				})
			}
			for tool, bucket := range map[string]string{"archive_note": "archive", "move_to_trash": "trash"} {
				t.Run(tool+" through a link below its folder", func(t *testing.T) {
					root := seedLinkedVault(t, map[string]string{bucket + "/Projects": "Hidden"})
					hidden := filepath.Join(root, bucket, "Hidden")
					if err := os.MkdirAll(hidden, 0o755); err != nil {
						t.Fatal(err)
					}
					cs := open(t, root, testScope(t, "write", nil, []string{bucket + "/Hidden"}))
					text, _ := contractCall(cs, tool, map[string]any{"path": "Projects/Plan.md"})
					if want := "Error: This assistant cannot put notes there."; text != want {
						t.Errorf("got %q, want %q", text, want)
					}
					if entries, err := os.ReadDir(hidden); err != nil || len(entries) != 0 {
						t.Errorf("the hidden folder holds %v (%v)", entries, err)
					}
					if _, err := os.Stat(filepath.Join(root, "Projects", "Plan.md")); err != nil {
						t.Errorf("the note moved: %v", err)
					}
				})
			}
			t.Run("a scope with no paths still archives", func(t *testing.T) {
				root := seedLinkedVault(t, map[string]string{"archive": "Journal"})
				cs := open(t, root, testScope(t, "write", nil, nil))
				if text, failed := contractCall(cs, "archive_note", map[string]any{"path": "Projects/Plan.md"}); failed {
					t.Errorf("archive_note: %s", text)
				}
			})
		})
	}
}

// A restore keeps the note's own spelling of its folder, so a check that folds
// case and accents must not stand in for it.
func TestRestoresLandWhereTheyWereChecked(t *testing.T) {
	seed := func(t *testing.T, links map[string]string, notes ...string) string {
		t.Helper()
		root := seedLinkedVault(t, links)
		for _, rel := range notes {
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte("# Planted\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	for name, open := range map[string]func(*testing.T, string, *Scope) *sdk.ClientSession{
		"on a local vault": runScoped,
		"on a server": func(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
			scopeServerBinary(t)
			return runScopedOnServer(t, root, scope)
		},
	} {
		t.Run(name, func(t *testing.T) {
			for tool, bucket := range map[string]string{"unarchive_note": "archive", "restore_from_trash": "trash"} {
				t.Run(tool+" out of the allow list", func(t *testing.T) {
					root := seed(t, nil, bucket+"/Pr\u00f6jects/Planted.md")
					cs := open(t, root, testScope(t, "write", []string{"Projects", bucket}, nil))
					text, _ := contractCall(cs, tool, map[string]any{"path": bucket + "/Pr\u00f6jects/Planted.md"})
					if want := "Error: This assistant can create and move notes only inside: Projects, " + bucket + "."; text != want {
						t.Errorf("got %q, want %q", text, want)
					}
					if _, err := os.Stat(filepath.Join(root, "Pr\u00f6jects")); err == nil {
						t.Error("the note landed outside the scope")
					}
				})
				t.Run(tool+" through a link beside a real folder", func(t *testing.T) {
					root := seed(t, map[string]string{"Pr\u00f6jects": "Journal"}, bucket+"/Pr\u00f6jects/Planted.md")
					cs := open(t, root, testScope(t, "write", nil, []string{"Journal"}))
					text, _ := contractCall(cs, tool, map[string]any{"path": bucket + "/Pr\u00f6jects/Planted.md"})
					if want := "Error: This assistant cannot put notes there."; text != want {
						t.Errorf("got %q, want %q", text, want)
					}
					if got := journalNames(t, root); got != "Diary.md" {
						t.Errorf("Journal holds %s", got)
					}
				})
			}
			t.Run("a restore to its own spelling still works", func(t *testing.T) {
				root := seed(t, nil, "archive/Projects/Back.md")
				cs := open(t, root, testScope(t, "write", []string{"Projects", "archive"}, nil))
				if text, failed := contractCall(cs, "unarchive_note", map[string]any{"path": "archive/Projects/Back.md"}); failed {
					t.Errorf("unarchive_note: %s", text)
				}
				if _, err := os.Stat(filepath.Join(root, "Projects", "Back.md")); err != nil {
					t.Error(err)
				}
			})
		})
	}
}
