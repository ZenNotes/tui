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

func TestParseTasksReadsScheduledWithAndWithoutTime(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		timed := scanOne(t, "- [ ] Draft report scheduled:2026-11-05T09:30", dialect)
		if timed.Scheduled != "2026-11-05" || timed.ScheduledTime != "09:30" {
			t.Fatalf("timed scheduled: got %q %q", timed.Scheduled, timed.ScheduledTime)
		}
		plain := scanOne(t, "- [ ] Draft report scheduled:2026-11-05", dialect)
		if plain.Scheduled != "2026-11-05" || plain.ScheduledTime != "" {
			t.Fatalf("date-only scheduled: got %q %q", plain.Scheduled, plain.ScheduledTime)
		}
		both := scanOne(t, "- [ ] Draft report due:2026-11-10 scheduled:2026-11-05T09:30", dialect)
		if both.Due != "2026-11-10" || both.DueTime != "" || both.Scheduled != "2026-11-05" || both.ScheduledTime != "09:30" {
			t.Fatalf("both: %+v", both)
		}
	}
}

func TestParseTaskFileReadsFrontmatterTimes(t *testing.T) {
	body := "---\ntags: [task]\ndue: 2026-11-03T14:00\nscheduled: 2026-11-05T09:30\n---\n\n# Report\n"
	tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	got := tasks[0]
	if got.Due != "2026-11-03" || got.DueTime != "14:00" || got.Scheduled != "2026-11-05" || got.ScheduledTime != "09:30" {
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
	for _, tc := range []struct{ line, due, sched string }{
		{"- [ ] Call Ana due:2026-11-03T25:00", "2026-11-03", ""},
		{"- [ ] Call Ana due:2026-11-03T14:60", "2026-11-03", ""},
		{"- [ ] Call Ana due:2026-11-03T14", "2026-11-03", ""},
		{"- [ ] Draft report scheduled:2026-11-05T9:30", "", "2026-11-05"},
	} {
		got := scanOne(t, tc.line, DialectApp)
		if got.Due != tc.due || got.DueTime != "" || got.Scheduled != tc.sched || got.ScheduledTime != "" {
			t.Errorf("%q: %+v", tc.line, got)
		}
	}
}

func TestParseTasksStripsScheduledFromContent(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Draft scheduled:2026-11-05 the report", dialect)
		if got.Content != "Draft the report" {
			t.Errorf("dialect %d: content %q", dialect, got.Content)
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
