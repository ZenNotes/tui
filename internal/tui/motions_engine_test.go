package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/helix"
)

// TestMotionsOptionPicksTheEngine checks [helix] motions: "vim" restores the
// vim model (dw deletes a word, w just moves) while the default helix model
// selects as it moves (w leaves a spanning selection, wd deletes a word).
func TestMotionsOptionPicksTheEngine(t *testing.T) {
	for _, tc := range []struct {
		motions string
		move    string
		delete  string
		want    string
		wantSel bool
	}{
		{"vim", "w", "dw", "two three\n", false},
		{"helix", "w", "wd", "two three\n", true},
	} {
		a, root := newFeatureTestApp(t)
		a.prefs.Motions = tc.motions
		if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("one two three\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.openNote("note.md", true)
		a.activeBuffer().ed.Feed(tc.move)
		_, _, _, active := a.activeBuffer().ed.VisualRange()
		if active != tc.wantSel {
			t.Fatalf("%s %q: selection active = %t, want %t", tc.motions, tc.move, active, tc.wantSel)
		}
		// the delete runs from a fresh cursor, the way the user would type it
		fresh := a.newModalEditor("one two three\n", helix.Hooks{}, &a.registers)
		fresh.Feed(tc.delete)
		if got := fresh.Text(); got != tc.want {
			t.Fatalf("%s %q: got %q, want %q", tc.motions, tc.delete, got, tc.want)
		}
	}
}

// TestHelixWordMotionLeavesTheNextWordOut checks that the helix engine keeps
// the word a motion lands on out of the selection, and draws the block cursor
// inside the selection, the way Helix does.
func TestHelixWordMotionLeavesTheNextWordOut(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.prefs.Motions = "helix"
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("one two three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.openNote("note.md", true)
	ed := a.activeBuffer().ed
	ed.Feed("w")
	start, end, _, ok := ed.VisualRange()
	if !ok || start != (helix.Pos{Line: 0, Col: 0}) || end != (helix.Pos{Line: 0, Col: 3}) {
		t.Fatalf("selection after w = %v..%v ok=%t, want the text before the next word", start, end, ok)
	}
	if got, want := ed.BlockCursor(), (helix.Pos{Line: 0, Col: 3}); got != want {
		t.Fatalf("block cursor after w = %v, want %v", got, want)
	}
}

// TestVimMotionsKeepAppKeys checks that with the vim engine the app-level
// sequences still work and visual mode reports its label.
func TestVimMotionsKeepAppKeys(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.prefs.Motions = "vim"
	for name, text := range map[string]string{"a.md": "A\n", "b.md": "B\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.openNote("a.md", true)
	a.openNote("b.md", true)
	// gn is a helix-motion key: with the vim engine it stays with the engine
	// (a no-op here) and must not switch tabs.
	a.handleKey(helix.R('g'))
	a.handleKey(helix.R('n'))
	if got := a.activeBuffer().path; got != "b.md" {
		t.Fatalf("gn switched tabs with the vim engine, active %q", got)
	}
	// vim visual mode keeps its label through the interface
	a.activeBuffer().ed.Feed("v")
	if label := a.activeBuffer().ed.ModeLabel(); label != "VISUAL" {
		t.Fatalf("vim visual label = %q", label)
	}
	a.activeBuffer().ed.Feed(helix.KeyEsc.String())
}
