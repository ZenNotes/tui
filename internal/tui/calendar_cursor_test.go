package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

// calCursorApp opens the calendar on 2026-11-03 over note files on disk, so
// that a key can change a task and a note can be opened. Events are read from
// the same files. The keys are those of Vim mode unless vimOff is set.
func calCursorApp(t *testing.T, files map[string]string, sub calSub, vimOff bool) (*App, *tasksView, string) {
	t.Helper()
	a, root := newFeatureTestApp(t)
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := a.loadIndexCmd()().(indexLoadedMsg)
	if m.err != nil {
		t.Fatal(m.err)
	}
	a.idx = m.idx
	tm := a.loadTasksCmd()().(tasksLoadedMsg)
	if tm.err != nil {
		t.Fatal(tm.err)
	}
	a.idx.tasks, a.idx.events = tm.tasks, tm.events
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.TimeFormat = "24h"
	a, tv := calOpen(t, a)
	a.prefs.VimMode = !vimOff
	tv.selectDay(a, calDate(2026, time.November, 3))
	calToSub(t, a, tv, sub)
	a.focus = focusPane
	return a, tv, root
}

// calFixClock makes the calendar and the task paths read the given day as
// today for the rest of the test.
func calFixClock(t *testing.T, a *App, day time.Time) {
	t.Helper()
	prev := calendarNow
	calendarNow = func() time.Time { return day }
	t.Cleanup(func() { calendarNow = prev })
	a.taskNow = func() time.Time { return day }
}

// calDownTo presses j until the cursor is on the hour row with the given
// hour. The cursor must be on the 00 row before.
func calDownTo(a *App, tv *tasksView, hour int) {
	for i := 0; i < hour; i++ {
		calPress(a, tv, vim.R('j'))
	}
}

func requireCursor(t *testing.T, tv *tasksView, minute int, context string) {
	t.Helper()
	if got := tv.calc().curMin; got != minute {
		t.Errorf("%s: cursor at minute %d of the day, want %d", context, got, minute)
	}
}

func requireSelectedContent(t *testing.T, tv *tasksView, want, context string) {
	t.Helper()
	got := tv.selectedTask()
	switch {
	case want == "" && got != nil:
		t.Errorf("%s: selected task = %q, want none", context, got.Content)
	case want != "" && got == nil:
		t.Errorf("%s: no task selected, want %q", context, want)
	case want != "" && got.Content != want:
		t.Errorf("%s: selected task = %q, want %q", context, got.Content, want)
	}
}

const calCursorWork = "- [ ] Nine due:2026-11-03T09:00\n- [ ] Late due:2026-11-03T14:00\n"

func TestSlotCursorMovesOneSlot(t *testing.T) {
	files := map[string]string{"Work.md": calCursorWork}

	a, tv, _ := calCursorApp(t, files, calDay, false)
	calDownTo(a, tv, 9)
	requireCursor(t, tv, 9*60, "nine presses of j")
	requireSelectedContent(t, tv, "Nine", "cursor on the 09 row")
	calPress(a, tv, vim.R('j'))
	requireCursor(t, tv, 10*60, "j from 09")
	requireSelectedContent(t, tv, "", "cursor on the empty 10 row")
	calPress(a, tv, vim.R('k'))
	requireCursor(t, tv, 9*60, "k from 10")
	requireDay(t, tv, "2026-11-03", "j and k never change the day")

	// The half-hourly zoom moves by half an hour.
	calPress(a, tv, vim.R('z'), vim.R('j'))
	requireCursor(t, tv, 9*60+30, "j from 09:00 in the half-hourly zoom")
	calPress(a, tv, vim.R('k'))
	requireCursor(t, tv, 9*60, "k in the half-hourly zoom")
}

func TestSlotCursorStopsAtBothEnds(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, sub, false)
		calPress(a, tv, vim.R('k'))
		requireCursor(t, tv, 0, "k on the 00 row")
		for i := 0; i < 30; i++ {
			calPress(a, tv, vim.R('j'))
		}
		requireCursor(t, tv, 23*60, "j past the 23 row")
		requireDay(t, tv, "2026-11-03", "j on the last row")
	}
}

