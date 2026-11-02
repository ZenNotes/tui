package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// calHalfHours24 is the 48 half-hourly labels in the 24h format.
func calHalfHours24() []string {
	var out []string
	for h := 0; h < 24; h++ {
		out = append(out, fmt.Sprintf("%02d", h), fmt.Sprintf("%02d:30", h))
	}
	return out
}

// calHalfHours12 is the 48 half-hourly labels in the 12h format.
func calHalfHours12() []string {
	var out []string
	for _, h := range calHours12() {
		out = append(out, h, strings.Replace(h, " ", ":30 ", 1))
	}
	return out
}

func calLabels(a *App, tv *tasksView, w, bodyH int) []string {
	return calHourLabels(calScreen(a, tv, w, bodyH), calLabel24)
}

func requireVariant(t *testing.T, a *App, tv *tasksView, half bool, context string) {
	t.Helper()
	want := 24
	if half {
		want = 48
	}
	lines := calScreen(a, tv, 120, 110)
	if got := calHourLabels(lines, calLabel24); len(got) != want {
		t.Errorf("%s: %d hour rows shown, want %d (half-hourly=%v):\n%s", context, len(got), want, half, calScreenText(lines))
	}
}

func TestMonthIgnoresZ(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 3, "zt"))
	a.prefs.VimMode = true
	before := calScreenText(calScreen(a, tv, 120, 40))
	calPress(a, tv, vim.R('z'))
	if after := calScreenText(calScreen(a, tv, 120, 40)); after != before {
		t.Errorf("z changed the month view:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if tv.calc().sub != calMonth {
		t.Errorf("layout after z = %v, want month", tv.calc().sub)
	}
	calEsc(a, tv)
	requireVariant(t, a, tv, false, "week after z in month view")
}

func TestHourListStartsHourlyAtMidnight(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		got := calLabels(a, tv, 120, 70)
		if strings.Join(got, ",") != strings.Join(calHours24(), ",") {
			t.Errorf("layout %v: rows = %v, want 00..23", sub, got)
		}
		if small := calLabels(a, tv, 120, 20); len(small) == 0 || small[0] != "00" {
			t.Errorf("layout %v: first visible row in a short body = %v, want 00", sub, small)
		}
	}
}

func TestZTogglesVariantInWeek(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calWeek)
	if !tv.handleKey(a, vim.R('z')) {
		t.Error("z was not handled in week view")
	}
	requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHalfHours24(), "week after z")
	calPress(a, tv, vim.R('z'))
	requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHours24(), "week after z twice")
}

func TestZTogglesVariantInDay(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calDay)
	if !tv.handleKey(a, vim.R('z')) {
		t.Error("z was not handled in day view")
	}
	requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHalfHours24(), "day after z")
	calPress(a, tv, vim.R('z'))
	requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHours24(), "day after z twice")
}

func TestZWorksInItemFocusWithVimMode(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 2, "zi"))
	a.prefs.VimMode = true
	calToSub(t, a, tv, calWeek)
	calPress(a, tv, vim.KeyEnter)
	if tv.calc().focus != calFocusItems {
		t.Fatalf("Enter left focus on %v, want the day's column", tv.calc().focus)
	}
	calPress(a, tv, vim.R('z'))
	requireVariant(t, a, tv, true, "z in item focus")
}

func TestVariantResetsOnCalendarEntry(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	calToSub(t, a, tv, calWeek)
	calPress(a, tv, vim.R('z'))
	requireVariant(t, a, tv, true, "before leaving")
	for i := 0; i < 3; i++ {
		calPress(a, tv, vim.R('v'))
	}
	if tv.mode != "calendar" {
		t.Fatalf("mode after v round trip = %q, want calendar", tv.mode)
	}
	requireVariant(t, a, tv, false, "after the v round trip")

	calPress(a, tv, vim.R('z'))
	requireVariant(t, a, tv, true, "before :tasks calendar")
	tv.setMode("list")
	if err := a.openTasksMode("calendar"); err != nil {
		t.Fatal(err)
	}
	tv2, ok := a.activeTab().view.(*tasksView)
	if !ok {
		t.Fatalf("active tab is %T", a.activeTab().view)
	}
	requireVariant(t, a, tv2, false, "after :tasks calendar")
}

func TestVariantSurvivesMovesAndWeekDaySwitch(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	calToSub(t, a, tv, calWeek)
	calPress(a, tv, vim.R('z'))
	// Over the end of the week into the next one, then back.
	calPress(a, tv, calRunes("lllllll")...)
	requireDay(t, tv, "2026-11-25", "seven l presses")
	requireVariant(t, a, tv, true, "after moving to another week")
	calPress(a, tv, calRunes("hhhhhhh")...)
	requireVariant(t, a, tv, true, "after moving back")
	calEsc(a, tv)
	if tv.calc().sub != calDay {
		t.Fatalf("Esc gave layout %v, want day", tv.calc().sub)
	}
	requireVariant(t, a, tv, true, "after switching to day view")
}

