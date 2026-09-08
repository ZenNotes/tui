package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommentsRoundTripThreadingAndAnchors(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := "# Plan\n\nShip the café build.\n\nSecond paragraph.\n"
	start, end, text, err := AnchorForText(doc, "café build")
	if err != nil || text != "café build" || start != 17 || end != 27 {
		t.Fatalf("anchor: %d %d %q %v", start, end, text, err)
	}
	if _, _, _, err := AnchorForText(doc, "not there"); err == nil {
		t.Fatal("missing anchor text must fail")
	}
	if s, e, _, err := AnchorForText(doc, "SHIP THE"); err != nil || s != 8 || e != 16 {
		t.Fatalf("case-insensitive fallback: %d %d %v", s, e, err)
	}
	written, err := v.WriteNoteComments("Plan.md", []NoteComment{
		{Body: "Check the umlaut", AnchorStart: start, AnchorEnd: end, AnchorText: text, CreatedAt: 10, UpdatedAt: 10},
		{Body: "", CreatedAt: 11},
		{ID: "r1", Body: "Agreed", ParentID: "ghost", CreatedAt: 12, UpdatedAt: 12, Author: "  Claude  Code "},
	})
	if err != nil || len(written) != 2 {
		t.Fatalf("write: %v %+v", err, written)
	}
	if written[1].ParentID != "" || written[1].Author != "Claude Code" {
		t.Fatalf("orphan reply becomes a comment, author squeezed: %+v", written[1])
	}
	side := filepath.Join(root, ".zennotes", "comments", "Plan.md.comments.json")
	raw, err := os.ReadFile(side)
	if err != nil || !strings.Contains(string(raw), `"version": 1`) || !strings.Contains(string(raw), `"notePath": "Plan.md"`) {
		t.Fatalf("sidecar: %v %s", err, raw)
	}
	back, err := v.ReadNoteComments("Plan.md")
	if err != nil || len(back) != 2 || back[0].ID != written[0].ID {
		t.Fatalf("read back: %v %+v", err, back)
	}
	reply := NoteComment{Body: "Fixed", ParentID: back[0].ID, CreatedAt: 20, UpdatedAt: 20}
	back, _ = v.WriteNoteComments("Plan.md", append(back, reply))
	threads := ThreadNoteComments(back)
	if len(threads) != 2 || len(threads[0].Replies) != 1 || threads[0].Replies[0].Body != "Fixed" {
		t.Fatalf("threads: %+v", threads)
	}
	if root, ok := ThreadRootOf(back, threads[0].Replies[0].ID); !ok || root.ID != back[0].ID {
		t.Fatal("a reply id resolves to its thread")
	}
	// The anchor follows the text when the note changes above it.
	moved := "# Plan\n\nNew intro line.\n\nShip the café build.\n"
	from, to := ResolveCommentAnchor(back[0], moved)
	if LineOfOffset(moved, from) != 5 || to-from != end-start {
		t.Fatalf("re-anchored: %d %d line %d", from, to, LineOfOffset(moved, from))
	}
	if _, err := v.WriteNoteComments("Plan.md", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatal("an empty list removes the sidecar")
	}
}
