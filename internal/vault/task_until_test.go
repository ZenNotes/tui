package vault

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func untilTasks(body string) []Task {
	return ParseTasks("n.md", "n", FolderInbox, body, ParseTasksOptions{IncludeExcluded: true, Dialect: DialectApp})
}

func TestParseTasksReadsUntilAndStripsIt(t *testing.T) {
	body := "- [ ] Water plants due:2026-11-02 repeat:1w UNTIL:2026-12-01 until:2027-01-01\n"
	got := untilTasks(body)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Until != "2026-12-01" {
		t.Errorf("Until = %q, want the first token, 2026-12-01", got[0].Until)
	}
	if got[0].Content != "Water plants" {
		t.Errorf("Content = %q, want the tokens removed", got[0].Content)
	}

	note := "---\ntags: [task]\ndue: 2026-11-02\nrepeat: 1w\nuntil: 2026-12-01\n---\nbody\n"
	file := untilTasks(note)
	if len(file) == 0 || file[0].Until != "2026-12-01" {
		t.Errorf("task note Until: %+v", file)
	}
}

func TestUntilWithoutRepeatIsIgnored(t *testing.T) {
	for _, line := range []string{
		"- [ ] A due:2026-11-02 until:2026-12-01",
		"- [ ] A due:2026-11-02 repeat:nonsense until:2026-12-01",
		"- [ ] A due:2026-11-02 repeat:1w until:2026-12-01T10:00",
		"- [ ] A due:2026-11-02 repeat:1w until:2026-02-30",
		"- [ ] A due:2026-11-02 repeat:1w until:soon",
	} {
		got := untilTasks(line + "\n")
		if len(got) != 1 || got[0].Until != "" {
			t.Errorf("%q: %+v, want no Until", line, got)
			continue
		}
		if got[0].Content != "A" {
			t.Errorf("%q: Content = %q, want the token removed", line, got[0].Content)
		}
	}
	note := "---\ntags: [task]\ndue: 2026-11-02\nuntil: 2026-12-01\n---\n"
	if file := untilTasks(note); len(file) == 0 || file[0].Until != "" {
		t.Errorf("task note without a rule: %+v", file)
	}
}