func TestHourlyShownWholeWhenTallEnough(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		requireLabels(t, calScreen(a, tv, 120, 70), calLabel24, calHours24(), fmt.Sprintf("layout %v", sub))
		calPress(a, tv, vim.R('j'))
		requireLabels(t, calScreen(a, tv, 120, 70), calLabel24, calHours24(), fmt.Sprintf("layout %v after j", sub))
	}
}

func TestHalfHourlyShownWholeWhenTallEnough(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		if sub == calWeek {
			calPress(a, tv, vim.R('z'))
		}
		requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHalfHours24(), fmt.Sprintf("layout %v", sub))
	}
}

func TestHourListScrollsAtMinimumSize(t *testing.T) {
	for _, c := range []struct {
		sub  calSub
		w, h int
	}{{calWeek, 64, 12}, {calDay, 40, 10}} {
		for _, half := range []bool{false, true} {
			a, tv := calTestApp(t, nil)
			a.prefs.VimMode = true
			a.prefs.TimeFormat = "24h"
			calToSub(t, a, tv, c.sub)
			if half {
				calPress(a, tv, vim.R('z'))
			}
			lines := calScreen(a, tv, c.w, c.h)
			ctx := fmt.Sprintf("layout %v half=%v %dx%d", c.sub, half, c.w, c.h)
			requireTooSmall(t, lines, false, ctx)
			got := calHourLabels(lines, calLabel24)
			total := 24
			if half {
				total = 48
			}
			if len(got) == 0 || len(got) >= total {
				t.Fatalf("%s: %d rows shown, want some but fewer than %d:\n%s", ctx, len(got), total, calScreenText(lines))
			}
			if got[0] != "00" {
				t.Errorf("%s: first row = %s, want 00", ctx, got[0])
			}
			calPress(a, tv, vim.R('j'))
			scrolled := calHourLabels(calScreen(a, tv, c.w, c.h), calLabel24)
			if len(scrolled) == 0 || scrolled[0] == "00" {
				t.Errorf("%s: j did not scroll the list: %v", ctx, scrolled)
			}
		}
	}
}

func TestHalfHourlyLabels24h(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		if sub == calWeek {
			calPress(a, tv, vim.R('z'))
		}
		requireLabels(t, calScreen(a, tv, 120, 110), calLabel24, calHalfHours24(), fmt.Sprintf("layout %v", sub))
	}
}

func TestHalfHourlyLabels12h(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "12h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		if sub == calWeek {
			calPress(a, tv, vim.R('z'))
		}
		requireLabels(t, calScreen(a, tv, 120, 110), calLabel12, calHalfHours12(), fmt.Sprintf("layout %v", sub))
	}
}

func TestHourListJKScrollClamped(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = true
		a.prefs.TimeFormat = "24h"
		calToSub(t, a, tv, sub)
		first := calLabels(a, tv, 120, 20)
		n := len(first)
		if n == 0 || n >= 24 || first[0] != "00" {
			t.Fatalf("layout %v: unexpected short-body rows %v", sub, first)
		}
		calPress(a, tv, vim.R('k'))
		if got := calLabels(a, tv, 120, 20); got[0] != "00" {
			t.Errorf("layout %v: k before the first row moved the list to %s", sub, got[0])
		}
		calPress(a, tv, vim.R('j'))
		if got := calLabels(a, tv, 120, 20); got[0] != "01" {
			t.Errorf("layout %v: j moved the list to %s, want 01", sub, got[0])
		}
		requireDay(t, tv, "2026-11-18", fmt.Sprintf("layout %v after j", sub))
		for i := 0; i < 60; i++ {
			calPress(a, tv, vim.R('j'))
		}
		got := calLabels(a, tv, 120, 20)
		if len(got) != n || got[len(got)-1] != "23" || got[0] != fmt.Sprintf("%02d", 24-n) {
			t.Errorf("layout %v: after scrolling to the end rows = %v, want %d rows ending at 23", sub, got, n)
		}
		requireDay(t, tv, "2026-11-18", fmt.Sprintf("layout %v after many j", sub))
		for i := 0; i < 60; i++ {
			calPress(a, tv, vim.R('k'))
		}
		if got := calLabels(a, tv, 120, 20); got[0] != "00" {
			t.Errorf("layout %v: after scrolling back first row = %s, want 00", sub, got[0])
		}
	}
}

func TestHourListArrowsScrollWithoutVimMode(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = false
		a.prefs.TimeFormat = "24h"
		calToSub(t, a, tv, sub)
		if got := calLabels(a, tv, 120, 20); len(got) >= 24 || got[0] != "00" {
			t.Fatalf("layout %v: unexpected rows %v", sub, got)
		}
		calPress(a, tv, vim.KeyDown)
		if got := calLabels(a, tv, 120, 20); got[0] != "01" {
			t.Errorf("layout %v: Down moved the list to %s, want 01", sub, got[0])
		}
		calPress(a, tv, vim.KeyDown)
		calPress(a, tv, vim.KeyUp)
		if got := calLabels(a, tv, 120, 20); got[0] != "01" {
			t.Errorf("layout %v: Down, Down, Up left the list at %s, want 01", sub, got[0])
		}
		requireDay(t, tv, "2026-11-18", fmt.Sprintf("layout %v", sub))
	}
}

