package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The probe cases behind ZenNotes/tui#21's Phase 0: every tool that creates,
// moves, renames or deletes a note or folder stays inside the note folders.
// Mirrors apps/desktop/src/mcp/vault-ops-path-safety.test.ts in
// ZenNotes/zennotes.

// newRootModeVault is a root-mode vault (the inbox is the vault root, so the
// internal dir, the attachments dir and the other buckets are all one
// subpath away from it) with a folder icon, color and Tasks entry to follow
// renames.
func newRootModeVault(t *testing.T) *Vault {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), `{
  "primaryNotesLocation": "root",
  "folderIcons": {"inbox:Projects": "star", "inbox:Projects/Old": "box", "inbox:ProjectsX": "dot", "quick:Projects": "bolt"},
  "folderColors": {"inbox:Projects": "red"},
  "tasks": {"excludedFolders": ["Projects/Old"]},
  "favorites": ["Projects/Plan.md"]
}`)
	writeFile(t, filepath.Join(root, ".zennotes", "templates", "Daily.md"), "- [ ] stand up\n")
	writeFile(t, filepath.Join(root, "Projects", "Plan.md"), "# Plan\n")
	writeFile(t, filepath.Join(root, "Projects", "Old", "Before.md"), "# Before\n")
	writeFile(t, filepath.Join(root, "Projects", "notes.txt"), "plain text\n")
	writeFile(t, filepath.Join(root, "Journal", "Diary.md"), "# Diary\n")
	writeFile(t, filepath.Join(root, "attachements", "img.png"), "PNG")
	writeFile(t, filepath.Join(root, "archive", "Old.md"), "# Old\n")
	writeFile(t, filepath.Join(root, "quick", "Q.md"), "# Q\n")
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if !v.PrimaryAtRoot() {
		t.Fatal("expected root mode")
	}
	return v
}

func mustExist(t *testing.T, v *Vault, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(v.Root(), filepath.FromSlash(rel))); err != nil {
		t.Fatalf("%s must survive: %v", rel, err)
	}
}

func mustNotExist(t *testing.T, v *Vault, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(v.Root(), filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("%s must not exist: %v", rel, err)
	}
}

func wantErr(t *testing.T, what string, err error, text string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s must be refused", what)
	}
	if !strings.Contains(err.Error(), text) {
		t.Fatalf("%s: error %q, want it to say %q", what, err, text)
	}
}

// G1 and the rest of the note tools: the source must be a note. Unchecked,
// move_note carried vault.json into a note folder for read_note to read, and
// archive_note, move_to_trash and delete_note took it away.
func TestNoteToolsRefuseFilesOutsideTheNoteFolders(t *testing.T) {
	const internal = ".zennotes/vault.json"
	const refused = "Note not in a known folder: " + internal
	v := newRootModeVault(t)
	_, err := v.MoveNote(internal, FolderInbox, "Projects")
	wantErr(t, "move_note", err, refused)
	_, err = v.ArchiveNote(internal)
	wantErr(t, "archive_note", err, refused)
	_, err = v.MoveToTrash(internal)
	wantErr(t, "move_to_trash", err, refused)
	_, err = v.RestoreFromTrash(internal)
	wantErr(t, "restore_from_trash", err, refused)
	_, err = v.RenameNote(internal, "settings")
	wantErr(t, "rename_note", err, refused)
	_, err = v.DuplicateNote(internal)
	wantErr(t, "duplicate_note", err, refused)
	_, err = v.AppendToNote(internal, "x")
	wantErr(t, "append_to_note", err, refused)
	_, err = v.ReadNote(internal)
	wantErr(t, "read_note", err, refused)
	wantErr(t, "delete_note", v.DeleteNote(internal), refused)
	mustExist(t, v, internal)
	mustNotExist(t, v, "Projects/vault.json")
	mustNotExist(t, v, "archive/vault.md")
	mustNotExist(t, v, "trash/vault.md")

	const template = ".zennotes/templates/Daily.md"
	_, err = v.ToggleTask(template+"#0", DialectApp)
	wantErr(t, "toggle_task in a template", err, "Note not in a known folder: "+template)
	if body, _ := os.ReadFile(filepath.Join(v.Root(), filepath.FromSlash(template))); string(body) != "- [ ] stand up\n" {
		t.Fatalf("template changed: %q", body)
	}

	// A dot directory below the top is no note folder either: the listings
	// never descend into one.
	_, err = v.WriteNote("Projects/.hidden/x.md", "x")
	wantErr(t, "write_note into a dot directory", err, "Note not in a known folder: Projects/.hidden/x.md")
	mustNotExist(t, v, "Projects/.hidden")
}

