package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// calHourRowApp opens the calendar on 2026-11-05 in the given layout, with
// 24-hour labels and Vim keys on, and half-hourly rows when half is set.
func calHourRowApp(t *testing.T, tasks []vault.Task, sub calSub, half bool) (*App, *tasksView) {
	t.Helper()
	a, tv := calTestApp(t, tasks)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	tv.selectDay(a, calDate(2026, 11, 5))
	calToSub(t, a, tv, sub)
	if half {
		calPress(a, tv, vim.R('z'))
	}
	return a, tv
}

// calRowLine is the screen line of the hour row with the given label.
func calRowLine(lines []string, re *regexp.Regexp, label string) (string, bool) {
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil && m[1] == label {
			return l, true
		}
	}
	return "", false
}

// requireOnlyInRow fails unless text is on screen exactly once and that line
// is the hour row with the given label. It returns that line.
func requireOnlyInRow(t *testing.T, lines []string, label, text, context string) string {
	t.Helper()
	screen := calScreenText(lines)
	if got := strings.Count(screen, text); got != 1 {
		t.Fatalf("%s: %q shown %d times, want once:\n%s", context, text, got, screen)
	}
	row, ok := calRowLine(lines, calLabel24, label)
	if !ok {
		t.Fatalf("%s: no %s row:\n%s", context, label, screen)
	}
	if !strings.Contains(row, text) {
		t.Fatalf("%s: %q is not in the %s row %q:\n%s", context, text, label, row, screen)
	}
	return row
}

func TestDayViewPlacesTimedTaskInHalfHourRow(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "standup", Status: "open", Due: "2026-11-05", DueTime: "09:30"},
	}, calDay, true)
	requireOnlyInRow(t, calScreen(a, tv, 120, 110), "09:30", "standup", "half-hourly day view")
}

func TestDayViewPlacesTimedTaskInHourRow(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "standup", Status: "open", Due: "2026-11-05", DueTime: "09:40"},
	}, calDay, false)
	requireOnlyInRow(t, calScreen(a, tv, 120, 70), "09", "standup", "hourly day view")
}

func TestWeekViewPlacesDueTimeInHourRow(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "dentist", Status: "open", Due: "2026-11-05", DueTime: "14:00"},
	}, calWeek, false)
	lines := calScreen(a, tv, 160, 70)
	row := requireOnlyInRow(t, lines, "14", "dentist", "hourly week view")
	cols := requireWeekHeader(t, lines, calWeekdayNames, "week of 2026-11-05")
	// 2026-11-05 is a Thursday, the fourth column.
	if x := calCellOf(row, "dentist"); x < cols[3]-1 || x >= cols[4]-1 {
		t.Errorf("entry starts at cell %d, want inside the Thursday column %d..%d:\n%s", x, cols[3]-1, cols[4]-1, calScreenText(lines))
	}
}

func TestWeekViewPlacesEachEntryByItsOwnTime(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "both", Status: "open", Due: "2026-11-10", DueTime: "17:00"},
		{ID: "n#1", SourcePath: "n.md", Content: "split", Status: "open", Due: "2026-11-09"},
	}, calWeek, false)
	// The week of 2026-11-05 runs Monday the 2nd to Sunday the 8th.
	calPress(a, tv, calRunes("lllll")...)
	requireDay(t, tv, "2026-11-10", "five l presses")
	lines := calScreen(a, tv, 160, 70)
	cols := requireWeekHeader(t, lines, calWeekdayNames, "next week")
	row, ok := calRowLine(lines, calLabel24, "17")
	if !ok || !strings.Contains(row, "both") {
		t.Fatalf("due entry is not in the 17 row:\n%s", calScreenText(lines))
	}
	if x := calCellOf(row, "both"); x < cols[1]-1 || x >= cols[2]-1 {
		t.Errorf("due entry starts at cell %d, want inside the Tuesday column:\n%s", x, calScreenText(lines))
	}
	// The untimed due entry of the second task sits in no hour row.
	for _, l := range lines {
		if calLabel24.MatchString(l) && strings.Contains(l, "split") {
			t.Errorf("untimed due entry is in the hour row %q", l)
		}
	}
	if got := strings.Count(calScreenText(lines), "split"); got != 1 {
		t.Errorf("untimed due entry shown %d times, want once:\n%s", got, calScreenText(lines))
	}
}