func TestSlotCursorScrollsTheGrid(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, false)
	lines := calScreen(a, tv, 120, 20)
	if _, ok := calRowLine(lines, calLabel24, "20"); ok {
		t.Fatalf("the 20 row is on screen before any scroll, so the body is too tall for this test:\n%s", calScreenText(lines))
	}
	calDownTo(a, tv, 20)
	lines = calScreen(a, tv, 120, 20)
	if _, ok := calRowLine(lines, calLabel24, "20"); !ok {
		t.Errorf("the cursor row 20 is off screen:\n%s", calScreenText(lines))
	}
	if _, ok := calRowLine(lines, calLabel24, "00"); ok {
		t.Errorf("the 00 row is still on screen after the scroll:\n%s", calScreenText(lines))
	}
	for i := 0; i < 20; i++ {
		calPress(a, tv, vim.R('k'))
	}
	lines = calScreen(a, tv, 120, 20)
	if _, ok := calRowLine(lines, calLabel24, "00"); !ok {
		t.Errorf("the cursor row 00 is off screen:\n%s", calScreenText(lines))
	}
}

func TestSlotCursorRowUsesFocusedSelectionStyle(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, false)
	calDownTo(a, tv, 10)
	plain := calScreen(a, tv, 120, 70)
	lines := calRawScreen(t, a, tv, 120, 70)
	mark := strings.SplitN(a.theme.SelectedFocus.Render("x"), "x", 2)[0]
	if mark == "" {
		t.Fatal("the focused selection style adds no escape code")
	}
	if row := lines[calHourRowIndex(t, plain, "10")]; !strings.Contains(row, mark) {
		t.Errorf("the cursor row is not in the focused selection style: %q", row)
	}
	if row := lines[calHourRowIndex(t, plain, "11")]; strings.Contains(row, mark) {
		t.Errorf("a row without the cursor is in the focused selection style: %q", row)
	}
}

func TestSlotCursorHLChangeTheDayInWeekView(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calWeek, false)
	calDownTo(a, tv, 14)
	calPress(a, tv, vim.R('l'))
	requireDay(t, tv, "2026-11-04", "l")
	requireCursor(t, tv, 14*60, "l keeps the row")
	calPress(a, tv, vim.R('h'))
	requireDay(t, tv, "2026-11-03", "h")
	requireCursor(t, tv, 14*60, "h keeps the row")
	requireSelectedContent(t, tv, "Late", "cursor back on the 14 row")
}

const calStepWork = "- [ ] A due:2026-11-03T09:00\n- [ ] B due:2026-11-03T10:15\n" +
	"- [ ] B2 due:2026-11-03T10:15\n- [ ] C due:2026-11-04T08:00\n"

func TestTabStepsThroughEntriesAcrossDays(t *testing.T) {
	type keys struct{ next, prev vim.Key }
	for _, c := range []struct {
		name   string
		keys   keys
		vimOff bool
	}{
		{"tab", keys{vim.KeyTab, vim.KeyShiftTab}, false},
		{"tab without Vim mode", keys{vim.KeyTab, vim.KeyShiftTab}, true},
		{"ctrl-n", keys{vim.Ctrl('n'), vim.Ctrl('p')}, false},
		{"ctrl-n without Vim mode", keys{vim.Ctrl('n'), vim.Ctrl('p')}, true},
	} {
		a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calStepWork}, calDay, c.vimOff)
		// From the empty 00 row the first step goes to the first entry.
		calPress(a, tv, c.keys.next)
		requireSelectedContent(t, tv, "A", c.name+": first step")
		requireCursor(t, tv, 9*60, c.name+": first step")
		calPress(a, tv, c.keys.next)
		requireSelectedContent(t, tv, "B", c.name+": second step")
		requireCursor(t, tv, 10*60, c.name+": second step")
		calPress(a, tv, c.keys.next)
		requireSelectedContent(t, tv, "B2", c.name+": third step")
		requireCursor(t, tv, 10*60, c.name+": third step")
		requireDay(t, tv, "2026-11-03", c.name+": third step")
		calPress(a, tv, c.keys.next)
		requireSelectedContent(t, tv, "C", c.name+": fourth step")
		requireCursor(t, tv, 8*60, c.name+": fourth step")
		requireDay(t, tv, "2026-11-04", c.name+": fourth step")
		calPress(a, tv, c.keys.prev)
		requireSelectedContent(t, tv, "B2", c.name+": step back")
		requireDay(t, tv, "2026-11-03", c.name+": step back")
		calPress(a, tv, c.keys.next, c.keys.next)
		requireSelectedContent(t, tv, "C", c.name+": forward again")
		calPress(a, tv, c.keys.next)
		requireSelectedContent(t, tv, "C", c.name+": no later entry")
		requireDay(t, tv, "2026-11-04", c.name+": no later entry")
		requireCursor(t, tv, 8*60, c.name+": no later entry")
	}
}

