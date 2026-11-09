package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

// calOpeningApp opens the calendar in the month layout on the given day with
// the clock fixed, so that the first switch to a week or day layout is the
// one under test.
func calOpeningApp(t *testing.T, files map[string]string, clock, day time.Time) (*App, *tasksView) {
	t.Helper()
	a, tv, _ := calCursorApp(t, files, calMonth, false)
	calFixClock(t, a, clock)
	tv.selectDay(a, day)
	return a, tv
}

func calAt(day int, hour, minute int) time.Time {
	return time.Date(2026, time.November, day, hour, minute, 0, 0, time.Local)
}

// requireFirstLabel checks the first hour label that a short body shows.
func requireFirstLabel(t *testing.T, a *App, tv *tasksView, want, context string) {
	t.Helper()
	labels := calLabels(a, tv, 120, 20)
	if len(labels) == 0 || labels[0] != want {
		t.Errorf("%s: hour labels = %v, want the first to be %s", context, labels, want)
	}
}

func TestWeekAndDayOpenAtNow(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calOpeningApp(t, nil, calAt(2, 9, 20), calAt(2, 0, 0))
		calSwitchTo(t, a, tv, sub)
		requireCursor(t, tv, 9*60, "the first switch")
		requireFirstLabel(t, a, tv, "09", "the first switch")

		// Moving between days keeps the slot and the scroll.
		calPress(a, tv, vim.R('j'), vim.R('l'), vim.R('h'))
		requireCursor(t, tv, 10*60, "j, l and h")
		requireFirstLabel(t, a, tv, "09", "j, l and h")

		// A new switch to a week or day layout opens at the time again.
		calEsc(a, tv)
		calSwitchTo(t, a, tv, sub)
		requireCursor(t, tv, 9*60, "a second switch")
		requireFirstLabel(t, a, tv, "09", "a second switch")
	}
}

func TestWeekAndDayOpenAtNowLateInTheDay(t *testing.T) {
	a, tv := calOpeningApp(t, nil, calAt(2, 23, 10), calAt(2, 0, 0))
	calSwitchTo(t, a, tv, calDay)
	requireCursor(t, tv, 23*60, "now at 23:10")
	lines := calScreen(a, tv, 120, 20)
	if _, ok := calRowLine(lines, calLabel24, "23"); !ok {
		t.Errorf("the 23 row is off screen:\n%s", calScreenText(lines))
	}
	labels := calHourLabels(lines, calLabel24)
	if len(labels) < 2 || labels[0] == "23" {
		t.Errorf("the list shows %v: it must end on 23 and not start on it", labels)
	}
}

func TestDayOpensAtFirstTimedEntry(t *testing.T) {
	files := map[string]string{
		"Plans.md": "- Review when:2026-11-12T09:40\n- Work block when:2026-11-12T16:00\n",
	}
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calOpeningApp(t, files, calAt(2, 14, 20), calAt(12, 0, 0))
		calSwitchTo(t, a, tv, sub)
		requireCursor(t, tv, 9*60, "first timed entry")
		requireFirstLabel(t, a, tv, "09", "first timed entry")
	}
}

func TestDayOpensAtEightWithoutTimedEntries(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		day := calAt(5, 0, 0)
		if sub == calWeek {
			day = calAt(11, 0, 0)
		}
		a, tv := calOpeningApp(t, nil, calAt(2, 14, 20), day)
		calSwitchTo(t, a, tv, sub)
		requireCursor(t, tv, 8*60, "no timed entry")
		requireFirstLabel(t, a, tv, "08", "no timed entry")
	}
}

func TestNowLabelStyle(t *testing.T) {
	a, tv := calOpeningApp(t, nil, calAt(2, 14, 20), calAt(2, 0, 0))
	calSwitchTo(t, a, tv, calWeek)
	// The cursor starts on the row of the current time, where its own style
	// wins, so it moves away first.
	calPress(a, tv, vim.R('j'))
	plain := calScreen(a, tv, 120, 70)
	raw := calRawScreen(t, a, tv, 120, 70)
	mark := strings.SplitN(a.theme.Base.Foreground(a.theme.Accent).Bold(true).Render("x"), "x", 2)[0]
	if mark == "" {
		t.Fatal("the now style adds no escape code")
	}
	if row := raw[calHourRowIndex(t, plain, "14")]; !strings.Contains(row, mark) {
		t.Errorf("the 14 label is not in the now style: %q", row)
	}
	if row := raw[calHourRowIndex(t, plain, "13")]; strings.Contains(row, mark) {
		t.Errorf("the 13 label is in the now style: %q", row)
	}

	// The half-hourly zoom marks the row that holds the time.
	calPress(a, tv, vim.R('z'))
	plain = calScreen(a, tv, 120, 110)
	raw = calRawScreen(t, a, tv, 120, 110)
	if row := raw[calHourRowIndex(t, plain, "14")]; !strings.Contains(row, mark) {
		t.Errorf("the 14 label (14:00 to 14:29) is not in the now style: %q", row)
	}
	if row := raw[calHourRowIndex(t, plain, "14:30")]; strings.Contains(row, mark) {
		t.Errorf("the 14:30 label is in the now style: %q", row)
	}

	// Another week has no label in the now style.
	tv.selectDay(a, calAt(11, 0, 0))
	raw = calRawScreen(t, a, tv, 120, 110)
	for _, l := range raw {
		if strings.Contains(l, mark) && calLabel24.MatchString(stripAnsi(l)) {
			t.Errorf("a label is in the now style in another week: %q", l)
		}
	}
}

