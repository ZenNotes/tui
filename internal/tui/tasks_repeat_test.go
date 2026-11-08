package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// repeatTasksApp opens a vault holding inbox/note.md with the given body, in
// an App whose task clock reads noon on date.
func repeatTasksApp(t *testing.T, body, date string) (*App, string) {
	t.Helper()
	root := newSessionParityVault(t)
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inbox", "note.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newSessionParityApp(t, root)
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	a.taskNow = func() time.Time { return day.Add(12 * time.Hour) }
	return a, path
}

// repeatTaskAt is the first task the note holds: its first checkbox, or the
// note itself when it is a task note.
func repeatTaskAt(t *testing.T, path string) vault.Task {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opts := vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectApp}
	tasks := vault.ParseTasks("inbox/note.md", "note", vault.FolderInbox, string(raw), opts)
	if len(tasks) == 0 {
		t.Fatalf("note holds no task: %q", raw)
	}
	return tasks[0]
}

func readRepeatNote(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const repeatingLine = "- [ ] Water plants due:2026-11-02 @repeat:mon/thu\n"

func wantRolled(mark string) string {
	return "- [ ] Water plants due:2026-11-05 @repeat:mon/thu\n" +
		"- [" + mark + "] Water plants due:2026-11-02 @repeat:mon/thu\n"
}

func TestTasksViewToggleInsertsNextOccurrence(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
	a.toggleTask(repeatTaskAt(t, path))
	if got := readRepeatNote(t, path); got != wantRolled("x") {
		t.Errorf("note = %q, want %q", got, wantRolled("x"))
	}
}

func TestTasksBoardDoneColumnInsertsNextOccurrence(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
	a.setTaskStatusColumn(repeatTaskAt(t, path), "done")
	if got := readRepeatNote(t, path); got != wantRolled("x") {
		t.Errorf("note = %q, want %q", got, wantRolled("x"))
	}
}

func TestTasksViewCancelInsertsNextOccurrence(t *testing.T) {
	t.Run("tasks view", func(t *testing.T) {
		a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
		a.setTaskCancelled(repeatTaskAt(t, path), true)
		if got := readRepeatNote(t, path); got != wantRolled("-") {
			t.Errorf("note = %q, want %q", got, wantRolled("-"))
		}
	})
	t.Run("editor command", func(t *testing.T) {
		a, _ := repeatTasksApp(t, repeatingLine, "2026-11-02")
		a.openNote("inbox/note.md", true)
		buf := a.activeBuffer()
		if buf == nil {
			t.Fatal("note did not open")
		}
		if err := a.setTaskStateAtCursor(buf, "cancel"); err != nil {
			t.Fatal(err)
		}
		if got := buf.ed.Text(); got != wantRolled("-") {
			t.Errorf("buffer = %q, want %q", got, wantRolled("-"))
		}
	})
	t.Run("reopening inserts nothing", func(t *testing.T) {
		in := "- [-] Water plants due:2026-11-02 @repeat:mon/thu\n"
		a, path := repeatTasksApp(t, in, "2026-11-02")
		a.setTaskCancelled(repeatTaskAt(t, path), false)
		want := "- [ ] Water plants due:2026-11-02 @repeat:mon/thu\n"
		if got := readRepeatNote(t, path); got != want {
			t.Errorf("note = %q, want %q", got, want)
		}
	})
}

func TestTasksViewInProgressDoesNotRoll(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
	a.setTaskInProgress(repeatTaskAt(t, path), true)
	want := "- [/] Water plants due:2026-11-02 @repeat:mon/thu\n"
	if got := readRepeatNote(t, path); got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestForwardingRepeatingTaskInsertsNoOccurrence(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
	a.forwardTask(repeatTaskAt(t, path))
	got := readRepeatNote(t, path)
	if strings.Contains(got, "due:2026-11-05") {
		t.Errorf("forwarding computed an occurrence: %q", got)
	}
}

const repeatingTaskNote = "---\ntags: [task]\nstatus: open\nscheduled: 2026-11-02\ndue: 2026-11-04\nrepeat: 3d\n---\n"

func TestTasksViewToggleAdvancesRepeatingTaskNote(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingTaskNote, "2026-11-02")
	a.toggleTask(repeatTaskAt(t, path))
	got := readRepeatNote(t, path)
	for _, want := range []string{"scheduled: 2026-11-02", "due: 2026-11-07", "status: open", "lastCompleted: 2026-11-02"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "status: done") {
		t.Errorf("note was finished:\n%s", got)
	}
}

func TestTasksViewCancelAdvancesRepeatingTaskNote(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingTaskNote, "2026-11-02")
	a.setTaskCancelled(repeatTaskAt(t, path), true)
	got := readRepeatNote(t, path)
	for _, want := range []string{"scheduled: 2026-11-02", "due: 2026-11-07", "status: open"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "cancelled") {
		t.Errorf("note was cancelled:\n%s", got)
	}
}

