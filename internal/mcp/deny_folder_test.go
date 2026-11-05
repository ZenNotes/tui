package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ZenNotes/tui/internal/backend"
)

// A folder holding a note a deny entry hides cannot go as a whole, whichever system folder
// it is in, and however the entry names the note.
func denyFolderCases() []struct {
	name, vaultJSON, deny, folder, sub string
	files, keep                        []string
} {
	return []struct {
		name, vaultJSON, deny, folder, sub string
		files, keep                        []string
	}{
		{
			name: "root mode, the note's folders in the archive", vaultJSON: `{"primaryNotesLocation":"root"}`,
			deny: "Foo/x.md", folder: "archive", sub: "Foo",
			files: []string{"Foo/x.md", "archive/Foo/x.md", "archive/Foo/y.md"},
			keep:  []string{"archive/Foo/x.md", "archive/Foo/y.md"},
		},
		{
			name: "inbox mode, an entry in the archive", vaultJSON: `{"primaryNotesLocation":"inbox"}`,
			deny: "archive/Foo/n.md", folder: "inbox", sub: "Foo",
			files: []string{"archive/Foo/n.md", "inbox/Foo/n.md", "inbox/Foo/o.md"},
			keep:  []string{"inbox/Foo/n.md", "inbox/Foo/o.md"},
		},
		{
			name: "another case", vaultJSON: `{"primaryNotesLocation":"inbox"}`,
			deny: "INBOX/foo/N.md", folder: "archive", sub: "Foo",
			files: []string{"inbox/Foo/n.md", "archive/Foo/n.md", "archive/Foo/o.md"},
			keep:  []string{"archive/Foo/n.md", "archive/Foo/o.md"},
		},
		{
			name: "an entry on a subfolder", vaultJSON: `{"primaryNotesLocation":"root"}`,
			deny: "Foo/Sub", folder: "archive", sub: "Foo",
			files: []string{"Foo/Sub/a.md", "archive/Foo/Sub/a.md", "archive/Foo/b.md"},
			keep:  []string{"archive/Foo/Sub/a.md", "archive/Foo/b.md"},
		},
	}
}

func seedDenyFolder(t *testing.T, vaultJSON string, files []string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	for _, rel := range append([]string{".zennotes/vault.json"}, files...) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "# note\n"
		if rel == ".zennotes/vault.json" {
			body = vaultJSON
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testDenyFolder(t *testing.T, open func(*testing.T, string, *Scope) *sdk.ClientSession) {
	for _, tc := range denyFolderCases() {
		t.Run(tc.name, func(t *testing.T) {
			root := seedDenyFolder(t, tc.vaultJSON, tc.files)
			scope := Scope{Access: AccessWrite, Deny: []string{tc.deny}}
			cs := open(t, root, &scope)
			if text, failed := callTool(t, cs, "delete_folder", map[string]any{"folder": tc.folder, "subpath": tc.sub, "confirm": true}); !failed {
				t.Errorf("delete_folder went ahead: %s", text)
			}
			if text, failed := callTool(t, cs, "rename_folder", map[string]any{"folder": tc.folder, "oldSubpath": tc.sub, "newSubpath": "Moved"}); !failed {
				t.Errorf("rename_folder went ahead: %s", text)
			}
			for _, rel := range tc.keep {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Errorf("%s is gone", rel)
				}
			}
		})
	}
}

// A folder that holds nothing hidden still goes, and one in the same system folder under
// another name.
func TestFoldersHoldingNothingHiddenCanGo(t *testing.T) {
	root := seedDenyFolder(t, `{"primaryNotesLocation":"root"}`, []string{"Foo/x.md", "archive/Foo/x.md", "archive/Other/y.md", "Keep/z.md"})
	cs := serveScoped(t, backend.Target{Kind: backend.KindLocal, Root: root}, &Scope{Access: AccessWrite, Deny: []string{"Foo/x.md"}})
	mustCall(t, cs, "delete_folder", map[string]any{"folder": "archive", "subpath": "Other", "confirm": true})
	mustCall(t, cs, "rename_folder", map[string]any{"folder": "inbox", "oldSubpath": "Keep", "newSubpath": "Kept"})
	if _, err := os.Stat(filepath.Join(root, "archive/Other/y.md")); err == nil {
		t.Error("archive/Other stayed")
	}
	if _, err := os.Stat(filepath.Join(root, "Kept/z.md")); err != nil {
		t.Error("Keep was not renamed")
	}
}

// An entry written with the accent finds that folder, even with one beside it without.
func TestAllowEntryFindsItsFolderBesideAnother(t *testing.T) {
	const decomposed, composed = "Cafe\u0301", "Caf\u00e9"
	root := seedDenyFolder(t, `{"primaryNotesLocation":"root"}`, []string{"Cafe/plain.md", composed + "/menu.md"})
	cs := serveScoped(t, backend.Target{Kind: backend.KindLocal, Root: root}, &Scope{Access: AccessRead, Allow: []string{decomposed}})
	mustCall(t, cs, "read_note", map[string]any{"path": composed + "/menu.md"})
	mustFail(t, cs, "read_note", map[string]any{"path": "Cafe/plain.md"})
}

func TestFoldersHoldingHiddenNotesStayOnALocalVault(t *testing.T) {
	testDenyFolder(t, func(t *testing.T, root string, scope *Scope) *sdk.ClientSession {
		return serveScoped(t, backend.Target{Kind: backend.KindLocal, Root: root}, scope)
	})
}

func TestFoldersHoldingHiddenNotesStayOnAServer(t *testing.T) {
	scopeServerBinary(t)
	testDenyFolder(t, runScopedOnServer)
}

// An allow entry written with a decomposed accent finds the composed folder beside a
// plain one in the attachments folder, which the note listings do not show, as it does
// among folders they do.
func TestDecomposedAllowEntryFindsAnAttachmentFolderBesideAPlainOne(t *testing.T) {
	const decomposed, composed = "Café", "Café"
	root := seedFiles(t, map[string]string{"assets/Cafe/plain.png": "png", "assets/" + composed + "/menu.png": "png", "assets/" + composed + "/card.png": "png", "n.md": "x"})
	cs := runScoped(t, root, testScope(t, "write", []string{"assets/" + decomposed}, nil))
	got := mustCall(t, cs, "list_assets", map[string]any{})
	if !strings.Contains(got, "menu.png") || !strings.Contains(got, "card.png") {
		t.Errorf("the entry shares nothing: %s", got)
	}
	if strings.Contains(got, "plain.png") {
		t.Errorf("the entry reached the folder without the accent: %s", got)
	}
}

// A destination finds the same folder for the same spelling.
func TestDecomposedDestinationFindsTheComposedFolderBesideAPlainOne(t *testing.T) {
	const decomposed, composed = "Café", "Café"
	root := seedDenyFolder(t, `{"primaryNotesLocation":"root"}`, []string{"Cafe/plain.md", composed + "/menu.md"})
	cs := runScoped(t, root, testScope(t, "write", []string{composed}, nil))
	got := mustCall(t, cs, "create_note", map[string]any{"folder": "inbox", "subpath": decomposed, "title": "new"})
	if !strings.Contains(got, composed+"/new.md") {
		t.Errorf("the note went elsewhere: %s", got)
	}
}
