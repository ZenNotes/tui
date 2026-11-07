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
