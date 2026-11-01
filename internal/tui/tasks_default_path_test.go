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
				marker := ""
				if entry.scheduled {
					marker = " (scheduled)"
				}
				fmt.Fprintf(&b, "  %s%s\n", entry.task.Content, marker)
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
