package vault

import (
	"strings"
	"testing"
	"time"
)

const rollNoteBody = "\n# Water the plants\n\nKeep the ferns damp.\n"

// rollNote builds a task note whose frontmatter holds the given lines, after
// `tags: [task]` and `status: open`.
func rollNote(lines ...string) string {
	head := append([]string{"---", "tags: [task]", "status: open"}, lines...)
	return strings.Join(append(head, "---"), "\n") + "\n" + rollNoteBody
}

func rollNoteAt(t *testing.T, body string, action RollAction, today string) string {
	t.Helper()
	next, _ := RollTaskFileInBody(body, action, rollNow(today), time.Monday)
	return next
}

// A `scheduled` key is neither read nor moved: the roll counts from `due`.
func TestTaskNoteRollLeavesScheduledKeyAlone(t *testing.T) {
	in := rollNote("scheduled: 2026-11-02", "due: 2026-11-04", "repeat: 3d")
	want := rollNote("scheduled: 2026-11-02", "due: 2026-11-07", "repeat: 3d", "lastCompleted: 2026-11-02")

	next, rolled := RollTaskFileInBody(in, RollComplete, rollNow("2026-11-02"), time.Monday)
	if !rolled {
		t.Fatal("a repeating task note did not roll")
	}
	if next != want {
		t.Fatalf("got:\n%s\nwant:\n%s", next, want)
	}
}

