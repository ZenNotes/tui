package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vim"
)

// calMouseFiles gives the calendar dated entries on 2026-11-18 and 19 and
// three undated tasks for the No-date strip.
func calMouseFiles() map[string]string {
	return map[string]string{"work.md": calWorkNote, "strip.md": calStripNote}
}

// calMouseSubs are the layouts a click has to work in, each with a day that
// holds no entries and is on screen next to 2026-11-21.
var calMouseSubs = []struct {
	name   string
	sub    calSub
	target time.Time
}{
	{"month", calMonth, calDate(2026, time.November, 25)},
	{"week", calWeek, calDate(2026, time.November, 20)},
	{"day", calDay, calDate(2026, time.November, 21)},
}

// calPaneBody renders the Tasks view at the size its pane gives it, as the
// frame does, and returns the lines (line 0 is the view's own header).
func calPaneBody(a *App, tv *tasksView) []string {
	cr := a.contentRect(a.activePane)
	return strings.Split(stripAnsi(tv.render(a, cr.w, cr.h, true)), "\n")
}

// calCellOf is the cell column where text starts on a line.
func calCellOf(line, text string) int {
	i := strings.Index(line, text)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(line[:i])
}

// calFindIn looks for text on the rendered lines inside a layout rectangle
// (body cells, so line 0 of the view is the header above it) and returns the
// view-relative cell.
func calFindIn(lines []string, r calRect, text string) (int, int, bool) {
	for y := r.y; y < r.y+r.h && y+1 < len(lines); y++ {
		line := lines[y+1]
		if x := calCellOf(line, text); x >= r.x && x < r.x+r.w {
			return x, y + 1, true
		}
	}
	return 0, 0, false
}

func calBoxFor(t *testing.T, tv *tasksView, day time.Time) calRect {
	t.Helper()
	for _, b := range tv.calc().layout.boxes {
		if b.day.Format("2006-01-02") == day.Format("2006-01-02") {
			// A week column is its all-day band and the hour rows under it.
			// The day view has the all-day pane only, beside the hour rows.
			r := b.rect
			if tv.calc().sub == calWeek {
				r.h += b.hours.h
			}
			return r
		}
	}
	t.Fatalf("layout has no box for %s", day.Format("2006-01-02"))
	return calRect{}
}

// calInDayArea reports whether a cell is in the box of a day or, in the day
// view, in its hour rows, where the cursor of the grid is.
func calInDayArea(tv *tasksView, box calRect, x, y int) bool {
	if box.contains(x, y) {
		return true
	}
	for _, b := range tv.calc().layout.boxes {
		if b.hours.contains(x, y) {
			return true
		}
	}
	return false
}

// calMouse sends one mouse press at a view-relative cell through the app's
// real mouse entry point.
func calMouse(a *App, button tea.MouseButton, x, y int) {
	cr := a.contentRect(a.activePane)
	a.handleMouse(tea.MouseMsg{X: cr.x + x, Y: cr.y + y, Button: button, Action: tea.MouseActionPress})
}

// calDouble clicks the same cell twice, quickly.
func calDouble(a *App, x, y int) {
	calMouse(a, tea.MouseButtonLeft, x, y)
	calMouse(a, tea.MouseButtonLeft, x, y)
}

// calEmptyDayCell is a view-relative cell inside a day's box or column, low
// enough to be clear of its entries.
func calEmptyDayCell(r calRect) (int, int) {
	return r.x + r.w/2, 1 + r.y + r.h - 1
}

func requireMenuTitled(t *testing.T, a *App, want, context string) {
	t.Helper()
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Errorf("%s: overlay = %T, want the task menu", context, a.overlay)
		return
	}
	if !strings.Contains(m.title, want) {
		t.Errorf("%s: menu title = %q, want the task %q", context, m.title, want)
	}
}

func TestClickDaySelectsWithGridFocus(t *testing.T) {
	for _, s := range calMouseSubs {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		tv.selectDay(a, calDate(2026, time.November, 21))
		tv.calc().focus = calFocusStrip
		requireFocus(t, tv, calFocusStrip, s.name+" set-up")
		calPaneBody(a, tv)
		x, y := calEmptyDayCell(calBoxFor(t, tv, s.target))
		calMouse(a, tea.MouseButtonLeft, x, y)
		requireDay(t, tv, s.target.Format("2006-01-02"), s.name+" click")
		requireFocus(t, tv, calFocusGrid, s.name+" click")
		if tv.calc().sub != s.sub {
			t.Errorf("%s: a click changed the layout to %v", s.name, tv.calc().sub)
		}
	}
}

