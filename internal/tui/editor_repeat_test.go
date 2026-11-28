package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

// repeatApp opens the note at rel with body and fixes the clock to 2026-11-04.
func repeatApp(t *testing.T, rel, body string) (*App, *noteBuffer) {
	t.Helper()
	prev := calendarNow
	calendarNow = func() time.Time { return time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local) }
	t.Cleanup(func() { calendarNow = prev })
	a, _ := linkTestApp(t, map[string]string{rel: body})
	a.openNote(rel, true)
	buf := a.activeBuffer()
	if buf == nil || buf.path != rel {
		t.Fatalf("did not open %s", rel)
	}
	return a, buf
}

func rollKey(a *App, buf *noteBuffer, line, col int) {
	buf.ed.SetCursor(vim.Pos{Line: line, Col: col})
	a.handleKey(vim.Ctrl('l'))
}

const weekly = "- [ ] Water plants @repeat:weekly due:2026-11-02"

func TestCtrlLRollsARepeatingTask(t *testing.T) {
	a, buf := repeatApp(t, "n.md", weekly+"\n")
	rollKey(a, buf, 0, 3)
	want := "- [ ] Water plants @repeat:weekly due:2026-11-09\n- [x] Water plants due:2026-11-02\n"
	if buf.ed.Text() != want {
		t.Fatalf("got %q, want %q", buf.ed.Text(), want)
	}
	if c := buf.ed.Cursor(); c.Line != 0 || c.Col != 3 {
		t.Errorf("cursor = %+v, want line 0 col 3", c)
	}
	buf.ed.Feed("u")
	if buf.ed.Text() != weekly+"\n" {
		t.Errorf("one undo must restore the note, got %q", buf.ed.Text())
	}
}

func TestCtrlLRollsInInsertAndVisualMode(t *testing.T) {
	a, buf := repeatApp(t, "n.md", weekly+"\n")
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	buf.ed.Feed("i")
	a.handleKey(vim.Ctrl('l'))
	if !strings.Contains(buf.ed.Text(), "due:2026-11-09\n- [x]") {
		t.Errorf("insert mode: got %q", buf.ed.Text())
	}
	a, buf = repeatApp(t, "n.md", weekly+"\n")
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	buf.ed.Feed("v")
	a.handleKey(vim.Ctrl('l'))
	if !strings.Contains(buf.ed.Text(), "due:2026-11-09\n- [x]") {
		t.Errorf("visual mode: got %q", buf.ed.Text())
	}
}

func TestCtrlLLeavesOtherStatesAlone(t *testing.T) {
	body := "- [x] done @repeat:weekly due:2026-11-02\n- [-] dropped @repeat:weekly\n- [>] moved @repeat:weekly\n- [ ] plain\n- [ ] odd @repeat:sometimes\nword\n"
	a, buf := repeatApp(t, "n.md", body)
	for i := 0; i < 6; i++ {
		rollKey(a, buf, i, 0)
	}
	want := "- [ ] done @repeat:weekly due:2026-11-02\n- [-] dropped @repeat:weekly\n- [>] moved @repeat:weekly\n- [x] plain\n- [x] odd @repeat:sometimes\n- [ ] word\n"
	if buf.ed.Text() != want {
		t.Errorf("got %q, want %q", buf.ed.Text(), want)
	}
}

func TestInProgressRepeatingTaskRolls(t *testing.T) {
	a, buf := repeatApp(t, "n.md", "- [/] Water plants @repeat:weekly due:2026-11-02\n")
	rollKey(a, buf, 0, 0)
	if !strings.HasPrefix(buf.ed.Text(), "- [ ] Water plants @repeat:weekly due:2026-11-09\n- [x] ") {
		t.Errorf("got %q", buf.ed.Text())
	}
}

func TestRollKeepsCursorAtTheEndOfAShorterLine(t *testing.T) {
	a, buf := repeatApp(t, "n.md", "- [ ] a @repeat:daily due:2026-11-02 and more text here\n")
	rollKey(a, buf, 0, 50)
	if c := buf.ed.Cursor(); c.Line != 0 || c.Col >= len([]rune(buf.ed.Line(0))) {
		t.Errorf("cursor = %+v on %q", c, buf.ed.Line(0))
	}
}

func TestRollCountsFromTheDailyNoteDate(t *testing.T) {
	const rel = "Daily Notes/2026-11-10.md"
	const open = "- [ ] Water plants @repeat:weekly\n"
	cases := []struct {
		name  string
		setup func(a *App)
		body  string
		want  string
	}{
		{"daily note date", func(a *App) {}, open, "due:2026-11-17\n"},
		{"settings turn the date off", func(a *App) { a.idx.settings.DailyNotes.TasksDueOnNoteDate = false }, open, "due:2026-11-11\n"},
		{"daily notes off", func(a *App) { a.idx.settings.DailyNotes.Enabled = false }, open, "due:2026-11-11\n"},
		{"line due wins", func(a *App) {}, "- [ ] Water plants @repeat:weekly due:2026-11-02\n", "due:2026-11-09\n"},
		{"frontmatter due wins", func(a *App) {}, "---\ndue: 2026-12-01\n---\n" + open, "due:2026-12-08\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, buf := repeatApp(t, rel, c.body)
			a.idx.settings.DailyNotes.Enabled = true
			a.idx.settings.DailyNotes.TasksDueOnNoteDate = true
			c.setup(a)
			line := strings.Count(c.body, "\n") - 1
			rollKey(a, buf, line, 0)
			if !strings.Contains(buf.ed.Text(), c.want) {
				t.Errorf("got %q, want it to hold %q", buf.ed.Text(), c.want)
			}
		})
	}
}

