package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func calDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// calTestApp opens the Tasks tab in calendar mode on 2026-11-18 with the given
// tasks as the vault's scan result.
func calTestApp(t *testing.T, tasks []vault.Task) (*App, *tasksView) {
	t.Helper()
	a, _ := newFeatureTestApp(t)
	a.idx.tasks = tasks
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.CalendarShowWeekNumbers = true
	return calOpen(t, a)
}

func calOpen(t *testing.T, a *App) (*App, *tasksView) {
	t.Helper()
	if err := a.openTasksMode("calendar"); err != nil {
		t.Fatal(err)
	}
	tab := a.activeTab()
	if tab == nil {
		t.Fatal("no active tab after opening the Tasks tab")
	}
	tv, ok := tab.view.(*tasksView)
	if !ok {
		t.Fatalf("active tab is %T, want *tasksView", tab.view)
	}
	tv.refresh(a)
	tv.selectDay(a, calDate(2026, time.November, 18))
	return a, tv
}

// calScreen renders the calendar into a body of w x bodyH cells (the Tasks
// view adds its one-line toolbar on top) and returns the lines without styling.
func calScreen(a *App, tv *tasksView, w, bodyH int) []string {
	return strings.Split(stripAnsi(tv.render(a, w, bodyH+1, true)), "\n")
}

var calWeekdayNames = []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
var calSundayFirst = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}

// calWeekdayHeader finds the line carrying all seven weekday names in the
// given order and returns the column each name starts at.
func calWeekdayHeader(lines []string, order []string) []int {
	for _, l := range lines {
		cols := make([]int, len(order))
		ok := true
		prev := -1
		for i, n := range order {
			c := strings.Index(l, n)
			if c <= prev {
				ok = false
				break
			}
			cols[i], prev = c, c
		}
		if ok {
			return cols
		}
	}
	return nil
}

// requireFullMonthGrid fails unless the screen shows a month grid spread over
// a 120-wide page (day boxes), not the compact mini-grid with a day list.
func requireFullMonthGrid(t *testing.T, lines []string) {
	t.Helper()
	cols := calWeekdayHeader(lines, calWeekdayNames)
	if cols == nil {
		cols = calWeekdayHeader(lines, calSundayFirst)
	}
	if cols == nil {
		t.Fatalf("no weekday header found:\n%s", calScreenText(lines))
	}
	if last := cols[len(cols)-1]; last < 45 {
		t.Fatalf("weekday header ends at column %d: the grid is not full page:\n%s", last, calScreenText(lines))
	}
	joined := calScreenText(lines)
	if strings.Contains(joined, "No tasks due this day") || strings.Contains(joined, "tasks)") {
		t.Fatalf("the old mini-grid day list is still shown:\n%s", joined)
	}
}

// requireFullMonthGridAt is requireFullMonthGrid for other body widths.
func requireFullMonthGridAt(t *testing.T, lines []string, w int) {
	t.Helper()
	cols := calWeekdayHeader(lines, calWeekdayNames)
	if cols == nil {
		t.Fatalf("w=%d: no weekday header found:\n%s", w, calScreenText(lines))
	}
	if cols[6] < w/3 {
		t.Fatalf("w=%d: header ends at column %d: the grid is not full page:\n%s", w, cols[6], calScreenText(lines))
	}
	joined := calScreenText(lines)
	if strings.Contains(joined, "No tasks due this day") || strings.Contains(joined, "tasks)") {
		t.Fatalf("w=%d: the old mini-grid day list is still shown:\n%s", w, joined)
	}
}

func calScreenText(lines []string) string { return strings.Join(lines, "\n") }