func TestUntimedTaskStaysOutOfHourRows(t *testing.T) {
	tasks := []vault.Task{{ID: "n#0", SourcePath: "n.md", Content: "errand", Status: "open", Due: "2026-11-05"}}
	for _, sub := range []calSub{calWeek, calDay} {
		for _, half := range []bool{false, true} {
			a, tv := calHourRowApp(t, tasks, sub, half)
			lines := calScreen(a, tv, 160, 110)
			ctx := fmt.Sprintf("layout %v half=%v", sub, half)
			if got := strings.Count(calScreenText(lines), "errand"); got != 1 {
				t.Fatalf("%s: entry shown %d times, want once:\n%s", ctx, got, calScreenText(lines))
			}
			// The day view lists untimed entries in the pane right of the
			// hours, so only the cells left of the pane are the hour rows.
			grid := 1 << 20
			if sub == calDay {
				grid = tv.calc().layout.pane.x
			}
			for _, l := range lines {
				if calLabel24.MatchString(l) && strings.Contains(l, "errand") && calCellOf(l, "errand") < grid {
					t.Errorf("%s: entry sits in the hour row %q", ctx, l)
				}
			}
		}
	}
}

func TestAllDayBandSitsAboveHourRows(t *testing.T) {
	tasks := []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "errand", Status: "open", Due: "2026-11-05"},
		{ID: "n#1", SourcePath: "n.md", Content: "standup", Status: "open", Due: "2026-11-05", DueTime: "10:00"},
	}
	for _, sub := range []calSub{calWeek} {
		a, tv := calHourRowApp(t, tasks, sub, false)
		lines := calScreen(a, tv, 160, 70)
		ctx := fmt.Sprintf("layout %v", sub)
		band, timed := calRowOf(lines, "errand"), calRowOf(lines, "standup")
		firstLabel := -1
		for i, l := range lines {
			if calLabel24.MatchString(l) {
				firstLabel = i
				break
			}
		}
		if band < 0 || firstLabel < 0 || band >= firstLabel || firstLabel >= timed {
			t.Errorf("%s: band row %d, first hour row %d, timed row %d: want the band above the rows:\n%s", ctx, band, firstLabel, timed, calScreenText(lines))
			continue
		}
		if calLabel24.MatchString(lines[band]) {
			t.Errorf("%s: an hour label sits beside the all-day entry: %q", ctx, lines[band])
		}
	}
}

func TestSelectedTimedTaskScrollsIntoView(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "late call", Status: "open", Due: "2026-11-05", DueTime: "21:00"},
	}, calDay, false)
	if _, ok := calRowLine(calScreen(a, tv, 120, 20), calLabel24, "21"); ok {
		t.Fatal("the 21 row is already on screen before the list scrolls")
	}
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusItems, "Enter on a day with an entry")
	requireOnlyInRow(t, calScreen(a, tv, 120, 20), "21", "late call", "selected timed entry in a short body")
}

func TestItemFocusFollowsBandThenHourRows(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "late", Status: "open", Due: "2026-11-05", DueTime: "14:00"},
		{ID: "n#1", SourcePath: "n.md", Content: "plain1", Status: "open", Due: "2026-11-05"},
		{ID: "n#2", SourcePath: "n.md", Content: "early", Status: "open", Due: "2026-11-05", DueTime: "09:00"},
		{ID: "n#3", SourcePath: "n.md", Content: "plain2", Status: "open", Due: "2026-11-05"},
	}, calWeek, false)
	calScreen(a, tv, 160, 70)
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusItems, "Enter on a day with entries")
	want := []string{"plain1", "plain2", "early", "late"}
	selected := func() string {
		calScreen(a, tv, 160, 70)
		if sel := tv.selectedTask(); sel != nil {
			return sel.Content
		}
		return ""
	}
	for i, name := range want {
		if i > 0 {
			calPress(a, tv, vim.R('j'))
		}
		if got := selected(); got != name {
			t.Fatalf("after %d j presses the selection is %q, want %q", i, got, name)
		}
	}
	for i := len(want) - 2; i >= 0; i-- {
		calPress(a, tv, vim.R('k'))
		if got := selected(); got != want[i] {
			t.Fatalf("going back, the selection is %q, want %q", got, want[i])
		}
	}
}

func TestClickTimedTaskSelectsIt(t *testing.T) {
	a, tv := calHourRowApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "plain", Status: "open", Due: "2026-11-05"},
		{ID: "n#1", SourcePath: "n.md", Content: "dentist", Status: "open", Due: "2026-11-05", DueTime: "14:00"},
	}, calDay, false)
	row := calRowOf(calScreen(a, tv, 120, 70), "dentist")
	// The body starts one line under the view's toolbar.
	tv.calc().click(a, tv, 12, row-1, false, false)
	requireFocus(t, tv, calFocusItems, "click on a timed entry")
	if sel := tv.selectedTask(); sel == nil || sel.Content != "dentist" {
		t.Errorf("selected task = %+v, want dentist", sel)
	}
}
