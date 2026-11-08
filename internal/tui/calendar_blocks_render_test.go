package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// calBlockApp opens the calendar over the given note bodies on 2026-11-05, a
// Thursday, in the given layout. Weeks start on Monday and times are in the
// 24-hour format unless twelve is set.
func calBlockApp(t *testing.T, notes map[string]string, sub calSub, twelve bool) (*App, *tasksView) {
	t.Helper()
	a, tv := calEventApp(t, notes)
	a.prefs.VimMode = true
	a.prefs.TimeFormat = "24h"
	if twelve {
		a.prefs.TimeFormat = "12h"
	}
	tv.selectDay(a, calDate(2026, 11, 5))
	calToSub(t, a, tv, sub)
	return a, tv
}

// calRawScreen renders the body like calScreen and keeps the colour codes.
// The line numbers equal those of calScreen.
func calRawScreen(t *testing.T, a *App, tv *tasksView, w, bodyH int) []string {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	a.theme = buildTheme(true)
	return strings.Split(tv.render(a, w, bodyH+1, true), "\n")
}

// calHourRowIndex is the line of the hour row with the given label.
func calHourRowIndex(t *testing.T, lines []string, label string) int {
	t.Helper()
	for i, l := range lines {
		if m := calLabel24.FindStringSubmatch(l); m != nil && m[1] == label {
			return i
		}
	}
	t.Fatalf("no %s row:\n%s", label, calScreenText(lines))
	return -1
}

func TestBlockFirstRowShowsMarkAndText(t *testing.T) {
	notes := map[string]string{
		"Work.md": "- [ ] Report ^aaaaaa\n- [x] Filed ^bbbbbb\n" +
			"- [ ] Dentist due:2026-11-05T14:00-15:30\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-05T10:00-12:00\n" +
			"- task:[[Work#^bbbbbb]] when:2026-11-05T16:00\n" +
			"- Lunch when:2026-11-05T12:30-13:30\n",
	}
	for _, sub := range []calSub{calDay, calWeek} {
		a, tv := calBlockApp(t, notes, sub, false)
		lines := calScreen(a, tv, 160, 70)
		ctx := fmt.Sprintf("layout %v", sub)
		for _, c := range []struct{ label, want, absent string }{
			{"10", "☐Report", ""},
			{"11", "", "Report"},
			{"12", "◆Lunch", ""},
			{"13", "", "Lunch"},
			{"14", "Dentist", "◆Dentist"},
			{"15", "", "Dentist"},
			{"16", "☑Filed", ""},
		} {
			row := lines[calHourRowIndex(t, lines, c.label)]
			if c.want != "" && !strings.Contains(row, c.want) {
				t.Errorf("%s: row %s = %q, want %q", ctx, c.label, row, c.want)
			}
			if c.absent != "" && strings.Contains(row, c.absent) {
				t.Errorf("%s: row %s = %q must not hold %q", ctx, c.label, row, c.absent)
			}
		}
		if row := lines[calHourRowIndex(t, lines, "14")]; strings.Contains(row, "☐Dentist") || strings.Contains(row, "☑Dentist") {
			t.Errorf("%s: a due entry shows a state mark: %q", ctx, row)
		}
	}
}

func TestBlockTextIsCutToTheColumn(t *testing.T) {
	notes := map[string]string{"Work.md": "- [ ] Quarterly planning meeting with the whole team due:2026-11-05T09:00-10:00\n"}
	a, tv := calBlockApp(t, notes, calWeek, false)
	lines := calScreen(a, tv, 80, 70)
	row := lines[calHourRowIndex(t, lines, "09")]
	if !strings.Contains(row, "Quarter") || !strings.Contains(row, "…") || strings.Contains(row, "whole team") {
		t.Errorf("row 09 = %q, want the text cut with an ellipsis", row)
	}
	if w := lipgloss.Width(row); w > 80 {
		t.Errorf("row 09 is %d cells wide, want at most 80", w)
	}
}