func TestCalendarOpensOnMonthGrid(t *testing.T) {
	a, tv := calTestApp(t, nil)
	requireFullMonthGrid(t, calScreen(a, tv, 120, 40))

	// Coming from the Tasks tab's kanban mode with v.
	tv.setMode("kanban")
	if !tv.handleKey(a, vim.Key{Rune: 'v'}) {
		t.Fatal("v was not handled in kanban mode")
	}
	if tv.mode != "calendar" {
		t.Fatalf("mode after v = %q, want calendar", tv.mode)
	}
	requireFullMonthGrid(t, calScreen(a, tv, 120, 40))
}

func TestMonthWeekdayHeaderFollowsWeekStart(t *testing.T) {
	a, tv := calTestApp(t, nil)
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	if calWeekdayHeader(lines, calWeekdayNames) == nil {
		t.Errorf("Monday-first header not found:\n%s", calScreenText(lines))
	}

	a.prefs.CalendarWeekStart = "sunday"
	lines = calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	if calWeekdayHeader(lines, calSundayFirst) == nil {
		t.Errorf("Sunday-first header not found:\n%s", calScreenText(lines))
	}
}

func TestMonthDayBoxesShowDateNumbers(t *testing.T) {
	a, tv := calTestApp(t, nil)
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	re := regexp.MustCompile(`(^|\s)(\d{1,2})(\s|$)`)
	seen := map[string]bool{}
	for _, l := range lines {
		for _, m := range re.FindAllStringSubmatch(l, -1) {
			seen[m[2]] = true
		}
	}
	for d := 1; d <= 30; d++ {
		if !seen[fmt.Sprint(d)] {
			t.Errorf("date number %d not shown", d)
		}
	}
}

func weekNumberShown(lines []string, n int) bool {
	re := regexp.MustCompile(fmt.Sprintf(`(^|\s)%d(\s|$)`, n))
	for _, l := range lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

func TestMonthWeekNumbersFollowPreference(t *testing.T) {
	// November 2026 spans ISO weeks 44 to 49.
	a, tv := calTestApp(t, nil)
	a.prefs.CalendarShowWeekNumbers = true
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	for n := 44; n <= 49; n++ {
		if !weekNumberShown(lines, n) {
			t.Errorf("week number %d missing with week numbers on:\n%s", n, calScreenText(lines))
		}
	}

	a.prefs.CalendarShowWeekNumbers = false
	lines = calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	for n := 44; n <= 49; n++ {
		if weekNumberShown(lines, n) {
			t.Errorf("week number %d shown with week numbers off", n)
		}
	}
}

func TestMonthBoxListsEntriesCutToWidth(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "zebra alpha long text", Due: "2026-11-10"},
		{ID: "n#1", SourcePath: "n.md", Content: "zebra bravo long text", Due: "2026-11-10"},
		{ID: "n#2", SourcePath: "n.md", Content: "zebra charlie long text", Due: "2026-11-10"},
	})
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	text := calScreenText(lines)
	if got := strings.Count(text, "zebra"); got != 3 {
		t.Errorf("entries listed = %d, want one line per entry (3):\n%s", got, text)
	}
	if strings.Contains(text, "long text") {
		t.Errorf("entry text is not cut to the box width:\n%s", text)
	}
}

func TestMonthBoxCutsWideCharactersToWidth(t *testing.T) {
	wide := strings.Repeat("日本語", 12)
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: wide, Due: "2026-11-10"},
	})
	for _, w := range []int{72, 100, 120} {
		lines := calScreen(a, tv, w, 40)
		requireFullMonthGridAt(t, lines, w)
		text := calScreenText(lines)
		if !strings.Contains(text, "日") {
			t.Errorf("w=%d: wide-character entry not shown:\n%s", w, text)
		}
		if strings.Contains(text, wide) {
			t.Errorf("w=%d: wide-character entry not cut", w)
		}
		for i, l := range lines {
			if lipgloss.Width(l) > w {
				t.Errorf("w=%d line %d is %d cells wide: %q", w, i, lipgloss.Width(l), l)
			}
		}
	}
}

