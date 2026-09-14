package vault_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

func TestDeletedAssetListingSkipsSymlinkedMetadata(t *testing.T) {
	v, root := openAssetParityVault(t)
	writeAssetParityFile(t, root, "assets/report.pdf", "pdf")
	d, err := v.DeleteAsset("assets/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(root, ".zennotes", "deleted-assets", d.UndoToken, ".zn-deleted.json")
	outside := filepath.Join(t.TempDir(), "outside.json")
	raw, err := os.ReadFile(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(meta); err != nil {
		t.Fatal(err)
	}
	assetParitySymlink(t, outside, meta)
	list, err := v.ListDeletedAssets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("read metadata through an external symlink: %v", list)
	}
}

func openAssetParityVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	root := t.TempDir()
	v, err := vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v, root
}

func writeAssetParityFile(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertAssetParityBytes(t *testing.T, path, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Errorf("%s contains %q, want %q", path, body, want)
	}
}

func assetParitySymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestAssetImportUsesUniqueVaultRelativeMarkdown(t *testing.T) {
	v, root := openAssetParityVault(t)
	for _, tc := range []struct {
		note, filename, body, name, kind, markdown string
	}{
		{"Note.md", "Photo.png", "first image", "Photo.png", "image", "![[assets/Photo.png]]"},
		{"inbox/deep/Note.md", "Photo.png", "second image", "Photo 2.png", "image", "![[assets/Photo 2.png]]"},
		{"inbox/Note.md", "report.pdf", "document", "report.pdf", "pdf", "[report.pdf](<assets/report.pdf>)"},
	} {
		got, err := v.ImportAsset(tc.note, tc.filename, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != tc.name || got.Path != "assets/"+tc.name || got.Kind != tc.kind || got.Markdown != tc.markdown {
			t.Errorf("ImportAsset(%q) = %+v, want name=%q kind=%q markdown=%q", tc.filename, got, tc.name, tc.kind, tc.markdown)
		}
		assertAssetParityBytes(t, filepath.Join(root, "assets", tc.name), tc.body)
	}
	assertAssetParityBytes(t, filepath.Join(root, "assets", "Photo.png"), "first image")
}

func TestAssetRenamePreservesBytesAndRejectsUnsafeNames(t *testing.T) {
	t.Run("renames in place", func(t *testing.T) {
		v, root := openAssetParityVault(t)
		writeAssetParityFile(t, root, "assets/pic.png", "image bytes")
		got, err := v.RenameAsset("assets/pic.png", "renamed.png")
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != "assets/renamed.png" || got.Name != "renamed.png" || got.Size != int64(len("image bytes")) {
			t.Errorf("RenameAsset() = %+v", got)
		}
		assertAssetParityBytes(t, filepath.Join(root, "assets", "renamed.png"), "image bytes")
		if _, err := os.Stat(filepath.Join(root, "assets", "pic.png")); !os.IsNotExist(err) {
			t.Errorf("old path still exists: %v", err)
		}
	})
	for _, name := range []string{"occupied.png", "note.md", "note.MD", "../escaped.png", "sub/file.png", `sub\file.png`} {
		t.Run(name, func(t *testing.T) {
			v, root := openAssetParityVault(t)
			writeAssetParityFile(t, root, "assets/pic.png", "source")
			writeAssetParityFile(t, root, "assets/occupied.png", "destination")
			if _, err := v.RenameAsset("assets/pic.png", name); err == nil {
				t.Errorf("RenameAsset accepted %q", name)
			}
			assertAssetParityBytes(t, filepath.Join(root, "assets", "pic.png"), "source")
			assertAssetParityBytes(t, filepath.Join(root, "assets", "occupied.png"), "destination")
		})
	}
}

func TestAssetDeleteWritesDesktopTrashAndRestoresWithoutOverwrite(t *testing.T) {
	v, root := openAssetParityVault(t)
	writeAssetParityFile(t, root, "assets/report.pdf", "original document")
	deleted, err := v.DeleteAsset("assets/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Path != "assets/report.pdf" || deleted.Name != "report.pdf" {
		t.Fatalf("DeleteAsset() = %+v", deleted)
	}
	if !regexp.MustCompile(`^[0-9a-fA-F-]{36}$`).MatchString(deleted.UndoToken) {
		t.Fatalf("undo token %q does not match the desktop format", deleted.UndoToken)
	}
	if _, err := time.Parse(time.RFC3339Nano, deleted.DeletedAt); err != nil {
		t.Errorf("deletedAt %q is not an ISO timestamp: %v", deleted.DeletedAt, err)
	}
	store := filepath.Join(root, ".zennotes", "deleted-assets", deleted.UndoToken)
	assertAssetParityBytes(t, filepath.Join(store, "report.pdf"), "original document")
	if _, err := os.Stat(filepath.Join(root, "assets", "report.pdf")); !os.IsNotExist(err) {
		t.Errorf("deleted source still exists: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(store, ".zn-deleted.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["path"] != deleted.Path || metadata["name"] != deleted.Name || metadata["deletedAt"] != deleted.DeletedAt {
		t.Errorf("desktop restore metadata = %v, want %+v", metadata, deleted)
	}

	// Reopen to prove the disk metadata, rather than an in-memory undo stack,
	// is enough to discover and restore the deleted asset.
	v, err = vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := v.ListDeletedAssets()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDeletedAssets() = %+v, %v", listed, err)
	}
	if listed[0] != deleted {
		t.Errorf("listed entry = %+v, want %+v", listed[0], deleted)
	}
	writeAssetParityFile(t, root, "assets/report.pdf", "new document")
	restored, err := v.RestoreDeletedAsset(listed[0])
	if err != nil {
		t.Fatal(err)
	}
	if restored.Path != "assets/report 2.pdf" {
		t.Errorf("restored path = %q, want assets/report 2.pdf", restored.Path)
	}
	assertAssetParityBytes(t, filepath.Join(root, "assets", "report.pdf"), "new document")
	assertAssetParityBytes(t, filepath.Join(root, "assets", "report 2.pdf"), "original document")
	if listed, err := v.ListDeletedAssets(); err != nil || len(listed) != 0 {
		t.Errorf("trash after restore = %+v, %v", listed, err)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("consumed trash entry remains: %v", err)
	}
}

func TestAssetRestoreReadsDesktopMetadataInsteadOfRequestFields(t *testing.T) {
	v, root := openAssetParityVault(t)
	const token = "12345678-1234-4234-8234-123456789abc"
	store := ".zennotes/deleted-assets/" + token
	writeAssetParityFile(t, root, store+"/.zn-deleted.json", `{"path":"media/photo.png","name":"photo.png","deletedAt":"2026-09-14T12:00:00.000Z"}`)
	writeAssetParityFile(t, root, store+"/photo.png", "desktop image")
	// Old or incomplete entries are not restorable from the desktop Trash view.
	writeAssetParityFile(t, root, ".zennotes/deleted-assets/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/.zn-deleted.json", `{"path":"assets/missing.png","name":"missing.png"}`)
	listed, err := v.ListDeletedAssets()
	if err != nil || len(listed) != 1 {
		t.Fatalf("desktop entries = %+v, %v", listed, err)
	}
	if listed[0].UndoToken != token || listed[0].Path != "media/photo.png" || listed[0].Name != "photo.png" {
		t.Fatalf("listed desktop entry = %+v", listed[0])
	}
	request := listed[0]
	request.Path = "assets/wrong.json"
	request.Name = ".zn-deleted.json"
	got, err := v.RestoreDeletedAsset(request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "media/photo.png" {
		t.Errorf("restore trusted request fields: %+v", got)
	}
	assertAssetParityBytes(t, filepath.Join(root, "media", "photo.png"), "desktop image")
}

func TestAssetActionsRejectNotesAndInternalFiles(t *testing.T) {
	for _, rel := range []string{"inbox/Note.md", ".zennotes/vault.json"} {
		t.Run(rel, func(t *testing.T) {
			v, root := openAssetParityVault(t)
			writeAssetParityFile(t, root, rel, "keep")
			if _, err := v.RenameAsset(rel, "changed.png"); err == nil {
				t.Error("RenameAsset accepted a note or internal file")
			}
			if _, err := v.DeleteAsset(rel); err == nil {
				t.Error("DeleteAsset accepted a note or internal file")
			}
			assertAssetParityBytes(t, filepath.Join(root, filepath.FromSlash(rel)), "keep")
		})
	}
}

func TestAssetImportRejectsSymlinkOutsideVault(t *testing.T) {
	v, root := openAssetParityVault(t)
	outside := t.TempDir()
	assetParitySymlink(t, outside, filepath.Join(root, "assets"))
	if _, err := v.ImportAsset("Note.md", "photo.png", strings.NewReader("image")); err == nil {
		t.Error("ImportAsset followed assets/ outside the vault")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Errorf("outside directory was changed: %v, %v", entries, err)
	}
}

func TestAssetDeleteRejectsSymlinkOutsideVault(t *testing.T) {
	t.Run("source file", func(t *testing.T) {
		v, root := openAssetParityVault(t)
		outside := t.TempDir()
		writeAssetParityFile(t, outside, "photo.png", "outside image")
		link := filepath.Join(root, "assets", "photo.png")
		assetParitySymlink(t, filepath.Join(outside, "photo.png"), link)
		if _, err := v.DeleteAsset("assets/photo.png"); err == nil {
			t.Error("DeleteAsset accepted an outside symlink")
		}
		assertAssetParityBytes(t, filepath.Join(outside, "photo.png"), "outside image")
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("rejected source symlink was removed: %v", err)
		}
	})
	t.Run("trash directory", func(t *testing.T) {
		v, root := openAssetParityVault(t)
		outside := t.TempDir()
		writeAssetParityFile(t, root, "assets/photo.png", "vault image")
		assetParitySymlink(t, outside, filepath.Join(root, ".zennotes", "deleted-assets"))
		if _, err := v.DeleteAsset("assets/photo.png"); err == nil {
			t.Error("DeleteAsset followed the trash directory outside the vault")
		}
		assertAssetParityBytes(t, filepath.Join(root, "assets", "photo.png"), "vault image")
		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Errorf("outside directory was changed: %v, %v", entries, err)
		}
	})
}

func TestAssetRestoreRejectsSymlinkOutsideVault(t *testing.T) {
	const token = "12345678-1234-4234-8234-123456789abc"
	for _, scenario := range []string{"stored file", "stored directory", "destination"} {
		t.Run(scenario, func(t *testing.T) {
			v, root := openAssetParityVault(t)
			outside := t.TempDir()
			store := filepath.Join(root, ".zennotes", "deleted-assets", token)
			metadata := `{"path":"assets/photo.png","name":"photo.png","deletedAt":"2026-09-14T12:00:00.000Z"}`
			switch scenario {
			case "stored file":
				writeAssetParityFile(t, store, ".zn-deleted.json", metadata)
				writeAssetParityFile(t, outside, "photo.png", "saved image")
				assetParitySymlink(t, filepath.Join(outside, "photo.png"), filepath.Join(store, "photo.png"))
			case "stored directory":
				writeAssetParityFile(t, outside, ".zn-deleted.json", metadata)
				writeAssetParityFile(t, outside, "photo.png", "saved image")
				assetParitySymlink(t, outside, store)
			case "destination":
				writeAssetParityFile(t, store, ".zn-deleted.json", metadata)
				writeAssetParityFile(t, store, "photo.png", "saved image")
				assetParitySymlink(t, outside, filepath.Join(root, "assets"))
			}
			if _, err := v.RestoreDeletedAsset(vault.DeletedAsset{UndoToken: token}); err == nil {
				t.Errorf("RestoreDeletedAsset followed %s outside the vault", scenario)
			}
			assertAssetParityBytes(t, filepath.Join(store, "photo.png"), "saved image")
			assertAssetParityBytes(t, filepath.Join(store, ".zn-deleted.json"), metadata)
			if scenario == "destination" {
				entries, err := os.ReadDir(outside)
				if err != nil || len(entries) != 0 {
					t.Errorf("outside destination was changed: %v, %v", entries, err)
				}
			}
		})
	}
}
