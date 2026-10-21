package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/helix"
)

// TestExpandSelectionReachesTheEngine checks that Alt+o and Alt+i grow and
// shrink the selection in the editor: the app must pass the chords on to the
// helix engine instead of swallowing them.
func TestExpandSelectionReachesTheEngine(t *testing.T) {
	a, root := newFeatureTestApp(t)
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("one two three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.openNote("note.md", true)
	ed := a.activeBuffer().ed
	a.handleKey(helix.Key{Rune: 'o', Alt: true})
	start, end, _, ok := ed.VisualRange()
	if !ok || start != (helix.Pos{Line: 0, Col: 0}) || end != (helix.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection after alt-o = %v..%v ok=%t, want the first word", start, end, ok)
	}
	// The arrow keys work the same way.
	a.handleKey(helix.Key{Name: "up", Alt: true})
	start, end, _, ok = ed.VisualRange()
	if !ok || start != (helix.Pos{Line: 0, Col: 0}) || end != (helix.Pos{Line: 0, Col: 12}) {
		t.Fatalf("selection after alt-up = %v..%v ok=%t, want the paragraph", start, end, ok)
	}
	// Alt+i walks back down the ladder.
	a.handleKey(helix.Key{Name: "down", Alt: true})
	start, end, _, ok = ed.VisualRange()
	if !ok || start != (helix.Pos{Line: 0, Col: 0}) || end != (helix.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection after alt-down = %v..%v ok=%t, want the first word", start, end, ok)
	}
}