func TestHourListScrollPersists(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calWeek)
	calLabels(a, tv, 120, 20)
	calPress(a, tv, calRunes("jjjjjj")...)
	requireFirst := func(context string) {
		t.Helper()
		got := calLabels(a, tv, 120, 20)
		if len(got) == 0 || got[0] != "06" {
			t.Errorf("%s: first row = %v, want 06", context, got)
		}
	}
	requireFirst("after scrolling")
	calPress(a, tv, calRunes("lllllll")...)
	requireFirst("in the next week")
	calPress(a, tv, calRunes("hhhhhhh")...)
	requireFirst("back in the first week")
	calEsc(a, tv)
	requireFirst("in day view")
	calPress(a, tv, calRunes("l")...)
	requireFirst("on another day")
	calEsc(a, tv)
	if tv.calc().sub != calMonth {
		t.Fatalf("layout = %v, want month", tv.calc().sub)
	}
	calEsc(a, tv)
	requireFirst("in week view after the month view")
	calEsc(a, tv)
	requireFirst("in day view after the month view")
}

func TestHourListScrollAfterZKeepsTime(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calWeek)
	n := len(calLabels(a, tv, 120, 20))
	if n == 0 || n >= 24 {
		t.Fatalf("unexpected short-body row count %d", n)
	}
	calPress(a, tv, calRunes("jjjjjj")...)
	calPress(a, tv, vim.R('z'))
	got := calLabels(a, tv, 120, 20)
	if len(got) == 0 || got[0] != "06" {
		t.Fatalf("hourly 06 became %v in the half-hourly variant, want first row 06", got)
	}
	calPress(a, tv, vim.R('j'))
	if got := calLabels(a, tv, 120, 20); got[0] != "06:30" {
		t.Fatalf("one j in the half-hourly variant gave %v, want first row 06:30", got)
	}
	calPress(a, tv, vim.R('z'))
	if got := calLabels(a, tv, 120, 20); got[0] != "06" {
		t.Errorf("06:30 became %v in the hourly variant, want first row 06", got)
	}

	// At the end of the half-hourly list the same time is past the end of
	// the hourly list, so the start is clamped to the last full page.
	calPress(a, tv, vim.R('z'))
	for i := 0; i < 120; i++ {
		calPress(a, tv, vim.R('j'))
	}
	end := calLabels(a, tv, 120, 20)
	if len(end) != n || end[len(end)-1] != "23:30" {
		t.Fatalf("half-hourly end of list = %v, want %d rows ending at 23:30", end, n)
	}
	calPress(a, tv, vim.R('z'))
	got = calLabels(a, tv, 120, 20)
	if len(got) != n || got[len(got)-1] != "23" {
		t.Errorf("hourly list after z at the end = %v, want %d rows ending at 23", got, n)
	}
}

func TestZWithoutVimModeInItemFocusDoesNothing(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 2, "zv"))
	a.prefs.VimMode = false
	calToSub(t, a, tv, calWeek)
	calPress(a, tv, vim.KeyEnter)
	if tv.calc().focus != calFocusItems {
		t.Fatalf("Enter left focus on %v, want the day's column", tv.calc().focus)
	}
	calPress(a, tv, vim.R('z'))
	requireVariant(t, a, tv, false, "z in item focus without Vim mode")
}

func TestZWithoutVimModeOnGridDoesNothing(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = false
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		calPress(a, tv, vim.R('z'))
		requireVariant(t, a, tv, false, fmt.Sprintf("z on the %v grid without Vim mode", sub))
	}
}

func TestEditorFoldKeysUnaffected(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.VimMode = true
	a.openNote("note.md", true)
	buf := a.activeBuffer()
	if buf == nil {
		t.Fatal("no active note buffer")
	}
	buf.ed.ReplaceText("# A\na1\na2\n# B\nb1")
	buf.ed.SetCursor(vim.Pos{Line: 0, Col: 0})
	press := func(s string) {
		for _, r := range s {
			a.handleKey(vim.R(r))
		}
	}
	press("zc")
	if end, ok := buf.ed.IsFoldStart(0); !ok || end != 2 {
		t.Errorf("zc: fold at line 0 = %d %v, want 0..2", end, ok)
	}
	press("zo")
	if _, ok := buf.ed.IsFoldStart(0); ok {
		t.Error("zo did not open the fold")
	}
	press("zM")
	if len(buf.ed.Folds()) != 2 {
		t.Errorf("zM: folds = %v, want both sections", buf.ed.Folds())
	}
	press("zR")
	if len(buf.ed.Folds()) != 0 {
		t.Errorf("zR: folds = %v, want none", buf.ed.Folds())
	}
}