func TestTabStepsFromTheSelectedEntry(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calStepWork}, calDay, false)
	calDownTo(a, tv, 9)
	requireSelectedContent(t, tv, "A", "cursor on the 09 row")
	calPress(a, tv, vim.KeyTab)
	requireSelectedContent(t, tv, "B", "Tab from A")
	requireCursor(t, tv, 10*60, "Tab from A")
	// A move of the cursor drops the choice of Tab.
	calPress(a, tv, vim.R('k'), vim.R('j'))
	requireSelectedContent(t, tv, "B", "back on the 10 row")
	calPress(a, tv, vim.KeyTab)
	requireSelectedContent(t, tv, "B2", "Tab chose B2")
	calPress(a, tv, vim.R('k'), vim.R('j'))
	requireSelectedContent(t, tv, "B", "the move chose the earliest entry again")
}

func TestTabStepsInWeekViewAcrossDays(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calStepWork}, calWeek, false)
	calPress(a, tv, vim.KeyTab, vim.KeyTab, vim.KeyTab, vim.KeyTab)
	requireSelectedContent(t, tv, "C", "four Tab presses")
	requireDay(t, tv, "2026-11-04", "four Tab presses")
}

func TestCtrlNReachesTheCalendarFromTheApp(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calStepWork}, calDay, false)
	a.handleKey(vim.Ctrl('n'))
	requireSelectedContent(t, tv, "A", "Ctrl-N through the app")
	a.handleKey(vim.Ctrl('n'))
	requireSelectedContent(t, tv, "B", "Ctrl-N through the app")
	a.handleKey(vim.Ctrl('p'))
	requireSelectedContent(t, tv, "A", "Ctrl-P through the app")
}

const calPaneWork = "- [ ] Pay rent due:2026-11-03\n- [ ] Standup due:2026-11-03T09:00\n- [ ] Someday\n"

func TestAllDayFocusKeys(t *testing.T) {
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv, root := calCursorApp(t, map[string]string{"Work.md": calPaneWork}, sub, false)
		calPress(a, tv, vim.R('a'))
		requireFocus(t, tv, calFocusItems, "a")
		requireSelectedContent(t, tv, "Pay rent", "a")
		calPress(a, tv, vim.R('j'), vim.R('j'))
		requireSelectedContent(t, tv, "Pay rent", "j in a pane of one entry")
		calPress(a, tv, vim.R('k'))
		requireSelectedContent(t, tv, "Pay rent", "k in a pane of one entry")

		calPress(a, tv, vim.KeyEsc)
		requireFocus(t, tv, calFocusGrid, "Esc")
		requireDay(t, tv, "2026-11-03", "Esc")
		calPress(a, tv, vim.R('a'), vim.KeyEsc)
		requireFocus(t, tv, calFocusGrid, "Esc")

		calPress(a, tv, vim.R('a'), vim.KeyTab)
		requireFocus(t, tv, calFocusStrip, "Tab from the pane")
		requireSelectedContent(t, tv, "Someday", "Tab from the pane")
		calPress(a, tv, vim.KeyEsc)
		requireFocus(t, tv, calFocusGrid, "Esc from the strip")

		calPress(a, tv, vim.R('a'), vim.R('x'))
		if got := calReadFile(t, root, "Work.md"); !strings.Contains(got, "- [x] Pay rent") {
			t.Errorf("layout %v: x in the pane did not toggle Pay rent:\n%s", sub, got)
		}
	}

	// Without a task in the strip, Tab goes back to the grid.
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": "- [ ] Pay rent due:2026-11-03\n"}, calDay, false)
	calPress(a, tv, vim.R('a'), vim.KeyTab)
	requireFocus(t, tv, calFocusGrid, "Tab with an empty strip")

	// Without an untimed task, a leaves the focus on the grid.
	a, tv, _ = calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, false)
	calPress(a, tv, vim.R('a'))
	requireFocus(t, tv, calFocusGrid, "a without an untimed task")
}