// G2 and friends: what a create or a move writes lands in a note folder of
// the bucket it names.
func TestCreatesAndMovesLandOnlyInNoteFolders(t *testing.T) {
	v := newRootModeVault(t)
	for _, tc := range []struct{ sub, want string }{
		{".zennotes", "Not a note folder: .zennotes"},
		{"attachements", "Not a note folder: attachements"},
		{"archive/Projects", "Not a note folder: archive/Projects"},
		{"trash", "Not a note folder: trash"},
		{"Projects/.hidden", "Not a note folder: Projects/.hidden"},
		{"Projects/../.zennotes", "Not a note folder: Projects/../.zennotes"},
	} {
		_, err := v.CreateNote(FolderInbox, "planted", tc.sub, nil)
		wantErr(t, "create_note in "+tc.sub, err, tc.want)
		wantErr(t, "create_folder "+tc.sub, v.CreateFolder(FolderInbox, tc.sub), tc.want)
		_, err = v.MoveNote("Projects/Plan.md", FolderInbox, tc.sub)
		wantErr(t, "move_note into "+tc.sub, err, tc.want)
	}
	mustNotExist(t, v, ".zennotes/planted.md")
	mustNotExist(t, v, "attachements/planted.md")
	mustNotExist(t, v, "trash/planted.md")
	mustExist(t, v, "Projects/Plan.md")
	wantErr(t, "create_folder x/..", v.CreateFolder(FolderInbox, "x/.."), "Folder name is required")

	// Ordinary subfolders of either bucket still take notes.
	created, err := v.CreateNote(FolderInbox, "Fine", "Projects/Sub", nil)
	if err != nil || created.Path != "Projects/Sub/Fine.md" {
		t.Fatalf("create in Projects/Sub = %+v, %v", created, err)
	}
	created, err = v.CreateNote(FolderQuick, "Fine", "Work", nil)
	if err != nil || created.Path != "quick/Work/Fine.md" {
		t.Fatalf("create in quick/Work = %+v, %v", created, err)
	}
}

// G3, G4 and the bucket root: folder operations act on a note folder of the
// bucket, never on the internal dir, the attachments dir, another bucket or
// the bucket root itself. `x/..` cleaned to the root, and in root mode
// delete_folder then removed the whole vault.
func TestFolderOpsRefuseSystemFoldersAndTheBucketRoot(t *testing.T) {
	v := newRootModeVault(t)
	wantErr(t, "delete attachements", v.DeleteFolder(FolderInbox, "attachements"), "Not a note folder: attachements")
	wantErr(t, "delete archive", v.DeleteFolder(FolderInbox, "archive"), "Not a note folder: archive")
	wantErr(t, "delete .zennotes", v.DeleteFolder(FolderInbox, ".zennotes"), "Not a note folder: .zennotes")
	wantErr(t, "delete x/..", v.DeleteFolder(FolderInbox, "x/.."), "Cannot delete the top-level folder")
	wantErr(t, "delete archive's x/..", v.DeleteFolder(FolderArchive, "x/.."), "Cannot delete the top-level folder")
	_, err := v.RenameFolder(FolderInbox, ".zennotes", "Projects/internals")
	wantErr(t, "rename .zennotes", err, "Not a note folder: .zennotes")
	_, err = v.RenameFolder(FolderInbox, "Projects", ".zennotes/x")
	wantErr(t, "rename into .zennotes", err, "Not a note folder: .zennotes/x")
	_, err = v.RenameFolder(FolderInbox, "Projects", "x/..")
	wantErr(t, "rename onto the bucket root", err, "Both old and new folder paths are required")
	mustExist(t, v, ".zennotes/vault.json")
	mustExist(t, v, "attachements/img.png")
	mustExist(t, v, "archive/Old.md")
	mustExist(t, v, "Projects/Plan.md")
	mustNotExist(t, v, "Projects/internals")

	// A case variant of the attachments dir is that same directory on macOS
	// and Windows, and is refused there; elsewhere it is a different folder.
	if _, err := os.Stat(filepath.Join(v.Root(), "Attachements")); err == nil {
		wantErr(t, "delete Attachements", v.DeleteFolder(FolderInbox, "Attachements"), "Not a note folder: Attachements")
		mustExist(t, v, "attachements/img.png")
	}

	// Ordinary folders still work.
	if err := v.DeleteFolder(FolderInbox, "Journal"); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, v, "Journal")
}

