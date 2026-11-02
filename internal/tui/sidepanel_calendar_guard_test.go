package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

func guardCalendarApp(t *testing.T) *App {
	t.Helper()
	a, _ := newFeatureTestApp(t)
	a.idx.tasks = []vault.Task{
		{ID: "n#0", SourcePath: "n.md", Content: "pay rent", Due: "2026-11-18"},
		{ID: "n#1", SourcePath: "n.md", Content: "call mum", Due: "2026-11-18", Scheduled: "2026-11-16"},
		{ID: "n#2", SourcePath: "n.md", Content: "scheduled only", Scheduled: "2026-11-20"},
		{ID: "n#3", SourcePath: "n.md", Content: "no date"},
		{ID: "n#4", SourcePath: "n.md", Content: "later", Due: "2026-11-25"},
	}
	a.calendarOpen = true
	a.calendar.month = time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	a.calendar.day = time.Date(2026, 11, 18, 0, 0, 0, 0, time.Local)
	a.calendar.hasDaily = nil
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.CalendarShowWeekNumbers = false
	return a
}

// The side calendar panel keeps its compact grid, its digit markers and its
// day list whatever the Tasks tab does with its own calendar.
func TestSideCalendarPanelRenderUnchanged(t *testing.T) {
	a := guardCalendarApp(t)
	got := stripAnsi(a.calendar.render(a, 40, 16))
	var trimmed []string
	for _, l := range strings.Split(got, "\n") {
		trimmed = append(trimmed, strings.TrimRight(l, " "))
	}
	want := strings.Join([]string{
		" Calendar",
		" November 2026",
		" Mo   Tu   We   Th   Fr   Sa   Su",
		" 26   27   28   29   30   31    1",
		"  2    3    4    5    6    7    8",
		"  9   10   11   12   13   14   15",
		" 16   17   18 2 19   20   21   22",
		" 23   24   25 1 26   27   28   29",
		" 30    1    2    3    4    5    6",
		"",
		" No daily note. Enter creates it.",
		" ☐ pay rent",
		" ☐ call mum",
		"",
		"",
		"",
	}, "\n")
	if strings.Join(trimmed, "\n") != want {
		t.Errorf("side calendar render changed:\n%s", strings.Join(trimmed, "\n"))
	}
}

// dayAtCell maps clicks on the side panel's grid to days, with and without
// the week-number column.
func TestSideCalendarPanelClickUnchanged(t *testing.T) {
	for _, weekNumbers := range []bool{false, true} {
		a := guardCalendarApp(t)
		a.prefs.CalendarShowWeekNumbers = weekNumbers
		a.calendar.render(a, 40, 16)
		first, weeks := a.calendar.calFirst, a.calendar.calWeeks
		if first.Format("2006-01-02") != "2026-10-26" || weeks != 6 {
			t.Fatalf("grid = %s x %d weeks, want 2026-10-26 x 6", first.Format("2006-01-02"), weeks)
		}
		offset := 0
		if weekNumbers {
			offset = 4
		}
		type cell struct {
			name string
			x, y int
			want string
			ok   bool
		}
		cases := []cell{
			{"first cell", offset, 2, "2026-10-26", true},
			{"second column", offset + 5, 2, "2026-10-27", true},
			{"last column", offset + 30, 2, "2026-11-01", true},
			{"third row", offset + 10, 4, "2026-11-11", true},
			{"last row", offset + 15, 7, "2026-12-03", true},
			{"title row", offset, 0, "", false},
			{"header row", offset, 1, "", false},
			{"below grid", offset, 8, "", false},
			{"right of grid", offset + 35, 2, "", false},
		}
		if weekNumbers {
			cases = append(cases, cell{"week number column", 2, 2, "", false})
		}
		for _, tc := range cases {
			day, ok := dayAtCell(first, weeks, tc.x, tc.y, weekNumbers)
			got := ""
			if ok {
				got = day.Format("2006-01-02")
			}
			if ok != tc.ok || got != tc.want {
				t.Errorf("weekNumbers=%v %s: dayAtCell(%d,%d) = %q,%v want %q,%v", weekNumbers, tc.name, tc.x, tc.y, got, ok, tc.want, tc.ok)
			}
		}
	}
}
