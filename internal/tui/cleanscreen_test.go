package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/termsafe"
)

// The real frame: a note's bytes are text on screen wherever they are drawn,
// in the editor and in both reading view styles, with the colours left alone.
func TestViewSendsNoControlSequencesFromANote(t *testing.T) {
	body := "# Title \x1b]0;name\x07\n\nline \x1b]52;c;aGk=\x07 and \x1b[2J more\n\n```\ncode \x1b]52;c;aGk=\x07\n```\n\n| a \x1b]52;c;aGk=\x07 | b |\n|---|---|\n| 1 | 2 |\n"
	for name, style := range map[string]string{"auto": "auto", "zen": "zen"} {
		out := viewOfNote(t, body, style)
		for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: the frame holds %q", name, bad)
			}
		}
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("%s: the frame lost its colours", name)
		}
		if !strings.Contains(stripAnsi(out), "line") {
			t.Errorf("%s: the frame lost the note's text", name)
		}
	}
}

// viewOfNote draws a note in the editor and in the reading view, as one frame
// each, and returns them joined.
func viewOfNote(t *testing.T, body, style string) string {
	t.Helper()
	a, _ := cursorApp(t, body, 100)
	a.prefs.PreviewStyle = style
	editor := a.View()
	a.setPaneMode(modePreview)
	return editor + a.View()
}

// A note's own colour never reaches the terminal, in the editor or either
// reading view, so it cannot hide its text.
func TestANoteCannotSetColours(t *testing.T) {
	for _, style := range []string{"auto", "zen"} {
		a, _ := cursorApp(t, "before \x1b[8mhidden text\x1b[0m after", 100)
		a.prefs.PreviewStyle = style
		for _, frame := range []string{a.View(), func() string { a.setPaneMode(modePreview); return a.View() }()} {
			if strings.Contains(frame, "\x1b[8m") {
				t.Fatalf("%s: the note's colour reached the frame", style)
			}
			if !strings.Contains(stripAnsi(frame), "hidden text") {
				t.Fatalf("%s: the text is gone:\n%s", style, stripAnsi(frame))
			}
		}
	}
}

// The insert cursor stays on the rune it is at: a control rune takes the one
// cell its picture is drawn in.
func TestControlRunesTakeOneCell(t *testing.T) {
	a, buf := cursorApp(t, "a\x1b[31mb", 100)
	a.View()
	buf.ed.Feed("A")
	withControl := a.computeCursor()
	b, buf2 := cursorApp(t, "a␛[31mb", 100)
	b.View()
	buf2.ed.Feed("A")
	if got, want := withControl, b.computeCursor(); got.x != want.x || got.y != want.y {
		t.Fatalf("cursor at %d,%d, want %d,%d", got.x, got.y, want.x, want.y)
	}
	if !strings.Contains(stripAnsi(a.View()), "a␛[31mb") {
		t.Fatalf("no picture of the control rune:\n%s", stripAnsi(a.View()))
	}
}

// A picture's placeholder rune in a note does not draw a picture.
func TestANoteCannotPlacePicturePlaceholders(t *testing.T) {
	frame := viewOfNote(t, "x \U0010EEEE̅̅ y", "zen")
	if strings.ContainsRune(frame, 0x10EEEE) {
		t.Fatal("a placeholder rune reached the frame")
	}
}

// Many control sequences in a note do not slow the reading view.
func TestManyControlSequencesRenderQuickly(t *testing.T) {
	body := strings.Repeat("\x1b[2J", 25000)
	for _, style := range []string{"auto", "zen"} {
		a := &App{theme: buildTheme(true), prefs: prefsView{Prefs: config.DefaultPrefs()}}
		a.prefs.PreviewStyle = style
		start := time.Now()
		a.renderPreviewLinesFrom(body, 80, "x.md")
		if took := time.Since(start); took > 5*time.Second {
			t.Fatalf("%s took %s", style, took)
		}
	}
}

// Opening a note is not an edit: bytes that are not valid UTF-8, and CRLF line
// ends, stay on disk until the note is really changed.
func TestOpeningANoteKeepsItsBytes(t *testing.T) {
	for name, body := range map[string]string{"invalid": "caf\xff\nnext\n", "crlf": "one\r\ntwo\r\n"} {
		t.Run(name, func(t *testing.T) {
			a, _ := cursorApp(t, "", 100)
			path := filepath.Join(a.backend.Root(), "Bytes.md")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			buf, err := a.openBuffer("Bytes.md")
			if err != nil {
				t.Fatal(err)
			}
			if buf.dirty() {
				t.Fatal("opening the note marked it changed")
			}
			a.saveAllBuffers()
			if got, _ := os.ReadFile(path); string(got) != body {
				t.Fatalf("file changed to %q", got)
			}
			buf.ed.SetText(buf.ed.Text() + "more")
			if !buf.dirty() {
				t.Fatal("a real edit is not a change")
			}
		})
	}
}

// A file name with a newline stays on its row in the sidebar.
func TestSidebarNameStaysOnOneRow(t *testing.T) {
	if got := termsafe.Line("two\nlines\x1b"); strings.ContainsAny(got, "\n\x1b") {
		t.Fatalf("got %q", got)
	}
}

// A long word is broken with spaces only, so source lines stay in step.
func TestBreakLongWordsKeepsLines(t *testing.T) {
	long := strings.Repeat("x", 100000)
	got := breakLongWords("a\n" + long + "\nb")
	if strings.Count(got, "\n") != 2 || strings.Join(strings.Fields(got), "") != "a"+long+"b" {
		t.Fatal("the text changed")
	}
	if plain := "short words\nstay as they are"; breakLongWords(plain) != plain {
		t.Fatal("short text changed")
	}
}