func TestHourGridShowsNoTimeOfDay(t *testing.T) {
	notes := map[string]string{
		"Work.md":  "- [ ] Dentist due:2026-11-05T14:00-15:30\n- [ ] Report ^aaaaaa\n",
		"Plans.md": "- Sync when:2026-11-05T09:30\n- task:[[Work#^aaaaaa]] when:2026-11-05T11:00-12:00\n",
	}
	for _, sub := range []calSub{calDay, calWeek} {
		for _, twelve := range []bool{false, true} {
			a, tv := calBlockApp(t, notes, sub, twelve)
			screen := calScreenText(calScreen(a, tv, 160, 70))
			ctx := fmt.Sprintf("layout %v twelve=%v", sub, twelve)
			for _, name := range []string{"Dentist", "Sync", "Report"} {
				if !strings.Contains(screen, name) {
					t.Errorf("%s: %s is not on screen:\n%s", ctx, name, screen)
				}
			}
			for _, clock := range []string{":00", ":30", "PM-", "AM-"} {
				if strings.Contains(screen, clock) {
					t.Errorf("%s: the grid shows a time of day %q:\n%s", ctx, clock, screen)
				}
			}
		}
	}
}

func TestOverlapsStayPartlyVisible(t *testing.T) {
	notes := map[string]string{"Work.md": "- [ ] Alpha due:2026-11-05T10:00-12:00\n- [ ] Beta due:2026-11-05T11:00-13:00\n"}
	a, tv := calBlockApp(t, notes, calWeek, false)
	// A column is 22 cells wide at 160 cells, so a level moves 2 cells.
	lines := calScreen(a, tv, 160, 70)
	cols := requireWeekHeader(t, lines, calWeekdayNames, "overlap week")
	start := cols[3] - 1
	first := lines[calHourRowIndex(t, lines, "10")]
	if x := calCellOf(first, "Alpha"); x != start+1 {
		t.Errorf("Alpha starts at cell %d, want %d:\n%s", x, start+1, calScreenText(lines))
	}
	second := lines[calHourRowIndex(t, lines, "11")]
	if x := calCellOf(second, "Beta"); x != start+2+1 {
		t.Errorf("Beta starts at cell %d, want %d (two cells in):\n%s", x, start+3, calScreenText(lines))
	}
	raw := calRawScreen(t, a, tv, 160, 70)
	th := a.theme
	row := raw[calHourRowIndex(t, lines, "11")]
	for i := 0; i < 2; i++ {
		if !strings.Contains(row, stylePrefix(th.CalLevel[i])) {
			t.Errorf("row 11 holds no cell of level %d: %q", i+1, row)
		}
	}
	// The block of level 1 still tells its slot on its second row.
	if strings.Index(row, stylePrefix(th.CalLevel[0])) > strings.Index(row, stylePrefix(th.CalLevel[1])) {
		t.Errorf("level 1 does not come before level 2 in %q", row)
	}
}

func TestBeyondFourShowsPlusCountAndDistinctColours(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&sb, "- [ ] T%d due:2026-11-05T09:00-10:00\n", i)
	}
	a, tv := calBlockApp(t, map[string]string{"Work.md": sb.String()}, calDay, false)
	lines := calScreen(a, tv, 120, 70)
	row := lines[calHourRowIndex(t, lines, "09")]
	grid := row
	if p := tv.calc().layout.pane.x; p > 0 {
		grid = string([]rune(row)[:p])
	}
	if !strings.HasSuffix(strings.TrimRight(grid, " │"), "+2") {
		t.Errorf("row 09 of the grid = %q, want it to end with +2", grid)
	}
	if strings.Contains(calScreenText(lines), "more") {
		t.Errorf("the screen holds the old `more` text:\n%s", calScreenText(lines))
	}
	raw := calRawScreen(t, a, tv, 120, 70)
	th := a.theme
	seen := map[string]bool{}
	for i := range th.CalLevel {
		prefix := stylePrefix(th.CalLevel[i])
		if prefix == "" || seen[prefix] {
			t.Fatalf("level %d colour %q is empty or repeats another level", i+1, prefix)
		}
		seen[prefix] = true
		if !strings.Contains(raw[calHourRowIndex(t, lines, "09")], prefix) {
			t.Errorf("row 09 holds no cell of level %d", i+1)
		}
		for name, other := range map[string]lipgloss.Style{"overdue": th.Overdue, "selection": th.SelectedFocus} {
			if prefix == stylePrefix(other) {
				t.Errorf("level %d uses the %s colour", i+1, name)
			}
		}
	}
	if calRowOf(lines, "T4") < 0 && calRowOf(lines, "T5") < 0 && calRowOf(lines, "T6") < 0 {
		t.Errorf("no block of level 4 shows its text:\n%s", calScreenText(lines))
	}
}

