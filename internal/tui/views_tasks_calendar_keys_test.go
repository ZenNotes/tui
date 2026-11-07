package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vim"
)

const calWorkNote = "- [ ] alpha job due:2026-11-18\n- [ ] bravo job due:2026-11-18\n- [ ] charlie job due:2026-11-19\n"

const calPlanNote = "---\ntags: [task]\ndue: 2026-11-18\nscheduled: 2026-11-16\n---\n# Plan the move\n"

// calVaultApp opens the calendar on 2026-11-18 over real note files, so key
// presses that edit a task change a file that can be read back.
func calVaultApp(t *testing.T, files map[string]string) (*App, *tasksView, string) {
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
	a.idx.tasks = tm.tasks
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.CalendarShowWeekNumbers = true
	a.prefs.VimMode = true
	a, tv := calOpen(t, a)
	return a, tv, root
}

func calPress(a *App, tv *tasksView, keys ...vim.Key) {
	for _, k := range keys {
		tv.handleKey(a, k)
	}
}

func calRunes(s string) []vim.Key {
	var out []vim.Key
	for _, r := range s {
		out = append(out, vim.R(r))
	}
	return out
}

func calReadFile(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func calFixToday(t *testing.T, day time.Time) {
	t.Helper()
	prev := calendarNow
	calendarNow = func() time.Time { return day }
	t.Cleanup(func() { calendarNow = prev })
}

func calDayISO(tv *tasksView) string {
	return tv.day.Format("2006-01-02")
}

func calDailyPath(a *App, day time.Time) string {
	_, _, _, rel := a.periodicLocation(periodic.Daily, day)
	return rel
}

func requireDay(t *testing.T, tv *tasksView, want string, context string) {
	t.Helper()
	if got := calDayISO(tv); got != want {
		t.Errorf("%s: selected day = %s, want %s", context, got, want)
	}
}

func requireMonth(t *testing.T, tv *tasksView, year int, month time.Month, context string) {
	t.Helper()
	if tv.month.Year() != year || tv.month.Month() != month {
		t.Errorf("%s: month shown = %s, want %d-%02d", context, tv.month.Format("2006-01"), year, int(month))
	}
}

func TestMonthGridHJKLMoveByDayAndWeek(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	for _, c := range []struct {
		key  rune
		want string
	}{{'h', "2026-11-17"}, {'l', "2026-11-19"}, {'j', "2026-11-25"}, {'k', "2026-11-11"}} {
		tv.selectDay(a, calDate(2026, time.November, 18))
		if !tv.handleKey(a, vim.R(c.key)) {
			t.Errorf("%c was not handled on the grid", c.key)
		}
		requireDay(t, tv, c.want, string(c.key))
	}
}

func TestMonthGridArrowsWithoutVimMode(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = false
	for _, c := range []struct {
		key  string
		want string
	}{{"left", "2026-11-17"}, {"right", "2026-11-19"}, {"down", "2026-11-25"}, {"up", "2026-11-11"}} {
		tv.selectDay(a, calDate(2026, time.November, 18))
		if !tv.handleKey(a, vim.Special(c.key)) {
			t.Errorf("%s was not handled on the grid without Vim mode", c.key)
		}
		requireDay(t, tv, c.want, c.key)
	}
	// Printable letters keep their Vim-only gate.
	for _, r := range "hjkl" {
		tv.selectDay(a, calDate(2026, time.November, 18))
		if tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was handled on the grid without Vim mode", r)
		}
		requireDay(t, tv, "2026-11-18", string(r)+" without Vim mode")
	}
}

func TestMonthHalfPageKeysChangeMonth(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = vimOn
		calPress(a, tv, vim.Ctrl('d'))
		requireMonth(t, tv, 2026, time.December, "half-page down")
		if tv.day.Month() != time.December {
			t.Errorf("vim=%v: selected day %s is outside the month shown", vimOn, calDayISO(tv))
		}
		calPress(a, tv, vim.Ctrl('u'), vim.Ctrl('u'))
		requireMonth(t, tv, 2026, time.October, "half-page up twice")
		if tv.day.Month() != time.October {
			t.Errorf("vim=%v: selected day %s is outside the month shown", vimOn, calDayISO(tv))
		}
		if text := calScreenText(calScreen(a, tv, 120, 40)); !strings.Contains(text, "October") {
			t.Errorf("vim=%v: October is not shown after half-page up:\n%s", vimOn, text)
		}
	}
}

