package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// assertFieldBar checks the cursor is a visible bar on the cell where the
// view draws want, and that no reverse-video cell sits on its row.
func assertFieldBar(t *testing.T, a *App, view, want string) {
	t.Helper()
	if !a.cursor.visible || a.cursor.shape != cursorShapeBar {
		t.Fatalf("cursor = %+v, want a visible bar", a.cursor)
	}
	if got := cellText(view, a.cursor.x, a.cursor.y); got != want {
		t.Fatalf("bar sits on %q, want the cell of %q", got, want)
	}
	for _, c := range reverseCells(view) {
		if c.y == a.cursor.y {
			t.Fatalf("reverse-video cell drawn in the field at %v", c)
		}
	}
}

func TestTextFieldCursorPalette(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.openNoteSearch("abc")
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if cellText(view, a.cursor.x-1, a.cursor.y) != "c" {
		t.Fatal("bar is not right after the typed text")
	}
	a.overlay.handleKey(a, vim.Key{Name: "left"})
	assertFieldBar(t, a, a.View(), "c")
}

func TestTextFieldCursorPlaceholder(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.openNoteSearch("")
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if got := cellText(view, a.cursor.x+1, a.cursor.y); got != "T" {
		t.Fatalf("placeholder starts at %q, want T", got)
	}
	a.overlay = newPickOverlay("Tags", "Filter options", []pickItem{{id: "a", label: "a"}}, nil)
	view = a.View()
	assertFieldBar(t, a, view, " ")
	if got := cellText(view, a.cursor.x+1, a.cursor.y); got != "F" {
		t.Fatalf("picker placeholder starts at %q, want F", got)
	}
}

func TestTextFieldCursorSecretPrompt(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.promptSecret("Token", "", func(*App, string) {})
	typeText(a, "abc")
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if got := cellText(view, a.cursor.x-1, a.cursor.y); got != "•" {
		t.Fatalf("cell before the bar is %q, want the third dot", got)
	}
}

const wrappingTitle = "New note in Projects/Work/Clients/Acme Corporation/Meetings"

func TestTextFieldCursorWrappedTitle(t *testing.T) {
	open := map[string]func(a *App){
		"prompt": func(a *App) { a.promptFor(wrappingTitle, "", "", func(*App, string) {}) },
		"palette": func(a *App) {
			a.overlay = &palette{title: wrappingTitle, input: newTextInput("")}
		},
		"picker": func(a *App) {
			a.overlay = newPickOverlay(wrappingTitle, "", []pickItem{{id: "a", label: "a"}}, nil)
		},
	}
	for name, fn := range open {
		t.Run(name, func(t *testing.T) {
			a, _ := cursorApp(t, "hello\n", 80)
			fn(a)
			typeText(a, "ab")
			view := a.View()
			assertFieldBar(t, a, view, " ")
			if got := cellText(view, a.cursor.x-1, a.cursor.y); got != "b" {
				t.Fatalf("cell before the bar is %q, want the last typed rune", got)
			}
		})
	}
}

func TestTextFieldCursorClampedToFieldWidth(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 40)
	a.promptFor("Name", "", "", func(*App, string) {})
	typeText(a, "this text is a lot longer than the prompt is wide")
	a.View()
	box := a.overlayRect
	if !a.cursor.visible || a.cursor.x >= box.x+box.w || a.cursor.x <= box.x {
		t.Fatalf("cursor = %+v escapes the overlay %+v", a.cursor, box)
	}
}

func TestTextFieldCursorPromptBeatsInsertionPoint(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	a.View()
	insertion := a.cursor
	a.promptFor("Name", "ab", "", func(*App, string) {})
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if a.cursor == insertion {
		t.Fatalf("bar stayed at the insertion point %+v", insertion)
	}
}

func TestTextFieldCursorEscapeBackToNormalBuffer(t *testing.T) {
	a, buf := cursorApp(t, "hello\n", 80)
	buf.ed.Feed("<Esc>")
	a.promptFor("Name", "ab", "", func(*App, string) {})
	a.View()
	if !a.cursor.visible {
		t.Fatal("no bar in the prompt")
	}
	a.overlay.handleKey(a, vim.Key{Name: "esc"})
	view := a.View()
	if a.cursor.visible {
		t.Fatalf("cursor = %+v, want hidden", a.cursor)
	}
	if len(reverseCells(view)) == 0 {
		t.Fatal("the buffer's block is not drawn")
	}
}

func TestTextFieldCursorCellAndTaskPrompts(t *testing.T) {
	prompts := map[string]func(a *App){
		"date":   func(a *App) { a.datePrompt("Due", "2026-11-01", func(*App, string) {}) },
		"number": func(a *App) { a.numberPrompt("Count", "42", func(*App, string) {}) },
		"text":   func(a *App) { a.textPrompt("Notes", "abc", "[[ links a note", func(*App, string) {}) },
		"due":    func(a *App) { a.taskDuePrompt(vault.Task{Due: "2026-11-01"}) },
		"field":  func(a *App) { a.taskFieldPrompt(vault.Task{}) },
	}
	for name, open := range prompts {
		t.Run(name, func(t *testing.T) {
			a, _ := cursorApp(t, "hello\n", 80)
			open(a)
			p := a.overlay.(*prompt)
			view := a.View()
			assertFieldBar(t, a, view, " ")
			if p.input.cursor > 0 {
				want := string(p.input.runes[p.input.cursor-1])
				if got := cellText(view, a.cursor.x-1, a.cursor.y); got != want {
					t.Fatalf("cell before the bar is %q, want %q", got, want)
				}
			}
		})
	}
}

func TestTextFieldCursorExPrompt(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.overlay = &exPrompt{input: newTextInput("set")}
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if got := cellText(view, a.cursor.x-1, a.cursor.y); got != "t" {
		t.Fatalf("cell before the bar is %q, want t", got)
	}
}

func TestTextFieldCursorQuickCapture(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.openQuickCapture()
	typeText(a, "ab")
	view := a.View()
	assertFieldBar(t, a, view, " ")
	if got := cellText(view, a.cursor.x-1, a.cursor.y); got != "b" {
		t.Fatalf("cell before the bar is %q, want b", got)
	}
	a.overlay.handleKey(a, vim.Key{Name: "esc"})
	view = a.View()
	if a.cursor.visible {
		t.Fatalf("cursor = %+v in normal mode, want hidden", a.cursor)
	}
	if len(reverseCells(view)) == 0 {
		t.Fatal("capture's block is not drawn in normal mode")
	}
}

func TestTextFieldCursorQuickCaptureVimOff(t *testing.T) {
	a, _ := cursorApp(t, "hello\n", 80)
	a.prefs.VimMode = false
	a.openQuickCapture()
	typeText(a, "ab")
	assertFieldBar(t, a, a.View(), " ")
}
