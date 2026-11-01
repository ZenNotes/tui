package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// calNavTasks puts one task in each of several weeks so a test can tell
// which week or day a render shows by the task texts on screen.
func calNavTasks() []vault.Task {
	return []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "wk09 task", Due: "2026-11-09"},
		{ID: "n#1", SourcePath: "n.md", Content: "wk15 task", Due: "2026-11-15"},
		{ID: "n#2", SourcePath: "n.md", Content: "wk16 task", Due: "2026-11-16"},
		{ID: "n#3", SourcePath: "n.md", Content: "wk22 task", Due: "2026-11-22"},
		{ID: "n#4", SourcePath: "n.md", Content: "wk23 task", Due: "2026-11-23"},
		{ID: "n#5", SourcePath: "n.md", Content: "wk25 task", Due: "2026-11-25"},
		{ID: "n#6", SourcePath: "n.md", Content: "wk11 task", Due: "2026-11-11"},
		{ID: "n#7", SourcePath: "n.md", Content: "today27 task", Due: "2026-10-27"},
		{ID: "n#8", SourcePath: "n.md", Content: "today29 task", Due: "2026-10-29"},
		{ID: "n#9", SourcePath: "n.md", Content: "wk19 task", Due: "2026-11-19"},
		{ID: "n#10", SourcePath: "n.md", Content: "wk17 task", Due: "2026-11-17"},
	}
}

func calNavApp(t *testing.T, sub calSub, vimOn bool) (*App, *tasksView) {
	t.Helper()
	a, tv := calTestApp(t, calNavTasks())
	a.prefs.VimMode = vimOn
	calToSub(t, a, tv, sub)
	return a, tv
}

func requireShown(t *testing.T, a *App, tv *tasksView, shown, hidden []string, context string) {
	t.Helper()
	text := calScreenText(calScreen(a, tv, 140, 60))
	for _, s := range shown {
		if !strings.Contains(text, s) {
			t.Errorf("%s: %q is not shown:\n%s", context, s, text)
		}
	}
	for _, s := range hidden {
		if strings.Contains(text, s) {
			t.Errorf("%s: %q is shown, but it is outside the view:\n%s", context, s, text)
		}
	}
}

func TestWeekHLMoveWithinWeek(t *testing.T) {
	a, tv := calNavApp(t, calWeek, true)
	if !tv.handleKey(a, vim.R('l')) {
		t.Error("l was not handled in week view")
	}
	requireDay(t, tv, "2026-11-19", "l")
	if !tv.handleKey(a, vim.R('h')) || !tv.handleKey(a, vim.R('h')) {
		t.Error("h was not handled in week view")
	}
	requireDay(t, tv, "2026-11-17", "h twice")
	if tv.calc().sub != calWeek {
		t.Errorf("layout after h/l = %v, want week", tv.calc().sub)
	}
}

func TestWeekLOnLastDayMovesToNextWeek(t *testing.T) {
	a, tv := calNavApp(t, calWeek, true)
	tv.selectDay(a, calDate(2026, time.November, 22))
	calPress(a, tv, vim.R('l'))
	requireDay(t, tv, "2026-11-23", "l on the last day of the week")
	requireShown(t, a, tv, []string{"wk23 task", "wk25 task"}, []string{"wk22 task", "wk16 task"}, "next week")
}

func TestWeekHOnFirstDayMovesToPreviousWeek(t *testing.T) {
	a, tv := calNavApp(t, calWeek, true)
	tv.selectDay(a, calDate(2026, time.November, 16))
	calPress(a, tv, vim.R('h'))
	requireDay(t, tv, "2026-11-15", "h on the first day of the week")
	requireShown(t, a, tv, []string{"wk09 task", "wk15 task"}, []string{"wk16 task", "wk22 task"}, "previous week")
}

func TestWeekHalfPageMovesByWeek(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		a, tv := calNavApp(t, calWeek, vimOn)
		if !tv.handleKey(a, vim.Ctrl('d')) {
			t.Errorf("vim=%v: half-page down was not handled in week view", vimOn)
		}
		requireDay(t, tv, "2026-11-25", "half-page down")
		requireShown(t, a, tv, []string{"wk23 task", "wk25 task"}, []string{"wk16 task"}, "week after half-page down")
		calPress(a, tv, vim.Ctrl('u'), vim.Ctrl('u'))
		requireDay(t, tv, "2026-11-11", "half-page up twice")
		requireShown(t, a, tv, []string{"wk09 task", "wk11 task"}, []string{"wk16 task", "wk25 task"}, "week after half-page up")
	}
}

