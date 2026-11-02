package vault

import (
	"strings"
	"testing"
	"time"
)

// rollNow is the fixed clock the roll tests run at.
func rollNow(date string) time.Time {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, time.Local)
}

func rollLines(t *testing.T, lines []string, index int, action RollAction, today string) ([]string, int) {
	t.Helper()
	body := strings.Join(lines, "\n")
	next, inserted, ok := RollTaskInBody(body, index, DialectCLI, action, rollNow(today), time.Monday)
	if !ok {
		t.Fatalf("RollTaskInBody(%q, %d) reported no task", body, index)
	}
	return strings.Split(next, "\n"), inserted
}

func equalLines(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines differ\n got: %q\nwant: %q", got, want)
	}
}

func TestRollInsertsNextOccurrenceAboveCompletedTask(t *testing.T) {
	in := []string{"intro", "- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu", "outro"}
	want := []string{
		"intro",
		"- [ ] Water plants scheduled:2026-11-05 repeat:mon,thu",
		"- [x] Water plants scheduled:2026-11-02 repeat:mon,thu",
		"outro",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}

	// A CRLF note is normalised the way the plain toggle normalises it, and no
	// further.
	crlf := strings.Join(in, "\r\n")
	next, _, ok := RollTaskInBody(crlf, 0, DialectCLI, RollComplete, rollNow("2026-11-02"), time.Monday)
	if !ok {
		t.Fatal("CRLF note: no task")
	}
	if next != strings.Join(want, "\n") {
		t.Fatalf("CRLF note: got %q", next)
	}
}

func TestRollCancelInsertsNextOccurrence(t *testing.T) {
	in := []string{"intro", "- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu", "outro"}
	want := []string{
		"intro",
		"- [ ] Water plants scheduled:2026-11-05 repeat:mon,thu",
		"- [-] Water plants scheduled:2026-11-02 repeat:mon,thu",
		"outro",
	}
	got, inserted := rollLines(t, in, 0, RollCancel, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}
}

func TestRollUntickLeavesInsertedOccurrence(t *testing.T) {
	in := []string{
		"intro",
		"- [ ] Water plants scheduled:2026-11-05 repeat:mon,thu",
		"- [x] Water plants scheduled:2026-11-02 repeat:mon,thu",
		"outro",
	}
	got, inserted := rollLines(t, in, 1, RollComplete, "2026-11-02")
	want := append([]string(nil), in...)
	want[2] = "- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu"
	equalLines(t, got, want)
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}

	// Un-cancelling leaves the occurrence alone as well.
	in[2] = "- [-] Water plants scheduled:2026-11-02 repeat:mon,thu"
	got, inserted = rollLines(t, in, 1, RollCancel, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 0 {
		t.Fatalf("un-cancel inserted = %d, want 0", inserted)
	}
}

func TestRollCopiesSubtasksUnticked(t *testing.T) {
	in := []string{
		"- [ ] Weekly review scheduled:2026-11-02 repeat:1w",
		"  - [x] Inbox zero",
		"  - [ ] Plan week",
	}
	want := []string{
		"- [ ] Weekly review scheduled:2026-11-09 repeat:1w",
		"  - [ ] Inbox zero",
		"  - [ ] Plan week",
		"- [x] Weekly review scheduled:2026-11-02 repeat:1w",
		"  - [x] Inbox zero",
		"  - [ ] Plan week",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 3 task lines", inserted)
	}
}

func TestRollCopiesChildLinesAndShiftsSubtaskDates(t *testing.T) {
	in := []string{
		"- [ ] Weekly review scheduled:2026-11-02 repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [x] Send summary due:2026-11-04T17:00",
	}
	want := []string{
		"- [ ] Weekly review scheduled:2026-11-09 repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [ ] Send summary due:2026-11-11T17:00",
		"- [x] Weekly review scheduled:2026-11-02 repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [x] Send summary due:2026-11-04T17:00",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2 task lines", inserted)
	}
}

