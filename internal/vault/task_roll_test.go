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
	in := []string{"intro", "- [ ] Water plants due:2026-11-02 @repeat:mon/thu", "outro"}
	want := []string{
		"intro",
		"- [ ] Water plants due:2026-11-05 @repeat:mon/thu",
		"- [x] Water plants due:2026-11-02",
		"outro",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}
}

func TestRollCancelInsertsNextOccurrence(t *testing.T) {
	in := []string{"intro", "- [ ] Water plants due:2026-11-02 @repeat:mon/thu", "outro"}
	want := []string{
		"intro",
		"- [ ] Water plants due:2026-11-05 @repeat:mon/thu",
		"- [-] Water plants due:2026-11-02",
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
		"- [ ] Water plants due:2026-11-05 @repeat:mon/thu",
		"- [x] Water plants due:2026-11-02 @repeat:mon/thu",
		"outro",
	}
	got, inserted := rollLines(t, in, 1, RollComplete, "2026-11-02")
	want := append([]string(nil), in...)
	want[2] = "- [ ] Water plants due:2026-11-02 @repeat:mon/thu"
	equalLines(t, got, want)
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}

	// Un-cancelling leaves the occurrence alone as well.
	in[2] = "- [-] Water plants due:2026-11-02 @repeat:mon/thu"
	got, inserted = rollLines(t, in, 1, RollCancel, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 0 {
		t.Fatalf("un-cancel inserted = %d, want 0", inserted)
	}
}

func TestRollCopiesSubtasksUnticked(t *testing.T) {
	in := []string{
		"- [ ] Weekly review due:2026-11-02 @repeat:1w",
		"  - [x] Inbox zero",
		"  - [ ] Plan week",
	}
	want := []string{
		"- [ ] Weekly review due:2026-11-09 @repeat:1w",
		"  - [ ] Inbox zero",
		"  - [ ] Plan week",
		"- [x] Weekly review due:2026-11-02",
		"  - [x] Inbox zero",
		"  - [-] Plan week",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 3 task lines", inserted)
	}
}

func TestRollCopiesChildLinesAsWritten(t *testing.T) {
	in := []string{
		"- [ ] Weekly review due:2026-11-02 @repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [x] Send summary due:2026-11-04T17:00",
		"  - [ ] Read it scheduled:2026-11-03",
	}
	want := []string{
		"- [ ] Weekly review due:2026-11-09 @repeat:1w",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [ ] Send summary due:2026-11-04T17:00",
		"  - [ ] Read it scheduled:2026-11-03",
		"- [x] Weekly review due:2026-11-02",
		"  Bring the notebook.",
		"  - plain bullet",
		"  - [x] Send summary due:2026-11-04T17:00",
		"  - [-] Read it scheduled:2026-11-03",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 2 task lines", inserted)
	}
}

func TestRollAddsDueWhenTaskHasNoDate(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T @repeat:3d"}, 0, RollComplete, "2026-11-02")
	want := []string{"- [ ] T @repeat:3d due:2026-11-05", "- [x] T"}
	equalLines(t, got, want)
	if strings.Contains(got[0], "scheduled:") {
		t.Fatalf("inserted line gained a scheduled token: %q", got[0])
	}
}

func TestRollWithoutRepeatOnlyTicks(t *testing.T) {
	for _, line := range []string{
		"- [ ] Pay rent due:2026-11-03",
		"- [ ] A due:2027-03-15 @skip:2027-03-13/2027-03-21",
	} {
		got, inserted := rollLines(t, []string{line}, 0, RollComplete, "2026-11-02")
		equalLines(t, got, []string{strings.Replace(line, "[ ]", "[x]", 1)})
		if inserted != 0 {
			t.Fatalf("%q inserted %d lines", line, inserted)
		}
	}
}

func TestRollWithInvalidRuleOnlyTicks(t *testing.T) {
	line := "- [ ] T due:2026-11-02 @repeat:fortnightly"
	got, inserted := rollLines(t, []string{line}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [x] T due:2026-11-02 @repeat:fortnightly"})
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}
}

func TestRollCountsFromDue(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T due:2026-11-02 @repeat:3d"}, 0, RollComplete, "2026-11-02")
	if got[0] != "- [ ] T due:2026-11-05 @repeat:3d" {
		t.Fatalf("got %q", got[0])
	}
}