func TestWeekTabFocusesColumnAndJKMove(t *testing.T) {
	for _, k := range []vim.Key{vim.KeyTab, vim.KeyEnter} {
		a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, calWeek)
		if !tv.handleKey(a, k) {
			t.Errorf("%s was not handled on the week grid", k)
		}
		if tv.calc().focus != calFocusItems {
			t.Fatalf("%s: focus = %v, want the day's column", k, tv.calc().focus)
		}
		if got := tv.selectedTask(); got == nil || got.Content != "alpha job" {
			t.Errorf("%s: selected = %+v, want alpha job", k, got)
		}
		calPress(a, tv, vim.R('j'))
		if got := tv.selectedTask(); got == nil || got.Content != "bravo job" {
			t.Errorf("%s: after j selected = %+v, want bravo job", k, got)
		}
		calPress(a, tv, vim.R('k'))
		if got := tv.selectedTask(); got == nil || got.Content != "alpha job" {
			t.Errorf("%s: after k selected = %+v, want alpha job", k, got)
		}
		requireDay(t, tv, "2026-11-18", "moving through the column")
	}
}

func TestDayHalfPageMovesByDay(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		a, tv := calNavApp(t, calDay, vimOn)
		if !tv.handleKey(a, vim.Ctrl('d')) {
			t.Errorf("vim=%v: half-page down was not handled in day view", vimOn)
		}
		requireDay(t, tv, "2026-11-19", "half-page down")
		requireShown(t, a, tv, []string{"wk19 task"}, []string{"wk17 task"}, "day after half-page down")
		calPress(a, tv, vim.Ctrl('u'), vim.Ctrl('u'))
		requireDay(t, tv, "2026-11-17", "half-page up twice")
		requireShown(t, a, tv, []string{"wk17 task"}, []string{"wk19 task"}, "day after half-page up")
		if tv.calc().sub != calDay {
			t.Errorf("vim=%v: layout = %v, want day", vimOn, tv.calc().sub)
		}
	}
}

func TestWeekArrowsWithoutVimMode(t *testing.T) {
	a, tv := calNavApp(t, calWeek, false)
	for _, c := range []struct{ key, want string }{{"right", "2026-11-19"}, {"left", "2026-11-17"}} {
		tv.selectDay(a, calDate(2026, time.November, 18))
		if !tv.handleKey(a, vim.Special(c.key)) {
			t.Errorf("%s was not handled in week view without Vim mode", c.key)
		}
		requireDay(t, tv, c.want, c.key)
	}
	tv.selectDay(a, calDate(2026, time.November, 22))
	calPress(a, tv, vim.Special("right"))
	requireDay(t, tv, "2026-11-23", "Right on the last day of the week")
	requireShown(t, a, tv, []string{"wk23 task"}, []string{"wk22 task"}, "next week by Right")
	tv.selectDay(a, calDate(2026, time.November, 16))
	calPress(a, tv, vim.Special("left"))
	requireDay(t, tv, "2026-11-15", "Left on the first day of the week")
	requireShown(t, a, tv, []string{"wk15 task"}, []string{"wk16 task"}, "previous week by Left")
}

func TestDayArrowsWithoutVimMode(t *testing.T) {
	a, tv := calNavApp(t, calDay, false)
	for _, c := range []struct{ key, want string }{{"right", "2026-11-19"}, {"left", "2026-11-17"}} {
		tv.selectDay(a, calDate(2026, time.November, 18))
		if !tv.handleKey(a, vim.Special(c.key)) {
			t.Errorf("%s was not handled in day view without Vim mode", c.key)
		}
		requireDay(t, tv, c.want, c.key)
	}
	requireShown(t, a, tv, []string{"wk17 task"}, []string{"wk19 task"}, "day by Left")
}

func TestWeekPrintableGridKeysNeedVimMode(t *testing.T) {
	a, tv := calNavApp(t, calWeek, false)
	tabBefore := a.activeTab()
	for _, r := range "z[]to" {
		if tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was handled in week view without Vim mode", r)
		}
		requireDay(t, tv, "2026-11-18", string(r)+" without Vim mode")
		if tv.calc().sub != calWeek || tv.calc().focus != calFocusGrid {
			t.Errorf("%c without Vim mode changed layout/focus to %v/%v", r, tv.calc().sub, tv.calc().focus)
		}
		if a.activeTab() != tabBefore {
			t.Errorf("%c without Vim mode changed the active tab", r)
		}
		if a.overlay != nil {
			t.Errorf("%c without Vim mode opened an overlay", r)
		}
	}
}

