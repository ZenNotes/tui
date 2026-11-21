package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// The engine contract: what the app relies on from any editing engine. Each
// test runs against every engine in engineProfiles, so a new engine, or a
// change in one, shows up here before it reaches a user.

func forEachEngine(t *testing.T, run func(t *testing.T, a *App, root string)) {
	for name := range engineProfiles {
		t.Run(name, func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			a.prefs.Motions = name
			run(t, a, root)
		})
	}
}

func pressKeys(a *App, keys ...vim.Key) {
	for _, k := range keys {
		a.handleKey(k)
	}
}

func TestEngineContractInsertAndCursorShape(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, _ string) {
		a.openNote("note.md", true)
		buf := a.activeBuffer()
		if _, ok := a.bufferCursorShape(buf); ok {
			t.Fatal("normal mode should keep the drawn block")
		}
		typeKeys(a, "ihey")
		if buf.ed.Mode() != vim.ModeInsert {
			t.Fatalf("i did not enter insert mode: %v", buf.ed.Mode())
		}
		if shape, ok := a.bufferCursorShape(buf); !ok || shape != cursorShapeBar {
			t.Fatalf("insert mode cursor = %v %v, want a bar", shape, ok)
		}
		pressKeys(a, vim.KeyEsc)
		if buf.ed.Mode() != vim.ModeNormal {
			t.Fatalf("esc did not return to normal mode: %v", buf.ed.Mode())
		}
		if got := buf.ed.Line(0); got != "hey# Note" {
			t.Fatalf("line 0 = %q", got)
		}
	})
}

func TestEngineContractReplacePendingCursor(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, _ string) {
		a.openNote("note.md", true)
		buf := a.activeBuffer()
		typeKeys(a, "r")
		if shape, ok := a.bufferCursorShape(buf); !ok || shape != cursorShapeUnderline {
			t.Fatalf("r pending cursor = %v %v, want an underline", shape, ok)
		}
		typeKeys(a, "X")
		if got := buf.ed.Line(0); got != "X Note" {
			t.Fatalf("r X: line 0 = %q", got)
		}
	})
}

// Ctrl+C leaves a selection instead of quitting, in either engine.
func TestEngineContractCtrlCLeavesSelection(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, _ string) {
		a.openNote("note.md", true)
		buf := a.activeBuffer()
		typeKeys(a, "vl")
		if _, _, _, ok := buf.ed.VisualRange(); !ok {
			t.Fatal("v l made no selection")
		}
		pressKeys(a, vim.Ctrl('c'))
		if a.quitting {
			t.Fatal("ctrl+c quit from a selection")
		}
		if buf.ed.Mode() != vim.ModeNormal {
			t.Fatalf("ctrl+c left mode %v", buf.ed.Mode())
		}
	})
}

// :w reaches the app through the ExCommand hook, which starts the save.
func TestEngineContractWriteReachesTheApp(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, _ string) {
		a.openNote("note.md", true)
		buf := a.activeBuffer()
		typeKeys(a, "iX")
		pressKeys(a, vim.KeyEsc)
		typeKeys(a, ":w")
		pressKeys(a, vim.KeyEnter)
		if !buf.saving && buf.dirty() {
			t.Fatal(":w did not start a save")
		}
	})
}

// The shared tab sequences work in either engine's editor.
func TestEngineContractTabSequences(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, root string) {
		writeTabNotes(t, root)
		a.openNote("a.md", true)
		a.openNote("b.md", true)
		typeKeys(a, "gT")
		if got := a.activeBuffer().path; got != "a.md" {
			t.Fatalf("gT: active %q", got)
		}
		typeKeys(a, "gt")
		if got := a.activeBuffer().path; got != "b.md" {
			t.Fatalf("gt: active %q", got)
		}
	})
}

// A double-click selects the word under the pointer.
func TestEngineContractSelectWord(t *testing.T) {
	forEachEngine(t, func(t *testing.T, a *App, root string) {
		if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("one two three\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.openNote("note.md", true)
		ed := a.activeBuffer().ed
		ed.SetCursor(vim.Pos{Line: 0, Col: 5})
		ed.Feed(a.engine().selectWord)
		start, end, _, ok := ed.VisualRange()
		if !ok || start != (vim.Pos{Line: 0, Col: 4}) || end != (vim.Pos{Line: 0, Col: 6}) {
			t.Fatalf("select word = %v..%v ok=%v, want two", start, end, ok)
		}
	})
}