func TestRollKeepsTimes(t *testing.T) {
	got, _ := rollLines(t, []string{"- [ ] T due:2026-11-02T09:30 @repeat:1d"}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{"- [ ] T due:2026-11-03T09:30 @repeat:1d", "- [x] T due:2026-11-02T09:30"})

	in := "- [ ] Assignment due:2027-03-12T23:59 @repeat:1w @skip:2027-03-13/2027-03-21"
	got, _ = rollLines(t, []string{in}, 0, RollComplete, "2027-03-08")
	equalLines(t, got, []string{
		"- [ ] Assignment due:2027-03-26T23:59 @repeat:1w @skip:2027-03-13/2027-03-21",
		"- [x] Assignment due:2027-03-12T23:59 @skip:2027-03-13/2027-03-21",
	})
}

func TestRollKeepsSkipTokensOnInsertedLine(t *testing.T) {
	cases := []struct{ line, next string }{
		{"- [ ] A due:2027-03-08 @repeat:1w @skip:2027-03-21/2027-03-13", "- [ ] A due:2027-03-15 @repeat:1w @skip:2027-03-21/2027-03-13"},
		{"- [ ] A due:2027-03-08 @repeat:1w @skip:2027-02-30", "- [ ] A due:2027-03-15 @repeat:1w @skip:2027-02-30"},
		{"- [ ] A due:2027-03-08 @repeat:1w @skip:2026-12-21/2027-01-03", "- [ ] A due:2027-03-15 @repeat:1w @skip:2026-12-21/2027-01-03"},
		{"- [ ] A due:2027-03-08 @repeat:1w @skip:spring-break", "- [ ] A due:2027-03-15 @repeat:1w @skip:spring-break"},
		{"- [ ] A due:2027-03-08 @repeat:1w @skip:2027-03-21/2027-03-13 @skip:2027-03-15", "- [ ] A due:2027-03-22 @repeat:1w @skip:2027-03-21/2027-03-13 @skip:2027-03-15"},
	}
	for _, c := range cases {
		got, _ := rollLines(t, []string{c.line}, 0, RollComplete, "2027-03-08")
		if got[0] != c.next {
			t.Errorf("%q\n got %q\nwant %q", c.line, got[0], c.next)
		}
		// The finished line drops the rule and keeps every skip field.
		if want := strings.Replace(strings.Replace(c.line, "[ ]", "[x]", 1), " @repeat:1w", "", 1); got[1] != want {
			t.Errorf("%q\n record %q\n  want %q", c.line, got[1], want)
		}
	}
}

func TestRollCountsTasksPastFencesAndLeavesOthersAlone(t *testing.T) {
	in := []string{
		"```",
		"- [ ] not a task @repeat:1d due:2026-11-02",
		"```",
		"- [ ] first",
		"- [ ] second due:2026-11-02 @repeat:1d",
		"  - [ ] child",
		"- [ ] third",
	}
	want := []string{
		"```",
		"- [ ] not a task @repeat:1d due:2026-11-02",
		"```",
		"- [ ] first",
		"- [ ] second due:2026-11-03 @repeat:1d",
		"  - [ ] child",
		"- [x] second due:2026-11-02",
		"  - [-] child",
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
		line := "- [" + state + "] T due:2026-11-02 @repeat:1d"
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
	body := "- [ ] Water plants due:2026-11-02 @repeat:mon/thu\n"
	next, ok := ToggleTaskInBody(body, 0, DialectCLI)
	if !ok || next != "- [x] Water plants due:2026-11-02 @repeat:mon/thu\n" {
		t.Fatalf("plain toggle: %q ok=%v", next, ok)
	}
	next, ok = ToggleTaskInBody(body, 0, DialectApp)
	if !ok || strings.Count(next, "\n") != 1 {
		t.Fatalf("plain toggle (app): %q ok=%v", next, ok)
	}
}

func TestRollReplacesInvalidDueTokenOnCopy(t *testing.T) {
	in := []string{"- [ ] A due:bad @repeat:1d"}
	want := []string{
		"- [ ] A due:2026-11-03 @repeat:1d",
		"- [x] A due:bad",
	}
	got, _ := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
}

func TestRollCancelCountsFromTodayOnDone(t *testing.T) {
	in := []string{"- [ ] A due:2026-10-01 @repeat:after-1w"}
	want := []string{
		"- [ ] A due:2026-11-18 @repeat:after-1w",
		"- [-] A due:2026-10-01",
	}
	got, _ := rollLines(t, in, 0, RollCancel, "2026-11-11")
	equalLines(t, got, want)
}

// A `scheduled:` token is text: the copy keeps it as written and the roll
// counts from `due:` alone.
func TestRollLeavesScheduledTokenAsText(t *testing.T) {
	in := []string{"- [ ] T scheduled:2026-10-01 due:2026-11-02 @repeat:3d"}
	want := []string{
		"- [ ] T scheduled:2026-10-01 due:2026-11-05 @repeat:3d",
		"- [x] T scheduled:2026-10-01 due:2026-11-02",
	}
	got, _ := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)

	got, _ = rollLines(t, []string{"- [ ] T scheduled:2026-11-02 @repeat:3d"}, 0, RollComplete, "2026-11-02")
	equalLines(t, got, []string{
		"- [ ] T scheduled:2026-11-02 @repeat:3d due:2026-11-05",
		"- [x] T scheduled:2026-11-02",
	})
}

