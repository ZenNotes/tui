package tui

import (
	"strings"
	"testing"
	"time"
)

// homeToday is the day that the Home tests treat as today.
var homeToday = time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)

// homeTaskRows draws the Today rows of the Home dashboard for some note
// bodies, with the clock set to day, and gives each row without style.
func homeTaskRows(day time.Time, timeFormat string, notes map[string]string) []string {
	a := sessionApp("note", notes)
	a.taskNow = func() time.Time { return day }
	a.theme = buildTheme(true)
	a.prefs.TimeFormat = timeFormat
	v := &homeView{}
	v.refresh(a)
	rows := []string{}
	for _, t := range v.tasksToday {
		rows = append(rows, stripAnsi(v.taskRow(a, t, 90, false)))
	}
	return rows
}

func findRow(t *testing.T, rows []string, name string) string {
	t.Helper()
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row
		}
	}
	t.Fatalf("no row for %q in %q", name, rows)
	return ""
}

func TestHomeTodayRowsShowTimesAndSession(t *testing.T) {
	notes := map[string]string{
		"Work":  "- [ ] Alpha due:2026-11-02T14:00\n- [ ] Bravo due:2026-11-02T14:00-15:30\n- [ ] Charlie ^cccccc\n- [ ] Delta due:2026-11-02\n",
		"Plans": "- Plan task:[[Work#^cccccc]] when:2026-11-02T09:30\n",
	}
	rows := homeTaskRows(homeToday, "24h", notes)
	for name, want := range map[string]string{
		"Alpha":   "2026-11-02 14:00",
		"Bravo":   "2026-11-02 14:00–15:30",
		"Charlie": "ses 09:30",
	} {
		if row := findRow(t, rows, name); !strings.Contains(row, want) {
			t.Errorf("%s row %q lacks %q", name, row, want)
		}
	}
	if row := findRow(t, rows, "Delta"); strings.Contains(row, "ses") || strings.Contains(row, ":") {
		t.Errorf("untimed row %q shows a time", row)
	}
	rows = homeTaskRows(homeToday, "12h", notes)
	if row := findRow(t, rows, "Alpha"); !strings.Contains(row, "2026-11-02 2:00 PM") {
		t.Errorf("12h row %q", row)
	}
	if row := findRow(t, rows, "Charlie"); !strings.Contains(row, "ses 9:30 AM") {
		t.Errorf("12h session row %q", row)
	}
}

func TestHomeOverdueRowShowsTime(t *testing.T) {
	day := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)
	rows := homeTaskRows(day, "24h", map[string]string{
		"Work": "- [ ] Alpha due:2026-11-02T14:00\n- [ ] Papa due:2026-11-04\n",
	})
	if row := findRow(t, rows, "Alpha"); !strings.Contains(row, "overdue 2026-11-02 14:00") {
		t.Errorf("overdue row %q", row)
	}
	if row := findRow(t, rows, "Papa"); !strings.Contains(row, "2026-11-04") || strings.Contains(row, "overdue") {
		t.Errorf("plain row %q", row)
	}
}