func TestTasksViewOnRemoteTargetDoesNotRoll(t *testing.T) {
	a, path := repeatTasksApp(t, repeatingLine, "2026-11-02")
	a.backend = remoteRefreshBackend{Backend: a.backend}
	a.opts.Backend = a.backend
	a.opts.Target = backend.Target{Kind: backend.KindRemote, BaseURL: "https://remote-vault.invalid"}
	task := repeatTaskAt(t, path)
	a.toggleTask(task)
	want := "- [x] Water plants due:2026-11-02 @repeat:mon/thu\n"
	if got := readRepeatNote(t, path); got != want {
		t.Errorf("toggle: note = %q, want %q", got, want)
	}
	if err := os.WriteFile(path, []byte(repeatingLine), 0o644); err != nil {
		t.Fatal(err)
	}
	a.setTaskCancelled(task, true)
	want = "- [-] Water plants due:2026-11-02 @repeat:mon/thu\n"
	if got := readRepeatNote(t, path); got != want {
		t.Errorf("cancel: note = %q, want %q", got, want)
	}

	// A task with a work session and a `skip:@` item is only ticked: the
	// session note stays as it was.
	line := "- [ ] Water plants due:2026-11-02 @repeat:mon/thu skip:@Holiday ^ab12cd\n"
	session := "- Water plants task:[[note#^ab12cd]] when:2026-11-03T09:00-09:15\n"
	plans := filepath.Join(filepath.Dir(path), "Plans.md")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plans, []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}
	a.toggleTask(repeatTaskAt(t, path))
	want = "- [x] Water plants due:2026-11-02 @repeat:mon/thu skip:@Holiday ^ab12cd\n"
	if got := readRepeatNote(t, path); got != want {
		t.Errorf("session toggle: note = %q, want %q", got, want)
	}
	if got := readRepeatNote(t, plans); got != session {
		t.Errorf("session toggle: Plans.md = %q, want %q", got, session)
	}
}

func openRepeatBuffer(t *testing.T, a *App) *noteBuffer {
	t.Helper()
	a.openNote("inbox/note.md", true)
	buf := a.activeBuffer()
	if buf == nil {
		t.Fatal("note did not open")
	}
	return buf
}

func TestEditorToggleCheckboxInsertsNextOccurrence(t *testing.T) {
	a, _ := repeatTasksApp(t, repeatingLine, "2026-11-02")
	buf := openRepeatBuffer(t, a)
	buf.ed.ToggleCheckboxAtCursor()
	if got := buf.ed.Text(); got != wantRolled("x") {
		t.Errorf("buffer = %q, want %q", got, wantRolled("x"))
	}
	if got := buf.ed.Cursor().Line; got != 1 {
		t.Errorf("cursor line = %d, want the ticked line, 1", got)
	}
}