func TestMonthHalfPageFollowsRebinding(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.keymap = keymaps.NewResolver(map[string]string{"nav.halfPageDown": "Ctrl+G"})
	calPress(a, tv, vim.Ctrl('d'))
	requireMonth(t, tv, 2026, time.November, "<C-D> after rebinding")
	requireDay(t, tv, "2026-11-18", "<C-D> after rebinding")
	calPress(a, tv, vim.Ctrl('g'))
	requireMonth(t, tv, 2026, time.December, "the rebound key")
}

func TestMonthBracketAndShiftHLChangeMonth(t *testing.T) {
	for _, c := range []struct {
		key   rune
		year  int
		month time.Month
	}{{']', 2026, time.December}, {'L', 2026, time.December}, {'[', 2026, time.October}, {'H', 2026, time.October}} {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = true
		if !tv.handleKey(a, vim.R(c.key)) {
			t.Errorf("%c was not handled on the grid", c.key)
		}
		requireMonth(t, tv, c.year, c.month, string(c.key))
	}
}

func TestMonthEnterFocusesItemPanel(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calPress(a, tv, vim.KeyEnter)
	if tv.calc().focus != calFocusItems {
		t.Fatalf("focus after Enter on a day with entries = %v, want the item panel", tv.calc().focus)
	}
	got := tv.selectedTask()
	if got == nil || got.Content != "alpha job" {
		t.Errorf("selected entry after Enter = %+v, want the first entry (alpha job)", got)
	}
}

func TestMonthEnterOnEmptyDayOpensDailyNote(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	tv.selectDay(a, calDate(2026, time.November, 25))
	calPress(a, tv, vim.KeyEnter)
	if tv.calc().focus != calFocusGrid {
		t.Errorf("focus after Enter on an empty day = %v, want the grid", tv.calc().focus)
	}
	want := calDailyPath(a, calDate(2026, time.November, 25))
	if tab := a.activeTab(); tab == nil || tab.path != want {
		t.Errorf("active tab = %+v, want the daily note %s", tab, want)
	}
}

func TestCalendarTodayKeysJumpToToday(t *testing.T) {
	calFixToday(t, calDate(2026, time.October, 29))
	for _, r := range "tT" {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = true
		if !tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was not handled on the grid", r)
		}
		requireDay(t, tv, "2026-10-29", string(r))
		requireMonth(t, tv, 2026, time.October, string(r))
	}
	// Without Vim mode the printable key keeps its gate.
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = false
	if tv.handleKey(a, vim.R('t')) {
		t.Error("t was handled without Vim mode")
	}
	requireDay(t, tv, "2026-11-18", "t without Vim mode")
}

func TestCalendarOpenKeyOpensDailyNote(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calPress(a, tv, vim.R('o'))
	want := calDailyPath(a, calDate(2026, time.November, 18))
	if tab := a.activeTab(); tab == nil || tab.path != want {
		t.Errorf("active tab = %+v, want the daily note %s", tab, want)
	}
}

func TestItemPanelEditKeysChangeDue(t *testing.T) {
	for _, c := range []struct {
		key  rune
		want string
	}{{'>', "alpha job due:2026-11-19"}, {'<', "alpha job due:2026-11-17"}} {
		a, tv, root := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calPress(a, tv, vim.KeyEnter, vim.R(c.key))
		if got := calReadFile(t, root, "work.md"); !strings.Contains(got, c.want) {
			t.Errorf("%c: note is\n%s\nwant it to contain %q", c.key, got, c.want)
		}
	}

	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calPress(a, tv, vim.KeyEnter, vim.R('d'))
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatalf("overlay after d = %T, want the due-date prompt", a.overlay)
	}
	if !strings.Contains(p.title, "Due") {
		t.Errorf("prompt title = %q, want the due-date prompt", p.title)
	}
}