func TestTaskNoteRollKeepsTime(t *testing.T) {
	in := rollNote("due: 2026-11-02T09:30", "repeat: 1w")
	want := rollNote("due: 2026-11-09T09:30", "repeat: 1w", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// A quoted value stays quoted, and its time stays too.
	quoted := rollNote(`due: "2026-11-02T18:00"`, "repeat: 1d")
	wantQuoted := rollNote(`due: "2026-11-03T18:00"`, "repeat: 1d", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, quoted, RollComplete, "2026-11-02"); got != wantQuoted {
		t.Fatalf("quoted: got:\n%s\nwant:\n%s", got, wantQuoted)
	}
}

// A task note with no `repeat` key completes the way it always did.
func TestTaskNoteWithoutRepeatCompletesAsBefore(t *testing.T) {
	now := rollNow("2026-11-04")
	in := rollNote("due: 2026-11-04")
	next, rolled := RollTaskFileInBody(in, RollComplete, now, time.Monday)
	if rolled {
		t.Fatal("a note without repeat rolled")
	}
	if next != ToggleFileTaskInBody(in, false, now) {
		t.Fatalf("got:\n%s", next)
	}
	for _, line := range []string{"status: done", "completedDate: 2026-11-04", "due: 2026-11-04"} {
		if !strings.Contains(next, line) {
			t.Fatalf("missing %q in:\n%s", line, next)
		}
	}
	if strings.Contains(next, "lastCompleted") {
		t.Fatalf("lastCompleted written without repeat:\n%s", next)
	}

	// An unusable rule is the same as none.
	bad := rollNote("due: 2026-11-04", "repeat: fortnightly")
	if next, rolled := RollTaskFileInBody(bad, RollComplete, now, time.Monday); rolled || next != ToggleFileTaskInBody(bad, false, now) {
		t.Fatalf("invalid rule: rolled=%v\n%s", rolled, next)
	}
}

func TestTaskNoteCancelAdvancesLikeCompletion(t *testing.T) {
	in := rollNote("due: 2026-11-02", "repeat: 3d")
	want := rollNote("due: 2026-11-05", "repeat: 3d", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollCancel, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoteRollPassesOverSkipAndKeepsSkipLine(t *testing.T) {
	skip := "skip: [2027-03-13..2027-03-21, 2027-04-02]"
	in := rollNote("due: 2027-03-08T09:00", "repeat: 1w", skip)
	want := rollNote("due: 2027-03-22T09:00", "repeat: 1w", skip, "lastCompleted: 2027-03-08")
	if got := rollNoteAt(t, in, RollComplete, "2027-03-08"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	single := rollNote("due: 2027-03-12", "repeat: 1d", "skip: 2027-03-13")
	wantSingle := rollNote("due: 2027-03-14", "repeat: 1d", "skip: 2027-03-13", "lastCompleted: 2027-03-12")
	if got := rollNoteAt(t, single, RollComplete, "2027-03-12"); got != wantSingle {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantSingle)
	}
}

func TestTaskNoteRollCountsFromDueAndFromTodayOnDone(t *testing.T) {
	due := rollNote("due: 2026-11-02", "repeat: 1w")
	wantDue := rollNote("due: 2026-11-09", "repeat: 1w", "lastCompleted: 2026-11-04")
	if got := rollNoteAt(t, due, RollComplete, "2026-11-04"); got != wantDue {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantDue)
	}

	// `/done` counts from the day of completion, and the dates move by the
	// distance from the base field to the next date.
	done := rollNote("scheduled: 2026-10-01", "due: 2026-10-03", "repeat: after-1w")
	wantDone := rollNote("scheduled: 2026-10-01", "due: 2026-11-18", "repeat: after-1w", "lastCompleted: 2026-11-11")
	if got := rollNoteAt(t, done, RollComplete, "2026-11-11"); got != wantDone {
		t.Fatalf("got:\n%s\nwant:\n%s", got, wantDone)
	}
}

func TestTaskNoteRollWithoutDatesAddsDue(t *testing.T) {
	in := rollNote("repeat: 1w")
	want := rollNote("repeat: 1w", "due: 2026-11-09", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoteRollDropsStaleCompletedDate(t *testing.T) {
	in := rollNote("due: 2026-11-02", "repeat: 1d", "completedDate: 2026-10-01")
	want := rollNote("due: 2026-11-03", "repeat: 1d", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Unticking a finished repeating note, and reopening a cancelled one, are
// the plain status changes: nothing advances.
func TestTaskNoteRollUntickReopensWithoutAdvancing(t *testing.T) {
	now := rollNow("2026-11-02")
	done := strings.Replace(rollNote("due: 2026-11-02", "repeat: 1d"), "status: open", "status: done", 1)
	next, rolled := RollTaskFileInBody(done, RollComplete, now, time.Monday)
	if rolled || next != SetTaskFileStatus(done, false, now) {
		t.Fatalf("untick: rolled=%v\n%s", rolled, next)
	}

	cancelled := strings.Replace(done, "status: done", "status: cancelled", 1)
	next, rolled = RollTaskFileInBody(cancelled, RollCancel, now, time.Monday)
	if rolled || next != SetTaskFileCancelled(cancelled, false) {
		t.Fatalf("reopen: rolled=%v\n%s", rolled, next)
	}
}

func TestTaskNoteRollDropsBlockListItemsOfAdvancedKeys(t *testing.T) {
	in := rollNote("due:", "  - 2026-11-04", "repeat: 3d")
	want := rollNote("due: 2026-11-07", "repeat: 3d", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoteRollDropsListItemsAfterBlankAndCommentLines(t *testing.T) {
	in := rollNote("due:", "", "  # next session", "  - 2026-11-02", "repeat: 3d")
	want := rollNote("due: 2026-11-05", "", "  # next session", "repeat: 3d", "lastCompleted: 2026-11-02")
	if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoteCancelCountsFromTodayOnDone(t *testing.T) {
	in := rollNote("due: 2026-10-03", "repeat: after-1w")
	want := rollNote("due: 2026-11-18", "repeat: after-1w", "lastCompleted: 2026-11-11")
	if got := rollNoteAt(t, in, RollCancel, "2026-11-11"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoteRepeatValues(t *testing.T) {
	for _, tc := range []struct {
		value, today, due, want string
	}{
		{"mon/thu", "2026-11-02", "2026-11-02", "2026-11-05"},
		{"weekdays", "2026-11-06", "2026-11-06", "2026-11-09"},
		{"after-3d", "2026-11-11", "2026-10-03", "2026-11-14"},
		{"after-weekly", "2026-11-11", "2026-10-03", "2026-11-18"},
		{"monthly", "2027-02-01", "2027-01-31", "2027-02-28"},
		{"2w-mon/thu", "2026-11-05", "2026-11-05", "2026-11-16"},
		{"monthly-last-fri", "2026-11-27", "2026-11-27", "2026-12-25"},
		{"quarterly-end", "2026-12-31", "2026-12-31", "2027-03-31"},
		{"yearly-03-15", "2026-03-15", "2026-03-15", "2027-03-15"},
	} {
		in := rollNote("due: "+tc.due, "repeat: "+tc.value)
		want := rollNote("due: "+tc.want, "repeat: "+tc.value, "lastCompleted: "+tc.today)
		if got := rollNoteAt(t, in, RollComplete, tc.today); got != want {
			t.Errorf("repeat: %s\n got %q\nwant %q", tc.value, got, want)
		}
	}
	for _, value := range []string{"mon,thu", "3d/done", "1m:15", "after-mon/thu"} {
		in := rollNote("due: 2026-11-02", "repeat: "+value)
		want := strings.Replace(in, "status: open", "status: done", 1)
		if got := rollNoteAt(t, in, RollComplete, "2026-11-02"); !strings.Contains(got, "status: done") || strings.Contains(got, "lastCompleted") {
			t.Errorf("repeat: %s rolled; got %q, want a plain tick like %q", value, got, want)
		}
	}
}
