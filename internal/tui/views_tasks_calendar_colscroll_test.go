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

// colScrollTotal is more than the day view's pane can list at 120x70.
const colScrollTotal = 80

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

func TestGridKeysDoNotScrollTheAllDayColumn(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		before := calColumnAfter(t, a, tv)
		if before.above != -1 || before.below <= 0 || before.entries[0] != 0 {
			t.Fatalf("%s: unscrolled column = %v", ctx, before)
		}
		calPress(a, tv, calRunes("jjj")...)
		if got := calColumnAfter(t, a, tv); got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: j in the grid scrolled the column to %v", ctx, got)
		}
		if tv.calc().focus != calFocusGrid {
			t.Errorf("%s: j moved focus to %v, want the grid", ctx, tv.calc().focus)
		}
		requireDay(t, tv, "2026-11-18", ctx+" after j")
	}
}

func TestAllDayColumnShowsMoreAboveAndBelow(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		calPress(a, tv, vim.R('a'))
		first := calColumnAfter(t, a, tv)
		if first.above != -1 || first.below <= 0 {
			t.Fatalf("%s: column with the first entry selected = %v", ctx, first)
		}
		room := len(first.entries)
		for i := 0; i < room+5; i++ {
			calPress(a, tv, vim.R('j'))
		}
		got := calColumnAfter(t, a, tv)
		if got.above <= 0 || got.below <= 0 {
			t.Errorf("%s: a column scrolled to the middle needs a count above and below: %v", ctx, got)
		}
		lines := calScreen(a, tv, 120, 70)
		if top, firstEntry := calRowOf(lines, fmt.Sprintf("+%d more", got.above)), calRowOf(lines, fmt.Sprintf("zq%02d", got.entries[0])); top < 0 || top != firstEntry-1 {
			t.Errorf("%s: the top count is on row %d, want the line right above the first entry (row %d)", ctx, top, firstEntry)
		}
	}
}

func TestAllDayColumnScrolledToEndShowsOnlyTopMore(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		calPress(a, tv, vim.R('a'))
		for i := 0; i < colScrollTotal+20; i++ {
			calPress(a, tv, vim.R('j'))
		}
		got := calColumnAfter(t, a, tv)
		if got.below != -1 || got.above <= 0 || got.entries[len(got.entries)-1] != colScrollTotal-1 {
			t.Errorf("%s: column scrolled to the end = %v", ctx, got)
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
		calPress(a, tv, vim.R('a'))
		calPress(a, tv, calRunes("jjjj")...)
		calPress(a, tv, calRunes("kkkkk")...)
		if got := calColumnAfter(t, a, tv); got.above != -1 || got.entries[0] != 0 {
			t.Errorf("%s: column scrolled back to the top = %v", ctx, got)
		}
	}
}

func TestColumnFollowsSelectionInItemFocus(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calColumnApp(t, sub, true)
		ctx := fmt.Sprintf("layout %v", sub)
		calPress(a, tv, vim.R('a'))
		if tv.calc().focus != calFocusItems {
			t.Fatalf("%s: a did not focus the column", ctx)
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
