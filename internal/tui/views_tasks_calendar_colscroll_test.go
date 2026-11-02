package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// colView is what the busy day's column shows: the entries listed, in order,
// and the two `+N more` counts around them (-1 when a count is absent).
type colView struct {
	entries []int
	above   int
	below   int
}

const colScrollTotal = 50

// calColumnView reads the zq entries and the `+N more` lines of one render.
func calColumnView(t *testing.T, lines []string) colView {
	t.Helper()
	v := colView{above: -1, below: -1}
	first, last := -1, -1
	for i, l := range lines {
		for _, m := range calZqRe.FindAllString(l, -1) {
			n, err := strconv.Atoi(strings.TrimPrefix(m, "zq"))
			if err != nil {
				t.Fatal(err)
			}
			v.entries = append(v.entries, n)
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		t.Fatalf("no entries listed:\n%s", calScreenText(lines))
	}
	for i, l := range lines {
		m := calMoreRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		switch {
		case i < first:
			if v.above >= 0 {
				t.Fatalf("two `+N more` lines above the entries:\n%s", calScreenText(lines))
			}
			v.above = n
		case i > last:
			if v.below >= 0 {
				t.Fatalf("two `+N more` lines below the entries:\n%s", calScreenText(lines))
			}
			v.below = n
		default:
			t.Fatalf("`+N more` line between entries:\n%s", calScreenText(lines))
		}
	}
	return v
}

func (v colView) String() string {
	return fmt.Sprintf("above=%d entries=%v below=%d", v.above, v.entries, v.below)
}

// calColumnApp opens a layout with one very busy day and an hour list that is
// shown whole.
func calColumnApp(t *testing.T, sub calSub, vimOn bool) (*App, *tasksView) {
	t.Helper()
	a, tv := calTestApp(t, calManyTasks("2026-11-18", colScrollTotal, "zq"))
	a.prefs.VimMode = vimOn
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, sub)
	if got := calLabels(a, tv, 120, 70); len(got) != 24 || got[0] != "00" {
		t.Fatalf("layout %v: hour list not shown whole: %v", sub, got)
	}
	return a, tv
}

func calColumnAfter(t *testing.T, a *App, tv *tasksView) colView {
	t.Helper()
	return calColumnView(t, calScreen(a, tv, 120, 70))
}

func TestColumnScrollWhenHourListWhole(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		before := calColumnAfter(t, a, tv)
		if before.above != -1 || before.below <= 0 || before.entries[0] != 0 {
			t.Fatalf("%s: unscrolled column = %v", ctx, before)
		}

		calPress(a, tv, vim.R('j'))
		got := calColumnAfter(t, a, tv)
		if got.above != 1 || got.entries[0] != 1 {
			t.Errorf("%s: after j column = %v, want one entry scrolled off the top", ctx, got)
		}
		if tv.calc().focus != calFocusGrid {
			t.Errorf("%s: j moved focus to %v, want the grid", ctx, tv.calc().focus)
		}
		if sel := tv.selectedTask(); sel != nil {
			t.Errorf("%s: j selected entry %q, want none", ctx, sel.Content)
		}
		requireDay(t, tv, "2026-11-18", ctx+" after j")
		if labels := calLabels(a, tv, 120, 70); len(labels) != 24 || labels[0] != "00" {
			t.Errorf("%s: j scrolled the hour list: %v", ctx, labels)
		}

		calPress(a, tv, calRunes("jj")...)
		if got := calColumnAfter(t, a, tv); got.above != 3 || got.entries[0] != 3 {
			t.Errorf("%s: after three j column = %v, want three entries scrolled off", ctx, got)
		}

		calPress(a, tv, calRunes("kkk")...)
		got = calColumnAfter(t, a, tv)
		if got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: after k back to the top column = %v", ctx, got)
		}
		calPress(a, tv, vim.R('k'))
		if got := calColumnAfter(t, a, tv); got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: k at the top scrolled the column to %v", ctx, got)
		}
	}
}

