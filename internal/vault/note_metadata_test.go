package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const originalCreatedAt int64 = 1600000000123

func seedNoteMetadata(t *testing.T, v *Vault, rel string) {
	t.Helper()
	writeFile(t, filepath.Join(v.root, ".zennotes", "note-metadata", rel+".metadata.json"), `{"version":1,"createdAt":1600000000123}`)
}

func TestNoteCreationMetadataSurvivesWritesAndLifecycle(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Work/Original.md"
	writeFile(t, filepath.Join(v.root, rel), "# Original\n")
	seedNoteMetadata(t, v, rel)
	assertDate := func(rel string) {
		t.Helper()
		note, err := v.ReadNote(rel)
		if err != nil {
			t.Fatal(err)
		}
		if note.CreatedAt != originalCreatedAt {
			t.Fatalf("%s createdAt = %d, want %d", rel, note.CreatedAt, originalCreatedAt)
		}
	}
	assertDate(rel)
	body := "---\r\ntitle: untouched\r\n---\r\nUnicode café 漢字  \r\n"
	if _, err := v.WriteNote(rel, body); err != nil {
		t.Fatal(err)
	}
	assertDate(rel)
	if raw, err := os.ReadFile(filepath.Join(v.root, rel)); err != nil || string(raw) != body {
		t.Fatalf("Markdown changed: %q, %v", raw, err)
	}
	meta, err := v.RenameNote(rel, "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	rel = meta.Path
	assertDate(rel)
	if _, err := v.RenameFolder(FolderInbox, "Work", "Moved"); err != nil {
		t.Fatal(err)
	}
	rel = "inbox/Moved/Renamed.md"
	assertDate(rel)
	meta, err = v.MoveToTrash(rel)
	if err != nil {
		t.Fatal(err)
	}
	rel = meta.Path
	assertDate(rel)
	meta, err = v.RestoreFromTrash(rel)
	if err != nil {
		t.Fatal(err)
	}
	rel = meta.Path
	assertDate(rel)
	copy, err := v.DuplicateNote(rel)
	if err != nil {
		t.Fatal(err)
	}
	if copy.CreatedAt == originalCreatedAt {
		t.Fatal("duplicate inherited original creation date")
	}
	if err := v.DeleteNote(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.root, ".zennotes", "note-metadata", rel+".metadata.json")); !os.IsNotExist(err) {
		t.Fatalf("metadata left after deletion: %v", err)
	}
	meta, err = v.WriteNote(rel, "new note\n")
	if err != nil {
		t.Fatal(err)
	}
	if meta.CreatedAt == originalCreatedAt {
		t.Fatal("recreated note inherited deleted date")
	}
}

// On NTFS a file made under a name deleted in the last 15 seconds gets the
// deleted file's birthtime back (file system tunneling), so a new note's date
// cannot come from the file. Each way of making one records it in the sidecar
// at once, and the first save keeps that record.
func TestNewNoteRecordsItsOwnCreationDate(t *testing.T) {
	create := map[string]func(v *Vault) (NoteMeta, error){
		"CreateNote": func(v *Vault) (NoteMeta, error) { return v.CreateNote(FolderInbox, "Original", "", nil) },
		"WriteNote":  func(v *Vault) (NoteMeta, error) { return v.WriteNote("inbox/Original.md", "# Original\n") },
	}
	for name, make := range create {
		t.Run(name, func(t *testing.T) {
			v := newTestVault(t)
			rel := "inbox/Original.md"
			writeFile(t, filepath.Join(v.root, rel), "# Original\n")
			seedNoteMetadata(t, v, rel)
			if err := v.DeleteNote(rel); err != nil {
				t.Fatal(err)
			}
			before := time.Now().UnixMilli()
			meta, err := make(v)
			after := time.Now().UnixMilli()
			if err != nil {
				t.Fatal(err)
			}
			if meta.Path != rel {
				t.Fatalf("path = %s, want %s", meta.Path, rel)
			}
			sidecar := filepath.Join(v.root, ".zennotes", "note-metadata", rel+".metadata.json")
			recorded, err := readCreationMetadata(sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if recorded < before || recorded > after {
				t.Fatalf("recorded createdAt = %d, want between %d and %d", recorded, before, after)
			}
			if meta.CreatedAt != recorded {
				t.Fatalf("createdAt = %d, want the recorded %d", meta.CreatedAt, recorded)
			}
			saved, err := v.WriteNote(rel, "# Original\n\nEdited.\n")
			if err != nil {
				t.Fatal(err)
			}
			if saved.CreatedAt != recorded {
				t.Fatalf("after a save createdAt = %d, want %d", saved.CreatedAt, recorded)
			}
		})
	}
}

func TestNoteMetadataCorruptionDoesNotMutateNote(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Original.md"
	writeFile(t, filepath.Join(v.root, rel), "original\n")
	writeFile(t, filepath.Join(v.root, ".zennotes", "note-metadata", rel+".metadata.json"), "{broken")
	if _, err := v.WriteNote(rel, "replacement\n"); err == nil {
		t.Fatal("save accepted corrupt creation metadata")
	}
	if body, _ := os.ReadFile(filepath.Join(v.root, rel)); string(body) != "original\n" {
		t.Fatalf("failed save changed note: %q", body)
	}
}

func TestWriteNotePreservesCreationTimeWithoutExistingMetadata(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Original.md"
	writeFile(t, filepath.Join(v.root, rel), "original\n")
	before, err := v.ReadNote(rel)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := v.WriteNote(rel, "changed\n"); err != nil {
			t.Fatal(err)
		}
		after, err := v.ReadNote(rel)
		if err != nil {
			t.Fatal(err)
		}
		if after.CreatedAt != before.CreatedAt {
			t.Fatalf("createdAt changed: %d -> %d", before.CreatedAt, after.CreatedAt)
		}
	}
}
