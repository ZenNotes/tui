package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// TestExpandSelectionReachesTheEngine checks that Alt+o and Alt+i grow and
// shrink the selection in the editor: the app must pass the chords on to the
// helix engine instead of swallowing them.
func TestExpandSelectionReachesTheEngine(t *testing.T) {
	a, root := newHelixTestApp(t)
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("one two three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.openNote("note.md", true)
	ed := a.activeBuffer().ed
	a.handleKey(vim.Key{Rune: 'o', Alt: true})
	start, end, _, ok := ed.VisualRange()
	if !ok || start != (vim.Pos{Line: 0, Col: 0}) || end != (vim.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection after alt-o = %v..%v ok=%t, want the first word", start, end, ok)
	}
	// The arrow keys work the same way.
	a.handleKey(vim.Key{Name: "up", Alt: true})
	start, end, _, ok = ed.VisualRange()
	if !ok || start != (vim.Pos{Line: 0, Col: 0}) || end != (vim.Pos{Line: 0, Col: 12}) {
		t.Fatalf("selection after alt-up = %v..%v ok=%t, want the paragraph", start, end, ok)
	}
	// Alt+i walks back down the ladder.
	a.handleKey(vim.Key{Name: "down", Alt: true})
	start, end, _, ok = ed.VisualRange()
	if !ok || start != (vim.Pos{Line: 0, Col: 0}) || end != (vim.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection after alt-down = %v..%v ok=%t, want the first word", start, end, ok)
	}
}
