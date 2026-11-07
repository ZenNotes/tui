package tui

import (
	"strings"
	"testing"
)

// submitDuePrompt opens the `d` prompt for the note's first task and answers
// it.
func submitDuePrompt(t *testing.T, a *App, path, answer string) string {
	t.Helper()
	a.taskDuePrompt(repeatTaskAt(t, path))
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatal("the d key did not open its prompt")
	}
	p.onSubmit(a, answer)
	return readRepeatNote(t, path)
}

func TestDuePromptKeepsTime(t *testing.T) {
	cases := map[string]string{
		"2026-12-24": "2026-12-24",
		"tomorrow":   "2026-11-03",
		"+3":         "2026-11-05",
		"fri":        "2026-11-06",
	}
	for answer, date := range cases {
		a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00\n", "2026-11-02")
		got := submitDuePrompt(t, a, path, answer)
		if want := "- [ ] Call Ana due:" + date + "T14:00\n"; got != want {
			t.Errorf("answer %q: got %q, want %q", answer, got, want)
		}
	}
}

func TestDuePromptEmptyAnswerClearsDueAndTime(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00 !high\n", "2026-11-02")
	got := submitDuePrompt(t, a, path, "")
	if want := "- [ ] Call Ana !high\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDuePromptAcceptsDateTime(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00\n", "2026-11-02")
	got := submitDuePrompt(t, a, path, "2026-12-01T09:30")
	if want := "- [ ] Call Ana due:2026-12-01T09:30\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	a, path = repeatTasksApp(t, "- [ ] Call Ana\n", "2026-11-02")
	if got := submitDuePrompt(t, a, path, "2026-12-01T25:00"); strings.Contains(got, "due:") {
		t.Fatalf("an invalid time was written: %q", got)
	}
}

// rowText renders the Tasks list row of the note's first task, without colour.
func rowText(t *testing.T, a *App, path string) string {
	t.Helper()
	loadTasksForTest(t, a)
	v := newTasksView(a)
	return stripAnsi(v.taskLine(a, repeatTaskAt(t, path), 120, false))
}

func TestTasksListRowShowsDueTime(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-03T14:00\n", "2026-11-02")
	a.prefs.TimeFormat = "24h"
	if got := rowText(t, a, path); !strings.Contains(got, "due 2026-11-03 14:00") {
		t.Fatalf("24h row: %q", got)
	}
	a.prefs.TimeFormat = "12h"
	if got := rowText(t, a, path); !strings.Contains(got, "due 2026-11-03 2:00 PM") {
		t.Fatalf("12h row: %q", got)
	}
}

func TestTasksBoardCardShowsDueTime(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-03T14:00\n", "2026-11-02")
	a.prefs.TimeFormat = "24h"
	a.prefs.KanbanGroupBy = "status"
	loadTasksForTest(t, a)
	_ = path
	v := newTasksView(a)
	if got := stripAnsi(v.renderBoard(a, 160, 20, true)); !strings.Contains(got, "2026-11-03 14:00") {
		t.Fatalf("board: %q", got)
	}
}

func TestDuePromptKeepsRange(t *testing.T) {
	cases := map[string]string{
		"2026-12-24": "2026-12-24",
		"tomorrow":   "2026-11-03",
		"+3":         "2026-11-05",
	}
	for answer, date := range cases {
		a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00-15:30\n", "2026-11-02")
		got := submitDuePrompt(t, a, path, answer)
		if want := "- [ ] Call Ana due:" + date + "T14:00-15:30\n"; got != want {
			t.Errorf("answer %q: got %q, want %q", answer, got, want)
		}
	}
	a, path := repeatTasksApp(t, "---\npage-type: task\ndue: 2026-11-01T14:00-15:30\n---\n\n# Call Ana\n", "2026-11-02")
	got := submitDuePrompt(t, a, path, "tomorrow")
	if !strings.Contains(got, "2026-11-03T14:00-15:30") {
		t.Errorf("task note lost its range: %q", got)
	}
}

func TestDuePromptAnswerWithTimeReplacesRange(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00-15:30\n", "2026-11-02")
	got := submitDuePrompt(t, a, path, "2026-12-01T09:30")
	if want := "- [ ] Call Ana due:2026-12-01T09:30\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	a, path = repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00\n", "2026-11-02")
	got = submitDuePrompt(t, a, path, "2026-12-01T09:30-11:00")
	if want := "- [ ] Call Ana due:2026-12-01T09:30-11:00\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	a, path = repeatTasksApp(t, "- [ ] Call Ana\n", "2026-11-02")
	if got := submitDuePrompt(t, a, path, "2026-12-01T11:00-09:30"); strings.Contains(got, "due:") {
		t.Fatalf("a range that ends before it starts was written: %q", got)
	}
}

func TestDuePromptEmptyAnswerClearsRange(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-01T14:00-15:30 !high\n", "2026-11-02")
	got := submitDuePrompt(t, a, path, "")
	if want := "- [ ] Call Ana !high\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTasksListRowShowsDueRange(t *testing.T) {
	a, path := repeatTasksApp(t, "- [ ] Call Ana due:2026-11-03T14:00-15:30\n", "2026-11-02")
	a.prefs.TimeFormat = "24h"
	if got := rowText(t, a, path); !strings.Contains(got, "due 2026-11-03 14:00–15:30") {
		t.Fatalf("24h row: %q", got)
	}
	a.prefs.TimeFormat = "12h"
	if got := rowText(t, a, path); !strings.Contains(got, "due 2026-11-03 2:00 PM–3:30 PM") {
		t.Fatalf("12h row: %q", got)
	}
}

func TestFormatAcrossDaysFollowsTimeFormat(t *testing.T) {
	if got, want := formatWhenRange("2027-03-13", "", "2027-03-21", "", "24h"), "2027-03-13 – 2027-03-21"; got != want {
		t.Fatalf("dates: got %q, want %q", got, want)
	}
	if got, want := formatWhenRange("2026-11-03", "22:00", "2026-11-04", "02:00", "24h"), "2026-11-03 22:00 – 2026-11-04 02:00"; got != want {
		t.Fatalf("24h: got %q, want %q", got, want)
	}
	if got, want := formatWhenRange("2026-11-03", "22:00", "2026-11-04", "02:00", "12h"), "2026-11-03 10:00 PM – 2026-11-04 2:00 AM"; got != want {
		t.Fatalf("12h: got %q, want %q", got, want)
	}
	if got, want := formatWhenRange("2026-11-03", "14:00", "2026-11-03", "15:30", "12h"), "2026-11-03 2:00 PM–3:30 PM"; got != want {
		t.Fatalf("one day: got %q, want %q", got, want)
	}
}