func TestTodayKeysGoToNow(t *testing.T) {
	for _, key := range []vim.Key{vim.R('t'), vim.KeyHome} {
		for _, vimOff := range []bool{false, true} {
			if vimOff && key.IsRune('t') {
				continue
			}
			a, tv, _ := calCursorApp(t, nil, calMonth, vimOff)
			calFixClock(t, a, calAt(2, 9, 20))
			calSwitchTo(t, a, tv, calDay)
			tv.selectDay(a, calAt(5, 0, 0))
			calPress(a, tv, vim.KeyDown, vim.KeyDown, vim.KeyDown)
			calPress(a, tv, key)
			requireDay(t, tv, "2026-11-02", "the today key")
			requireCursor(t, tv, 9*60, "the today key")
			requireFirstLabel(t, a, tv, "09", "the today key")
		}
	}
}

func TestZoomCyclesThreeSteps(t *testing.T) {
	a, tv := calOpeningApp(t, nil, calAt(2, 14, 20), calAt(2, 0, 0))
	a.prefs.VimMode = true
	calSwitchTo(t, a, tv, calDay)
	calPress(a, tv, vim.R('j'))
	calPress(a, tv, vim.R('z'))
	if got := tv.calc().hourStep(); got != 30 {
		t.Fatalf("one z: slot of %d minutes, want 30", got)
	}
	// The cursor keeps its time: 15:00 is the 15:00 row.
	requireCursor(t, tv, 15*60, "half-hourly")
	calPress(a, tv, vim.R('z'))
	if got := tv.calc().hourStep(); got != 15 {
		t.Fatalf("two z: slot of %d minutes, want 15", got)
	}
	calPress(a, tv, vim.R('j'), vim.R('j'))
	requireCursor(t, tv, 15*60+30, "two j in the quarter-hourly zoom")
	calPress(a, tv, vim.R('z'))
	if got := tv.calc().hourStep(); got != 60 {
		t.Fatalf("three z: slot of %d minutes, want 60", got)
	}
	// The cursor is on the row that holds its time, and the way back to a
	// finer zoom starts at the start of that row.
	requireCursor(t, tv, 15*60, "hourly after 15:30")
	calPress(a, tv, vim.R('z'), vim.R('z'))
	requireCursor(t, tv, 15*60, "quarter-hourly after hourly")
}

func TestZoomMovesFromHalfHourToHourToHalfHour(t *testing.T) {
	a, tv := calOpeningApp(t, nil, calAt(2, 14, 20), calAt(2, 0, 0))
	a.prefs.VimMode = true
	calSwitchTo(t, a, tv, calDay)
	calPress(a, tv, vim.R('z'), vim.R('j'))
	requireCursor(t, tv, 14*60+30, "j in the half-hourly zoom")
	calPress(a, tv, vim.R('z'), vim.R('z'))
	requireCursor(t, tv, 14*60, "hourly")
	calPress(a, tv, vim.R('z'))
	requireCursor(t, tv, 14*60, "half-hourly again")
}

func TestQuarterHourlyLabelsAndSlots(t *testing.T) {
	files := map[string]string{"Work.md": "- [ ] Task due:2026-11-03T09:40\n"}
	a, tv, _ := calCursorApp(t, files, calDay, false)
	a.prefs.VimMode = true
	calPress(a, tv, vim.R('z'), vim.R('z'))
	lines := calScreen(a, tv, 120, 200)
	text := calScreenText(lines)
	for _, want := range []string{"09 ", "09:15", "09:30", "09:45"} {
		if !strings.Contains(text, want) {
			t.Errorf("24h labels lack %q:\n%s", want, text)
		}
	}
	if row, ok := calRowLine(lines, calLabel24, "09:30"); !ok || !strings.Contains(row, "Task") {
		t.Errorf("the 09:30 row does not start the task due at 09:40: %q", row)
	}
	a.prefs.TimeFormat = "12h"
	text = calScreenText(calScreen(a, tv, 120, 200))
	for _, want := range []string{" 9 AM", " 9:15 AM", " 9:30 AM", " 9:45 AM"} {
		if !strings.Contains(text, want) {
			t.Errorf("12h labels lack %q:\n%s", want, text)
		}
	}
}