func TestMonthBoxOverflowShowsMoreCount(t *testing.T) {
	var tasks []vault.Task
	for i := 0; i < 50; i++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", i), SourcePath: "n.md", Content: fmt.Sprintf("zq%02d", i), Due: "2026-11-10"})
	}
	a, tv := calTestApp(t, tasks)
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	text := calScreenText(lines)
	listed := len(regexp.MustCompile(`zq\d\d`).FindAllString(text, -1))
	if listed == 0 || listed >= 50 {
		t.Fatalf("listed %d of 50 entries in a box, want some but not all:\n%s", listed, text)
	}
	more := regexp.MustCompile(`\+(\d+) more`).FindAllStringSubmatch(text, -1)
	if len(more) != 1 {
		t.Fatalf("found %d `+N more` lines, want 1:\n%s", len(more), text)
	}
	if want := fmt.Sprint(50 - listed); more[0][1] != want {
		t.Errorf("+%s more, want +%s more for %d listed", more[0][1], want, listed)
	}
}

func TestMonthItemPanelListsSelectedDay(t *testing.T) {
	var tasks []vault.Task
	for i := 0; i < 6; i++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", i), SourcePath: "n.md", Content: fmt.Sprintf("panel entry number %d in full", i), Due: "2026-11-18"})
	}
	tasks = append(tasks,
		vault.Task{ID: "n#6", SourcePath: "n.md", Kind: "file", Content: "panel scheduled entry in full", Scheduled: "2026-11-18"},
		vault.Task{ID: "n#7", SourcePath: "n.md", Content: "other day entry in full text", Due: "2026-11-19"},
	)
	a, tv := calTestApp(t, tasks)
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	text := calScreenText(lines)
	for i := 0; i < 6; i++ {
		if want := fmt.Sprintf("panel entry number %d in full", i); !strings.Contains(text, want) {
			t.Errorf("item panel is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "panel scheduled entry in full") {
		t.Errorf("item panel is missing the scheduled entry:\n%s", text)
	}
	if strings.Contains(text, "other day entry in full text") {
		t.Errorf("item panel lists another day's entry:\n%s", text)
	}
}

func TestScheduledEntryCarriesMarker(t *testing.T) {
	const text = "twin task with a long tail"
	tasks := []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Kind: "file", Content: text, Due: "2026-11-18", Scheduled: "2026-11-16"},
	}
	around := func(a *App, tv *tasksView, day time.Time) string {
		t.Helper()
		tv.selectDay(a, day)
		lines := calScreen(a, tv, 120, 40)
		requireFullMonthGrid(t, lines)
		for _, l := range lines {
			if i := strings.Index(l, text); i >= 0 {
				r := []rune(l[:i])
				if len(r) > 3 {
					r = r[len(r)-3:]
				}
				return string(r) + "|" + l[i:i+len(text)]
			}
		}
		t.Fatalf("%s: %q not found in the item panel:\n%s", day.Format("2006-01-02"), text, calScreenText(lines))
		return ""
	}
	a, tv := calTestApp(t, tasks)
	due := around(a, tv, calDate(2026, time.November, 18))
	sched := around(a, tv, calDate(2026, time.November, 16))
	if due == sched {
		t.Errorf("scheduled entry looks the same as the due entry: %q", due)
	}
}

func TestCalendarAppliesTaskFilter(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "alpha item in the panel", Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "beta item in the panel", Due: "2026-11-18"},
		{ID: "n#2", SourcePath: "n.md", Content: "alpha loose end"},
		{ID: "n#3", SourcePath: "n.md", Content: "beta loose end"},
	})
	tv.filter = "alpha"
	tv.refresh(a)
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	text := calScreenText(lines)
	for _, want := range []string{"alpha item in the panel", "alpha loose end"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing under the filter:\n%s", want, text)
		}
	}
	for _, bad := range []string{"beta item", "beta loose"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q shown despite the filter:\n%s", bad, text)
		}
	}
}