func TestClickDayFromItemsFocus(t *testing.T) {
	for _, s := range calMouseSubs[:2] {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		calPress(a, tv, vim.KeyEnter)
		requireFocus(t, tv, calFocusItems, s.name+" set-up")
		calPaneBody(a, tv)
		x, y := calEmptyDayCell(calBoxFor(t, tv, s.target))
		calMouse(a, tea.MouseButtonLeft, x, y)
		requireDay(t, tv, s.target.Format("2006-01-02"), s.name+" click")
		requireFocus(t, tv, calFocusGrid, s.name+" click")
	}
}

func TestDoubleClickDayActsAsEnter(t *testing.T) {
	for _, s := range calMouseSubs {
		// An empty day opens its daily note. There is no No-date strip here,
		// because Enter on a grid with a strip moves focus to it first.
		a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, s.sub)
		tv.selectDay(a, calDate(2026, time.November, 21))
		calPaneBody(a, tv)
		x, y := calEmptyDayCell(calBoxFor(t, tv, s.target))
		calDouble(a, x, y)
		want := calDailyPath(a, s.target)
		if tab := a.activeTab(); tab == nil || tab.path != want {
			t.Errorf("%s: double-click on an empty day: active tab = %+v, want the daily note %s", s.name, tab, want)
		}
	}
	for _, s := range calMouseSubs[:2] {
		// A day with entries focuses them.
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		tv.selectDay(a, calDate(2026, time.November, 21))
		calPaneBody(a, tv)
		x, y := calEmptyDayCell(calBoxFor(t, tv, calDate(2026, time.November, 18)))
		calDouble(a, x, y)
		requireDay(t, tv, "2026-11-18", s.name+" double-click")
		requireFocus(t, tv, calFocusItems, s.name+" double-click on a day with entries")
		requireSelected(t, tv, "alpha job", s.name+" double-click")
	}
}

// calEntryRegion is the rectangle the 2026-11-18 entries are listed in: the
// item panel in month view, the day's column in week and day view.
func calEntryRegion(t *testing.T, tv *tasksView, sub calSub) calRect {
	t.Helper()
	if sub == calMonth {
		return tv.calc().layout.items
	}
	return calBoxFor(t, tv, calDate(2026, time.November, 18))
}

// calFindEntry renders the calendar and finds the bravo entry on screen.
func calFindEntry(t *testing.T, a *App, tv *tasksView, name string, sub calSub) (int, int) {
	t.Helper()
	lines := calPaneBody(a, tv)
	x, y, ok := calFindIn(lines, calEntryRegion(t, tv, sub), "bravo job")
	if !ok {
		t.Fatalf("%s: bravo job is not shown where the entries are listed:\n%s", name, calScreenText(lines))
	}
	return x, y
}

func TestClickEntryFocusesItems(t *testing.T) {
	for _, s := range calMouseSubs {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		x, y := calFindEntry(t, a, tv, s.name, s.sub)
		calMouse(a, tea.MouseButtonLeft, x, y)
		requireFocus(t, tv, calFocusItems, s.name+" click on an entry")
		requireSelected(t, tv, "bravo job", s.name+" click on an entry")
		requireDay(t, tv, "2026-11-18", s.name+" click on an entry")
	}
}

func TestRightClickEntryOpensTaskMenu(t *testing.T) {
	for _, s := range calMouseSubs {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		x, y := calFindEntry(t, a, tv, s.name, s.sub)
		calMouse(a, tea.MouseButtonRight, x, y)
		requireMenuTitled(t, a, "bravo job", s.name+" right-click on an entry")
	}
}

func TestDoubleClickEntryOpensSource(t *testing.T) {
	for _, s := range calMouseSubs {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		x, y := calFindEntry(t, a, tv, s.name, s.sub)
		calDouble(a, x, y)
		if tab := a.activeTab(); tab == nil || tab.path != "work.md" {
			t.Errorf("%s: double-click on an entry: active tab = %+v, want work.md", s.name, tab)
		}
	}
}