// The finished line loses `@repeat:` and `@remind:`, so unticking it never
// starts a series again. The next occurrence keeps both.
func TestRollRecordDropsRepeatAndRemind(t *testing.T) {
	in := []string{"- [ ] Water plants @remind:9am due:2026-11-02 @repeat:mon/thu !high #home"}
	want := []string{
		"- [ ] Water plants @remind:9am due:2026-11-05 @repeat:mon/thu !high #home",
		"- [x] Water plants due:2026-11-02 !high #home",
	}
	got, _ := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)

	want[1] = "- [-] Water plants due:2026-11-02 !high #home"
	got, _ = rollLines(t, in, 0, RollCancel, "2026-11-02")
	equalLines(t, got, want)
}

// The finished line cancels the subtasks that were still open. It keeps the
// ones that were done, cancelled or forwarded, and the copy opens every
// checkbox again.
func TestRollRecordCancelsOpenSubtasks(t *testing.T) {
	in := []string{
		"## Chores",
		"- [ ] Weekly review due:2026-11-02 @repeat:1w ^keep",
		"  - [x] Inbox zero",
		"  - [ ] Plan week ^sub1",
		"  - [/] Draft the update",
		"    - [ ] Nested open",
		"  - [-] Calendar",
		"  - [>] Moved [[Elsewhere]]",
		"  notes stay",
		"- [ ] Other",
	}
	want := []string{
		"## Chores",
		"- [ ] Weekly review due:2026-11-09 @repeat:1w",
		"  - [ ] Inbox zero",
		"  - [ ] Plan week",
		"  - [ ] Draft the update",
		"    - [ ] Nested open",
		"  - [ ] Calendar",
		"  - [ ] Moved [[Elsewhere]]",
		"  notes stay",
		"- [x] Weekly review due:2026-11-02 ^keep",
		"  - [x] Inbox zero",
		"  - [-] Plan week ^sub1",
		"  - [-] Draft the update",
		"    - [-] Nested open",
		"  - [-] Calendar",
		"  - [>] Moved [[Elsewhere]]",
		"  notes stay",
		"- [ ] Other",
	}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-11-02")
	equalLines(t, got, want)
	if inserted != 7 {
		t.Fatalf("inserted = %d, want 7 task lines", inserted)
	}
}

// Every line keeps the ending that the note gives it, the inserted lines
// included.
func TestRollKeepsLineEndings(t *testing.T) {
	in := "- [ ] Win due:2026-11-02T08:00 @repeat:daily\r\n  - [ ] sub\r\n- [ ] after\r\n"
	want := "- [ ] Win due:2026-11-03T08:00 @repeat:daily\r\n  - [ ] sub\r\n" +
		"- [x] Win due:2026-11-02T08:00\r\n  - [-] sub\r\n- [ ] after\r\n"
	next, _, ok := RollTaskInBody(in, 0, DialectApp, RollComplete, rollNow("2026-11-02"), time.Monday)
	if !ok || next != want {
		t.Fatalf("got %q\nwant %q", next, want)
	}

	// A note with a mix of endings keeps each one on its own line.
	mixed := "- [ ] Win due:2026-11-02 @repeat:daily\r\n  - [ ] sub\n"
	want = "- [ ] Win due:2026-11-03 @repeat:daily\r\n  - [ ] sub\r\n" +
		"- [x] Win due:2026-11-02\r\n  - [-] sub\n"
	next, _, ok = RollTaskInBody(mixed, 0, DialectApp, RollComplete, rollNow("2026-11-02"), time.Monday)
	if !ok || next != want {
		t.Fatalf("mixed: got %q\nwant %q", next, want)
	}
}

