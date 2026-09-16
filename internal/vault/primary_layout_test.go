package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrimaryLayoutMatchesDesktopSettingsPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		want           PrimaryNotesLocation
		rootDirectory  string
	}{
		{"explicit inbox with remapped folders", `{"primaryNotesLocation":"inbox","systemFolderPaths":{"quick":"Journal/Scratch","trash":"Deleted","archive":"History"}}`, PrimaryNotesInbox, "archive"},
		{"explicit inbox with loose root content", `{"primaryNotesLocation":"inbox"}`, PrimaryNotesInbox, "Projects"},
		{"explicit root with only inbox content", `{"primaryNotesLocation":"root"}`, PrimaryNotesRoot, ""},
		{"inferred inbox with remapped and leftover system folders", `{"systemFolderPaths":{"trash":"Deleted","archive":"History"}}`, PrimaryNotesInbox, "archive"},
		{"inferred inbox with differently cased custom system folder", `{"systemFolderPaths":{"archive":"History"}}`, PrimaryNotesInbox, "HISTORY"},
		{"inferred root with loose root directory", `{}`, PrimaryNotesRoot, "Projects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), tc.settings)
			writeFile(t, filepath.Join(root, "inbox", "Existing.md"), "# Existing\n")
			if tc.rootDirectory != "" {
				if err := os.MkdirAll(filepath.Join(root, tc.rootDirectory), 0755); err != nil {
					t.Fatal(err)
				}
			}
			v, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := v.PrimaryNotesLocation(); got != tc.want {
				t.Fatalf("primary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPartialFolderRemapsKeepInboxWritesAndRestoresInInbox(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), `{"primaryNotesLocation":"inbox","systemFolderPaths":{"quick":"Scratch","trash":"Deleted","archive":"History"}}`)
	writeFile(t, filepath.Join(root, "inbox", "Existing.md"), "# Existing\n")
	for _, dir := range []string{"archive", "trash", "quick", "Scratch", "History", "Deleted"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := v.CreateNote(FolderInbox, "Captured", "", nil)
	if err != nil || meta.Path != "inbox/Captured.md" {
		t.Fatalf("create: %+v, %v", meta, err)
	}
	trashed, err := v.MoveToTrash(meta.Path)
	if err != nil || trashed.Path != "Deleted/Captured.md" {
		t.Fatalf("trash: %+v, %v", trashed, err)
	}
	restored, err := v.RestoreFromTrash(trashed.Path)
	if err != nil || restored.Path != "inbox/Captured.md" {
		t.Fatalf("restore: %+v, %v", restored, err)
	}
	if dirs, err := v.ListFolders(); err != nil || len(dirs) != 0 {
		t.Fatalf("system dirs were listed as inbox children: %+v, %v", dirs, err)
	}
	if got := v.RelDirFor(FolderInbox, "Migration.base"); got != "inbox/Migration.base" {
		t.Fatalf("database directory = %q", got)
	}
}
