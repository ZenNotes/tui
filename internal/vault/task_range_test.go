package vault

import (
	"strings"
	"testing"
)

func TestParseTasksReadsSameDayRangeOnDue(t *testing.T) {
	got := scanOne(t, "- [ ] Call Ana due:2026-11-03T14:00-15:30", DialectApp)
	if got.Due != "2026-11-03" || got.DueTime != "14:00" || got.DueEnd != "15:30" {
		t.Fatalf("line task: %+v", got)
	}
	body := "---\ntags: [task]\ndue: 2026-11-04T09:00-09:45\n---\n\n# Report\n"
	tasks := ParseTasks("inbox/r.md", "Report", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	if f := tasks[0]; f.Due != "2026-11-04" || f.DueTime != "09:00" || f.DueEnd != "09:45" {
		t.Fatalf("file task: %+v", f)
	}
	inherited := "---\ndue: 2026-11-04T09:00-09:45\n---\n\n- [ ] Call Ana\n"
	tasks = ParseTasks("inbox/n.md", "n", FolderInbox, inherited, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].DueTime != "09:00" || tasks[0].DueEnd != "09:45" {
		t.Fatalf("inherited: %+v", tasks)
	}
}

func TestParseTasksIgnoresReversedOrBadRangeEnd(t *testing.T) {
	for _, tc := range []struct{ value, time string }{
		{"2026-11-03T15:30-14:00", "15:30"},
		{"2026-11-03T14:00-14:00", "14:00"},
		{"2026-11-03T23:00-01:00", "23:00"},
		{"2026-11-03T14:00-25:00", "14:00"},
		{"2026-11-03T14:00-9:30", "14:00"},
		{"2026-11-03T14:00-24:00", "14:00"},
		{"2026-11-03T14:00-", "14:00"},
		{"2026-11-03T25:00-26:00", ""},
	} {
		got := scanOne(t, "- [ ] T due:"+tc.value, DialectApp)
		if got.Due != "2026-11-03" || got.DueTime != tc.time || got.DueEnd != "" {
			t.Errorf("%s: %+v", tc.value, got)
		}
	}
}

func TestParseTasksDropsRangeWithoutStartTime(t *testing.T) {
	for _, value := range []string{"2026-11-03-15:30", "2026-11-03T-15:30"} {
		got := scanOne(t, "- [ ] T due:"+value, DialectApp)
		if got.Due != "2026-11-03" || got.DueTime != "" || got.DueEnd != "" {
			t.Errorf("%s: %+v", value, got)
		}
	}
}

func TestParseTasksStripsRangedDueFromContent(t *testing.T) {
	for _, dialect := range []TaskDialect{DialectApp, DialectCLI} {
		got := scanOne(t, "- [ ] Meet due:2026-11-03T14:00-15:30 Ana", dialect)
		if got.Content != "Meet Ana" {
			t.Errorf("dialect %d: content %q", dialect, got.Content)
		}
	}
}

func TestDueAcrossDaysKeepsOnlyItsStartDate(t *testing.T) {
	got := scanOne(t, "- [ ] Essay due:2026-11-03/2026-11-05", DialectApp)
	if got.Due != "2026-11-03" || got.DueTime != "" || got.DueEnd != "" || got.Content != "Essay" {
		t.Fatalf("line task: %+v", got)
	}
	body := "---\ntags: [task]\ndue: 2026-11-03T09:00/2026-11-05\n---\n\n# Essay\n"
	tasks := ParseTasks("inbox/e.md", "Essay", FolderInbox, body, ParseTasksOptions{})
	if len(tasks) != 1 || tasks[0].Due != "2026-11-03" || tasks[0].DueTime != "" || tasks[0].DueEnd != "" {
		t.Fatalf("file task: %+v", tasks)
	}
}

func TestSetTaskDueKeepsExistingRange(t *testing.T) {
	got := SetTaskDue("- [ ] Call Ana due:2026-11-03T14:00-15:30 !high\n", 0, "2026-11-10")
	if want := "- [ ] Call Ana !high due:2026-11-10T14:00-15:30\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	note := "---\ntags: [task]\ndue: 2026-11-03T14:00-15:30\n---\n"
	got = SetTaskNoteDue(note, "2026-11-10")
	if !strings.Contains(got, "due: 2026-11-10T14:00-15:30\n") {
		t.Fatalf("note: %q", got)
	}
	// A value with its own time replaces the range.
	got = SetTaskDue("- [ ] Call Ana due:2026-11-03T14:00-15:30\n", 0, "2026-11-10T09:00")
	if want := "- [ ] Call Ana due:2026-11-10T09:00\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRollKeepsRanges(t *testing.T) {
	lines := []string{
		"- [ ] Weekly review due:2026-11-02 @repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [x] Send summary due:2026-11-04T17:00-18:00",
	}
	got, _ := rollLines(t, lines, 0, RollComplete, "2026-11-02")
	want := []string{
		"- [ ] Weekly review due:2026-11-09 @repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [ ] Send summary due:2026-11-04T17:00-18:00",
	}
	equalLines(t, got[:4], want)

	got, _ = rollLines(t, []string{"- [ ] T due:2026-11-02T09:00-09:15 @repeat:1d"}, 0, RollComplete, "2026-11-02")
	if got[0] != "- [ ] T due:2026-11-03T09:00-09:15 @repeat:1d" {
		t.Fatalf("got %q", got[0])
	}
}

func TestTaskTokenProblemsListsBadRanges(t *testing.T) {
	body := strings.Join([]string{
		"# Plan",
		"- [ ] a due:2026-11-03T15:30-14:00",
		"- [ ] b due:2026-11-03T14:00",
		"- [ ] c due:2026-11-03T14:00-25:00",
		"- [ ] d due:2026-11-03T14:00-15:30",
		"- [ ] e due:2026-11-03-15:30",
		"- [ ] f due:2026-11-03/2026-11-05",
	}, "\n")
	got := problemSummary(TaskTokenProblems("d.md", body))
	want := []string{
		"d.md:2 due:2026-11-03T15:30-14:00",
		"d.md:4 due:2026-11-03T14:00-25:00",
		"d.md:6 due:2026-11-03-15:30",
		"d.md:7 due:2026-11-03/2026-11-05",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	for _, p := range TaskTokenProblems("d.md", body) {
		if strings.TrimSpace(p.Reason) == "" {
			t.Errorf("%s has no reason", p.Token)
		}
	}
	note := "---\ntags: [task]\ndue: 2026-11-03T15:30-14:00\n---\n"
	if got := TaskTokenProblems("f.md", note); len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("frontmatter: %+v", got)
	}
}
