package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestYankFollowsTheSessionAcrossPreviouslyOpenedNotes(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.prefs.VimYankToClipboard = false
	for name, text := range map[string]string{"source.md": "source line\nanother line\n", "target.md": "destination line\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.openNote("source.md", true)
	a.activeBuffer().ed.Feed("yy")
	a.openNote("target.md", true)
	a.activeBuffer().ed.Feed("yy")
	a.handleKey(vim.R('['))
	a.handleKey(vim.R('b'))
	if a.activeBuffer().path != "source.md" {
		t.Fatal("[b did not switch to the source note")
	}
	a.activeBuffer().ed.Feed("jyy")
	a.handleKey(vim.R(']'))
	a.handleKey(vim.R('b'))
	target := a.activeBuffer()
	target.ed.Feed("p")
	if got := target.ed.Text(); got != "destination line\nanother line\n" {
		t.Fatalf("paste used a note-local register: %q", got)
	}
	target.ed.Feed("u")
	if target.ed.Text() != "destination line\n" || a.buffers["source.md"].ed.Text() != "source line\nanother line\n" {
		t.Fatal("sharing registers changed per-note undo or the source note")
	}
}

func TestYankSurvivesClosedSourceAndAsynchronousDestination(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.prefs.VimYankToClipboard = false
	source, err := a.openBuffer("note.md")
	if err != nil {
		t.Fatal(err)
	}
	source.ed.Feed("yy")
	a.closeBufferIfUnused("note.md")
	if _, ok := a.buffers["note.md"]; ok {
		t.Fatal("source buffer was not closed")
	}
	if err := os.WriteFile(filepath.Join(root, "next.md"), []byte("Next\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.deferReads = true
	destination, err := a.openBuffer("next.md")
	if err != nil {
		t.Fatal(err)
	}
	a.applyBufferRead(a.readBufferCmd(destination)().(bufferReadMsg))
	destination.ed.Feed("p")
	if got := destination.ed.Text(); got != "Next\n# Note\n" {
		t.Fatalf("closing/loading lost the yank: %q", got)
	}
}

func TestTemplateEditorSharesNamedRegistersWithNotes(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.VimYankToClipboard = false
	a.openNote("note.md", true)
	a.activeBuffer().ed.Feed(`"ayy`)
	file := vault.CustomTemplateFile{SourcePath: ".zennotes/templates/example.md", Raw: "# Template\n"}
	a.openTemplateFile(file)
	a.activeBuffer().ed.Feed(`"ap`)
	if got := a.activeBuffer().ed.Text(); !strings.Contains(got, "# Template\n# Note\n") {
		t.Fatalf("template editor could not read the note register: %q", got)
	}
}

func TestSeparateTUISessionsDoNotShareRegisters(t *testing.T) {
	first, _ := newFeatureTestApp(t)
	second, _ := newFeatureTestApp(t)
	first.prefs.VimYankToClipboard, second.prefs.VimYankToClipboard = false, false
	first.openNote("note.md", true)
	first.activeBuffer().ed.Feed("yy")
	second.openNote("note.md", true)
	before := second.activeBuffer().ed.Text()
	second.activeBuffer().ed.Feed("p")
	if second.activeBuffer().ed.Text() != before {
		t.Fatal("a yank escaped its TUI session")
	}
}