func TestEditorToggleCheckboxOnPlainTaskTicksOnly(t *testing.T) {
	a, _ := repeatTasksApp(t, "- [ ] Plain task due:2026-11-02\n", "2026-11-02")
	buf := openRepeatBuffer(t, a)
	buf.ed.ToggleCheckboxAtCursor()
	if got, want := buf.ed.Text(), "- [x] Plain task due:2026-11-02\n"; got != want {
		t.Errorf("buffer = %q, want %q", got, want)
	}
}

func TestEditorToggleOnRemoteTargetDoesNotRoll(t *testing.T) {
	a, _ := repeatTasksApp(t, repeatingLine, "2026-11-02")
	buf := openRepeatBuffer(t, a)
	a.backend = remoteRefreshBackend{Backend: a.backend}
	a.opts.Backend = a.backend
	a.opts.Target = backend.Target{Kind: backend.KindRemote, BaseURL: "https://remote-vault.invalid"}
	buf.ed.ToggleCheckboxAtCursor()
	if got, want := buf.ed.Text(), "- [x] Water plants due:2026-11-02 @repeat:mon/thu\n"; got != want {
		t.Errorf("buffer = %q, want %q", got, want)
	}
}

func TestEditorToggleRollUndoesInOneStep(t *testing.T) {
	a, _ := repeatTasksApp(t, repeatingLine, "2026-11-02")
	buf := openRepeatBuffer(t, a)
	buf.ed.ToggleCheckboxAtCursor()
	if got := buf.ed.Text(); got != wantRolled("x") {
		t.Fatalf("buffer = %q, want %q", got, wantRolled("x"))
	}
	buf.ed.Feed("u")
	if got := buf.ed.Text(); got != repeatingLine {
		t.Errorf("after one undo buffer = %q, want %q", got, repeatingLine)
	}
}

func TestTypedXInCheckboxDoesNotRoll(t *testing.T) {
	a, _ := repeatTasksApp(t, repeatingLine, "2026-11-02")
	buf := openRepeatBuffer(t, a)
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 3})
	buf.ed.Feed("rx")
	want := "- [x] Water plants due:2026-11-02 @repeat:mon/thu\n"
	if got := buf.ed.Text(); got != want {
		t.Errorf("buffer = %q, want %q", got, want)
	}
}

func TestPlainTaskChangesKeepCRLF(t *testing.T) {
	in := "a\r\n- [ ] x\r\nb\r\n"
	t.Run("tasks view cancel", func(t *testing.T) {
		a, path := repeatTasksApp(t, in, "2026-11-02")
		a.setTaskCancelled(repeatTaskAt(t, path), true)
		if got, want := readRepeatNote(t, path), "a\r\n- [-] x\r\nb\r\n"; got != want {
			t.Errorf("note = %q, want %q", got, want)
		}
	})
	t.Run("board done column", func(t *testing.T) {
		a, path := repeatTasksApp(t, in, "2026-11-02")
		a.setTaskStatusColumn(repeatTaskAt(t, path), "done")
		if got, want := readRepeatNote(t, path), "a\r\n- [x] x\r\nb\r\n"; got != want {
			t.Errorf("note = %q, want %q", got, want)
		}
	})
}

