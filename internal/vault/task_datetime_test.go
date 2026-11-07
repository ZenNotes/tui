package vault

import (
	"strings"
	"testing"
)

func scanOne(t *testing.T, line string, dialect TaskDialect) Task {
	t.Helper()
	tasks := ParseTasks("inbox/n.md", "n", FolderInbox, line+"\n", ParseTasksOptions{Dialect: dialect})
	if len(tasks) != 1 {
		t.Fatalf("%q scanned to %d tasks, want 1", line, len(tasks))
	}
	return tasks[0]
}

func TestParseTasksReadsTimeOnDue(t *testing.T) {
	timed := scanOne(t, "- [ ] Call Ana due:2026-11-03T14:00", DialectApp)
	if timed.Due != "2026-11-03" || timed.DueTime != "14:00" {
		t.Fatalf("timed due: got %q %q", timed.Due, timed.DueTime)
	}
	if timed.Content != "Call Ana" {
		t.Fatalf("content: %q", timed.Content)
	}
	plain := scanOne(t, "- [ ] Call Ana due:2026-11-03", DialectApp)
	if plain.Due != "2026-11-03" || plain.DueTime != "" {
		t.Fatalf("date-only due: got %q %q", plain.Due, plain.DueTime)
	}
}

func TestParseTaskFileReadsFrontmatterTimes(t *testing.T) {
	body := "---\ntags: [task]\ndue: 2026-11-03T14:00\n---\n\n# Report\n"
	tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	got := tasks[0]
	if got.Due != "2026-11-03" || got.DueTime != "14:00" || got.Scheduled != "" {
		t.Fatalf("file task: %+v", got)
	}
}

func TestParseTasksCarriesTimedFrontmatterDueToCheckboxes(t *testing.T) {
	body := "---\ndue: 2026-11-03T14:00\n---\n\n- [ ] Call Ana\n"
	tasks := ParseTasks("inbox/n.md", "n", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Due != "2026-11-03" || tasks[0].DueTime != "14:00" {
		t.Fatalf("got %+v", tasks)
	}
}

func TestParseTasksKeepsDateWhenTimeIsInvalid(t *testing.T) {
	for _, tc := range []struct{ line, due string }{
		{"- [ ] Call Ana due:2026-11-03T25:00", "2026-11-03"},
		{"- [ ] Call Ana due:2026-11-03T14:60", "2026-11-03"},
		{"- [ ] Call Ana due:2026-11-03T14", "2026-11-03"},
	} {
		got := scanOne(t, tc.line, DialectApp)
		if got.Due != tc.due || got.DueTime != "" {
			t.Errorf("%q: %+v", tc.line, got)
		}
	}
}

// A `scheduled:` token is plain text: it stays in the content and sets no date.
func TestParseTasksLeavesScheduledTokenInContent(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Draft scheduled:2026-11-05T09:30 the report", dialect)
		if got.Content != "Draft scheduled:2026-11-05T09:30 the report" {
			t.Errorf("dialect %d: content %q", dialect, got.Content)
		}
		if got.Scheduled != "" || got.Due != "" {
			t.Errorf("dialect %d: scheduled token set a date: %+v", dialect, got)
		}
	}
}

// A task note never gets a scheduled date from its frontmatter, with or
// without a time.
func TestParseTaskFileIgnoresScheduledKey(t *testing.T) {
	for _, value := range []string{"2026-11-05", "2026-11-05T09:30"} {
		body := "---\ntags: [task]\nscheduled: " + value + "\n---\n\n# Report\n"
		tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
		if len(tasks) != 1 || tasks[0].Scheduled != "" || tasks[0].Due != "" {
			t.Fatalf("%s: %+v", value, tasks)
		}
	}
}

func TestSplitDateTimeRejectsMalformedValues(t *testing.T) {
	for _, in := range []string{"", "2026-11-0", "2026-02-30", "2026-11-03x", "2026-11-03 14:00", "2026-11-03t14:00"} {
		if d, c := SplitDateTime(in); d != "" || c != "" {
			t.Errorf("%q: got %q %q", in, d, c)
		}
	}
}

func TestSetTaskDueKeepsExistingTime(t *testing.T) {
	got := SetTaskDue("- [ ] Call Ana due:2026-11-03T14:00 !high\n", 0, "2026-11-10")
	if want := "- [ ] Call Ana !high due:2026-11-10T14:00\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := SetTaskDue("- [ ] Call Ana\n", 0, "2026-11-10"); got != "- [ ] Call Ana due:2026-11-10\n" {
		t.Fatalf("untimed: %q", got)
	}
	if got := SetTaskDue("- [ ] Call Ana due:2026-11-03T99:00\n", 0, "2026-11-10"); got != "- [ ] Call Ana due:2026-11-10\n" {
		t.Fatalf("invalid time must not be carried: %q", got)
	}
}

func TestSetTaskDueWithTimeReplacesTime(t *testing.T) {
	got := SetTaskDue("- [ ] Call Ana due:2026-11-03T14:00\n", 0, "2026-11-10T08:15")
	if want := "- [ ] Call Ana due:2026-11-10T08:15\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = SetTaskDue("- [ ] Call Ana due:2026-11-03\n", 0, "2026-11-10T08:15")
	if want := "- [ ] Call Ana due:2026-11-10T08:15\n"; got != want {
		t.Fatalf("date-only before: got %q, want %q", got, want)
	}
}

func TestSetTaskDueClearRemovesTime(t *testing.T) {
	got := SetTaskDue("- [ ] Call Ana due:2026-11-03T14:00 !high\n", 0, "")
	if want := "- [ ] Call Ana !high\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSetTaskNoteDueKeepsTime(t *testing.T) {
	body := "---\ntags: [task]\nstatus: open\ndue: 2026-11-03T14:00\n---\n\n# Pay rent\n"
	if got, want := SetTaskNoteDue(body, "2026-11-10"), strings.Replace(body, "2026-11-03T14:00", "2026-11-10T14:00", 1); got != want {
		t.Fatalf("date-only: got %q, want %q", got, want)
	}
	if got, want := SetTaskNoteDue(body, "2026-11-10T08:15"), strings.Replace(body, "2026-11-03T14:00", "2026-11-10T08:15", 1); got != want {
		t.Fatalf("date-time: got %q, want %q", got, want)
	}
	if got, want := SetTaskNoteDue(body, ""), "---\ntags: [task]\nstatus: open\n---\n\n# Pay rent\n"; got != want {
		t.Fatalf("clear: got %q, want %q", got, want)
	}
	quoted := strings.Replace(body, "2026-11-03T14:00", `"2026-11-03T14:00"`, 1)
	if got, want := SetTaskNoteDue(quoted, "2026-11-10"), strings.Replace(body, "2026-11-03T14:00", `"2026-11-10T14:00"`, 1); got != want {
		t.Fatalf("quoted: got %q, want %q", got, want)
	}
}
