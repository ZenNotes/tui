package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

var (
	calLabel24 = regexp.MustCompile(`^\s*(\d{2}(?::30)?)(?:\s|\x{2502}|\|$|$)`)
	calLabel12 = regexp.MustCompile(`^\s*(\d{1,2}(?::30)? [AP]M)`)
	calMoreRe  = regexp.MustCompile(`\+(\d+) more`)
	calZqRe    = regexp.MustCompile(`zq\d\d`)
)

func calEsc(a *App, tv *tasksView) { calPress(a, tv, vim.KeyEsc) }

// calToSub presses Esc until the calendar shows the given layout.
func calToSub(t *testing.T, a *App, tv *tasksView, want calSub) {
	t.Helper()
	for i := 0; i < 3 && tv.calc().sub != want; i++ {
		calEsc(a, tv)
	}
	if tv.calc().sub != want {
		t.Fatalf("Esc did not reach layout %v (stuck on %v)", want, tv.calc().sub)
	}
}

func calHourLabels(lines []string, re *regexp.Regexp) []string {
	var out []string
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// calColumnOf is the cell column where text starts, or -1.
func calColumnOf(lines []string, text string) int {
	for _, l := range lines {
		if i := strings.Index(l, text); i >= 0 {
			return len([]rune(l[:i]))
		}
	}
	return -1
}

func calRowOf(lines []string, text string) int {
	for i, l := range lines {
		if strings.Contains(l, text) {
			return i
		}
	}
	return -1
}

func requireWeekHeader(t *testing.T, lines []string, order []string, context string) []int {
	t.Helper()
	cols := calWeekdayHeader(lines, order)
	if cols == nil {
		t.Fatalf("%s: no weekday header in %v order:\n%s", context, order, calScreenText(lines))
	}
	return cols
}

func requireTooSmall(t *testing.T, lines []string, want bool, context string) {
	t.Helper()
	got := strings.Contains(calScreenText(lines), "Terminal too small")
	if got != want {
		t.Errorf("%s: `Terminal too small` shown = %v, want %v:\n%s", context, got, want, calScreenText(lines))
	}
}

func calManyTasks(day string, n int, prefix string) []vault.Task {
	var tasks []vault.Task
	for i := 0; i < n; i++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", i), SourcePath: "n.md", Content: fmt.Sprintf("%s%02d", prefix, i), Due: day})
	}
	return tasks
}

func TestEscCyclesSubModes(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		a, tv := calTestApp(t, nil)
		a.prefs.VimMode = vimOn
		if tv.calc().sub != calMonth {
			t.Fatalf("vim=%v: calendar did not open on the month layout", vimOn)
		}
		for i, want := range []calSub{calWeek, calDay, calMonth} {
			if !tv.handleKey(a, vim.KeyEsc) {
				t.Errorf("vim=%v: Esc %d was not handled", vimOn, i+1)
			}
			if got := tv.calc().sub; got != want {
				t.Errorf("vim=%v: after Esc %d layout = %v, want %v", vimOn, i+1, got, want)
			}
			if tv.mode != "calendar" {
				t.Errorf("vim=%v: Esc %d left calendar mode: %q", vimOn, i+1, tv.mode)
			}
		}
	}
}

func TestEscToWeekKeepsSelectedDay(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "edge16 task", Due: "2026-11-16"},
		{ID: "n#1", SourcePath: "n.md", Content: "edge22 task", Due: "2026-11-22"},
		{ID: "n#2", SourcePath: "n.md", Content: "before15 task", Due: "2026-11-15"},
		{ID: "n#3", SourcePath: "n.md", Content: "after23 task", Due: "2026-11-23"},
	})
	calEsc(a, tv)
	requireDay(t, tv, "2026-11-18", "week view")
	if tv.calc().sub != calWeek {
		t.Fatalf("Esc did not open week view")
	}
	lines := calScreen(a, tv, 120, 60)
	requireWeekHeader(t, lines, calWeekdayNames, "week of 2026-11-18")
	text := calScreenText(lines)
	for _, want := range []string{"edge16 task", "edge22 task"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from the week containing 2026-11-18:\n%s", want, text)
		}
	}
	for _, bad := range []string{"before15", "after23"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q shown, but it is outside the week:\n%s", bad, text)
		}
	}
	calEsc(a, tv)
	requireDay(t, tv, "2026-11-18", "day view")
}