func TestClickStripTaskFocusesStrip(t *testing.T) {
	for _, s := range calMouseSubs {
		find := func() (*App, *tasksView, int, int) {
			a, tv, _ := calVaultApp(t, calMouseFiles())
			calToSub(t, a, tv, s.sub)
			lines := calPaneBody(a, tv)
			x, y, ok := calFindIn(lines, tv.calc().layout.strip, "bbb strip")
			if !ok {
				t.Fatalf("%s: bbb strip is not shown in the No-date strip:\n%s", s.name, calScreenText(lines))
			}
			return a, tv, x, y
		}

		a, tv, x, y := find()
		calMouse(a, tea.MouseButtonLeft, x, y)
		requireFocus(t, tv, calFocusStrip, s.name+" click on a strip task")
		requireSelected(t, tv, "bbb strip", s.name+" click on a strip task")

		a, _, x, y = find()
		calMouse(a, tea.MouseButtonRight, x, y)
		requireMenuTitled(t, a, "bbb strip", s.name+" right-click on a strip task")

		a, _, x, y = find()
		calDouble(a, x, y)
		if tab := a.activeTab(); tab == nil || tab.path != "strip.md" {
			t.Errorf("%s: double-click on a strip task: active tab = %+v, want strip.md", s.name, tab)
		}
	}
}

func TestWheelScrollsHourListOneRow(t *testing.T) {
	for _, c := range []struct {
		sub  calSub
		w, h int
	}{{calWeek, 64, 16}, {calDay, 60, 14}} {
		for _, half := range []bool{false, true} {
			a, tv := calTestApp(t, nil)
			a.prefs.VimMode = true
			a.prefs.TimeFormat = "24h"
			calToSub(t, a, tv, c.sub)
			if half {
				calPress(a, tv, vim.R('z'))
			}
			ctx := fmt.Sprintf("layout %v half=%v", c.sub, half)
			first := func() string {
				got := calHourLabels(calScreen(a, tv, c.w, c.h), calLabel24)
				if len(got) == 0 {
					t.Fatalf("%s: no hour rows shown", ctx)
				}
				return got[0]
			}
			day := calDayISO(tv)
			if got := first(); got != "00" {
				t.Fatalf("%s: first row = %s before the wheel, want 00", ctx, got)
			}
			next := "01"
			if half {
				next = "00:30"
			}
			// The app sends the wheel as three steps; the list moves one row.
			tv.scrollBy(a, 3)
			if got := first(); got != next {
				t.Errorf("%s: first row after one wheel step down = %s, want %s", ctx, got, next)
			}
			tv.scrollBy(a, -3)
			if got := first(); got != "00" {
				t.Errorf("%s: first row after one step down and one up = %s, want 00", ctx, got)
			}
			tv.scrollBy(a, -3)
			if got := first(); got != "00" {
				t.Errorf("%s: wheel up at the top scrolled to %s, want 00", ctx, got)
			}
			requireDay(t, tv, day, ctx+" wheel")
		}
	}
}

func TestSelectionPosFollowsFocus(t *testing.T) {
	for _, s := range calMouseSubs {
		a, tv, _ := calVaultApp(t, calMouseFiles())
		calToSub(t, a, tv, s.sub)
		p := a.activePane
		cr := a.contentRect(p)
		at := func(context string) (int, int, []string) {
			t.Helper()
			lines := calPaneBody(a, tv)
			x, y, ok := tv.selectionPos(a, p)
			if !ok {
				t.Fatalf("%s %s: selectionPos reported no position", s.name, context)
			}
			return x - cr.x, y - cr.y, lines
		}

		// Grid focus: inside the selected day's box or column.
		x, y, _ := at("grid focus")
		if box := calBoxFor(t, tv, calDate(2026, time.November, 18)); !calInDayArea(tv, box, x, y-1) {
			t.Errorf("%s: grid focus position (%d,%d) is outside the selected day's box %+v", s.name, x, y-1, box)
		}
		tv.selectDay(a, s.target)
		x, y, _ = at("moved selection")
		if box := calBoxFor(t, tv, s.target); !calInDayArea(tv, box, x, y-1) {
			t.Errorf("%s: position (%d,%d) did not follow the selection to %s's box %+v", s.name, x, y-1, s.target.Format("2006-01-02"), box)
		}
		tv.selectDay(a, calDate(2026, time.November, 18))

		// Items focus: on the selected entry's line.
		calPress(a, tv, vim.KeyEnter, vim.R('j'))
		requireFocus(t, tv, calFocusItems, s.name+" set-up")
		requireSelected(t, tv, "bravo job", s.name+" set-up")
		_, y, lines := at("items focus")
		if y < 0 || y >= len(lines) || !strings.Contains(lines[y], "bravo job") {
			t.Errorf("%s: items focus position row %d is not the selected entry's line", s.name, y)
		}
		calPress(a, tv, vim.R('k'))
		_, y2, lines := at("items focus moved")
		if y2 == y || y2 < 0 || y2 >= len(lines) || !strings.Contains(lines[y2], "alpha job") {
			t.Errorf("%s: position did not follow the selection to alpha job: row %d then %d", s.name, y, y2)
		}

		// Strip focus: on the selected strip task's line.
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusStrip, s.name+" set-up")
		calPress(a, tv, vim.R('j'))
		requireSelected(t, tv, "bbb strip", s.name+" set-up")
		_, y, lines = at("strip focus")
		if y < 0 || y >= len(lines) || !strings.Contains(lines[y], "bbb strip") {
			t.Errorf("%s: strip focus position row %d is not the selected strip task's line", s.name, y)
		}
	}
}