func TestScrolledColumnShowsMoreAboveAndBelow(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		base := calColumnAfter(t, a, tv)
		// The unscrolled column spends one line on the bottom count, so its
		// room is the entries listed plus that line.
		room := len(base.entries) + 1
		if room < 5 {
			t.Fatalf("%s: column room %d is too small for the check", ctx, room)
		}
		calPress(a, tv, calRunes("jjj")...)
		got := calColumnAfter(t, a, tv)
		// Scrolled by three: a top line, a bottom line, entries between.
		wantEntries := room - 2
		if got.above != 3 {
			t.Errorf("%s: top count = %d, want +3 more", ctx, got.above)
		}
		if len(got.entries) != wantEntries || got.entries[0] != 3 || got.entries[len(got.entries)-1] != 3+wantEntries-1 {
			t.Errorf("%s: entries = %v, want %d entries from the 4th", ctx, got.entries, wantEntries)
		}
		if want := colScrollTotal - 3 - wantEntries; got.below != want {
			t.Errorf("%s: bottom count = %d, want +%d more", ctx, got.below, want)
		}
		lines := calScreen(a, tv, 120, 70)
		if top, firstEntry := calRowOf(lines, "+3 more"), calRowOf(lines, "zq03"); top < 0 || top != firstEntry-1 {
			t.Errorf("%s: `+3 more` on row %d, want the line right above the first entry (row %d)", ctx, top, firstEntry)
		}
	}
}

func TestColumnScrolledToEndShowsOnlyTopMore(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		room := len(calColumnAfter(t, a, tv).entries) + 1
		for i := 0; i < colScrollTotal+20; i++ {
			calPress(a, tv, vim.R('j'))
		}
		got := calColumnAfter(t, a, tv)
		wantEntries := room - 1
		if got.below != -1 {
			t.Errorf("%s: scrolled to the end still shows a bottom count: %v", ctx, got)
		}
		if want := colScrollTotal - wantEntries; got.above != want {
			t.Errorf("%s: top count = %d, want +%d more", ctx, got.above, want)
		}
		if len(got.entries) != wantEntries || got.entries[len(got.entries)-1] != colScrollTotal-1 {
			t.Errorf("%s: entries = %v, want the last %d ending at the 50th", ctx, got.entries, wantEntries)
		}
		requireDay(t, tv, "2026-11-18", ctx)
	}
}

func TestUnscrolledColumnHasNoTopMore(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		got := calColumnAfter(t, a, tv)
		if got.above != -1 {
			t.Errorf("%s: unscrolled column has a top count: %v", ctx, got)
		}
		if got.below != colScrollTotal-len(got.entries) {
			t.Errorf("%s: bottom count = %d, want +%d more", ctx, got.below, colScrollTotal-len(got.entries))
		}
		// Scroll down and all the way back: the top line goes away again.
		calPress(a, tv, calRunes("jjjj")...)
		calPress(a, tv, calRunes("kkkkk")...)
		if got := calColumnAfter(t, a, tv); got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: column scrolled back to the top = %v", ctx, got)
		}
	}
}

func TestColumnScrollArrowsWithoutVimMode(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, false)
		ctx := fmt.Sprintf("layout %v", sub)
		calPress(a, tv, vim.KeyDown, vim.KeyDown)
		got := calColumnAfter(t, a, tv)
		if got.above != 2 || got.entries[0] != 2 {
			t.Errorf("%s: Down twice gave %v, want two entries scrolled off", ctx, got)
		}
		calPress(a, tv, vim.KeyUp)
		if got := calColumnAfter(t, a, tv); got.above != 1 || got.entries[0] != 1 {
			t.Errorf("%s: Down twice then Up gave %v, want one entry scrolled off", ctx, got)
		}
		if tv.calc().focus != calFocusGrid || tv.selectedTask() != nil {
			t.Errorf("%s: arrows changed focus or selected an entry", ctx)
		}
		requireDay(t, tv, "2026-11-18", ctx)
	}
}

func TestColumnFollowsSelectionInItemFocus(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		calPress(a, tv, vim.KeyTab)
		if tv.calc().focus != calFocusItems {
			t.Fatalf("%s: Tab did not focus the column", ctx)
		}
		for i := 0; i < 40; i++ {
			calPress(a, tv, vim.R('j'))
			sel := tv.selectedTask()
			if sel == nil {
				t.Fatalf("%s: no selection after %d j", ctx, i+1)
			}
			n, _ := strconv.Atoi(strings.TrimPrefix(sel.Content, "zq"))
			if got := calColumnAfter(t, a, tv); !slices.Contains(got.entries, n) {
				t.Fatalf("%s: selected zq%02d is off screen: %v", ctx, n, got)
			}
		}
		calPress(a, tv, calRunes(strings.Repeat("k", 40))...)
		if got := calColumnAfter(t, a, tv); got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: back on the first entry the column is still scrolled: %v", ctx, got)
		}
	}
}
