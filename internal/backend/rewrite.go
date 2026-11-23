package backend

import (
	"context"
	"fmt"

	"github.com/ZenNotes/tui/internal/remote"
	"github.com/ZenNotes/tui/internal/vault"
)

// RewriteAttempts is how many times RewriteNote reads and writes a note
// before it gives up on a server where the note keeps changing. The desktop's
// rewriteNoteOnServer stops at the same count.
const RewriteAttempts = 3

// RewriteNote reads a note, changes it with edit and writes the result back on
// the version it read (ZenNotes/zennotes#1108). Against a server with
// conditional writes the write names the read's sha256, so a save another
// device made in between is never written over: the server refuses, and the
// note is read again and edit applied to the newer text, since an edit here
// is a function of the body (append a line, replace a word, toggle a task). A
// few refusals in a row end in a remote.NoteChangedError with nothing written.
//
// edit returns false to write nothing; RewriteNote then returns false too. It
// may run more than once, so whatever it records for its caller (a count, an
// id) is from the attempt that was written. A local vault or an older server
// hands out no hash, so the write is unconditional, exactly as before.
func RewriteNote(ctx context.Context, b Backend, rel string, edit func(note vault.NoteContent) (string, bool, error)) (vault.NoteMeta, bool, error) {
	for attempt := 1; ; attempt++ {
		note, err := b.ReadNote(ctx, rel)
		if err != nil {
			return vault.NoteMeta{}, false, err
		}
		next, write, err := edit(note)
		if err != nil || !write {
			return vault.NoteMeta{}, false, err
		}
		target := note.Path
		if target == "" {
			target = rel
		}
		meta, err := b.WriteNoteIfUnchanged(ctx, target, next, note.Sha256)
		if err == nil {
			return meta, true, nil
		}
		if !remote.IsNoteChanged(err) {
			return vault.NoteMeta{}, false, err
		}
		if attempt >= RewriteAttempts {
			return vault.NoteMeta{}, false, fmt.Errorf("%w It kept changing while zn tried %d times; run the command again.", err, RewriteAttempts)
		}
	}
}