const calSlotWork = "- [ ] Nine due:2026-11-05T09:00\n"

// calSlotApp opens a layout on 2026-11-05 over a timed task at 09:00 and the
// event Dentist from 14:00 to 15:30, with the cursor on the 00 slot.
func calSlotApp(t *testing.T, sub calSub) (*App, *tasksView) {
	t.Helper()
	a, tv, _ := calEditApp(t, map[string]string{"Plans.md": calEditPlans, "Work.md": calSlotWork}, sub)
	tv.selectDay(a, calDate(2026, time.November, 5))
	tv.calc().setCursorRow(0)
	return a, tv
}

// calSlotCell finds text in the hour rows of a day and gives its cell.
func calSlotCell(t *testing.T, a *App, tv *tasksView, day time.Time, text string) (int, int) {
	t.Helper()
	lines := calPaneBody(a, tv)
	for _, b := range tv.calc().layout.boxes {
		if sameDay(b.day, day) {
			if x, y, ok := calFindIn(lines, b.hours, text); ok {
				return x, y
			}
		}
	}
	t.Fatalf("%q is not in the hour rows of %s:\n%s", text, day.Format("2006-01-02"), calScreenText(lines))
	return 0, 0
}

func TestClickSelectsSlotEntry(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calSlotApp(t, sub)
		tv.calc().setCursorRow(11)
		day := calDate(2026, time.November, 5)
		x, y := calSlotCell(t, a, tv, day, "Dentist")
		// A click on the second row of the block selects the block and that slot.
		a.focus = focusSidebar
		calMouse(a, tea.MouseButtonLeft, x, y+1)
		if a.focus != focusPane {
			t.Errorf("layout %v: a click in the pane left the focus on %v", sub, a.focus)
		}
		requireFocus(t, tv, calFocusGrid, "click on an event")
		requireCursor(t, tv, 15*60, "click on the second row of an event")
		if e, ok := tv.calc().selectedEvent(day); !ok || e.content() != "Dentist" {
			t.Errorf("layout %v: selected event = %+v, want Dentist", sub, e)
		}

		x, y = calSlotCell(t, a, tv, day, "Nine")
		calMouse(a, tea.MouseButtonLeft, x, y)
		requireCursor(t, tv, 9*60, "click on a timed task")
		requireSelected(t, tv, "Nine", "click on a timed task")

		// An empty slot moves the cursor and selects nothing.
		calMouse(a, tea.MouseButtonLeft, x, y+1)
		requireCursor(t, tv, 10*60, "click on an empty slot")
		requireSelectedContent(t, tv, "", "click on an empty slot")
		requireDay(t, tv, "2026-11-05", "click on an empty slot")
	}
}