func TestSubModeNeverWritesPreferences(t *testing.T) {
	a, tv := calTestApp(t, nil)
	dir := os.Getenv("ZENNOTES_CONFIG_DIR")
	if dir == "" {
		t.Fatal("test config dir is not set")
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(p)
			out[p] = string(b)
			return nil
		})
		return out
	}
	// The user's saved view mode; opening the tab through a command does not set it.
	a.prefs.TasksViewMode = "calendar"
	before := snapshot()
	for _, want := range []calSub{calWeek, calDay, calMonth, calWeek} {
		calEsc(a, tv)
		if tv.calc().sub != want {
			t.Fatalf("layout = %v, want %v", tv.calc().sub, want)
		}
	}
	after := snapshot()
	if len(after) != len(before) {
		t.Errorf("config dir has %d files after cycling, had %d", len(after), len(before))
	}
	for p, body := range after {
		if old, ok := before[p]; !ok || old != body {
			t.Errorf("config file %s was written while cycling", p)
		}
	}
	if a.prefs.TasksViewMode != "calendar" || tv.mode != "calendar" {
		t.Errorf("tasks_view_mode = %q, view mode = %q, want calendar for both", a.prefs.TasksViewMode, tv.mode)
	}
}

func TestNewSessionOpensMonth(t *testing.T) {
	a, tv := calTestApp(t, nil)
	calEsc(a, tv)
	if tv.calc().sub != calWeek {
		t.Fatal("Esc did not leave the month layout in the first session")
	}
	a2, tv2 := calTestApp(t, nil)
	if tv2.calc().sub != calMonth {
		t.Errorf("new session opened on layout %v, want month", tv2.calc().sub)
	}
	requireFullMonthGrid(t, calScreen(a2, tv2, 120, 40))
}

func TestSubModeSurvivesViewCycle(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.VimMode = true
	calEsc(a, tv)
	if tv.calc().sub != calWeek {
		t.Fatal("Esc did not open week view")
	}
	for i, want := range []string{"list", "kanban", "calendar"} {
		if !tv.handleKey(a, vim.R('v')) {
			t.Fatalf("v %d was not handled", i+1)
		}
		if tv.mode != want {
			t.Fatalf("v %d gave mode %q, want %q", i+1, tv.mode, want)
		}
	}
	if tv.calc().sub != calWeek {
		t.Errorf("layout after the view cycle = %v, want week", tv.calc().sub)
	}
	requireWeekHeader(t, calScreen(a, tv, 120, 60), calWeekdayNames, "week after v cycle")
}

func TestSubModeSurvivesTasksCalendarCommand(t *testing.T) {
	a, tv := calTestApp(t, nil)
	calToSub(t, a, tv, calDay)
	tv.setMode("list")
	if err := a.openTasksMode("calendar"); err != nil {
		t.Fatal(err)
	}
	tab := a.activeTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	tv2, ok := tab.view.(*tasksView)
	if !ok {
		t.Fatalf("active tab is %T", tab.view)
	}
	if tv2.mode != "calendar" {
		t.Fatalf("mode = %q, want calendar", tv2.mode)
	}
	if tv2.calc().sub != calDay {
		t.Errorf("layout after :tasks calendar = %v, want day", tv2.calc().sub)
	}
	if cols := calWeekdayHeader(calScreen(a, tv2, 120, 60), calWeekdayNames); cols != nil {
		t.Errorf("day view shows a seven-day header")
	}
}

func TestWeekDayViewsHideWeekNumbers(t *testing.T) {
	a, tv := calTestApp(t, nil)
	a.prefs.CalendarShowWeekNumbers = true
	if !weekNumberShown(calScreen(a, tv, 120, 60), 47) {
		t.Fatal("month view should show ISO week 47 with week numbers on")
	}
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		lines := calScreen(a, tv, 120, 60)
		requireTooSmall(t, lines, false, fmt.Sprint(sub))
		if weekNumberShown(lines, 47) {
			t.Errorf("layout %v shows a week number:\n%s", sub, calScreenText(lines))
		}
		if sub == calWeek {
			requireWeekHeader(t, lines, calWeekdayNames, "week view")
		}
	}
}

// Tasks carry a date but no time of day, so the week and day columns list
// them with no hour axis beside them, in either time format.
func TestColumnsShowNoHourAxis(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "call at 15:00 sharp", Due: "2026-11-18"},
	})
	for _, format := range []string{"24h", "12h"} {
		a.prefs.TimeFormat = format
		for _, sub := range []calSub{calWeek, calDay} {
			calToSub(t, a, tv, sub)
			lines := calScreen(a, tv, 120, 70)
			text := calScreenText(lines)
			if got := strings.Count(text, "call at 15:00"); got != 1 {
				t.Fatalf("%s layout %v: entry shown %d times, want once in its column:\n%s", format, sub, got, text)
			}
			if got := calHourLabels(lines, calLabel24); len(got) != 0 {
				t.Errorf("%s layout %v: hour labels %v shown:\n%s", format, sub, got, text)
			}
			if got := calHourLabels(lines, calLabel12); len(got) != 0 {
				t.Errorf("%s layout %v: hour labels %v shown:\n%s", format, sub, got, text)
			}
		}
	}
}

