package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
	"github.com/charmbracelet/lipgloss"
)

// calNoon is the fixed time that the tests of these keys read as now.
var calNoon = time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local)

// calTabsApp opens the calendar over note files with a second tab open and
// the calendar tab active, so that a tab key has a tab to switch to.
func calTabsApp(t *testing.T, sub calSub) (*App, *tasksView) {
	t.Helper()
	files := map[string]string{"Work.md": calCursorWork, "Other.md": "# Other\n"}
	a, tv, _ := calCursorApp(t, files, sub, false)
	calFixClock(t, a, calNoon)
	a.openNote("Other.md", true)
	a.selectTab(1)
	if a.activeTab() == nil || a.activeTab().view != tv {
		t.Fatal("the calendar tab is not active")
	}
	return a, tv
}

func TestBracketKeysDoNothingInWeekAndDayView(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv := calTabsApp(t, sub)
		day, month := calDayISO(tv), tv.month
		for _, r := range []rune{'[', ']'} {
			calPress(a, tv, vim.R(r))
			requireDay(t, tv, day, "a bracket key")
			if !tv.month.Equal(month) {
				t.Errorf("layout %v: %c moved the month to %s", sub, r, tv.month.Format("2006-01"))
			}
		}
	}
}

func TestTabKeysSwitchTabsInWeekAndDayView(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		for _, keys := range []string{"]b", "[b"} {
			a, tv := calTabsApp(t, sub)
			day := calDayISO(tv)
			for _, k := range calRunes(keys) {
				a.handleKey(k)
			}
			if a.activeTab() != nil && a.activeTab().view == tv {
				t.Errorf("layout %v: %s did not switch the tab", sub, keys)
			}
			requireDay(t, tv, day, keys)
		}
	}
}

func TestBracketKeysStillMoveTheMonthInMonthView(t *testing.T) {
	a, tv := calTabsApp(t, calMonth)
	a.handleKey(vim.R(']'))
	requireMonth(t, tv, 2026, time.December, "]")
	if a.activeTab() == nil || a.activeTab().view != tv {
		t.Error("] switched the tab in the month view")
	}
	a.handleKey(vim.R('['))
	a.handleKey(vim.R('['))
	requireMonth(t, tv, 2026, time.October, "[")
}

func TestShiftHLMoveTheMonthInEveryView(t *testing.T) {
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, sub, false)
		calFixClock(t, a, calNoon)
		calPress(a, tv, vim.R('H'))
		requireDay(t, tv, "2026-10-03", "H")
		requireMonth(t, tv, 2026, time.October, "H")
		calPress(a, tv, vim.R('L'), vim.R('L'))
		requireDay(t, tv, "2026-12-03", "L L")
		requireMonth(t, tv, 2026, time.December, "L L")
	}
}

func TestHourLabelOfTheCursorSlotIsInTheCursorStyle(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, false)
	calFixClock(t, a, time.Date(2026, time.November, 3, 9, 30, 0, 0, time.Local))
	label := func(hour int) string {
		return padRight(hourLabel(a, hour*60), tv.calc().layout.gutter-2)
	}
	now := func(th Theme) lipgloss.Style { return th.Base.Foreground(th.Accent).Bold(true) }
	cursor := func(th Theme) lipgloss.Style { return th.SelectedFocus }
	requireLabel := func(hour int, style func(Theme) lipgloss.Style, context string) {
		t.Helper()
		raw := calRawScreen(t, a, tv, 120, 70)
		plain := calScreen(a, tv, 120, 70)
		row := raw[calHourRowIndex(t, plain, fmt.Sprintf("%02d", hour))]
		if want := style(a.theme).Render(label(hour)); !strings.Contains(row, want) {
			t.Errorf("%s: the %02d label is %q, want %q in it", context, hour, row, want)
		}
	}
	// The 09 row holds the current time and a block.
	calDownTo(a, tv, 9)
	requireLabel(9, cursor, "cursor on the row of the current time")
	calPress(a, tv, vim.R('j'))
	requireLabel(9, now, "cursor moved away from the current time")
	requireLabel(10, cursor, "cursor on an empty slot")
	calDownTo(a, tv, 4)
	requireLabel(14, cursor, "cursor on a slot that a block covers")
	requireLabel(9, now, "the current time row after the cursor left")
}

const calBraceWork = "- [ ] A due:2026-11-03T09:00-11:30\n- [ ] B due:2026-11-03T14:00-16:30\n- [ ] C due:2026-11-04T08:00\n"

func calBraceApp(t *testing.T, sub calSub, vimOff bool) (*App, *tasksView) {
	t.Helper()
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calBraceWork}, sub, vimOff)
	calFixClock(t, a, calNoon)
	return a, tv
}

