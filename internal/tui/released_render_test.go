package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// releasedDay is the day the notes in internal/cli/testdata/released-tasks were
// written for. A vault made for another day moves every date by the same
// number of days.
var releasedDay = time.Date(2026, time.November, 6, 0, 0, 0, 0, time.Local)

var releasedDateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// shiftDates moves each ISO date in text by the days between from and to.
func shiftDates(text string, from, to time.Time) string {
	return releasedDateRe.ReplaceAllStringFunc(text, func(s string) string {
		d, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return s
		}
		return d.AddDate(0, 0, daysBetween(from, to)).Format("2006-01-02")
	})
}

func daysBetween(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 12, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// releasedRenderVault holds the notes of the released-tasks fixture and the
// long-standing notes of defaultTasksVault, with every date moved to be
// relative to day.
func releasedRenderVault(t *testing.T, day time.Time) string {
	t.Helper()
	root := defaultTasksVault(t, day)
	src := filepath.Join("..", "cli", "testdata", "released-tasks")
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, "released", rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(shiftDates(string(data), releasedDay, day)), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// releasedRelative writes each absolute date in text as an offset from now.
func releasedRelative(text string, now time.Time) string {
	return releasedDateRe.ReplaceAllStringFunc(text, func(s string) string {
		return relativeDay(s, now)
	})
}

var releasedHomeStatsRe = regexp.MustCompile(`\d+ notes .*$`)

// releasedHomeText keeps the parts of the Home dashboard that depend on the
// tasks alone: the counts in its header and the left half of its Today card.
// The vault name, the date, the week strip and the recent notes change from
// one run to the next.
func releasedHomeText(view string) string {
	var out []string
	inToday := false
	for i, line := range strings.Split(stripAnsi(view), "\n") {
		if i == 0 {
			out = append(out, strings.TrimRight(releasedHomeStatsRe.FindString(line), " "))
			continue
		}
		left := strings.TrimRight(ansi.Truncate(line, 71, ""), " ")
		if strings.Contains(left, "╭─ Today") {
			inToday = true
		}
		if inToday {
			out = append(out, left)
		}
		if inToday && strings.Contains(left, "╰") {
			break
		}
	}
	return strings.Join(out, "\n")
}

// releasedRenderReport draws the Tasks list, the board for each grouping, the
// Home dashboard and the calendar's month view at 120 by 40 cells, without
// styling.
func releasedRenderReport(t *testing.T) string {
	t.Helper()
	now := time.Now()
	var b strings.Builder
	// Each section writes its dates as offsets from the day its vault was made
	// for, so the report is the same on every day.
	section := func(title, body string, day time.Time) {
		b.WriteString("== " + title + " ==\n")
		b.WriteString(releasedRelative(stripAnsi(body), day))
		b.WriteString("\n")
	}

	a := newSessionParityApp(t, releasedRenderVault(t, now))
	loadTasksForTest(t, a)
	a.prefs.TasksViewMode = "list"
	section("list", newTasksView(a).render(a, 120, 40, true), now)
	a.prefs.TasksViewMode = "kanban"
	for _, group := range []string{"status", "priority", "due"} {
		a.prefs.KanbanGroupBy = group
		section("board by "+group, newTasksView(a).render(a, 120, 40, true), now)
	}
	home := &homeView{}
	home.refresh(a)
	section("home", releasedHomeText(home.render(a, 120, 40, true)), now)

	// The month grid names real days, so the calendar reads a fixed day.
	prev := calendarNow
	calendarNow = func() time.Time { return releasedDay }
	t.Cleanup(func() { calendarNow = prev })
	a = newSessionParityApp(t, releasedRenderVault(t, releasedDay))
	loadTasksForTest(t, a)
	a.prefs.TasksViewMode = "calendar"
	v := newTasksView(a)
	v.month = time.Date(releasedDay.Year(), releasedDay.Month(), 1, 0, 0, 0, 0, time.Local)
	v.day = releasedDay
	section("calendar month", v.render(a, 120, 40, true), releasedDay)
	return b.String()
}

// The Tasks list, the board, the Home dashboard and the calendar's month view
// draw the same text as released zn v0.17.1 for notes without the new syntax.
// The golden file was made by running this report with zn v0.17.1.
func TestTasksViewRendersAsReleasedWithoutNewSyntax(t *testing.T) {
	got := releasedRenderReport(t)
	golden := filepath.Join("testdata", "released-tasks-render.golden")
	if os.Getenv("ZN_WRITE_RELEASED_RENDER_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the Tasks view differs from released zn v0.17.1.\ngot:\n%s\nwant:\n%s", got, want)
	}
}