func TestMonthStripBelowGrid(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "bottom row task", Due: "2026-11-30"},
		{ID: "n#1", SourcePath: "n.md", Content: "loose strip task"},
	})
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	gridRow, stripRow := -1, -1
	for i, l := range lines {
		if gridRow < 0 && strings.Contains(l, "bottom r") {
			gridRow = i
		}
		if strings.Contains(l, "loose strip task") {
			stripRow = i
		}
	}
	if gridRow < 0 {
		t.Fatalf("the task on the last grid row is not shown:\n%s", calScreenText(lines))
	}
	if stripRow < 0 {
		t.Fatalf("the undated task is not shown:\n%s", calScreenText(lines))
	}
	if stripRow <= gridRow {
		t.Errorf("strip row %d is not below the last grid row %d:\n%s", stripRow, gridRow, calScreenText(lines))
	}
	if got := strings.Count(calScreenText(lines), "loose strip task"); got != 1 {
		t.Errorf("undated task shown %d times, want once", got)
	}
}

// Below the month minimum the calendar keeps a compact month: a line per
// week with a count per day, and the selected day's items under it.
func TestMonthCompactBelowMinimum(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "something", Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "undated thing"},
	})
	for _, size := range [][2]int{{71, 18}, {72, 17}, {43, 18}, {38, 12}} {
		ctx := fmt.Sprintf("%dx%d", size[0], size[1])
		lines := calScreen(a, tv, size[0], size[1])
		text := calScreenText(lines)
		if strings.Contains(text, "Terminal too small") {
			t.Errorf("%s: `Terminal too small` instead of the compact month:\n%s", ctx, text)
			continue
		}
		if !tv.calc().layout.compact {
			t.Errorf("%s: layout is not the compact month", ctx)
		}
		if calWeekdayHeader(lines, calWeekdayNames) == nil {
			t.Errorf("%s: no weekday header:\n%s", ctx, text)
		}
		for _, want := range []string{"November 2026", "18 1", "Wednesday, November 18 (1)", "something", "No date (1)", "undated thing"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: compact month lacks %q:\n%s", ctx, want, text)
			}
		}
		if len(lines) != size[1]+1 {
			t.Errorf("%s: render is %d lines, want toolbar plus %d", ctx, len(lines), size[1])
		}
		for i, l := range lines {
			if lipgloss.Width(l) > size[0] {
				t.Errorf("%s: line %d is %d cells wide", ctx, i, lipgloss.Width(l))
			}
		}
	}
}

func TestMonthTooSmallBelowCompactMinimum(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{{ID: "n#0", SourcePath: "n.md", Content: "something", Due: "2026-11-18"}})
	// Seven cells of five beside the week numbers need 38 columns; six weeks
	// with the item and strip lines under them need 12 rows.
	for _, size := range [][2]int{{37, 18}, {72, 11}} {
		text := calScreenText(calScreen(a, tv, size[0], size[1]))
		if !strings.Contains(text, "Terminal too small") {
			t.Errorf("%dx%d: no `Terminal too small`:\n%s", size[0], size[1], text)
		}
		for _, name := range []string{"Tu", "We", "Th", "Fr", "something"} {
			if strings.Contains(text, name) {
				t.Errorf("%dx%d: grid content %q shown while too small:\n%s", size[0], size[1], name, text)
			}
		}
	}
}

