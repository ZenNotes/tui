package mcp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAssetBase64(t *testing.T) {
	want := "hello?>"
	std := base64.StdEncoding.EncodeToString([]byte(want))
	for name, encoded := range map[string]string{
		"standard": std,
		"unpadded": strings.TrimRight(std, "="),
		"urlsafe":  base64.URLEncoding.EncodeToString([]byte(want)),
		"wrapped":  std[:4] + "\n" + std[4:] + "\n",
		"data url": "data:text/plain;base64," + std,
	} {
		got, err := decodeAssetBase64(encoded)
		if err != nil || string(got) != want {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	for name, encoded := range map[string]string{
		"not base64":   "!!!",
		"data url":     "data:text/plain,hello",
		"only padding": "====",
	} {
		if _, err := decodeAssetBase64(encoded); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if _, err := decodeAssetBase64(strings.Repeat("A", base64.StdEncoding.EncodedLen(maxUploadAssetBytes+3))); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Errorf("over the limit: %v", err)
	}
}

func TestCheckAssetName(t *testing.T) {
	for _, name := range []string{"plot.png", "a b.pdf", "no-extension", "data.csv"} {
		if err := checkAssetName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", " ", ".", "..", "a/b.png", `a\b.png`, ".hidden", "note.md", "NOTE.MD", "x.excalidraw", "Books.csv.base.json", "a\x00b.png"} {
		if err := checkAssetName(name); err == nil {
			t.Errorf("%q should be refused", name)
		}
	}
}

// A loose CSV with a schema file is a database: the pair moves together or not
// at all, and only the database code knows how.
func TestAssetToolsLeaveDatabasesAlone(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".zennotes/vault.json":      `{"primaryNotesLocation":"root"}`,
		"Lists/Books.csv":           "Title\nDune\n",
		"Lists/Books.csv.base.json": "{}",
		"Lists/plain.csv":           "a\n1\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs := runScoped(t, root, nil)
	for _, path := range []string{"Lists/Books.csv", "Lists/Books.csv.base.json"} {
		for tool, args := range map[string]map[string]any{
			"rename_asset":    {"path": path, "name": "x.csv"},
			"move_asset":      {"path": path, "targetDir": "Elsewhere"},
			"duplicate_asset": {"path": path},
			"delete_asset":    {"path": path},
		} {
			if got := mustFail(t, cs, tool, args); !strings.Contains(got, "belongs to a ZenNotes database") {
				t.Errorf("%s %s: %s", tool, path, got)
			}
		}
	}
	if text := mustCall(t, cs, "read_asset", map[string]any{"path": "Lists/Books.csv"}); !strings.Contains(text, "Dune") {
		t.Errorf("reading stays allowed: %s", text)
	}
	if text := mustCall(t, cs, "rename_asset", map[string]any{"path": "Lists/plain.csv", "name": "renamed.csv"}); !strings.Contains(text, "Lists/renamed.csv") {
		t.Errorf("a CSV with no schema is just a file: %s", text)
	}
}

// Every edit to a note's text is offered for review, the new ones too.
func TestNewEditToolsAreRecordedForReview(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "inbox", "N.md")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("# N\n\n## A\n\n- [ ] one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := runScoped(t, root, nil)
	mustCall(t, cs, "edit_section", map[string]any{"path": "inbox/N.md", "heading": "A", "mode": "append", "text": "- [ ] two"})
	mustCall(t, cs, "update_task", map[string]any{"id": "inbox/N.md#0", "due": "2026-02-02"})
	record, err := os.ReadFile(filepath.Join(root, ".zennotes", "agent-edits", "inbox", "N.md.json"))
	if err != nil {
		entries, _ := os.ReadDir(filepath.Join(root, ".zennotes", "agent-edits", "inbox"))
		t.Fatalf("%v, found %v", err, entries)
	}
	for _, tool := range []string{"edit_section", "update_task"} {
		if !strings.Contains(string(record), tool) {
			t.Errorf("the record does not name %s: %s", tool, record)
		}
	}
}

func seedFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The walks skip symbolic links, so a link looks like nothing at all, and a
// write through it would land in whatever hidden folder it points at.
func TestNoAssetIsWrittenThroughALink(t *testing.T) {
	root := seedFiles(t, map[string]string{
		".zennotes/vault.json": `{"primaryNotesLocation":"root"}`,
		"Journal/Diary.md":     "# Diary\n",
		"Shared/pic.png":       "png",
		".zennotes/deleted-assets/11111111-1111-4111-8111-111111111111/.zn-deleted.json": `{"path":"Shared/inner/old.png","name":"old.png","deletedAt":"2026-01-01T00:00:00.000Z"}`,
		".zennotes/deleted-assets/11111111-1111-4111-8111-111111111111/old.png":          "old",
	})
	if err := os.Symlink("../Journal", filepath.Join(root, "Shared", "inner")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Journal", filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	cs := runScoped(t, root, testScope(t, "write", []string{"Shared", "assets"}, nil))
	if got := mustFail(t, cs, "upload_asset", map[string]any{"filename": "x.png", "content_base64": "cG5n"}); !strings.Contains(got, "assets folder is not shared") {
		t.Errorf("upload through a link: %s", got)
	}
	if got := mustFail(t, cs, "move_asset", map[string]any{"path": "Shared/pic.png", "targetDir": "Shared/inner"}); !strings.Contains(got, "can put files only inside") {
		t.Errorf("move through a link: %s", got)
	}
	if got := mustFail(t, cs, "restore_asset", map[string]any{"undoToken": "11111111-1111-4111-8111-111111111111"}); !strings.Contains(got, "Deleted asset not found") {
		t.Errorf("restore through a link: %s", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, "Journal"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("something landed in the hidden folder: %v %v", entries, err)
	}
}

func TestTheServerRewritesEveryNoteSoAPathScopeCannotRenameThere(t *testing.T) {
	b := &assetOnlyBackend{Backend: mustLocalBackend(t)}
	sb := newScopedBackend(b, *testScope(t, "write", []string{"assets"}, nil))
	if _, err := sb.RenameAsset(t.Context(), "assets/pic.png", "b.png"); err != errAssetLinksEverywhere {
		t.Fatalf("rename: %v", err)
	}
	if _, err := sb.MoveAsset(t.Context(), "assets/pic.png", "assets/sub"); err != errAssetLinksEverywhere {
		t.Fatalf("move: %v", err)
	}
}