func TestRollAddsScheduledWhenTaskHasNoDate(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T repeat:3d"}, 0, RollComplete, "2026-11-02")
	want := []string{"- [ ] T repeat:3d scheduled:2026-11-05", "- [x] T repeat:3d"}
	equalLines(t, got, want)
	if strings.Contains(got[0], "due:") {
		t.Fatalf("inserted line gained a due date: %q", got[0])
	}
}

func TestRollWithoutRepeatOnlyTicks(t *testing.T) {
	for _, line := range []string{
		"- [ ] Pay rent due:2026-11-03",
		"- [ ] A due:2027-03-15 skip:2027-03-13..2027-03-21",
	} {
		got, inserted := rollLines(t, []string{line}, 0, RollComplete, "2026-11-02")
		equalLines(t, got, []string{strings.Replace(line, "[ ]", "[x]", 1)})
		if inserted != 0 {
			t.Fatalf("%q inserted %d lines", line, inserted)
		}
	}
}

func TestRollWithInvalidRuleOnlyTicks(t *testing.T) {
	line := "- [ ] T scheduled:2026-11-02 repeat:fortnightly"
	got, inserted := rollLines(t, []string{line}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [x] T scheduled:2026-11-02 repeat:fortnightly"})
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}
}

func TestRollMovesOtherDateBySameOffset(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T scheduled:2026-11-02 due:2026-11-04 repeat:mon,thu"}, 0, RollComplete, "2026-11-02")
	if got[0] != "- [ ] T scheduled:2026-11-05 due:2026-11-07 repeat:mon,thu" {
		t.Fatalf("got %q", got[0])
	}

	// From the completion date, the offset is still measured from the base
	// field's own value.
	got, _ = rollLines(t, []string{"- [ ] T scheduled:2026-11-02 due:2026-11-03 repeat:3d/done"}, 0, RollComplete, "2026-11-04")
	if got[0] != "- [ ] T scheduled:2026-11-07 due:2026-11-08 repeat:3d/done" {
		t.Fatalf("done-counted: got %q", got[0])
	}
}

func TestRollCountsFromDueWhenNoScheduled(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T due:2026-11-02 repeat:3d"}, 0, RollComplete, "2026-11-02")
	if got[0] != "- [ ] T due:2026-11-05 repeat:3d" {
		t.Fatalf("got %q", got[0])
	}
	if strings.Contains(got[0], "scheduled:") {
		t.Fatalf("inserted line gained a scheduled date: %q", got[0])
	}
}

func TestRollKeepsTimes(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T scheduled:2026-11-02T09:30 due:2026-11-02T17:00 repeat:1d"}, 0, RollComplete, "2026-11-02")
	if got[0] != "- [ ] T scheduled:2026-11-03T09:30 due:2026-11-03T17:00 repeat:1d" {
		t.Fatalf("got %q", got[0])
	}

	in := "- [ ] Assignment scheduled:2027-03-08T09:00 due:2027-03-12T23:59 repeat:1w skip:2027-03-13..2027-03-21"
	got, _ = rollLines(t, []string{in}, 0, RollComplete, "2027-03-08")
	want := "- [ ] Assignment scheduled:2027-03-22T09:00 due:2027-03-26T23:59 repeat:1w skip:2027-03-13..2027-03-21"
	if got[0] != want {
		t.Fatalf("got %q\nwant %q", got[0], want)
	}
}