func TestWeekShowsSevenColumnsInWeekStartOrder(t *testing.T) {
	var tasks []vault.Task
	for d := 14; d <= 24; d++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", d), SourcePath: "n.md", Content: fmt.Sprintf("day%d", d), Due: fmt.Sprintf("2026-11-%02d", d)})
	}
	for _, c := range []struct {
		start   string
		order   []string
		in      []int
		outside []int
	}{
		{"monday", calWeekdayNames, []int{16, 17, 18, 19, 20, 21, 22}, []int{14, 15, 23, 24}},
		{"sunday", calSundayFirst, []int{15, 16, 17, 18, 19, 20, 21}, []int{14, 22, 23, 24}},
	} {
		a, tv := calTestApp(t, tasks)
		a.prefs.CalendarWeekStart = c.start
		calToSub(t, a, tv, calWeek)
		lines := calScreen(a, tv, 120, 70)
		requireWeekHeader(t, lines, c.order, c.start)
		prev := -1
		for _, d := range c.in {
			x := calColumnOf(lines, fmt.Sprintf("day%d", d))
			if x < 0 {
				t.Errorf("%s: day%d missing from the week:\n%s", c.start, d, calScreenText(lines))
				continue
			}
			if x <= prev {
				t.Errorf("%s: day%d at column %d is not right of the previous day (%d)", c.start, d, x, prev)
			}
			prev = x
		}
		for _, d := range c.outside {
			if strings.Contains(calScreenText(lines), fmt.Sprintf("day%d", d)) {
				t.Errorf("%s: day%d shown outside the week", c.start, d)
			}
		}
	}
}

func TestWeekColumnOverflowShowsMoreCount(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 50, "zq"))
	calToSub(t, a, tv, calWeek)
	lines := calScreen(a, tv, 120, 30)
	requireWeekHeader(t, lines, calWeekdayNames, "week view")
	requireOverflowCount(t, lines, 50)
}

func TestDayColumnOverflowShowsMoreCount(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 50, "zq"))
	calToSub(t, a, tv, calDay)
	requireOverflowCount(t, calScreen(a, tv, 120, 30), 50)
}

func requireOverflowCount(t *testing.T, lines []string, total int) {
	t.Helper()
	text := calScreenText(lines)
	listed := len(calZqRe.FindAllString(text, -1))
	if listed == 0 || listed >= total {
		t.Fatalf("listed %d of %d entries, want some but not all:\n%s", listed, total, text)
	}
	more := calMoreRe.FindAllStringSubmatch(text, -1)
	if len(more) != 1 {
		t.Fatalf("found %d `+N more` lines, want 1:\n%s", len(more), text)
	}
	if want := fmt.Sprint(total - listed); more[0][1] != want {
		t.Errorf("+%s more, want +%s more for %d listed", more[0][1], want, listed)
	}
	lastEntry := -1
	for i, l := range lines {
		if calZqRe.MatchString(l) {
			lastEntry = i
		}
	}
	if row := calRowOf(lines, "more"); row <= lastEntry {
		t.Errorf("`+N more` on row %d is not after the last listed entry (row %d)", row, lastEntry)
	}
}

func TestDayShowsOneFullWidthColumn(t *testing.T) {
	long := "fullwidth " + strings.Repeat("abcdefghi ", 6) + "end"
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: long, Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "nextday task", Due: "2026-11-19"},
	})
	a.prefs.TimeFormat = "24h"
	calToSub(t, a, tv, calDay)
	lines := calScreen(a, tv, 120, 60)
	requireTooSmall(t, lines, false, "day view")
	text := calScreenText(lines)
	if !strings.Contains(text, long) {
		t.Errorf("a %d-cell entry is cut in a 120-wide day column:\n%s", len(long), text)
	}
	if strings.Contains(text, "nextday task") {
		t.Errorf("day view shows another day's entry:\n%s", text)
	}
	if cols := calWeekdayHeader(lines, calWeekdayNames); cols != nil {
		t.Errorf("day view shows seven day columns:\n%s", text)
	}
}

func TestWeekAndDayStripBelowGrid(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "column entry", Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "loose strip task"},
	})
	a.prefs.TimeFormat = "24h"
	for _, sub := range []calSub{calWeek, calDay} {
		calToSub(t, a, tv, sub)
		lines := calScreen(a, tv, 120, 70)
		text := calScreenText(lines)
		if got := strings.Count(text, "loose strip task"); got != 1 {
			t.Fatalf("layout %v: undated task shown %d times, want once:\n%s", sub, got, text)
		}
		entry := calRowOf(lines, "column entry")
		if entry < 0 {
			t.Fatalf("layout %v: dated entry missing:\n%s", sub, text)
		}
		strip := calRowOf(lines, "No date")
		if strip <= entry || calRowOf(lines, "loose strip task") != strip+1 {
			t.Errorf("layout %v: strip at row %d is not under the columns (entry row %d):\n%s", sub, strip, entry, text)
		}
		if strip != len(lines)-1-tv.calc().layout.stripRows {
			t.Errorf("layout %v: strip header on row %d is not pinned to the bottom:\n%s", sub, strip, text)
		}
	}
}