func TestBracesMoveToTheEdgesOfEntries(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calBraceApp(t, sub, false)
		step := func(key rune, row int, entry, context string) {
			t.Helper()
			calPress(a, tv, vim.R(key))
			requireCursor(t, tv, row*60, context)
			requireSelectedContent(t, tv, entry, context)
			requireDay(t, tv, "2026-11-03", context)
		}
		step('}', 9, "A", "} from an empty slot")
		step('}', 11, "A", "} inside an entry")
		step('}', 14, "B", "} at the end of an entry")
		step('}', 16, "B", "} inside B")
		step('}', 16, "B", "} with no later entry")
		step('{', 14, "B", "{ inside an entry")
		step('{', 11, "A", "{ at the start of an entry")
		step('{', 9, "A", "{ inside A")
		step('{', 9, "A", "{ with no earlier entry")

		tv.calc().setCursorRow(12)
		step('}', 14, "B", "} from an empty slot between entries")
		tv.calc().setCursorRow(12)
		step('{', 11, "A", "{ from an empty slot between entries")
		tv.calc().setCursorRow(10)
		step('{', 9, "A", "{ from the middle of an entry")
		tv.calc().setCursorRow(10)
		step('}', 11, "A", "} from the middle of an entry")
	}
}

func TestBracesNeverChangeTheDay(t *testing.T) {
	a, tv := calBraceApp(t, calWeek, false)
	tv.calc().setCursorRow(16)
	calPress(a, tv, vim.R('}'))
	requireDay(t, tv, "2026-11-03", "} after the last entry of the day")
	requireCursor(t, tv, 16*60, "} after the last entry of the day")
	tv.calc().setCursorRow(9)
	calPress(a, tv, vim.R('{'))
	requireDay(t, tv, "2026-11-03", "{ before the first entry of the day")
}

func TestBracesNeedVimMode(t *testing.T) {
	a, tv := calBraceApp(t, calDay, true)
	for _, r := range []rune{'}', '{'} {
		if tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was handled without Vim mode", r)
		}
	}
	requireCursor(t, tv, 0, "braces without Vim mode")
}

const calPaneDays = "- [ ] Pay rent due:2026-11-03\n- [ ] Standup due:2026-11-04T09:00\n" +
	"- [ ] Tea due:2026-11-05\n- [ ] Coffee due:2026-11-05\n- [ ] Someday\n"

func TestAllDayPaneHLMoveTheDayAndKeepTheFocus(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneDays}, sub, false)
		calFixClock(t, a, calNoon)
		calPress(a, tv, vim.R('a'))
		requireSelectedContent(t, tv, "Pay rent", "a")
		calPress(a, tv, vim.R('l'))
		requireDay(t, tv, "2026-11-04", "l")
		requireFocus(t, tv, calFocusItems, "l onto a day with no all-day entry")
		requireSelectedContent(t, tv, "", "l onto a day with no all-day entry")
		calPress(a, tv, vim.R('j'), vim.R('k'))
		requireSelectedContent(t, tv, "", "j and k in an empty pane")
		if sub == calDay {
			lines := calScreen(a, tv, 120, 70)
			if text := footerText(footerLines(t, lines, 70)); text != "" {
				t.Errorf("the footer of an empty pane reads %q, want it blank", text)
			}
		}
		calPress(a, tv, vim.R('l'))
		requireDay(t, tv, "2026-11-05", "second l")
		requireFocus(t, tv, calFocusItems, "second l")
		requireSelectedContent(t, tv, "Tea", "the first entry of the new day")
		calPress(a, tv, vim.R('j'))
		requireSelectedContent(t, tv, "Coffee", "j")
		calPress(a, tv, vim.R('h'), vim.R('h'), vim.R('h'))
		requireDay(t, tv, "2026-11-02", "three h")
		requireFocus(t, tv, calFocusItems, "three h")
		calPress(a, tv, vim.R('l'))
		requireSelectedContent(t, tv, "Pay rent", "back on the first day")
	}
}

func TestAllDayPaneEscAndGReturnToTheGrid(t *testing.T) {
	for _, key := range []vim.Key{vim.KeyEsc, vim.R('G')} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneDays}, calDay, false)
		calFixClock(t, a, calNoon)
		calPress(a, tv, vim.R('a'), vim.R('l'))
		requireFocus(t, tv, calFocusItems, "l")
		calPress(a, tv, key)
		requireFocus(t, tv, calFocusGrid, "return to the grid")
		requireDay(t, tv, "2026-11-04", "return to the grid")
	}
}

