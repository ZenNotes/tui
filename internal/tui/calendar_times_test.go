package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// calShownLeftOfPanel is the screen with the month's item panel cut away, so
// a check reads the day boxes and not the panel beside them.
func calShownLeftOfPanel(tv *tasksView, lines []string) string {
	if tv.calc().sub != calMonth {
		return calScreenText(lines)
	}
	cut := tv.calc().layout.items.x
	out := make([]string, len(lines))
	for i, l := range lines {
		r := []rune(l)
		if len(r) > cut {
			r = r[:cut]
		}
		out[i] = string(r)
	}
	return calScreenText(out)
}

func TestCalendarEntryShowsTimeInTwelveHourFormat(t *testing.T) {
	tasks := []vault.Task{{ID: "a", Content: "Call", Due: "2026-11-18", DueTime: "14:00", Status: "open"}}
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		a, tv := calTestApp(t, tasks)
		a.prefs.TimeFormat = "12h"
		calToSub(t, a, tv, sub)
		text := calShownLeftOfPanel(tv, calScreen(a, tv, 160, 40))
		if !strings.Contains(text, "2:00 PM") {
			t.Errorf("sub %v: no 2:00 PM on the screen:\n%s", sub, text)
		}
		if strings.Contains(text, "14:00") {
			t.Errorf("sub %v: 24-hour time shown with time_format = 12h:\n%s", sub, text)
		}
	}
	a, tv := calTestApp(t, tasks)
	a.prefs.TimeFormat = "24h"
	if text := calShownLeftOfPanel(tv, calScreen(a, tv, 160, 40)); !strings.Contains(text, "14:00") {
		t.Errorf("no 14:00 with time_format = 24h:\n%s", text)
	}
}

func TestCalendarOrdersTimedEntriesEarliestFirst(t *testing.T) {
	tasks := []vault.Task{
		{ID: "plain1", Content: "plain1", Due: "2026-11-18", Status: "open"},
		{ID: "late", Content: "late", Due: "2026-11-18", DueTime: "16:00", Status: "open"},
		{ID: "early", Content: "early", Due: "2026-11-18", DueTime: "09:00", Status: "open"},
		{ID: "plain3", Content: "plain3", Due: "2026-11-18", Status: "open"},
	}
	byDay, _ := bucketCalendarEntries(tasks)
	want := []string{"early", "late", "plain1", "plain3"}
	if got := entryIDs(byDay["2026-11-18"]); !reflect.DeepEqual(got, want) {
		t.Errorf("order on 2026-11-18 = %v, want %v", got, want)
	}

	a, tv := calTestApp(t, tasks)
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calDay)
	lines := calScreen(a, tv, 120, 40)
	// Timed entries sit in their hour rows, earliest first, and the untimed
	// ones are in the all-day band above the rows, in the day's order.
	prev := -1
	for _, name := range []string{"plain1", "plain3", "early", "late"} {
		row := calRowOf(lines, name)
		if row <= prev {
			t.Errorf("%s is on row %d, not below the previous entry (row %d):\n%s", name, row, prev, calScreenText(lines))
		}
		prev = row
	}
}

const calTimedNote = "- [ ] alpha job due:2026-11-18T14:00\n"

// calPickDue opens the @ due-date menu on the first item of the selected day,
// picks the choice naming word and returns the note afterwards.
func calPickDue(t *testing.T, word string) string {
	t.Helper()
	a, _, root, m := calDueMenu(t, map[string]string{"work.md": calTimedNote}, true)
	calPick(t, a, m, word)
	return calReadFile(t, root, "work.md")
}

func TestDueMenuKeepsTimeWhenPickingADate(t *testing.T) {
	for _, c := range []struct{ word, due string }{
		{"2026-11-18", "2026-11-18"}, {"today", "2026-10-29"},
		{"tomorrow", "2026-10-30"}, {"next week", "2026-11-05"},
	} {
		want := "alpha job due:" + c.due + "T14:00"
		if got := calPickDue(t, c.word); !strings.Contains(got, want) {
			t.Errorf("%s: line should read %q:\n%s", c.word, want, got)
		}
	}
}

func TestTKeepsTimeWhenMovingToToday(t *testing.T) {
	calFixToday(t, calDate(2026, time.October, 29))
	a, tv, root := calVaultApp(t, map[string]string{"work.md": calTimedNote})
	calPress(a, tv, vim.KeyEnter, vim.R('T'))
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "alpha job due:2026-10-29T14:00") {
		t.Errorf("T should move the task to today and keep its time:\n%s", got)
	}
}

func TestDueMenuClearRemovesTime(t *testing.T) {
	if got := calPickDue(t, "clear"); got != "- [ ] alpha job\n" {
		t.Errorf("clear should leave no due token and no time:\n%q", got)
	}
}