func TestWeekTooSmallBelowMinimum(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{{ID: "n#0", SourcePath: "n.md", Content: "something", Due: "2026-11-18"}})
	calToSub(t, a, tv, calWeek)
	m := calMin[calWeek]
	for _, size := range [][2]int{{m[0] - 1, m[1]}, {m[0], m[1] - 1}} {
		lines := calScreen(a, tv, size[0], size[1])
		requireTooSmall(t, lines, true, fmt.Sprintf("%dx%d", size[0], size[1]))
		if strings.Contains(calScreenText(lines), "something") {
			t.Errorf("%dx%d: entry shown while too small", size[0], size[1])
		}
	}
	lines := calScreen(a, tv, m[0], m[1])
	requireTooSmall(t, lines, false, "at the minimum")
	requireWeekHeader(t, lines, calWeekdayNames, "at the minimum")
	if !strings.Contains(calScreenText(lines), "some") {
		t.Errorf("at the minimum the week does not show the entry:\n%s", calScreenText(lines))
	}
	if len(lines) != m[1]+1 {
		t.Errorf("render is %d lines, want toolbar plus %d", len(lines), m[1])
	}
	for i, l := range lines {
		if lipgloss.Width(l) > m[0] {
			t.Errorf("line %d is %d cells wide", i, lipgloss.Width(l))
		}
	}
}

func TestDayTooSmallBelowMinimum(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{{ID: "n#0", SourcePath: "n.md", Content: "dt1", Due: "2026-11-18"}})
	calToSub(t, a, tv, calDay)
	m := calMin[calDay]
	for _, size := range [][2]int{{m[0] - 1, m[1]}, {m[0], m[1] - 1}} {
		lines := calScreen(a, tv, size[0], size[1])
		requireTooSmall(t, lines, true, fmt.Sprintf("%dx%d", size[0], size[1]))
		if strings.Contains(calScreenText(lines), "dt1") {
			t.Errorf("%dx%d: entry shown while too small", size[0], size[1])
		}
	}
	lines := calScreen(a, tv, m[0], m[1])
	requireTooSmall(t, lines, false, "at the minimum")
	if !strings.Contains(calScreenText(lines), "dt1") {
		t.Errorf("at the minimum the day column does not show the entry:\n%s", calScreenText(lines))
	}
	if len(lines) != m[1]+1 {
		t.Errorf("render is %d lines, want toolbar plus %d", len(lines), m[1])
	}
	for i, l := range lines {
		if lipgloss.Width(l) > m[0] {
			t.Errorf("line %d is %d cells wide", i, lipgloss.Width(l))
		}
	}
}

func TestTooSmallMonthDoesNotSwitchModeButEscDoes(t *testing.T) {
	a, tv := calTestApp(t, nil)
	// A body too small for even the compact month.
	for i := 0; i < 2; i++ {
		requireTooSmall(t, calScreen(a, tv, 30, 12), true, "month at 30x12")
		if tv.calc().sub != calMonth {
			t.Fatalf("render switched the layout to %v by itself", tv.calc().sub)
		}
	}
	calEsc(a, tv)
	if tv.calc().sub != calWeek {
		t.Fatalf("Esc while too small left layout %v, want week", tv.calc().sub)
	}
	lines := calScreen(a, tv, 64, 12)
	requireTooSmall(t, lines, false, "week at 64x12")
	requireWeekHeader(t, lines, calWeekdayNames, "week at 64x12")
}

// z is not a calendar key in any view; the editor keeps its folds.
func TestCalendarIgnoresZ(t *testing.T) {
	a, tv := calTestApp(t, calManyTasks("2026-11-18", 3, "zt"))
	a.prefs.VimMode = true
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		calToSub(t, a, tv, sub)
		before := calScreenText(calScreen(a, tv, 120, 40))
		calPress(a, tv, vim.R('z'))
		if after := calScreenText(calScreen(a, tv, 120, 40)); after != before {
			t.Errorf("z changed layout %v:\nbefore:\n%s\nafter:\n%s", sub, before, after)
		}
		if tv.calc().sub != sub {
			t.Errorf("layout after z = %v, want %v", tv.calc().sub, sub)
		}
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