func TestPreviewToggleRollsARepeatingTask(t *testing.T) {
	a, _ := repeatApp(t, "n.md", "# T\n\n"+weekly+"\n\nafter\n")
	buf := a.activeBuffer()
	a.setPaneMode(modePreview)
	a.View()
	tb := a.activeTab()
	idx := -1
	for i, row := range tb.preview.cache.lines {
		if row.task {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no task row in the preview")
	}
	tb.preview.cursor = idx
	buf.ed.SetCursor(vim.Pos{Line: 4, Col: 1})
	a.previewToggleTask(tb, buf)
	want := "# T\n\n- [ ] Water plants @repeat:weekly due:2026-11-09\n- [x] Water plants due:2026-11-02\n\nafter\n"
	if buf.ed.Text() != want {
		t.Fatalf("got %q, want %q", buf.ed.Text(), want)
	}
	if c := buf.ed.Cursor(); c.Line != 5 || c.Col != 1 {
		t.Errorf("the editor cursor must follow its line: %+v", c)
	}
}

func TestSelectedSubtasksAreDoneInTheRecord(t *testing.T) {
	body := "- [ ] Water plants @repeat:weekly due:2026-11-02\n  - [ ] fill can\n  - [ ] check soil\n  - [/] prune\n"
	a, buf := repeatApp(t, "n.md", body)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
	buf.ed.Feed("Vjj")
	a.handleKey(vim.Ctrl('l'))
	want := "- [ ] Water plants @repeat:weekly due:2026-11-09\n  - [ ] fill can\n  - [ ] check soil\n  - [ ] prune\n" +
		"- [x] Water plants due:2026-11-02\n  - [x] fill can\n  - [x] check soil\n  - [-] prune\n"
	if buf.ed.Text() != want {
		t.Errorf("got %q, want %q", buf.ed.Text(), want)
	}
	buf.ed.Feed("u")
	if buf.ed.Text() != body {
		t.Errorf("one undo must restore the note, got %q", buf.ed.Text())
	}
}

func TestUnselectedSubtasksAreCancelledInTheRecord(t *testing.T) {
	body := "- [ ] Water plants @repeat:weekly due:2026-11-02\n  - [ ] fill can\n"
	a, buf := repeatApp(t, "n.md", body)
	rollKey(a, buf, 0, 0)
	if !strings.Contains(buf.ed.Text(), "- [x] Water plants due:2026-11-02\n  - [-] fill can\n") {
		t.Errorf("got %q", buf.ed.Text())
	}
}

func TestRollOfACRLFNoteLeavesNoStrayCarriageReturn(t *testing.T) {
	a, buf := repeatApp(t, "n.md", strings.ReplaceAll(weekly+"\n  - [ ] sub\n", "\n", "\r\n"))
	rollKey(a, buf, 0, 0)
	if strings.Contains(buf.ed.Text(), "\r") {
		t.Errorf("got %q", buf.ed.Text())
	}
	want := "- [ ] Water plants @repeat:weekly due:2026-11-09\n  - [ ] sub\n- [x] Water plants due:2026-11-02\n  - [-] sub\n"
	if buf.ed.Text() != want {
		t.Errorf("got %q, want %q", buf.ed.Text(), want)
	}
}

func TestSelectedSubtasksStayMatchedPastABlockIDLine(t *testing.T) {
	// The engine drops `^note` from the record and keeps `^-x`, which is not a block id.
	for _, id := range []string{"^note", "^-x"} {
		body := "- [ ] Water plants @repeat:weekly due:2026-11-02\n  - [ ] fill can\n  " + id + "\n  - [ ] check soil\n"
		a, buf := repeatApp(t, "n.md", body)
		buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
		buf.ed.Feed("Vjjj")
		a.handleKey(vim.Ctrl('l'))
		if !strings.Contains(buf.ed.Text(), "  - [x] fill can\n") || !strings.HasSuffix(buf.ed.Text(), "  - [x] check soil\n") {
			t.Errorf("%s: got %q", id, buf.ed.Text())
		}
	}
}

func TestTwoRepeatingTasksRollUnderOneSelection(t *testing.T) {
	body := "- [ ] A @repeat:weekly due:2026-11-02\n  - [ ] a1\n- [ ] B @repeat:daily due:2026-11-03\n  - [ ] b1\n  - [/] b2\nend\n"
	a, buf := repeatApp(t, "n.md", body)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
	buf.ed.Feed("Vjjjj")
	a.handleKey(vim.Ctrl('l'))
	want := "- [ ] A @repeat:weekly due:2026-11-09\n  - [ ] a1\n- [x] A due:2026-11-02\n  - [x] a1\n" +
		"- [ ] B @repeat:daily due:2026-11-05\n  - [ ] b1\n  - [ ] b2\n- [x] B due:2026-11-03\n  - [x] b1\n  - [x] b2\nend\n"
	if buf.ed.Text() != want {
		t.Fatalf("got %q, want %q", buf.ed.Text(), want)
	}
	if c := buf.ed.Cursor(); c.Line != 6 {
		t.Errorf("cursor line = %d, want 6 (the last selected line, in the open occurrence)", c.Line)
	}
	buf.ed.Feed("u")
	if buf.ed.Text() != body {
		t.Errorf("one undo must restore the note, got %q", buf.ed.Text())
	}
	a.handleKey(vim.Ctrl('r'))
	if buf.ed.Text() != want {
		t.Errorf("redo got %q, want %q", buf.ed.Text(), want)
	}
}
