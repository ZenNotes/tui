package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/remote/remotetest"
)

func TestAReadCarriesTheServersHashAndAWriteNamesIt(t *testing.T) {
	ctx := context.Background()
	server := remotetest.New(t, true, map[string]string{"inbox/A.md": "one\n"})
	client := NewClient(server.URL, "")
	note, err := client.ReadNote(ctx, "inbox/A.md")
	if err != nil {
		t.Fatal(err)
	}
	if note.Sha256 != remotetest.Sha256("one\n") {
		t.Fatalf("read hash: %q", note.Sha256)
	}
	meta, err := client.WriteNoteIfUnchanged(ctx, "inbox/A.md", "two\n", note.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Sha256 != remotetest.Sha256("two\n") {
		t.Fatalf("write hash: %q", meta.Sha256)
	}
	if w := server.Writes(); len(w) != 1 || w[0].Base != note.Sha256 {
		t.Fatalf("writes: %+v", w)
	}
}

func TestAWriteOnAStaleVersionIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	server := remotetest.New(t, true, map[string]string{"inbox/A.md": "one\n"})
	client := NewClient(server.URL, "")
	note, err := client.ReadNote(ctx, "inbox/A.md")
	if err != nil {
		t.Fatal(err)
	}
	server.Set("inbox/A.md", "theirs\n")
	_, err = client.WriteNoteIfUnchanged(ctx, "inbox/A.md", "mine\n", note.Sha256)
	if !IsNoteChanged(err) {
		t.Fatalf("expected a NoteChangedError, got %v", err)
	}
	if !strings.Contains(err.Error(), "inbox/A.md changed on the server since it was read, so it was not saved over") {
		t.Fatalf("message: %q", err)
	}
	if body, _ := server.Body("inbox/A.md"); body != "theirs\n" {
		t.Fatalf("written over: %q", body)
	}
	// Gone is refused the same way, and nothing is created.
	server.Delete("inbox/A.md")
	if _, err := client.WriteNoteIfUnchanged(ctx, "inbox/A.md", "mine\n", note.Sha256); !IsNoteChanged(err) {
		t.Fatalf("vanished note: %v", err)
	}
	if _, ok := server.Body("inbox/A.md"); ok {
		t.Fatal("a refused write created the note")
	}
}

func TestAPlainWriteSendsNoBase(t *testing.T) {
	ctx := context.Background()
	server := remotetest.New(t, true, map[string]string{"inbox/A.md": "one\n"})
	client := NewClient(server.URL, "")
	if _, err := client.WriteNote(ctx, "inbox/A.md", "two\n"); err != nil {
		t.Fatal(err)
	}
	// Not a hash: never sent, so never a 400, and the write is unconditional.
	if _, err := client.WriteNoteIfUnchanged(ctx, "inbox/A.md", "three\n", "abc"); err != nil {
		t.Fatal(err)
	}
	for _, w := range server.Writes() {
		if w.SentBase {
			t.Fatalf("a base was sent: %+v", w)
		}
	}
}

func TestAnOlderServerHandsOutNoHash(t *testing.T) {
	ctx := context.Background()
	server := remotetest.New(t, false, map[string]string{"inbox/A.md": "one\n"})
	client := NewClient(server.URL, "")
	note, err := client.ReadNote(ctx, "inbox/A.md")
	if err != nil {
		t.Fatal(err)
	}
	if note.Sha256 != "" {
		t.Fatalf("hash from an older server: %q", note.Sha256)
	}
}

func TestShaHexValidation(t *testing.T) {
	a := strings.Repeat("a", 64)
	for value, want := range map[string]bool{a: true, strings.ToUpper(a): true, "": false, "abc": false, strings.Repeat("g", 64): false, a + "0": false} {
		if IsSha256Hex(value) != want {
			t.Errorf("IsSha256Hex(%q) = %v", value, !want)
		}
	}
}