func TestDayViewAllDayPane(t *testing.T) {
	notes := map[string]string{
		"Work.md":  "- [ ] errand due:2026-11-05\n- [ ] laundry due:2026-11-05\n- [ ] Standup due:2026-11-05T10:00\n",
		"Plans.md": "- Trip when:2026-11-04..2026-11-06\n",
	}
	a, tv := calBlockApp(t, notes, calDay, false)
	for _, c := range []struct{ w, pane int }{{70, 24}, {120, 36}, {200, 40}} {
		lines := calScreen(a, tv, c.w, 70)
		ctx := fmt.Sprintf("width %d", c.w)
		if got := tv.calc().layout.pane.w; got != c.pane {
			t.Errorf("%s: the pane is %d wide, want %d", ctx, got, c.pane)
		}
		if got := strings.Count(calScreenText(lines), "All day (3)"); got != 1 {
			t.Fatalf("%s: `All day (3)` shown %d times, want once:\n%s", ctx, got, calScreenText(lines))
		}
		px := tv.calc().layout.pane.x
		for _, name := range []string{"errand", "laundry", "◆Trip"} {
			row := calRowOf(lines, name)
			if row < 0 || calCellOf(lines[row], name) < px {
				t.Errorf("%s: %s is not in the pane (x %d):\n%s", ctx, name, px, calScreenText(lines))
			}
		}
		if row := calRowOf(lines, "Standup"); row < 0 || calCellOf(lines[row], "Standup") >= px {
			t.Errorf("%s: the timed entry is not in the hour grid:\n%s", ctx, calScreenText(lines))
		}
		// The pane starts on the first row of the hours, so no band is above.
		head := calRowOf(lines, "All day (3)")
		if m := calLabel24.FindStringSubmatch(lines[head]); m == nil || m[1] != "00" {
			t.Errorf("%s: `All day` is on %q, want the 00 row", ctx, lines[head])
		}
	}
	tv.selectDay(a, calDate(2026, 11, 8))
	if text := calScreenText(calScreen(a, tv, 120, 70)); !strings.Contains(text, "All day (0)") {
		t.Errorf("an empty day shows no `All day (0)`:\n%s", text)
	}
}

