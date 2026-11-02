package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/backend"
)

// The ZenNotes/tui#21 probe cases, through the real tools on a root-mode
// vault, where the inbox is the vault root and the internal dir, the
// attachments dir and the other buckets are all one subpath away. Mirrors
// the path-safety cases in apps/desktop/src/mcp/server.test.ts in
// ZenNotes/zennotes.
func TestMCPToolsStayInsideTheNoteFolders(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"Projects/Plan.md":     "# Plan\n",
		"Projects/notes.txt":   "plain text\n",
		"attachements/img.png": "PNG",
		"archive/Old.md":       "# Old\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("# OUTSIDE SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Projects", "link")); err != nil {
		t.Fatal(err)
	}
	cs := runServer(t, func() (backend.Target, error) {
		return backend.Target{Kind: backend.KindLocal, Root: root}, nil
	})

	refused := func(name string, args map[string]any, want string) {
		t.Helper()
		text, isErr := callTool(t, cs, name, args)
		if !isErr || !strings.Contains(text, want) {
			t.Fatalf("%s %v = %q (error %v), want an error saying %q", name, args, text, isErr, want)
		}
	}
	refused("move_note", map[string]any{"path": ".zennotes/vault.json", "targetFolder": "inbox", "targetSubpath": "Projects"}, "Note not in a known folder")
	refused("read_note", map[string]any{"path": "Projects/vault.json"}, "Not a Markdown note: Projects/vault.json")
	refused("create_note", map[string]any{"folder": "inbox", "subpath": ".zennotes", "title": "planted"}, "Not a note folder: .zennotes")
	refused("delete_folder", map[string]any{"folder": "inbox", "subpath": "attachements", "confirm": true}, "Not a note folder: attachements")
	refused("delete_folder", map[string]any{"folder": "inbox", "subpath": "archive", "confirm": true}, "Not a note folder: archive")
	refused("delete_folder", map[string]any{"folder": "inbox", "subpath": "x/..", "confirm": true}, "Cannot delete the top-level folder")
	refused("rename_folder", map[string]any{"folder": "inbox", "oldSubpath": ".zennotes", "newSubpath": "Projects/internals"}, "Not a note folder: .zennotes")
	refused("delete_note", map[string]any{"path": ".zennotes/vault.json", "confirm": true}, "Note not in a known folder")
	refused("archive_note", map[string]any{"path": ".zennotes/vault.json"}, "Note not in a known folder")
	refused("read_note", map[string]any{"path": "Projects/link/secret.md"}, "Path escapes vault")
	refused("read_note", map[string]any{"path": "Projects/notes.txt"}, "Not a Markdown note: Projects/notes.txt")

	for _, rel := range []string{".zennotes/vault.json", "Projects/Plan.md", "attachements/img.png", "archive/Old.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s must survive: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".zennotes", "planted.md")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be planted in .zennotes: %v", err)
	}
	if text, isErr := callTool(t, cs, "read_note", map[string]any{"path": "Projects/Plan.md"}); isErr || !strings.Contains(text, "# Plan") {
		t.Fatalf("read_note of a real note = %q (error %v)", text, isErr)
	}
}