func TestRollStopsAtUntil(t *testing.T) {
	line := "- [ ] T due:2026-11-02 repeat:1d until:2026-11-03"
	// Before the end the roll adds the next copy as before.
	got, inserted := rollLines(t, []string{line}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{
		"- [ ] T due:2026-11-03 repeat:1d until:2026-11-03",
		"- [x] T due:2026-11-02 repeat:1d until:2026-11-03",
	})
	if inserted != 1 {
		t.Errorf("inserted = %d, want 1", inserted)
	}

	// On the last date the task is only ticked or cancelled.
	last := "- [ ] T due:2026-11-03 repeat:1d until:2026-11-03"
	got, inserted = rollLines(t, []string{last}, 0, RollComplete, "2026-11-03")
	equalLines(t, got, []string{"- [x] T due:2026-11-03 repeat:1d until:2026-11-03"})
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}
	got, _ = rollLines(t, []string{last}, 0, RollCancel, "2026-11-03")
	equalLines(t, got, []string{"- [-] T due:2026-11-03 repeat:1d until:2026-11-03"})

	// A late tick, past the until date, ends the task too.
	got, _ = rollLines(t, []string{line}, 0, RollComplete, "2026-11-20")
	equalLines(t, got, []string{"- [x] T due:2026-11-02 repeat:1d until:2026-11-03"})

	// A rule counted from completion stops by the same test.
	done := "- [ ] T due:2026-11-02 repeat:3d/done until:2026-11-04"
	got, _ = rollLines(t, []string{done}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [x] T due:2026-11-02 repeat:3d/done until:2026-11-04"})

	// Without a due date the roll counts from today.
	got, _ = rollLines(t, []string{"- [ ] T repeat:1d until:2026-11-02"}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [x] T repeat:1d until:2026-11-02"})

	// Subtasks are not touched when nothing rolls.
	nested := []string{last, "  - [ ] sub due:2026-11-03"}
	got, _ = rollLines(t, nested, 0, RollComplete, "2026-11-03")
	equalLines(t, got, []string{"- [x] T due:2026-11-03 repeat:1d until:2026-11-03", "  - [ ] sub due:2026-11-03"})
}

func TestRollKeepsUntilOnCopy(t *testing.T) {
	in := "- [ ] T due:2026-11-02 repeat:1w Until:2027-01-01"
	got, _ := rollLines(t, []string{in}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{
		"- [ ] T due:2026-11-09 repeat:1w Until:2027-01-01",
		"- [x] T due:2026-11-02 repeat:1w Until:2027-01-01",
	})
}

func TestTaskNoteCompletesForGoodAtUntil(t *testing.T) {
	in := rollNote("due: 2026-11-03", "repeat: 1d", "until: 2026-11-03")
	next, rolled := RollTaskFileInBody(in, RollComplete, rollNow("2026-11-03"), time.Monday)
	if rolled {
		t.Error("a note on its last date rolled")
	}
	if !strings.Contains(next, "status: done") || !strings.Contains(next, "completedDate: 2026-11-03") {
		t.Errorf("complete: got:\n%s", next)
	}
	for _, line := range []string{"lastCompleted", "due: 2026-11-04"} {
		if strings.Contains(next, line) {
			t.Errorf("complete: %q in:\n%s", line, next)
		}
	}
	if !strings.Contains(next, "due: 2026-11-03") || !strings.Contains(next, "until: 2026-11-03") {
		t.Errorf("complete changed the dates:\n%s", next)
	}

	cancelled, rolled := RollTaskFileInBody(in, RollCancel, rollNow("2026-11-03"), time.Monday)
	if rolled || !strings.Contains(cancelled, "status: cancelled") || strings.Contains(cancelled, "lastCompleted") {
		t.Errorf("cancel: rolled=%v\n%s", rolled, cancelled)
	}

	// Before the end it still rolls.
	early := rollNote("due: 2026-11-02", "repeat: 1d", "until: 2026-11-03")
	if _, rolled := RollTaskFileInBody(early, RollComplete, rollNow("2026-11-02"), time.Monday); !rolled {
		t.Error("a note before its until date did not roll")
	}
}

func TestTaskTokenProblemsListsUntilProblems(t *testing.T) {
	body := strings.Join([]string{
		"- [ ] fine due:2026-11-02 repeat:1w until:2026-12-01",
		"- [ ] bad date due:2026-11-02 repeat:1w until:2026-12-01T10:00",
		"- [ ] no rule due:2026-11-02 until:2026-12-01",
		"- [ ] early due:2026-11-10 repeat:1w until:2026-11-02",
		"- [ ] twice due:2026-11-02 repeat:1w until:2026-12-01 until:2026-12-08",
		"- [ ] not a date until:soon repeat:1w",
	}, "\n")
	problems := TaskTokenProblems("n.md", body)
	var got []string
	var lines []int
	for _, p := range problems {
		if p.Reason == "" {
			t.Errorf("no reason for %+v", p)
		}
		got = append(got, p.Token)
		lines = append(lines, p.Line)
	}
	want := []string{
		"until:2026-12-01T10:00", "until:2026-12-01", "until:2026-11-02", "until:2026-12-08", "until:soon",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n got %q\nwant %q", got, want)
	}
	if !reflect.DeepEqual(lines, []int{2, 3, 4, 5, 6}) {
		t.Errorf("lines = %v", lines)
	}

	note := strings.Join([]string{
		"---", "tags: [task]", "due: 2026-11-10", "repeat: 1w", "until: 2026-11-02", "---", "",
	}, "\n")
	if got := TaskTokenProblems("t.md", note); len(got) != 1 || got[0].Line != 5 || got[0].Token != "until:2026-11-02" {
		t.Errorf("task note: %+v", got)
	}
	noRule := "---\ntags: [task]\nuntil: 2026-11-02\n---\n"
	if got := TaskTokenProblems("t.md", noRule); len(got) != 1 || got[0].Line != 3 {
		t.Errorf("task note without rule: %+v", got)
	}
}
