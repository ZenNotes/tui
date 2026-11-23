package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/remote/remotetest"
	"github.com/ZenNotes/tui/internal/vault"
)

const raceNote = "inbox/A.md"

func remoteOn(server *remotetest.Server) *Remote {
	return newRemote(Target{Kind: KindRemote, BaseURL: server.URL})
}

// raceOnce lands a save from another device between the first read and the
// first write, the window a read, change and write back used to lose.
func raceOnce(server *remotetest.Server, theirs string) {
	done := false
	server.BeforeWrite = func(string) {
		if !done {
			done = true
			server.Set(raceNote, theirs)
		}
	}
}

// Every read, change and write back keeps a save another device made between
// its read and its write, and makes its own change on top of it.
func TestAnEditKeepsASaveMadeBetweenItsReadAndItsWrite(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, start, theirs, want string
		edit                      func(r *Remote) error
	}{
		{"append", "one\n", "one\ntheirs\n", vault.AppendToBody("one\ntheirs\n", "mine"), func(r *Remote) error {
			_, err := r.AppendToNote(ctx, raceNote, "mine")
			return err
		}},
		{"prepend", "one\n", "theirs\none\n", vault.PrependToBody("theirs\none\n", "mine"), func(r *Remote) error {
			_, err := r.PrependToNote(ctx, raceNote, "mine")
			return err
		}},
		{"insert", "a\nb\n", "a\nb\ntheirs\n", vault.InsertAtLineInBody("a\nb\ntheirs\n", 2, "mine"), func(r *Remote) error {
			_, err := r.InsertAtLine(ctx, raceNote, 2, "mine")
			return err
		}},
		{"replace", "say old\n", "say old\ntheirs\n", "say new\ntheirs\n", func(r *Remote) error {
			_, n, err := r.ReplaceInNote(ctx, raceNote, "old", "new", false)
			if err == nil && n != 1 {
				t.Errorf("replace count %d", n)
			}
			return err
		}},
		{"toggle", "- [ ] a\n", "- [ ] a\ntheirs\n", "- [x] a\ntheirs\n", func(r *Remote) error {
			_, err := r.ToggleTask(ctx, raceNote+"#0", vault.DialectCLI)
			return err
		}},
		{"update task", "- [ ] a\n", "- [ ] a\ntheirs\n", "- [ ] a 🔺\ntheirs\n", func(r *Remote) error {
			high := "high"
			_, err := UpdateTask(ctx, r, UpdateTaskInput{ID: raceNote + "#0", Priority: &high})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := remotetest.New(t, true, map[string]string{raceNote: tc.start})
			raceOnce(server, tc.theirs)
			if err := tc.edit(remoteOn(server)); err != nil {
				t.Fatal(err)
			}
			got, _ := server.Body(raceNote)
			if tc.name == "toggle" {
				// A check-off may stamp a completion date after the box.
				if !strings.HasPrefix(got, "- [x] a") || !strings.HasSuffix(got, "\ntheirs\n") {
					t.Fatalf("got %q", got)
				}
			} else if tc.name == "update task" {
				if !strings.HasSuffix(got, "\ntheirs\n") || !strings.HasPrefix(got, "- [ ] a ") || got == tc.theirs {
					t.Fatalf("got %q", got)
				}
			} else if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			writes := server.Writes()
			if len(writes) != 2 || !writes[0].Refused || writes[1].Refused {
				t.Fatalf("expected one refused write then one written: %+v", writes)
			}
			if writes[1].Base != remotetest.Sha256(tc.theirs) {
				t.Fatalf("second write named %q, not the newer version", writes[1].Base)
			}
		})
	}
}

// A note that keeps changing ends in a clear error, with nothing written over.
func TestAnEditGivesUpWhenTheNoteKeepsChanging(t *testing.T) {
	server := remotetest.New(t, true, map[string]string{raceNote: "one\n"})
	n := 0
	server.BeforeWrite = func(string) {
		n++
		server.Set(raceNote, strings.Repeat("theirs\n", n))
	}
	_, err := remoteOn(server).AppendToNote(context.Background(), raceNote, "mine")
	if !remote.IsNoteChanged(err) {
		t.Fatalf("expected a NoteChangedError, got %v", err)
	}
	if !strings.Contains(err.Error(), "not saved over") || !strings.Contains(err.Error(), "tried 3 times") {
		t.Fatalf("message: %q", err)
	}
	if got, _ := server.Body(raceNote); strings.Contains(got, "mine") {
		t.Fatalf("written over: %q", got)
	}
	if len(server.Writes()) != RewriteAttempts {
		t.Fatalf("writes: %+v", server.Writes())
	}
}

// A note deleted between the read and the write is not made again.
func TestAnEditDoesNotBringBackADeletedNote(t *testing.T) {
	server := remotetest.New(t, true, map[string]string{raceNote: "one\n"})
	server.BeforeWrite = func(string) { server.Delete(raceNote) }
	if _, err := remoteOn(server).AppendToNote(context.Background(), raceNote, "mine"); err == nil {
		t.Fatal("append to a deleted note succeeded")
	}
	if _, ok := server.Body(raceNote); ok {
		t.Fatal("the deleted note was written again")
	}
}

// An older server keeps today's behaviour exactly: one read, one write, no
// base, and a save in between is written over as before.
func TestAnOlderServerWritesAsBefore(t *testing.T) {
	server := remotetest.New(t, false, map[string]string{raceNote: "one\n"})
	raceOnce(server, "one\ntheirs\n")
	if _, err := remoteOn(server).AppendToNote(context.Background(), raceNote, "mine"); err != nil {
		t.Fatal(err)
	}
	if got, _ := server.Body(raceNote); got != vault.AppendToBody("one\n", "mine") {
		t.Fatalf("got %q", got)
	}
	writes := server.Writes()
	if len(writes) != 1 || writes[0].SentBase || server.Reads() != 1 {
		t.Fatalf("reads %d, writes %+v", server.Reads(), writes)
	}
}

// No match writes nothing, on any server.
func TestAReplaceWithNoMatchWritesNothing(t *testing.T) {
	server := remotetest.New(t, true, map[string]string{raceNote: "one\n"})
	_, n, err := remoteOn(server).ReplaceInNote(context.Background(), raceNote, "absent", "x", true)
	if err != nil || n != 0 || len(server.Writes()) != 0 {
		t.Fatalf("n %d err %v writes %+v", n, err, server.Writes())
	}
}

// Plain write and create stay unconditional: they replace the whole note.
func TestAPlainWriteStaysUnconditional(t *testing.T) {
	ctx := context.Background()
	server := remotetest.New(t, true, map[string]string{raceNote: "one\n"})
	r := remoteOn(server)
	if _, err := r.WriteNote(ctx, raceNote, "two\n"); err != nil {
		t.Fatal(err)
	}
	body := "fresh\n"
	if _, err := r.CreateNote(ctx, vault.FolderInbox, "New", "", &body); err != nil {
		t.Fatal(err)
	}
	for _, w := range server.Writes() {
		if w.SentBase {
			t.Fatalf("a base was sent: %+v", w)
		}
	}
	if got, _ := server.Body("inbox/New.md"); got != body {
		t.Fatalf("created %q", got)
	}
}