func TestEnterOnEmptySlotWithVimOff(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneWork}, calDay, true)
	if tv.handleKey(a, vim.R('a')) {
		t.Error("a was handled without Vim mode")
	}
	if tv.handleKey(a, vim.R('j')) {
		t.Error("j was handled without Vim mode")
	}
	requireCursor(t, tv, 0, "letters without Vim mode")
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusItems, "Enter with an untimed task")
	requireSelectedContent(t, tv, "Pay rent", "Enter with an untimed task")

	// No untimed task on the day, one in the strip.
	a, tv, _ = calCursorApp(t, map[string]string{"Work.md": "- [ ] Someday\n"}, calDay, true)
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusStrip, "Enter with a task in the strip")
	requireSelectedContent(t, tv, "Someday", "Enter with a task in the strip")

	// Nothing to focus: the daily note opens.
	a, tv, _ = calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, true)
	calPress(a, tv, vim.KeyEnter)
	want := calDailyPath(a, calDate(2026, time.November, 3))
	if tab := a.activeTab(); tab == nil || tab.path != want {
		t.Errorf("active tab = %+v, want the daily note %s", tab, want)
	}
	requireFocus(t, tv, calFocusGrid, "Enter with nothing to focus")
}

func TestEnterOnEmptySlotWithVimOn(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneWork}, calWeek, false)
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusItems, "Enter in week view")
}

func TestOKeyOpensTheDailyNoteFromTheGrid(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": calPaneWork}, calDay, false)
	calPress(a, tv, vim.R('o'))
	want := calDailyPath(a, calDate(2026, time.November, 3))
	if tab := a.activeTab(); tab == nil || tab.path != want {
		t.Errorf("active tab = %+v, want the daily note %s", tab, want)
	}
}

const calOpenWork = "- [ ] Report ^aaaaaa\n- [ ] Dentist due:2026-11-03T14:00-15:30\n"

var calOpenFiles = map[string]string{
	"Work.md": calOpenWork,
	"Plans.md": "# Plans\n\n- task:[[Work#^aaaaaa]] when:2026-11-03T10:00-11:00\n" +
		"- Lunch when:2026-11-03T12:30-13:30\n",
	"Review.md": "---\npage-type: event\nwhen: 2026-11-03T16:00-17:00\n---\n# Review\n",
}

func TestEnterOpensTheEntry(t *testing.T) {
	for _, c := range []struct {
		name string
		hour int
		path string
		line int
	}{
		{"a due task", 14, "Work.md", -1},
		{"a session", 10, "Work.md", -1},
		{"an inline event", 12, "Plans.md", 3},
		{"an event note", 16, "Review.md", 0},
	} {
		for _, vimOff := range []bool{false, true} {
			a, tv, _ := calCursorApp(t, calOpenFiles, calDay, vimOff)
			if vimOff {
				// Without Vim mode, Tab steps to the entries.
				for i := 0; i < 8 && !(tv.calc().curMin/60 == c.hour); i++ {
					calPress(a, tv, vim.KeyTab)
				}
			} else {
				calDownTo(a, tv, c.hour)
			}
			requireCursor(t, tv, c.hour*60, c.name)
			calPress(a, tv, vim.KeyEnter)
			tab := a.activeTab()
			if tab == nil || tab.path != c.path {
				t.Errorf("%s (vim off %v): active tab = %+v, want %s", c.name, vimOff, tab, c.path)
				continue
			}
			if buf := a.buffers[c.path]; buf == nil || (c.line >= 0 && buf.ed.Cursor().Line != c.line) {
				t.Errorf("%s (vim off %v): cursor is not on line %d of %s", c.name, vimOff, c.line, c.path)
			}
		}
	}
}

var calPickerFiles = map[string]string{
	"Work.md": "- [ ] Report ^aaaaaa\n- [ ] Alpha due:2026-11-03T10:00-11:00\n",
	"Plans.md": "# Plans\n\n- task:[[Work#^aaaaaa]] when:2026-11-03T10:00-11:00\n" +
		"- Sync when:2026-11-03T10:30-11:30\n",
}