func TestNewTaskGoesToSelectedDayInEverySubMode(t *testing.T) {
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		a, tv, root := calVaultApp(t, nil)
		a.idx.settings.DailyNotes.Enabled = true
		calToSub(t, a, tv, sub)
		calPress(a, tv, vim.R('n'))
		p, ok := a.overlay.(*prompt)
		if !ok {
			t.Fatalf("layout %v: overlay after n = %T, want the new-task prompt", sub, a.overlay)
		}
		if !strings.Contains(p.title, "2026-11-18") {
			t.Errorf("layout %v: prompt title = %q, want it to name 2026-11-18", sub, p.title)
		}
		p.onSubmit(a, "navtask text")
		rel := calDailyPath(a, calDate(2026, time.November, 18))
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("layout %v: daily note %s not written: %v", sub, rel, err)
		}
		if !strings.Contains(string(b), "navtask text") {
			t.Errorf("layout %v: daily note is\n%s\nwant it to hold the new task", sub, b)
		}
	}
}

func TestWeekColumnFocusEditAndOpenKeys(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		for _, k := range []vim.Key{vim.KeyEnter, vim.R('o')} {
			a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
			calToSub(t, a, tv, sub)
			calPress(a, tv, vim.KeyTab, k)
			if tab := a.activeTab(); tab == nil || tab.path != "work.md" {
				t.Errorf("layout %v: %s opened %+v, want work.md", sub, k, tab)
			}
		}

		a, tv, root := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, sub)
		calPress(a, tv, vim.KeyTab, vim.R('x'))
		if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "- [x] alpha job") {
			t.Errorf("layout %v: x did not toggle the selected entry:\n%s", sub, got)
		}

		a, tv, root = calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, sub)
		calPress(a, tv, vim.KeyTab, vim.R('>'))
		if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "alpha job due:2026-11-19") {
			t.Errorf("layout %v: > did not move due:\n%s", sub, got)
		}

		a, tv, _ = calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, sub)
		keys := bindingKeys(a.keymap.Binding("nav.contextMenu"))
		if len(keys) != 1 {
			t.Fatalf("nav.contextMenu binding = %v, want one key", keys)
		}
		calPress(a, tv, vim.KeyTab, keys[0])
		if _, ok := a.overlay.(*menu); !ok {
			t.Errorf("layout %v: overlay after the context-menu key = %T, want the task menu", sub, a.overlay)
		}
	}
}

func TestWeekDayEscAndLeftReturnToGrid(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		for _, vimOn := range []bool{true, false} {
			for _, back := range []vim.Key{vim.KeyEsc, vim.Special("left"), vim.R('h')} {
				a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
				a.prefs.VimMode = vimOn
				calToSub(t, a, tv, sub)
				calPress(a, tv, vim.KeyTab)
				if tv.calc().focus != calFocusItems {
					t.Fatalf("layout %v vim=%v: Tab did not focus the column", sub, vimOn)
				}
				calPress(a, tv, back)
				if !vimOn && back.IsRune('h') {
					// With Vim off h is a letter, and letters do nothing.
					if tv.calc().focus != calFocusItems {
						t.Errorf("layout %v: h with Vim off moved focus to %v", sub, tv.calc().focus)
					}
					continue
				}
				if tv.calc().focus != calFocusGrid {
					t.Errorf("layout %v vim=%v: %s left focus on %v, want the grid", sub, vimOn, back, tv.calc().focus)
				}
				if tv.calc().sub != sub {
					t.Errorf("layout %v vim=%v: %s changed the layout to %v", sub, vimOn, back, tv.calc().sub)
				}
				requireDay(t, tv, "2026-11-18", "returning to the grid")
			}
		}
	}
}

func TestTodayKeysInWeekAndDay(t *testing.T) {
	calFixToday(t, calDate(2026, time.October, 29))
	for _, r := range "tT" {
		a, tv := calNavApp(t, calWeek, true)
		if !tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was not handled in week view", r)
		}
		requireDay(t, tv, "2026-10-29", "week "+string(r))
		requireShown(t, a, tv, []string{"today27 task", "today29 task"}, []string{"wk16 task", "wk22 task"}, "week of today")
		if tv.calc().sub != calWeek {
			t.Errorf("%c changed the layout to %v", r, tv.calc().sub)
		}

		a, tv = calNavApp(t, calDay, true)
		if !tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was not handled in day view", r)
		}
		requireDay(t, tv, "2026-10-29", "day "+string(r))
		requireShown(t, a, tv, []string{"today29 task"}, []string{"today27 task"}, "day of today")
	}
}

func TestWeekDayOpenKeyOpensDailyNote(t *testing.T) {
	for _, sub := range []calSub{calWeek, calDay} {
		a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
		calToSub(t, a, tv, sub)
		calPress(a, tv, vim.R('o'))
		want := calDailyPath(a, calDate(2026, time.November, 18))
		if tab := a.activeTab(); tab == nil || tab.path != want {
			t.Errorf("layout %v: active tab = %+v, want the daily note %s", sub, tab, want)
		}
	}
}