func TestGGFocusesThePaneAndGLeavesIt(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneDays}, sub, false)
		calFixClock(t, a, calNoon)
		tv.calc().setCursorRow(10)
		a.handleKey(vim.R('g'))
		a.handleKey(vim.R('g'))
		requireFocus(t, tv, calFocusItems, "gg from the grid")
		requireSelectedContent(t, tv, "Pay rent", "gg from the grid")
		a.handleKey(vim.R('G'))
		requireFocus(t, tv, calFocusGrid, "G from the pane")
		requireCursor(t, tv, 10*60, "G returns to the earlier slot")

		a.handleKey(vim.R('g'))
		a.handleKey(vim.R('g'))
		a.handleKey(vim.KeyTab)
		requireFocus(t, tv, calFocusStrip, "Tab from the pane")
		a.handleKey(vim.R('g'))
		a.handleKey(vim.R('g'))
		requireFocus(t, tv, calFocusItems, "gg from the strip")
		a.handleKey(vim.KeyTab)
		a.handleKey(vim.R('G'))
		requireFocus(t, tv, calFocusGrid, "G from the strip")
		requireCursor(t, tv, 10*60, "G from the strip")
	}
}

func TestGGNeedsVimModeAndKeepsTabKeys(t *testing.T) {
	a, tv := calTabsApp(t, calDay)
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('t'))
	if a.activeTab() != nil && a.activeTab().view == tv {
		t.Error("gt did not switch the tab")
	}
	a.selectTab(1)
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('T'))
	if a.activeTab() != nil && a.activeTab().view == tv {
		t.Error("gT did not switch the tab")
	}
	a.selectTab(1)
	requireFocus(t, tv, calFocusGrid, "tab keys")

	a.prefs.VimMode = false
	a.handleKey(vim.R('g'))
	a.handleKey(vim.R('g'))
	requireFocus(t, tv, calFocusGrid, "gg without Vim mode")
}

func TestCompleteAndCancelDoNothingOnCancelledSession(t *testing.T) {
	files := map[string]string{
		"inbox/Work.md":  "- [-] Old plan ^dddddd\n",
		"inbox/Plans.md": "- Old plan when:2026-11-09T09:00-09:30 task:[[Work#^dddddd]]\n",
	}
	day := calDate(2026, time.November, 9)
	for _, key := range []rune{'x', 'c'} {
		a, tv, root := calEditApp(t, files, calMonth)
		calFixClock(t, a, time.Date(2026, time.November, 9, 12, 0, 0, 0, time.Local))
		before := calEditNote(t, root, "inbox/Work.md")
		calSelectEntry(t, a, tv, day, "Old plan")
		a.toasts = nil
		calPress(a, tv, vim.R(key))
		if got := calEditNote(t, root, "inbox/Work.md"); got != before {
			t.Errorf("%c changed the note:\n%s", key, got)
		}
		if a.message != "The task of this work session is cancelled" {
			t.Errorf("%c: message = %q, want %q", key, a.message, "The task of this work session is cancelled")
		}
	}

	a, tv, root := calEditApp(t, files, calDay)
	calFixClock(t, a, time.Date(2026, time.November, 9, 12, 0, 0, 0, time.Local))
	before := calEditNote(t, root, "inbox/Work.md")
	tv.selectDay(a, day)
	tv.calc().setCursorRow(9)
	calPress(a, tv, vim.R('c'))
	e, ok := tv.calc().selectedEvent(tv.day)
	if !ok {
		t.Fatal("no session under the cursor")
	}
	tv.eventMenu(a, e)
	a.overlay.handleKey(a, vim.R('t'))
	a.message = ""
	a.overlay.handleKey(a, vim.R('c'))
	if got := calEditNote(t, root, "inbox/Work.md"); got != before {
		t.Errorf("the menu changed the note:\n%s", got)
	}
	if a.message != "The task of this work session is cancelled" {
		t.Errorf("menu message = %q", a.message)
	}
}

func TestAllDayPaneArrowsMoveTheDayWithVimOff(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneDays}, sub, true)
		calFixClock(t, a, calNoon)
		calPress(a, tv, vim.KeyEnter)
		requireFocus(t, tv, calFocusItems, "Enter")
		requireSelectedContent(t, tv, "Pay rent", "Enter")
		calPress(a, tv, vim.Special("right"))
		requireDay(t, tv, "2026-11-04", "Right")
		requireFocus(t, tv, calFocusItems, "Right")
		requireSelectedContent(t, tv, "", "Right onto a day with no all-day entry")
		calPress(a, tv, vim.Special("right"))
		requireDay(t, tv, "2026-11-05", "second Right")
		requireSelectedContent(t, tv, "Tea", "second Right")
		calPress(a, tv, vim.Special("left"), vim.Special("left"))
		requireDay(t, tv, "2026-11-03", "two Left")
		requireFocus(t, tv, calFocusItems, "two Left")
		requireSelectedContent(t, tv, "Pay rent", "two Left")
		calPress(a, tv, vim.KeyEsc)
		requireFocus(t, tv, calFocusGrid, "Esc")
	}
}