// The compact month takes the same keys and clicks as the full one.
func TestMonthCompactFocusAndClicks(t *testing.T) {
	a, tv := calTestApp(t, []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "first", Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "second", Due: "2026-11-18"},
		{ID: "n#2", SourcePath: "n.md", Content: "on the 20th", Due: "2026-11-20"},
	})
	a.prefs.VimMode = true
	calScreen(a, tv, 43, 18)
	lay := tv.calc().layout
	if !lay.compact {
		t.Fatal("43x18 is not the compact month")
	}
	tv.handleKey(a, vim.KeyEnter)
	tv.handleKey(a, vim.R('j'))
	if got := tv.selectedTask(); got == nil || got.Content != "second" {
		t.Fatalf("Enter then j selected %+v, want second", got)
	}
	calScreen(a, tv, 43, 18)
	x, y := tv.calc().selectionCell(tv.day)
	if !lay.items.contains(x, y) || y != lay.items.y+2 {
		t.Errorf("menu anchor (%d,%d) is not on the second item row of %+v", x, y, lay.items)
	}
	for _, b := range lay.boxes {
		if sameDay(b.day, calDate(2026, time.November, 20)) {
			tv.calc().click(a, tv, b.rect.x+1, b.rect.y, false, false)
		}
	}
	requireDay(t, tv, "2026-11-20", "click on the 20th")
	if tv.calc().focus != calFocusGrid {
		t.Errorf("click on a day left focus on %v", tv.calc().focus)
	}
	text := calScreenText(calScreen(a, tv, 43, 18))
	if !strings.Contains(text, "Friday, November 20 (1)") || !strings.Contains(text, "on the 20th") {
		t.Errorf("the 20th's items are not listed after the click:\n%s", text)
	}
}

func TestMonthRendersAtExactMinimum(t *testing.T) {
	a, tv := calTestApp(t, nil)
	lines := calScreen(a, tv, 72, 18)
	text := calScreenText(lines)
	if strings.Contains(text, "Terminal too small") {
		t.Fatalf("72x18 is the minimum and must render:\n%s", text)
	}
	requireFullMonthGridAt(t, lines, 72)
	for i, l := range lines {
		if lipgloss.Width(l) > 72 {
			t.Errorf("line %d is %d cells wide", i, lipgloss.Width(l))
		}
	}
	if len(lines) != 19 {
		t.Errorf("render is %d lines, want toolbar plus 18", len(lines))
	}
}

func TestMonthRendersInVeryLargeBody(t *testing.T) {
	var tasks []vault.Task
	for i := 0; i < 20; i++ {
		tasks = append(tasks, vault.Task{ID: fmt.Sprintf("n#%d", i), SourcePath: "n.md", Content: fmt.Sprintf("entry %d", i), Due: "2026-11-18"})
	}
	tasks = append(tasks, vault.Task{ID: "n#u", SourcePath: "n.md", Content: "undated"})
	a, tv := calTestApp(t, tasks)
	lines := calScreen(a, tv, 300, 100)
	requireFullMonthGridAt(t, lines, 300)
	if len(lines) != 101 {
		t.Errorf("render is %d lines, want toolbar plus 100", len(lines))
	}
	for i, l := range lines {
		if lipgloss.Width(l) > 300 {
			t.Errorf("line %d is %d cells wide", i, lipgloss.Width(l))
		}
	}
}

func TestCalendarRenderLeavesNotesUnchanged(t *testing.T) {
	a, root := newFeatureTestApp(t)
	body := []byte("---\ntags: [task]\nscheduled: 2026-11-16\n---\n# Plan the move\n")
	path := filepath.Join(root, "plan.md")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	a.idx.tasks = []vault.Task{{ID: "plan.md#task", SourcePath: "plan.md", Kind: "file", Content: "Plan the move", Scheduled: "2026-11-16"}}
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.CalendarShowWeekNumbers = true
	a, tv := calOpen(t, a)
	tv.selectDay(a, calDate(2026, time.November, 16))
	lines := calScreen(a, tv, 120, 40)
	requireFullMonthGrid(t, lines)
	if !strings.Contains(calScreenText(lines), "Plan the move") {
		t.Fatalf("the scheduled task is not shown on its day:\n%s", calScreenText(lines))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("note changed by rendering:\n%s", got)
	}
}

func TestScheduledMarkIsOneCellWide(t *testing.T) {
	if w := cellWidth(calScheduledMark); w != 1 {
		t.Errorf("scheduled marker %q is %d cells wide, want 1", calScheduledMark, w)
	}
}