func TestWeekViewKeepsAllDayBand(t *testing.T) {
	notes := map[string]string{
		"Work.md": "- [ ] errand due:2026-11-03\n- [ ] laundry due:2026-11-03\n- [ ] Standup due:2026-11-05T10:00\n",
		"Plans.md": "- Trip when:2026-11-04..2026-11-06\n" +
			"- Night when:2026-11-05T22:00..2026-11-06T02:00\n",
	}
	a, tv := calBlockApp(t, notes, calWeek, false)
	lines := calScreen(a, tv, 160, 70)
	text := calScreenText(lines)
	cols := requireWeekHeader(t, lines, calWeekdayNames, "week with bands")
	if strings.Contains(text, "All day") {
		t.Errorf("the week view shows the pane label:\n%s", text)
	}
	first := calHourRowIndex(t, lines, "00")
	column := func(name string, day int) {
		t.Helper()
		row := calRowOf(lines, name)
		if row < 0 || row >= first {
			t.Errorf("%s is not in the band above the hours:\n%s", name, text)
			return
		}
		if x := calCellOf(lines[row], name); x < cols[day]-1 || x >= cols[day+1]-1 {
			t.Errorf("%s starts at cell %d, want inside column %d", name, x, day)
		}
	}
	column("errand", 1)
	if got := strings.Count(text, "◆Trip"); got != 3 {
		t.Errorf("the cross-day event is in %d bands, want 3 (Wednesday to Friday):\n%s", got, text)
	}
	// The timed cross-day event is a block at the end of Thursday and at the
	// start of Friday.
	late := lines[calHourRowIndex(t, lines, "22")]
	if x := calCellOf(late, "◆Night"); x < cols[3]-1 || x >= cols[4]-1 {
		t.Errorf("Night starts at cell %d in the 22 row, want inside Thursday: %q", x, late)
	}
	early := lines[first]
	if x := calCellOf(early, "◆Night"); x < cols[4]-1 || x >= cols[5]-1 {
		t.Errorf("Night starts at cell %d in the 00 row, want inside Friday: %q", x, early)
	}
}

func TestSmallestBodiesAndTwelveHourWeek(t *testing.T) {
	notes := map[string]string{
		"Work.md":  "- [ ] Dentist due:2026-11-05T14:00-15:30\n- [ ] errand due:2026-11-05\n",
		"Plans.md": "- Night when:2026-11-05T22:00..2026-11-06T02:00\n",
	}
	for _, c := range []struct {
		sub     calSub
		w, h    int
		twelve  bool
		minimum string
	}{
		{calDay, 60, 14, false, "60×14"},
		{calDay, 60, 14, true, "60×14"},
		{calWeek, 64, 16, false, "64×16"},
		{calWeek, 64, 16, true, "64×16"},
	} {
		a, tv := calBlockApp(t, notes, c.sub, c.twelve)
		ctx := fmt.Sprintf("layout %v %dx%d twelve=%v", c.sub, c.w, c.h, c.twelve)
		lines := calScreen(a, tv, c.w, c.h)
		requireTooSmall(t, lines, false, ctx)
		if len(lines) != c.h+1 {
			t.Errorf("%s: %d lines, want toolbar plus %d", ctx, len(lines), c.h)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > c.w {
				t.Errorf("%s: line %d is %d cells wide", ctx, i, w)
			}
		}
		if got := calHourLabels(lines, calLabel24); !c.twelve && (len(got) == 0 || len(got) >= 24) {
			t.Errorf("%s: %d hour rows shown, want some but fewer than 24", ctx, len(got))
		}
		if c.sub == calDay && !strings.Contains(calScreenText(lines), "All day (1)") {
			t.Errorf("%s: no `All day (1)` pane:\n%s", ctx, calScreenText(lines))
		}
		// Scroll to the afternoon: the block text shows, cut to the column.
		for i := 0; i < 24; i++ {
			calPress(a, tv, calRunes("j")...)
		}
		lines = calScreen(a, tv, c.w, c.h)
		screen := calScreenText(lines)
		if !strings.Contains(screen, "Den") && !strings.Contains(screen, "◆Ni") && !strings.Contains(screen, "Nig") {
			t.Errorf("%s: no block text at the end of the day:\n%s", ctx, screen)
		}
		for _, d := range [][2]int{{c.w - 1, c.h}, {c.w, c.h - 1}} {
			requireTooSmall(t, calScreen(a, tv, d[0], d[1]), true, fmt.Sprintf("%s below %dx%d", ctx, d[0], d[1]))
		}
		if !strings.Contains(calScreenText(calScreen(a, tv, c.w-1, c.h)), "needs "+c.minimum) {
			t.Errorf("%s: the too small text does not name %s", ctx, c.minimum)
		}
	}
}