// A task with no `due:` counts from the `due` of the note. The copy gets no
// `due:` of its own when the note has no other date for it either.
func TestRollCountsFromFrontmatterDue(t *testing.T) {
	in := "---\ndue: 2026-10-31\n---\n- [ ] Pay invoice @repeat:monthly"
	want := "---\ndue: 2026-10-31\n---\n- [ ] Pay invoice @repeat:monthly due:2026-11-30\n- [x] Pay invoice"
	next, _, ok := RollTaskInBody(in, 0, DialectApp, RollComplete, rollNow("2026-11-01"), time.Monday)
	if !ok || next != want {
		t.Fatalf("got %q\nwant %q", next, want)
	}

	// The `due:` of the line wins over the `due` of the note.
	in = "---\ndue: 2026-10-31\n---\n- [ ] Pay invoice @repeat:monthly due:2026-12-15"
	next, _, _ = RollTaskInBody(in, 0, DialectApp, RollComplete, rollNow("2026-11-01"), time.Monday)
	if !strings.Contains(next, "due:2027-01-15") {
		t.Fatalf("got %q", next)
	}
}

// A task with a valid rule and no next date is finished and loses its rule
// and its reminder. It keeps `@until:`, and nothing is inserted.
func TestEndedSeriesDropsRepeatAndRemind(t *testing.T) {
	in := []string{"- [ ] Lab due:2026-12-14 @repeat:1w @until:2026-12-14 @remind:8am ^ab12cd"}
	got, inserted := rollLines(t, in, 0, RollComplete, "2026-12-14")
	equalLines(t, got, []string{"- [x] Lab due:2026-12-14 @until:2026-12-14 ^ab12cd"})
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}
	got, _ = rollLines(t, in, 0, RollCancel, "2026-12-14")
	equalLines(t, got, []string{"- [-] Lab due:2026-12-14 @until:2026-12-14 ^ab12cd"})

	// An in-progress task is finished the same way, and its subtasks stay.
	got, _ = rollLines(t, []string{"- [/] Lab due:2026-12-14 @repeat:1w @until:2026-12-14", "  - [ ] sub"}, 0, RollComplete, "2026-12-14")
	equalLines(t, got, []string{"- [x] Lab due:2026-12-14 @until:2026-12-14", "  - [ ] sub"})

	// A CRLF note keeps its endings here too.
	next, _, _ := RollTaskInBody("- [ ] Lab @repeat:1w @until:2026-12-14\r\nx\r\n", 0, DialectApp, RollComplete, rollNow("2026-12-14"), time.Monday)
	if next != "- [x] Lab @until:2026-12-14\r\nx\r\n" {
		t.Fatalf("CRLF: got %q", next)
	}

	// Unticking the finished line does not start a series.
	got, inserted = rollLines(t, []string{"- [x] Lab due:2026-12-14 @until:2026-12-14"}, 0, RollComplete, "2026-12-14")
	equalLines(t, got, []string{"- [ ] Lab due:2026-12-14 @until:2026-12-14"})
	if inserted != 0 {
		t.Fatalf("untick inserted = %d", inserted)
	}
}

func TestRollCopiesChildLinesThatAreNotSessions(t *testing.T) {
	children := []string{
		"  - [ ] Ask Bob task:[[Spec]] when:2026-11-03",
		"  - run `zn add task:[[x]] when:2026-11-03`",
		"  ask them about task:review when:soon",
	}
	in := append([]string{"- [ ] T due:2026-11-02 @repeat:1w"}, children...)
	got, _ := rollLines(t, in, 0, RollComplete, "2026-11-02")
	want := []string{
		"- [ ] T due:2026-11-09 @repeat:1w",
		"  - [ ] Ask Bob task:[[Spec]] when:2026-11-03",
		children[1], children[2],
		"- [x] T due:2026-11-02",
		"  - [-] Ask Bob task:[[Spec]] when:2026-11-03",
		children[1], children[2],
	}
	equalLines(t, got, want)
}

func TestRollCopiesCRLFCheckboxChildWithSessionText(t *testing.T) {
	in := "- [ ] T due:2026-11-02 @repeat:1w\r\n  - [ ] Sub task:[[a]] when:2026-11-03\r\n"
	next, _, ok := RollTaskInBody(in, 0, DialectCLI, RollComplete, rollNow("2026-11-02"), time.Monday)
	if !ok {
		t.Fatal("RollTaskInBody reported no task")
	}
	want := "- [ ] T due:2026-11-09 @repeat:1w\r\n  - [ ] Sub task:[[a]] when:2026-11-03\r\n" +
		"- [x] T due:2026-11-02\r\n  - [-] Sub task:[[a]] when:2026-11-03\r\n"
	if next != want {
		t.Fatalf("next = %q\nwant %q", next, want)
	}
}