func TestSlotPickerListsEntriesWithMarks(t *testing.T) {
	a, tv, _ := calCursorApp(t, calPickerFiles, calDay, false)
	calDownTo(a, tv, 10)
	calPress(a, tv, vim.KeyEnter)
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay after Enter on a slot of three entries = %T, want the picker", a.overlay)
	}
	if len(m.items) != 3 {
		t.Fatalf("picker lists %d entries, want 3", len(m.items))
	}
	for i, want := range []string{"Alpha", "☐", "◆"} {
		if !strings.Contains(m.items[i].label, want) {
			t.Errorf("picker entry %d = %q, want it to hold %q", i, m.items[i].label, want)
		}
	}
	if strings.Contains(m.items[0].label, "◆") || strings.Contains(m.items[0].label, "☐") {
		t.Errorf("a due entry has a mark in the picker: %q", m.items[0].label)
	}

	// Esc closes the picker and changes nothing.
	before := a.activeTab()
	a.overlay.handleKey(a, vim.KeyEsc)
	if a.overlay != nil {
		t.Errorf("overlay after Esc = %T, want none", a.overlay)
	}
	if a.activeTab() != before {
		t.Error("Esc in the picker changed the active tab")
	}
	requireCursor(t, tv, 10*60, "Esc in the picker")
	requireFocus(t, tv, calFocusGrid, "Esc in the picker")

	// Choosing the session opens the line of its task.
	calPress(a, tv, vim.KeyEnter)
	a.overlay.handleKey(a, vim.R('j'))
	a.overlay.handleKey(a, vim.KeyEnter)
	tab := a.activeTab()
	if tab == nil || tab.path != "Work.md" {
		t.Fatalf("active tab after choosing the session = %+v, want Work.md", tab)
	}
	if buf := a.buffers["Work.md"]; buf == nil || buf.ed.Cursor().Line != 0 {
		t.Error("the session did not open the line of its task")
	}
}

func TestTaskKeysActOnSelectedEntry(t *testing.T) {
	const work = "- [ ] T due:2026-11-04T14:00-15:30\n"
	today := calDate(2026, time.November, 2)
	open := func() (*App, *tasksView, string) {
		a, tv, root := calCursorApp(t, map[string]string{"Work.md": work}, calDay, false)
		calFixClock(t, a, today)
		tv.selectDay(a, calDate(2026, time.November, 4))
		calDownTo(a, tv, 14)
		requireSelectedContent(t, tv, "T", "cursor on the 14 row")
		return a, tv, root
	}

	a, tv, root := open()
	calPress(a, tv, vim.R('x'))
	if got := calReadFile(t, root, "Work.md"); !strings.Contains(got, "- [x] T") {
		t.Errorf("x did not toggle the selected task:\n%s", got)
	}

	a, tv, _ = open()
	calPress(a, tv, bindingKeys(a.keymap.Binding("nav.contextMenu"))[0])
	if _, ok := a.overlay.(*menu); !ok {
		t.Errorf("overlay after the context-menu key = %T, want the task menu", a.overlay)
	}

	a, tv, _ = open()
	calPress(a, tv, vim.R('@'))
	if m, ok := a.overlay.(*menu); !ok || !strings.Contains(m.title, "Due") {
		t.Errorf("overlay after @ = %T, want the due-date menu", a.overlay)
	}

	a, tv, root = open()
	calPress(a, tv, vim.R('T'))
	got := calReadFile(t, root, "Work.md")
	if !strings.Contains(got, "due:2026-11-02") || strings.Contains(got, "due:2026-11-04") {
		t.Errorf("T did not move the selected task to today:\n%s", got)
	}
	requireDay(t, tv, "2026-11-04", "T on a task keeps the day")

	// On an empty slot, T selects today.
	a, tv, _ = open()
	// The task covers the 14 and 15 rows.
	calPress(a, tv, vim.R('j'), vim.R('j'))
	requireSelectedContent(t, tv, "", "cursor on an empty slot")
	calPress(a, tv, vim.R('T'))
	requireDay(t, tv, "2026-11-02", "T on an empty slot")
}

func TestTaskKeysDoNothingWithoutASelectedEntry(t *testing.T) {
	a, tv, root := calCursorApp(t, map[string]string{"Work.md": calCursorWork}, calDay, false)
	before := calReadFile(t, root, "Work.md")
	calPress(a, tv, vim.R('x'), vim.R('d'), vim.R('@'))
	if got := calReadFile(t, root, "Work.md"); got != before {
		t.Errorf("a task key on an empty slot changed the note:\n%s", got)
	}
	if a.overlay != nil {
		t.Errorf("a task key on an empty slot opened %T", a.overlay)
	}
}