func TestClickSlotOnAnotherDayMovesTheDay(t *testing.T) {
	a, tv := calSlotApp(t, calWeek)
	tv.calc().setCursorRow(11)
	x, y := calSlotCell(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	var other calDayBox
	for _, b := range tv.calc().layout.boxes {
		if sameDay(b.day, calDate(2026, time.November, 6)) {
			other = b
		}
	}
	calMouse(a, tea.MouseButtonLeft, other.hours.x+1, y)
	requireDay(t, tv, "2026-11-06", "click on another day")
	requireFocus(t, tv, calFocusGrid, "click on another day")
	requireCursor(t, tv, 14*60, "click on another day")
	_ = x
}

func TestRightClickBlockMenu(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calSlotApp(t, sub)
		tv.calc().setCursorRow(11)
		day := calDate(2026, time.November, 5)
		x, y := calSlotCell(t, a, tv, day, "Dentist")
		calMouse(a, tea.MouseButtonRight, x, y)
		m, ok := a.overlay.(*menu)
		if !ok {
			t.Fatalf("layout %v: overlay = %T, want the event menu", sub, a.overlay)
		}
		if !strings.Contains(m.title, "Dentist") {
			t.Errorf("layout %v: menu title = %q", sub, m.title)
		}
		keys := ""
		for _, it := range m.items {
			keys += it.key
		}
		for _, want := range []string{"o", "T", "@", "d", "D"} {
			if !strings.Contains(keys, want) {
				t.Errorf("layout %v: event menu keys %q lack %q", sub, keys, want)
			}
		}
		for _, bad := range []string{"x", "p", "c"} {
			if strings.Contains(keys, bad) {
				t.Errorf("layout %v: event menu keys %q hold the task key %q", sub, keys, bad)
			}
		}
		// The entry of the menu does what the key does.
		for i, it := range m.items {
			if it.key == "T" {
				m.cursor = i
			}
		}
		a.handleKey(vim.KeyEnter)
		if got := calEditNote(t, a.opts.Target.Root, "Plans.md"); !strings.Contains(got, "Dentist when:2026-11-02T14:00-15:30") {
			t.Errorf("layout %v: Plans.md after the menu entry T:\n%s", sub, got)
		}

		a, tv = calSlotApp(t, sub)
		tv.calc().setCursorRow(5)
		x, y = calSlotCell(t, a, tv, day, "Nine")
		calMouse(a, tea.MouseButtonRight, x, y)
		requireMenuTitled(t, a, "Nine", "right-click on a timed task")
		requireCursor(t, tv, 9*60, "right-click on a timed task")
	}
}

func TestDoubleClickEventOpensItsNote(t *testing.T) {
	a, tv := calSlotApp(t, calDay)
	tv.calc().setCursorRow(11)
	x, y := calSlotCell(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	calDouble(a, x, y)
	if tab := a.activeTab(); tab == nil || tab.path != "Plans.md" {
		t.Errorf("double-click on an event: active tab = %+v, want Plans.md", tab)
	}
}

func TestDragChangesNothing(t *testing.T) {
	a, tv := calSlotApp(t, calDay)
	tv.calc().setCursorRow(11)
	day := calDate(2026, time.November, 5)
	x, y := calSlotCell(t, a, tv, day, "Dentist")
	cr := a.contentRect(a.activePane)
	a.handleMouse(tea.MouseMsg{X: cr.x + x, Y: cr.y + y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	a.handleMouse(tea.MouseMsg{X: cr.x + x, Y: cr.y + y - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	a.handleMouse(tea.MouseMsg{X: cr.x + x, Y: cr.y + y - 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if got := calEditNote(t, a.opts.Target.Root, "Plans.md"); got != calEditPlans {
		t.Errorf("a drag changed Plans.md:\n%s", got)
	}
	requireCursor(t, tv, 14*60, "after a drag")
	if e, ok := tv.calc().selectedEvent(day); !ok || e.when != "2026-11-05T14:00-15:30" {
		t.Errorf("event after a drag = %+v", e)
	}
}

func TestWheelDoesNotMoveTheCursor(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calSlotApp(t, sub)
		tv.calc().setCursorRow(3)
		calPaneBody(a, tv)
		cr := a.contentRect(a.activePane)
		a.handleMouse(tea.MouseMsg{X: cr.x + 10, Y: cr.y + 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
		requireCursor(t, tv, 3*60, "wheel down")
		requireDay(t, tv, "2026-11-05", "wheel down")
		a.handleMouse(tea.MouseMsg{X: cr.x + 10, Y: cr.y + 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
		requireCursor(t, tv, 3*60, "wheel up")
	}
}