func TestRollKeepsSkipTokensOnInsertedLine(t *testing.T) {
	cases := []struct{ line, next string }{
		{"- [ ] A scheduled:2027-03-08 repeat:1w skip:2027-03-21..2027-03-13", "- [ ] A scheduled:2027-03-15 repeat:1w skip:2027-03-21..2027-03-13"},
		{"- [ ] A scheduled:2027-03-08 repeat:1w skip:2027-02-30", "- [ ] A scheduled:2027-03-15 repeat:1w skip:2027-02-30"},
		{"- [ ] A scheduled:2027-03-08 repeat:1w skip:2026-12-21..2027-01-03", "- [ ] A scheduled:2027-03-15 repeat:1w skip:2026-12-21..2027-01-03"},
		{"- [ ] A scheduled:2027-03-08 repeat:1w skip:@spring-break", "- [ ] A scheduled:2027-03-15 repeat:1w skip:@spring-break"},
		{"- [ ] A scheduled:2027-03-08 repeat:1w skip:2027-03-21..2027-03-13,2027-03-15", "- [ ] A scheduled:2027-03-22 repeat:1w skip:2027-03-21..2027-03-13,2027-03-15"},
	}
	for _, c := range cases {
		got, _ := rollLines(t, []string{c.line}, 0, RollComplete, "2027-03-08")
		if got[0] != c.next {
			t.Errorf("%q\n got %q\nwant %q", c.line, got[0], c.next)
		}
	}
}

func TestRollCountsTasksPastFencesAndLeavesOthersAlone(t *testing.T) {
	in := []string{
		"```",
		"- [ ] not a task repeat:1d scheduled:2026-11-02",
		"```",
		"- [ ] first",
		"- [ ] second scheduled:2026-11-02 repeat:1d",
		"  - [ ] child",
		"- [ ] third",
	}
	want := []string{
		"```",
		"- [ ] not a task repeat:1d scheduled:2026-11-02",
		"```",
		"- [ ] first",
		"- [ ] second scheduled:2026-11-03 repeat:1d",
		"  - [ ] child",
		"- [x] second scheduled:2026-11-02 repeat:1d",
		"  - [ ] child",
		"- [ ] third",
	}
	got, inserted := rollLines(t, in, 1, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2", inserted)
	}
}

func TestRollOnlyTicksTasksThatAreNotOpen(t *testing.T) {
	for _, state := range []string{"x", "-", ">"} {
		line := "- [" + state + "] T scheduled:2026-11-02 repeat:1d"
		next, inserted, ok := RollTaskInBody(line, 0, DialectCLI, RollComplete, rollNow("2026-11-02"), time.Monday)
		if !ok || inserted != 0 || strings.Count(next, "\n") != 0 {
			t.Fatalf("state %q: next=%q inserted=%d ok=%v", state, next, inserted, ok)
		}
	}
	if _, _, ok := RollTaskInBody("- [ ] T", 3, DialectCLI, RollComplete, rollNow("2026-11-02"), time.Monday); ok {
		t.Fatal("an index past the last task reported ok")
	}
}

// The plain toggle is what a remote vault uses, so it must never insert a
// line, repeat rule or not.
func TestPlainToggleNeverInserts(t *testing.T) {
	body := "- [ ] Water plants scheduled:2026-11-02 repeat:mon,thu\n"
	next, ok := ToggleTaskInBody(body, 0, DialectCLI)
	if !ok || next != "- [x] Water plants scheduled:2026-11-02 repeat:mon,thu\n" {
		t.Fatalf("plain toggle: %q ok=%v", next, ok)
	}
	next, ok = ToggleTaskInBody(body, 0, DialectApp)
	if !ok || strings.Count(next, "\n") != 1 {
		t.Fatalf("plain toggle (app): %q ok=%v", next, ok)
	}
}

func TestRollReplacesInvalidScheduledTokenOnCopy(t *testing.T) {
	in := []string{"- [ ] A scheduled:bad repeat:1d"}
	want := []string{
		"- [ ] A scheduled:2026-11-03 repeat:1d",
		"- [x] A scheduled:bad repeat:1d",
	}
	got, _ := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
}

func TestRollCancelCountsFromTodayOnDone(t *testing.T) {
	in := []string{"- [ ] A scheduled:2026-10-01 repeat:1w/done"}
	want := []string{
		"- [ ] A scheduled:2026-11-18 repeat:1w/done",
		"- [-] A scheduled:2026-10-01 repeat:1w/done",
	}
	got, _ := rollLines(t, in, 0, RollCancel, "2026-11-11")
	equalLines(t, got, want)
}