func TestBoardDoneColumnChecksForwardedTask(t *testing.T) {
	a, path := repeatTasksApp(t, "- [>] Water plants due:2026-11-02 @repeat:mon/thu\n", "2026-11-02")
	a.setTaskStatusColumn(repeatTaskAt(t, path), "done")
	want := "- [x] Water plants due:2026-11-02 @repeat:mon/thu\n"
	if got := readRepeatNote(t, path); got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

const (
	untilLine = "- [ ] Water plants due:2026-11-03 @repeat:1d until:2026-11-03\n"
	untilNote = "---\ntags: [task]\nstatus: open\ndue: 2026-11-03\nrepeat: 1d\nuntil: 2026-11-03\n---\n"
)

// On the last date the Tasks view, the board, the editor and a cancel only
// change the one line, and a task note is finished for good.
func TestTasksViewBoardAndEditorStopAtUntil(t *testing.T) {
	ticked := "- [x] Water plants due:2026-11-03 @repeat:1d until:2026-11-03\n"
	cancelled := "- [-] Water plants due:2026-11-03 @repeat:1d until:2026-11-03\n"

	t.Run("tasks view toggle", func(t *testing.T) {
		a, path := repeatTasksApp(t, untilLine, "2026-11-03")
		a.toggleTask(repeatTaskAt(t, path))
		if got := readRepeatNote(t, path); got != ticked {
			t.Errorf("note = %q, want %q", got, ticked)
		}
	})
	t.Run("board done column", func(t *testing.T) {
		a, path := repeatTasksApp(t, untilLine, "2026-11-03")
		a.setTaskStatusColumn(repeatTaskAt(t, path), "done")
		if got := readRepeatNote(t, path); got != ticked {
			t.Errorf("note = %q, want %q", got, ticked)
		}
	})
	t.Run("tasks view cancel", func(t *testing.T) {
		a, path := repeatTasksApp(t, untilLine, "2026-11-03")
		a.setTaskCancelled(repeatTaskAt(t, path), true)
		if got := readRepeatNote(t, path); got != cancelled {
			t.Errorf("note = %q, want %q", got, cancelled)
		}
	})
	t.Run("editor toggle", func(t *testing.T) {
		a, _ := repeatTasksApp(t, untilLine, "2026-11-03")
		buf := openRepeatBuffer(t, a)
		buf.ed.ToggleCheckboxAtCursor()
		if got := buf.ed.Text(); got != ticked {
			t.Errorf("buffer = %q, want %q", got, ticked)
		}
	})
	t.Run("editor cancel", func(t *testing.T) {
		a, _ := repeatTasksApp(t, untilLine, "2026-11-03")
		buf := openRepeatBuffer(t, a)
		if err := a.setTaskStateAtCursor(buf, "cancel"); err != nil {
			t.Fatal(err)
		}
		if got := buf.ed.Text(); got != cancelled {
			t.Errorf("buffer = %q, want %q", got, cancelled)
		}
	})
	t.Run("task note", func(t *testing.T) {
		a, path := repeatTasksApp(t, untilNote, "2026-11-03")
		a.toggleTask(repeatTaskAt(t, path))
		got := readRepeatNote(t, path)
		if !strings.Contains(got, "status: done") || strings.Contains(got, "lastCompleted") {
			t.Errorf("note:\n%s", got)
		}
	})
	t.Run("task note cancel", func(t *testing.T) {
		a, path := repeatTasksApp(t, untilNote, "2026-11-03")
		a.setTaskCancelled(repeatTaskAt(t, path), true)
		got := readRepeatNote(t, path)
		if !strings.Contains(got, "status: cancelled") || strings.Contains(got, "lastCompleted") {
			t.Errorf("note:\n%s", got)
		}
	})
}

// Forwarding a task with a block id moves the id to the open copy and leaves
// the record without one.
func TestForwardingMovesBlockIdToTheCopy(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Write report due:2026-11-06 ^ab12cd\n  - [ ] Draft ^sub1\n  - [x] Outline ^sub2\n", "2026-11-02")
	a.idx.settings.DailyNotes.Enabled = true
	a.forwardTask(repeatTaskAt(t, path))

	record := readRepeatNote(t, path)
	if strings.Contains(record, "^") || !strings.HasPrefix(record, "- [>] Write report due:2026-11-06 [[") {
		t.Errorf("record = %q, want forwarded lines without any id", record)
	}
	_, _, title, _ := a.periodicLocation(periodic.Daily, time.Now())
	var daily string
	err := filepath.WalkDir(filepath.Dir(filepath.Dir(path)), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == title+".md" {
			daily = p
		}
		return err
	})
	if err != nil || daily == "" {
		t.Fatalf("no daily note %q (%v)", title, err)
	}
	raw, err := os.ReadFile(daily)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- [ ] Write report due:2026-11-06 ^ab12cd\n", "  - [ ] Draft ^sub1\n", "  - [x] Outline ^sub2"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("copy lacks %q:\n%s", want, raw)
		}
	}
}
