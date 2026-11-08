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
		if d, c, e := SplitDateTime(in); d != "" || c != "" || e != "" {
			t.Errorf("%q: got %q %q %q", in, d, c, e)
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

func TestParseWhenValueReadsAllDayRange(t *testing.T) {
	sd, st, ed, et, problem := ParseWhenValue("2027-03-13/2027-03-21")
	if sd != "2027-03-13" || st != "" || ed != "2027-03-21" || et != "" || problem != "" {
		t.Fatalf("got %q %q %q %q %q", sd, st, ed, et, problem)
	}
	if got := FormatWhenValue(sd, st, ed, et); got != "2027-03-13 – 2027-03-21" {
		t.Fatalf("display: %q", got)
	}
}

func TestParseWhenValueReadsTimedRangeAcrossMidnight(t *testing.T) {
	sd, st, ed, et, problem := ParseWhenValue("2026-11-03T22:00/2026-11-04T02:00")
	if sd != "2026-11-03" || st != "22:00" || ed != "2026-11-04" || et != "02:00" || problem != "" {
		t.Fatalf("got %q %q %q %q %q", sd, st, ed, et, problem)
	}
	if got := FormatWhenValue(sd, st, ed, et); got != "2026-11-03 22:00 – 2026-11-04 02:00" {
		t.Fatalf("display: %q", got)
	}
}

func TestParseWhenValueSameDaySlashEqualsDashForm(t *testing.T) {
	type parts [5]string
	read := func(s string) parts {
		a, b, c, d, e := ParseWhenValue(s)
		return parts{a, b, c, d, e}
	}
	slash, dash := read("2026-11-03T14:00/2026-11-03T15:30"), read("2026-11-03T14:00-15:30")
	if slash != dash || slash != (parts{"2026-11-03", "14:00", "2026-11-03", "15:30", ""}) {
		t.Fatalf("timed: slash %q, dash %q", slash, dash)
	}
	if got, want := read("2026-11-03/2026-11-03"), read("2026-11-03"); got != want {
		t.Fatalf("date: got %q, want %q", got, want)
	}
}

func TestParseWhenValueKeepsStartWhenEndIsInvalid(t *testing.T) {
	cases := []struct{ value, date, start string }{
		{"2026-11-04/2026-11-03", "2026-11-04", ""},
		{"2026-11-03T22:00/2026-11-03T21:00", "2026-11-03", "22:00"},
		{"2026-11-03T22:00/2026-11-04", "2026-11-03", "22:00"},
		{"2026-11-03/2026-11-04T02:00", "2026-11-03", ""},
		{"2026-11-03/2026-02-30", "2026-11-03", ""},
		{"2026-11-03T22:00/2026-11-04T25:00", "2026-11-03", "22:00"},
	}
	for _, c := range cases {
		sd, st, ed, et, problem := ParseWhenValue(c.value)
		if sd != c.date || st != c.start || ed != "" || et != "" {
			t.Errorf("%s: got %q %q %q %q", c.value, sd, st, ed, et)
		}
		if problem == "" {
			t.Errorf("%s: no problem text", c.value)
		}
	}
}

func TestParseWhenValueSuggestsSlashForSameDayEndBeforeStart(t *testing.T) {
	sd, st, ed, et, problem := ParseWhenValue("2026-11-03T23:00-01:00")
	if sd != "2026-11-03" || st != "23:00" || ed != "" || et != "" {
		t.Fatalf("got %q %q %q %q", sd, st, ed, et)
	}
	if !strings.Contains(problem, "2026-11-03T23:00/2026-11-04T01:00") {
		t.Fatalf("problem: %q", problem)
	}
}

func TestShiftWhenValueKeepsTimesAndLength(t *testing.T) {
	cases := []struct {
		value string
		days  int
		want  string
	}{
		{"2026-11-03", 2, "2026-11-05"},
		{"2026-11-03T14:00", -3, "2026-10-31T14:00"},
		{"2026-11-03T14:00-15:30", 1, "2026-11-04T14:00-15:30"},
		{"2027-03-13/2027-03-21", 20, "2027-04-02/2027-04-10"},
		{"2026-11-03T22:00/2026-11-04T02:00", 1, "2026-11-04T22:00/2026-11-05T02:00"},
		{"2026-11-04/2026-11-03", 1, "2026-11-04/2026-11-03"},
	}
	for _, c := range cases {
		if got := ShiftWhenValue(c.value, c.days); got != c.want {
			t.Errorf("%s by %d: got %q, want %q", c.value, c.days, got, c.want)
		}
	}
}

func TestWhenWithDotsKeepsItsStart(t *testing.T) {
	for _, value := range []string{"2027-03-13..2027-03-21", "2026-11-03T22:00..2026-11-04T02:00"} {
		sd, st, ed, et, problem := ParseWhenValue(value)
		if sd != value[:10] || ed != "" || et != "" || problem == "" {
			t.Errorf("%s: got %q %q %q %q %q", value, sd, st, ed, et, problem)
		}
		if want := map[bool]string{true: "22:00", false: ""}[strings.Contains(value, "T")]; st != want {
			t.Errorf("%s: start time %q, want %q", value, st, want)
		}
	}
}