// Classification follows the vault's systemFolderPaths, not literal names:
// once archive lives in `99 - Old`, an `archive/` folder at the root is the
// user's own, and `99 - Old` is the bucket.
func TestFolderGuardsFollowRemappedSystemFolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".zennotes", "vault.json"), `{"primaryNotesLocation":"root","systemFolderPaths":{"archive":"99 - Old"}}`)
	writeFile(t, filepath.Join(root, "99 - Old", "Gone.md"), "# Gone\n")
	writeFile(t, filepath.Join(root, "archive", "Mine.md"), "# Mine\n")
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	wantErr(t, "delete the remapped archive", v.DeleteFolder(FolderInbox, "99 - Old"), "Not a note folder: 99 - Old")
	mustExist(t, v, "99 - Old/Gone.md")
	created, err := v.CreateNote(FolderInbox, "Note", "archive", nil)
	if err != nil || created.Path != "archive/Note.md" || created.Folder != FolderInbox {
		t.Fatalf("archive/ is a user folder once remapped: %+v, %v", created, err)
	}
	if err := v.DeleteFolder(FolderInbox, "archive"); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, v, "archive")
}

// In inbox mode the same rules hold one level down.
func TestFolderGuardsInInboxMode(t *testing.T) {
	v := newTestVault(t)
	writeFile(t, filepath.Join(v.Root(), "inbox", "Work", "Plan.md"), "# Plan\n")
	_, err := v.CreateNote(FolderInbox, "planted", ".zennotes", nil)
	wantErr(t, "create in inbox/.zennotes", err, "Not a note folder: .zennotes")
	_, err = v.CreateNote(FolderQuick, "planted", "../.zennotes", nil)
	wantErr(t, "create in quick/.zennotes", err, "Not a note folder: ../.zennotes")
	wantErr(t, "delete inbox via ..", v.DeleteFolder(FolderInbox, ".."), "Cannot delete the top-level folder")
	mustExist(t, v, "inbox/Work/Plan.md")
	moved, err := v.MoveNote("inbox/Work/Plan.md", FolderArchive, "Work")
	if err != nil || moved.Path != "archive/Work/Plan.md" {
		t.Fatalf("move = %+v, %v", moved, err)
	}
}

// read_note reads notes, which are Markdown files; a root-mode vault can hold
// anything else beside them.
func TestReadNoteReadsOnlyMarkdown(t *testing.T) {
	v := newRootModeVault(t)
	_, err := v.ReadNote("Projects/notes.txt")
	wantErr(t, "read_note of a text file", err, "Not a Markdown note: Projects/notes.txt")
	if note, err := v.ReadNote("Projects/Plan.md"); err != nil || note.Body != "# Plan\n" {
		t.Fatalf("read Plan = %+v, %v", note, err)
	}
}

// A folder's icon, color and Tasks entries follow it through a rename, as
// desktop main and the server rewrite them.
func TestRenameFolderRewritesFolderIconsAndColors(t *testing.T) {
	v := newRootModeVault(t)
	if got, err := v.RenameFolder(FolderInbox, "Projects", "Work"); err != nil || got != "Work" {
		t.Fatalf("rename = %q, %v", got, err)
	}
	s := v.Settings()
	icons := mapOf(s.Raw["folderIcons"])
	wantIcons := map[string]any{"inbox:Work": "star", "inbox:Work/Old": "box", "inbox:ProjectsX": "dot", "quick:Projects": "bolt"}
	if !reflect.DeepEqual(icons, wantIcons) {
		t.Errorf("folderIcons = %v, want %v", icons, wantIcons)
	}
	if colors := mapOf(s.Raw["folderColors"]); !reflect.DeepEqual(colors, map[string]any{"inbox:Work": "red"}) {
		t.Errorf("folderColors = %v", colors)
	}
	if !reflect.DeepEqual(s.TasksExcludedFolders, []string{"Work/Old"}) {
		t.Errorf("tasks excluded = %v", s.TasksExcludedFolders)
	}
	if !reflect.DeepEqual(s.Favorites, []string{"Projects/Plan.md"}) {
		t.Errorf("the rewrite lost other vault.json keys: favorites = %v", s.Favorites)
	}
}