func TestItemPanelOpenToggleAndMenu(t *testing.T) {
	for _, k := range []vim.Key{vim.KeyEnter, vim.R('o')} {
		a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calPress(a, tv, vim.KeyEnter, k)
		if tab := a.activeTab(); tab == nil || tab.path != "work.md" {
			t.Errorf("%s: active tab = %+v, want work.md", k, tab)
		}
	}

	a, tv, root := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calPress(a, tv, vim.KeyEnter, vim.R('x'))
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "- [x] alpha job") {
		t.Errorf("x did not toggle the selected entry:\n%s", got)
	}

	a, tv, _ = calVaultApp(t, map[string]string{"work.md": calWorkNote})
	keys := bindingKeys(a.keymap.Binding("nav.contextMenu"))
	if len(keys) != 1 {
		t.Fatalf("nav.contextMenu binding = %v, want one key", keys)
	}
	calPress(a, tv, vim.KeyEnter, keys[0])
	if _, ok := a.overlay.(*menu); !ok {
		t.Errorf("overlay after the context-menu key = %T, want the task menu", a.overlay)
	}
}

func TestItemPanelSelectedTaskFeedsActions(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calPress(a, tv, vim.KeyEnter)
	want := []string{"bravo job", "bravo job", "alpha job", "alpha job"}
	moves := []vim.Key{vim.R('j'), vim.R('j'), vim.R('k'), vim.R('k')}
	if got := tv.selectedTask(); got == nil || got.Content != "alpha job" {
		t.Fatalf("selected after Enter = %+v, want alpha job", got)
	}
	// j stops at the last entry of the day; k stops at the first.
	for i, k := range moves {
		calPress(a, tv, k)
		got := tv.selectedTask()
		if got == nil || got.Content != want[i] {
			t.Errorf("after move %d selected = %+v, want %s", i+1, got, want[i])
		}
	}
}

func TestItemPanelLeftAndEscReturnToGrid(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		for _, back := range []vim.Key{vim.KeyEsc, vim.Special("left"), vim.R('h')} {
			a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
			a.prefs.VimMode = vimOn
			calPress(a, tv, vim.KeyEnter)
			if tv.calc().focus != calFocusItems {
				t.Fatalf("vim=%v: Enter did not focus the item panel", vimOn)
			}
			calPress(a, tv, back)
			if !vimOn && back.IsRune('h') {
				// With Vim off h is a letter, and letters do nothing.
				if tv.calc().focus != calFocusItems {
					t.Errorf("h with Vim off moved focus to %v", tv.calc().focus)
				}
				continue
			}
			if tv.calc().focus != calFocusGrid {
				t.Errorf("vim=%v: %s left focus on %v, want the grid", vimOn, back, tv.calc().focus)
			}
			requireDay(t, tv, "2026-11-18", "returning to the grid")
		}
	}
}

func TestItemPanelArrowsWithoutVimMode(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	a.prefs.VimMode = false
	calPress(a, tv, vim.KeyEnter)
	if tv.calc().focus != calFocusItems {
		t.Fatal("Enter did not focus the item panel without Vim mode")
	}
	calPress(a, tv, vim.Special("down"))
	if got := tv.selectedTask(); got == nil || got.Content != "bravo job" {
		t.Errorf("after Down selected = %+v, want bravo job", got)
	}
	calPress(a, tv, vim.Special("up"))
	if got := tv.selectedTask(); got == nil || got.Content != "alpha job" {
		t.Errorf("after Up selected = %+v, want alpha job", got)
	}
	calPress(a, tv, vim.Special("left"))
	if tv.calc().focus != calFocusGrid {
		t.Errorf("Left left focus on %v, want the grid", tv.calc().focus)
	}
}

// The key hints name only the keys that work where the calendar's focus is.
func TestCalendarHintsFollowFocus(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	month := stripAnsi(tv.hint(a))
	for _, absent := range []string{"hour step", "due today", "due date"} {
		if strings.Contains(month, absent) {
			t.Errorf("month grid hint names %q: %s", absent, month)
		}
	}
	calToSub(t, a, tv, calWeek)
	if week := stripAnsi(tv.hint(a)); !strings.Contains(week, "z hour step") || strings.Contains(week, "due today") {
		t.Errorf("week grid hint = %s", week)
	}
	calPress(a, tv, vim.KeyTab)
	requireFocus(t, tv, calFocusItems, "Tab")
	task := stripAnsi(tv.hint(a))
	for _, want := range []string{"T due today", "@ due date", "Esc back"} {
		if !strings.Contains(task, want) {
			t.Errorf("focused task hint lacks %q: %s", want, task)
		}
	}
	if strings.Contains(task, "hour step") || strings.Contains(task, "month/week/day") {
		t.Errorf("focused task hint names grid keys: %s", task)
	}
}
