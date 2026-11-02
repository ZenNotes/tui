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

func TestCalendarScheduledEntryShowsItsTime(t *testing.T) {
	tasks := []vault.Task{{
		ID: "a", Content: "Plan", Status: "open",
		Scheduled: "2026-11-18", ScheduledTime: "09:30", Due: "2026-11-24",
	}}
	byDay, _ := bucketCalendarEntries(tasks)
	got := byDay["2026-11-18"]
	if len(got) != 1 || !got[0].scheduled || got[0].clock() != "09:30" {
		t.Fatalf("entries on 2026-11-18 = %+v, want one scheduled entry at 09:30", got)
	}
	if due := byDay["2026-11-24"]; len(due) != 1 || due[0].clock() != "" {
		t.Errorf("the due entry = %+v, want it untimed", due)
	}
	a, tv := calTestApp(t, tasks)
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		calToSub(t, a, tv, sub)
		text := calShownLeftOfPanel(tv, calScreen(a, tv, 160, 40))
		if !strings.Contains(text, calScheduledMark+"09:30") {
			t.Errorf("sub %v: no scheduled entry carrying 09:30:\n%s", sub, text)
		}
	}
}

func TestCalendarOrdersTimedEntriesEarliestFirst(t *testing.T) {
	tasks := []vault.Task{
		{ID: "plain1", Content: "plain1", Due: "2026-11-18", Status: "open"},
		{ID: "late", Content: "late", Due: "2026-11-18", DueTime: "16:00", Status: "open"},
		{ID: "sched", Content: "sched", Scheduled: "2026-11-18", ScheduledTime: "12:00", Status: "open"},
		{ID: "plain2", Content: "plain2", Scheduled: "2026-11-18", Status: "open"},
		{ID: "early", Content: "early", Due: "2026-11-18", DueTime: "09:00", Status: "open"},
		{ID: "plain3", Content: "plain3", Due: "2026-11-18", Status: "open"},
	}
	byDay, _ := bucketCalendarEntries(tasks)
	want := []string{"early", "sched", "late", "plain1", "plain3", "plain2"}
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
	for _, name := range []string{"plain1", "plain3", "plain2", "early", "sched", "late"} {
		row := calRowOf(lines, name)
		if row <= prev {
			t.Errorf("%s is on row %d, not below the previous entry (row %d):\n%s", name, row, prev, calScreenText(lines))
		}
		prev = row
	}
}

func TestCalendarEntryUsesItsOwnFieldsTime(t *testing.T) {
	apart := []vault.Task{{
		ID: "a", Content: "Apart", Status: "open",
		Due: "2026-11-18", DueTime: "09:00", Scheduled: "2026-11-20", ScheduledTime: "15:00",
	}}
	byDay, _ := bucketCalendarEntries(apart)
	if got := byDay["2026-11-18"]; len(got) != 1 || got[0].clock() != "09:00" {
		t.Errorf("due entry = %+v, want the due time 09:00", got)
	}
	if got := byDay["2026-11-20"]; len(got) != 1 || got[0].clock() != "15:00" {
		t.Errorf("scheduled entry = %+v, want the scheduled time 15:00", got)
	}

	together := []vault.Task{{
		ID: "b", Content: "Together", Status: "open",
		Due: "2026-11-18", DueTime: "09:00", Scheduled: "2026-11-18", ScheduledTime: "15:00",
	}}
	byDay, _ = bucketCalendarEntries(together)
	got := byDay["2026-11-18"]
	if len(got) != 1 || got[0].clock() != "15:00" {
		t.Fatalf("merged entry = %+v, want one entry at the scheduled time 15:00", got)
	}
	a, tv := calTestApp(t, together)
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calDay)
	text := calScreenText(calScreen(a, tv, 120, 40))
	if !strings.Contains(text, "15:00") || strings.Contains(text, "09:00") {
		t.Errorf("day view of a merged entry should show 15:00 only:\n%s", text)
	}

	untimed := []vault.Task{{
		ID: "c", Content: "Untimed", Status: "open",
		Due: "2026-11-18", Scheduled: "2026-11-20", ScheduledTime: "15:00",
	}}
	byDay, _ = bucketCalendarEntries(untimed)
	if got := byDay["2026-11-18"]; len(got) != 1 || got[0].clock() != "" {
		t.Errorf("a due entry whose due has no time = %+v, want it untimed", got)
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
