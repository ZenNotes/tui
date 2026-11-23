package remote

import (
	"errors"
	"fmt"
)

// Conditional note writes (znserver supportsConditionalNoteWrites,
// ZenNotes/zennotes#1108). Such a server returns a note's sha256 with every
// read and write of it, and refuses a write that sends that value back as
// baseSha256 with 409 when the note changed in between. zn never hashes a note
// itself: it echoes the server's value, which names the bytes on disk exactly,
// whatever JSON did to the body on the way. An older server returns no hash, so
// nothing is sent and every write stays unconditional, exactly as before.

// NoteChangedError is a write the server refused because the note is no
// longer the version the write named, or no longer exists. Nothing was
// written. The message is the desktop's, word for word.
type NoteChangedError struct {
	Path string
}

func (e *NoteChangedError) Error() string {
	return fmt.Sprintf("%s changed on the server since it was read, so it was not saved over. Someone else saved it in the meantime.", e.Path)
}

// IsNoteChanged reports a write refused because the note moved on the server.
func IsNoteChanged(err error) bool {
	var changed *NoteChangedError
	return errors.As(err, &changed)
}

// IsSha256Hex reports a value worth sending as a base: 64 hex digits.
func IsSha256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F') {
			return false
		}
	}
	return true
}
