package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// relativeDay writes a date as "today", "today+3" or "today-1", so the golden
// file holds no calendar date and the test can run on any day.
func relativeDay(day string, now time.Time) string {
	d, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return day
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	offset := int(d.Sub(today).Round(24*time.Hour).Hours() / 24)
	switch {
	case offset == 0:
		return "today"
	case offset > 0:
		return fmt.Sprintf("today+%d", offset)
	}
	return fmt.Sprintf("today%d", offset)
}

// defaultTasksVault writes notes that use only the long-standing task syntax,
// with dates relative to now: yesterday, today, three days on, and none.
func defaultTasksVault(t *testing.T, now time.Time) string {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	day := func(offset int) string { return vault.TodayISO(now.AddDate(0, 0, offset)) }
	root := t.TempDir()
	notes := map[string]string{
		"inbox/errands.md": strings.Join([]string{
			"- [ ] Overdue errand due:" + day(-1) + " !high",
			"- [ ] Today errand due:" + day(0) + " !med",
			"- [ ] Soon errand due:" + day(3) + " #home @place:shop",
			"- [ ] Undated errand",
			"- [ ] Waiting errand @waiting due:" + day(0),
			"- [x] Finished errand due:" + day(-1),
			"- [-] Cancelled errand",
			"- [/] In progress errand due:" + day(0) + " !low",
			"- [>] Forwarded errand",
			"",
		}, "\n"),
		"inbox/work.md": strings.Join([]string{
			"---",
			"tags: [work]",
			"---",
			"",
			"- [ ] Low today due:" + day(0) + " !low",
			"- [ ] High soon due:" + day(3) + " !high",
			"- [ ] Medium undated !med",
			"- [ ] Overdue medium due:" + day(-1) + " !med",
			"",
			"```",
			"- [ ] Inside a fence due:" + day(0),
			"```",
			"",
		}, "\n"),
		"inbox/pay-rent.md": strings.Join([]string{
			"---",
			"tags: [task, finance]",
			"status: open",
			"due: " + day(3),
			"priority: high",
			"---",
			"",
			"# Pay rent",
			"",
		}, "\n"),
		"inbox/reference.md": "---\ntasks: false\n---\n\n- [ ] Opted out of tasks\n",
	}
	for rel, body := range notes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// tasksViewReport renders what the Tasks view holds, in order: the list's
// headers and tasks, the board's columns and cards for each grouping, and the
// calendar's tasks per day.
func tasksViewReport(t *testing.T, a *App, now time.Time) string {
	t.Helper()
	var b strings.Builder
	for _, order := range []string{"note", "priority"} {
		a.prefs.TasksListOrder = order
		fmt.Fprintf(&b, "== tasks_list_order %s ==\n", order)
		v := newTasksView(a)
		b.WriteString("-- list\n")
		for _, row := range v.rows {
			if row.task == nil {
				fmt.Fprintf(&b, "%s\n", row.header)
				continue
			}
			fmt.Fprintf(&b, "  %s (%s)\n", row.task.Content, row.task.SourcePath)
		}
		for _, group := range []string{"status", "priority", "due"} {
			a.prefs.KanbanGroupBy = group
			v := newTasksView(a)
			fmt.Fprintf(&b, "-- board by %s\n", group)
			for _, col := range v.columns {
				fmt.Fprintf(&b, "%s (%d)\n", col.title, len(col.cards))
				for _, card := range col.cards {
					fmt.Fprintf(&b, "  %s\n", card.Content)
				}
			}
		}
		b.WriteString("-- calendar\n")
		byDay, undated := bucketCalendarEntries(v.tasks)
		keys := []string{}
		for day := range byDay {
			keys = append(keys, day)
		}
		slices.Sort(keys)
		for _, day := range keys {
			fmt.Fprintf(&b, "%s\n", relativeDay(day, now))
			for _, entry := range byDay[day] {
				fmt.Fprintf(&b, "  %s\n", entry.task.Content)
			}
		}
		b.WriteString("no date\n")
		for _, task := range undated {
			fmt.Fprintf(&b, "  %s\n", task.Content)
		}
	}
	return b.String()
}

// Notes that use none of the scheduling syntax group, order and bucket exactly
// as the Tasks view always has, in the list, on the board and on the calendar.
func TestTasksViewGroupsAndOrderUnchangedWithoutSchedulingSyntax(t *testing.T) {
	now := time.Now()
	a := newSessionParityApp(t, defaultTasksVault(t, now))
	loadTasksForTest(t, a)
	got := tasksViewReport(t, a, now)
	golden := filepath.Join("testdata", "default-tasks-view.golden")
	if os.Getenv("ZN_WRITE_DEFAULT_TASKS_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the Tasks view changed for notes without scheduling syntax.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// loadTasksForTest fills the index with the tasks the app scans at start-up.
func loadTasksForTest(t *testing.T, a *App) {
	t.Helper()
	msg := a.loadTasksCmd()().(tasksLoadedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	a.idx.tasks = msg.tasks
}

// Without a task_note_marker setting, the new-task command writes a note
// tagged `task`, exactly as it always has.
func TestNewTaskNoteKeepsTagMarkerByDefault(t *testing.T) {
	root := defaultTasksVault(t, time.Now())
	a := newSessionParityApp(t, root)
	if err := a.newTaskFile(""); err != nil {
		t.Fatal(err)
	}
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatal("the new-task command did not open its prompt")
	}
	p.onSubmit(a, "Buy milk")
	got, err := os.ReadFile(filepath.Join(root, "inbox", "Buy milk.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "---\ntags: [task]\nstatus: open\n---\n\n# Buy milk\n\n"; string(got) != want {
		t.Fatalf("new task note = %q, want %q", got, want)
	}
}

// newTaskNoteWithMarker writes config.toml with the given [terminal] body,
// reloads the preferences and creates "Buy milk" through the command.
func newTaskNoteWithMarker(t *testing.T, terminal string) string {
	t.Helper()
	root := defaultTasksVault(t, time.Now())
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[terminal]\n"+terminal+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newSessionParityApp(t, root)
	if err := a.reloadPreferences(); err != nil {
		t.Fatal(err)
	}
	if err := a.newTaskFile(""); err != nil {
		t.Fatal(err)
	}
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatal("the new-task command did not open its prompt")
	}
	p.onSubmit(a, "Buy milk")
	got, err := os.ReadFile(filepath.Join(root, "inbox", "Buy milk.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// Choosing the tag marker writes the same note as the default.
func TestNewTaskNoteUsesTagMarkerWhenChosen(t *testing.T) {
	got := newTaskNoteWithMarker(t, `task_note_marker = "tag"`)
	if want := "---\ntags: [task]\nstatus: open\n---\n\n# Buy milk\n\n"; got != want {
		t.Fatalf("new task note = %q, want %q", got, want)
	}
}

// Choosing page-type marks the note with `page-type: task` and no tag.
func TestNewTaskNoteUsesPageTypeMarker(t *testing.T) {
	got := newTaskNoteWithMarker(t, `task_note_marker = "page-type"`)
	if want := "---\npage-type: task\nstatus: open\n---\n\n# Buy milk\n\n"; got != want {
		t.Fatalf("new task note = %q, want %q", got, want)
	}
}

// An unknown marker behaves as the tag marker.
func TestNewTaskNoteFallsBackToTagForUnknownMarker(t *testing.T) {
	got := newTaskNoteWithMarker(t, `task_note_marker = "foo"`)
	if want := "---\ntags: [task]\nstatus: open\n---\n\n# Buy milk\n\n"; got != want {
		t.Fatalf("new task note = %q, want %q", got, want)
	}
}

// writeTaskVault writes the given notes, by relative path, into a new vault.
func writeTaskVault(t *testing.T, notes map[string]string) string {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	for rel, body := range notes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A `scheduled:` token on a checkbox line is plain text: the row shows it, the
// task has no date and the calendar has no entry for it.
func TestCheckboxScheduledShowsInTaskText(t *testing.T) {
	root := writeTaskVault(t, map[string]string{
		"inbox/plans.md": "- [ ] Draft scheduled:2026-11-05T09:30 the report\n",
	})
	a := newSessionParityApp(t, root)
	loadTasksForTest(t, a)
	if len(a.idx.tasks) != 1 {
		t.Fatalf("scanned %d tasks, want 1", len(a.idx.tasks))
	}
	task := a.idx.tasks[0]
	if want := "Draft scheduled:2026-11-05T09:30 the report"; task.Content != want {
		t.Fatalf("content = %q, want %q", task.Content, want)
	}
	byDay, undated := bucketCalendarEntries(a.idx.tasks)
	if len(byDay) != 0 || len(undated) != 1 {
		t.Fatalf("calendar = %v with %d undated, want no entry and 1 undated", byDay, len(undated))
	}
	v := newTasksView(a)
	row := stripAnsi(v.taskLine(a, task, 120, false))
	if !strings.Contains(row, "scheduled:2026-11-05T09:30") {
		t.Errorf("row = %q, want the token in the task text", row)
	}
}

// A `scheduled` key in a task note, with a date or with a date and a time,
// places nothing: the note stays on its due day only and the calendar draws no
// mark for the other day.
func TestTaskNoteScheduledKeyPlacesNothing(t *testing.T) {
	day := time.Date(2026, time.November, 5, 8, 0, 0, 0, time.Local)
	for _, value := range []string{"2026-11-05", "2026-11-05T09:30"} {
		root := writeTaskVault(t, map[string]string{
			"inbox/class.md": "---\ntags: [task]\nstatus: open\nscheduled: " + value + "\ndue: 2026-11-10\n---\n\n# Class\n",
			"inbox/only.md":  "---\ntags: [task]\nstatus: open\nscheduled: " + value + "\n---\n\n# Only\n",
		})
		a := newSessionParityApp(t, root)
		loadTasksForTest(t, a)
		byDay, undated := bucketCalendarEntries(a.idx.tasks)
		if got := byDay["2026-11-05"]; len(got) != 0 {
			t.Errorf("%s: 2026-11-05 lists %d entries, want none", value, len(got))
		}
		if got := byDay["2026-11-10"]; len(got) != 1 || got[0].task.NoteTitle != "class" {
			t.Errorf("%s: 2026-11-10 lists %+v, want the class entry", value, got)
		}
		if len(undated) != 1 || undated[0].NoteTitle != "only" {
			t.Errorf("%s: undated = %+v, want the note with only a scheduled key", value, undated)
		}
		groups := vault.GroupTasks(a.idx.tasks, day)
		if len(groups.Today) != 0 || len(groups.Undated) != 1 {
			t.Errorf("%s: Today has %d tasks and Undated has %d, want 0 and 1", value, len(groups.Today), len(groups.Undated))
		}

		prev := calendarNow
		calendarNow = func() time.Time { return day }
		a.prefs.TasksViewMode = "calendar"
		v := newTasksView(a)
		v.month = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local)
		v.day = time.Date(2026, time.November, 10, 0, 0, 0, 0, time.Local)
		screen := stripAnsi(v.render(a, 120, 40, true))
		calendarNow = prev
		if strings.Contains(screen, "»") {
			t.Errorf("%s: the calendar draws a mark for a scheduled key:\n%s", value, screen)
		}
	}
}
