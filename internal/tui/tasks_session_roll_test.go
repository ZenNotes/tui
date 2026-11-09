package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// rollIDRe finds a block id. The id `ab12cd` is the one the tests write.
var rollIDRe = regexp.MustCompile(`\^[a-z0-9]{6}\b`)

// maskRollID replaces the one block id that a roll made with {ID}.
func maskRollID(t *testing.T, text string) string {
	t.Helper()
	made := ""
	return rollIDRe.ReplaceAllStringFunc(text, func(id string) string {
		if id == "^ab12cd" {
			return id
		}
		if made != "" && made != id {
			t.Fatalf("the roll made two different ids in %q", text)
		}
		made = id
		return "^{ID}"
	})
}

// writeRollNotes writes notes, given by vault path, into a vault root.
func writeRollNotes(t *testing.T, root string, notes map[string]string) {
	t.Helper()
	for rel, body := range notes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// sessionRollApp opens a vault that holds the given notes, by vault path, in
// an App whose task clock reads noon on date.
func sessionRollApp(t *testing.T, notes map[string]string, date string) (*App, string) {
	t.Helper()
	root := newSessionParityVault(t)
	writeRollNotes(t, root, notes)
	a := newSessionParityApp(t, root)
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	a.taskNow = func() time.Time { return day.Add(12 * time.Hour) }
	return a, root
}

func readRollNote(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// firstTaskOf is the first checkbox task of a note on disk.
func firstTaskOf(t *testing.T, root, rel string) vault.Task {
	t.Helper()
	body := readRollNote(t, root, rel)
	title := strings.TrimSuffix(filepath.Base(rel), ".md")
	opts := vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectApp}
	for _, task := range vault.ParseTasks(rel, title, vault.FolderInbox, body, opts) {
		if task.Kind == "" {
			return task
		}
	}
	t.Fatalf("%s holds no task", rel)
	return vault.Task{}
}

const (
	rollWork  = "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n"
	rollPlans = "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n"
)

// wantRolledWork is Work.md after a roll on 2026-11-02.
func wantRolledWork(mark string) string {
	return "intro\n- [ ] Water plants @repeat:mon/thu ^{ID}\n- [" + mark + "] Water plants ^ab12cd\noutro\n"
}

const wantRolledPlans = "- Water plants task:[[Work#^ab12cd]] when:2026-11-02T09:00-09:15\n" +
	"- Water plants task:[[Work#^{ID}]] when:2026-11-05T09:00-09:15\n"

func rollNotes() map[string]string {
	return map[string]string{"inbox/Work.md": rollWork, "quick/Plans.md": rollPlans}
}

func checkRolled(t *testing.T, root, mark string) {
	t.Helper()
	if got := maskRollID(t, readRollNote(t, root, "inbox/Work.md")); got != wantRolledWork(mark) {
		t.Errorf("Work.md:\n%s\nwant:\n%s", got, wantRolledWork(mark))
	}
	if got := maskRollID(t, readRollNote(t, root, "quick/Plans.md")); got != wantRolledPlans {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, wantRolledPlans)
	}
}

func TestTasksViewRollCopiesSessions(t *testing.T) {
	t.Run("tasks view toggle", func(t *testing.T) {
		a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
		a.toggleTask(firstTaskOf(t, root, "inbox/Work.md"))
		checkRolled(t, root, "x")
	})
	t.Run("board move to done", func(t *testing.T) {
		a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
		a.setTaskStatusColumn(firstTaskOf(t, root, "inbox/Work.md"), "done")
		checkRolled(t, root, "x")
	})
	t.Run("editor toggle", func(t *testing.T) {
		a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
		a.openNote("inbox/Work.md", true)
		buf := a.activeBuffer()
		buf.ed.GotoLine(2)
		buf.ed.ToggleCheckboxAtCursor()
		if got := maskRollID(t, buf.ed.Text()); got != wantRolledWork("x") {
			t.Errorf("buffer:\n%s\nwant:\n%s", got, wantRolledWork("x"))
		}
		if got := maskRollID(t, readRollNote(t, root, "quick/Plans.md")); got != wantRolledPlans {
			t.Errorf("Plans.md:\n%s\nwant:\n%s", got, wantRolledPlans)
		}
	})
	t.Run("tasks view cancel", func(t *testing.T) {
		a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
		a.setTaskCancelled(firstTaskOf(t, root, "inbox/Work.md"), true)
		checkRolled(t, root, "-")
	})
	t.Run("editor cancel", func(t *testing.T) {
		a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
		a.openNote("inbox/Work.md", true)
		buf := a.activeBuffer()
		buf.ed.GotoLine(2)
		if err := a.setTaskStateAtCursor(buf, "cancel"); err != nil {
			t.Fatal(err)
		}
		if got := maskRollID(t, buf.ed.Text()); got != wantRolledWork("-") {
			t.Errorf("buffer:\n%s\nwant:\n%s", got, wantRolledWork("-"))
		}
		if got := maskRollID(t, readRollNote(t, root, "quick/Plans.md")); got != wantRolledPlans {
			t.Errorf("Plans.md:\n%s\nwant:\n%s", got, wantRolledPlans)
		}
	})
	t.Run("cancel with a skip", func(t *testing.T) {
		notes := map[string]string{
			"inbox/Work.md":  "- [ ] Assignment due:2027-03-12T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^ab12cd\n",
			"quick/Plans.md": "- Assignment task:[[Work#^ab12cd]] when:2027-03-08T09:00-11:00\n",
		}
		a, root := sessionRollApp(t, notes, "2027-03-08")
		a.setTaskCancelled(firstTaskOf(t, root, "inbox/Work.md"), true)
		wantWork := "- [ ] Assignment due:2027-03-22T23:59 @repeat:1w @skip:2027-03-13/2027-03-21 ^{ID}\n" +
			"- [-] Assignment due:2027-03-12T23:59 @skip:2027-03-13/2027-03-21 ^ab12cd\n"
		if got := maskRollID(t, readRollNote(t, root, "inbox/Work.md")); got != wantWork {
			t.Errorf("Work.md:\n%s\nwant:\n%s", got, wantWork)
		}
		wantPlans := "- Assignment task:[[Work#^ab12cd]] when:2027-03-08T09:00-11:00\n" +
			"- Assignment task:[[Work#^{ID}]] when:2027-03-22T09:00-11:00\n"
		if got := maskRollID(t, readRollNote(t, root, "quick/Plans.md")); got != wantPlans {
			t.Errorf("Plans.md:\n%s\nwant:\n%s", got, wantPlans)
		}
	})
}

const (
	skipTask  = "- [ ] Assignment due:2027-03-08T23:59 @repeat:1w @skip:spring-break\n"
	skipEvent = "---\npage-type: event\nwhen: 2027-03-13/2027-03-21\n---\n"
	skipWant  = "- [ ] Assignment due:2027-03-22T23:59 @repeat:1w @skip:spring-break\n" +
		"- [x] Assignment due:2027-03-08T23:59 @skip:spring-break\n"
)

func TestTasksViewRollResolvesNamedSkip(t *testing.T) {
	notes := func() map[string]string {
		return map[string]string{"inbox/Work.md": skipTask, "inbox/Spring Break.md": skipEvent}
	}
	t.Run("tasks view", func(t *testing.T) {
		a, root := sessionRollApp(t, notes(), "2027-03-08")
		a.toggleTask(firstTaskOf(t, root, "inbox/Work.md"))
		if got := readRollNote(t, root, "inbox/Work.md"); got != skipWant {
			t.Errorf("Work.md:\n%s\nwant:\n%s", got, skipWant)
		}
	})
	t.Run("board", func(t *testing.T) {
		a, root := sessionRollApp(t, notes(), "2027-03-08")
		a.setTaskStatusColumn(firstTaskOf(t, root, "inbox/Work.md"), "done")
		if got := readRollNote(t, root, "inbox/Work.md"); got != skipWant {
			t.Errorf("Work.md:\n%s\nwant:\n%s", got, skipWant)
		}
	})
	t.Run("editor", func(t *testing.T) {
		a, _ := sessionRollApp(t, notes(), "2027-03-08")
		a.openNote("inbox/Work.md", true)
		buf := a.activeBuffer()
		buf.ed.ToggleCheckboxAtCursor()
		if got := buf.ed.Text(); got != skipWant {
			t.Errorf("buffer:\n%s\nwant:\n%s", got, skipWant)
		}
	})
}

func TestRollReportsSessionsNotCopied(t *testing.T) {
	notes := map[string]string{
		"inbox/Work.md": "- [ ] T due:2026-11-02 @repeat:1w ^ab12cd\n",
		"inbox/Meeting.md": "---\npage-type: event\nwhen: 2026-11-02T09:00\n" +
			"task: \"[[Work#^ab12cd]]\"\n---\n",
	}
	a, root := sessionRollApp(t, notes, "2026-11-02")
	a.toggleTask(firstTaskOf(t, root, "inbox/Work.md"))
	got := readRollNote(t, root, "inbox/Work.md")
	if !strings.HasPrefix(got, "- [ ] T due:2026-11-09 @repeat:1w\n") {
		t.Errorf("Work.md:\n%s", got)
	}
	if got, want := readRollNote(t, root, "inbox/Meeting.md"), notes["inbox/Meeting.md"]; got != want {
		t.Errorf("event note changed:\n%s", got)
	}
	if a.message != "1 work session in its own note was not copied" {
		t.Errorf("status line = %q", a.message)
	}
}

// makeUnwritable makes a vault folder read only.
func makeUnwritable(t *testing.T, root, folder string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes to a directory that is read only")
	}
	dir := filepath.Join(root, folder)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestRollWarnsWhenCategoryNoteIsNotWritable(t *testing.T) {
	a, root := sessionRollApp(t, rollNotes(), "2026-11-02")
	makeUnwritable(t, root, "quick")
	a.toggleTask(firstTaskOf(t, root, "inbox/Work.md"))
	if got := maskRollID(t, readRollNote(t, root, "inbox/Work.md")); got != wantRolledWork("x") {
		t.Errorf("Work.md:\n%s\nwant:\n%s", got, wantRolledWork("x"))
	}
	if got := readRollNote(t, root, "quick/Plans.md"); got != rollPlans {
		t.Errorf("Plans.md changed:\n%s", got)
	}
	if !strings.Contains(a.message, "Plans.md") || !a.messageErr {
		t.Errorf("status line = %q (error %v)", a.message, a.messageErr)
	}
}

// A roll warning from the vault goes to the status line. The terminal screen
// is active, so nothing may reach stderr.
func TestRollWarningsStayOffStderr(t *testing.T) {
	root := newSessionParityVault(t)
	writeRollNotes(t, root, rollNotes())
	makeUnwritable(t, root, "quick")
	var stderr bytes.Buffer
	target := backend.Target{Kind: backend.KindLocal, Root: root}
	b, err := backend.New(target, backend.Options{
		Now:         func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local) },
		TaskWarning: func(message string) { stderr.WriteString("warning: " + message + "\n") },
	})
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(context.Background(), Options{Backend: b, Target: target}, config.DefaultPrefs(), true)
	if _, err := b.ToggleTask(context.Background(), "inbox/Work.md#^ab12cd", vault.DialectApp); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
	if !strings.Contains(a.message, "Plans.md") {
		t.Errorf("status line = %q", a.message)
	}
}
