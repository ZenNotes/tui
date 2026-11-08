package tui

import (
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vim"
)

// footerLines gives the lines of the footer of a week or day view: the lines
// right above the strip of tasks without a date, one sixth of the body high
// and between two and five lines.
func footerLines(t *testing.T, lines []string, bodyH int) []string {
	t.Helper()
	at := -1
	for i, l := range lines {
		if strings.Contains(l, "No date") {
			at = i
		}
	}
	n := clampInt(bodyH/6, 2, 5)
	if at < n {
		t.Fatalf("no strip with room for a footer of %d lines:\n%s", n, calScreenText(lines))
	}
	return lines[at-n : at]
}

// footerText joins the lines of the footer, trimmed, with one space between
// them, so that a text that wrapped reads as it was written.
func footerText(lines []string) string {
	var parts []string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func requireFooterHas(t *testing.T, foot []string, context string, want ...string) {
	t.Helper()
	text := footerText(foot)
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s: the footer has no %q:\n%s", context, w, strings.Join(foot, "\n"))
		}
	}
	if strings.Contains(text, "scheduled") {
		t.Errorf("%s: the footer has the word scheduled:\n%s", context, strings.Join(foot, "\n"))
	}
}

const footerLongText = "Meet Ana about the quarterly budget and the hiring plan for the new team " +
	"and then write down what we agreed so that nobody forgets it"

var footerWork = map[string]string{
	"Work.md":  "- [ ] " + footerLongText + " due:2026-11-05T17:00 @repeat:1w ^ab12cd\n",
	"Plans.md": "- " + footerLongText + " task:[[Work#^ab12cd]] when:2026-11-03T14:00-15:30\n",
}

func TestFooterShowsWholeTextAndDetails(t *testing.T) {
	for _, c := range []struct {
		format, session, due string
	}{
		{"24h", "session 2026-11-03 14:00–15:30", "due 2026-11-05 17:00"},
		{"12h", "session 2026-11-03 2:00 PM–3:30 PM", "due 2026-11-05 5:00 PM"},
	} {
		a, tv, _ := calCursorApp(t, footerWork, calDay, false)
		a.prefs.TimeFormat = c.format
		calDownTo(a, tv, 14)

		// A narrow footer wraps the text and never cuts it.
		foot := footerLines(t, calScreen(a, tv, 64, 60), 60)
		text := footerText(foot)
		if !strings.Contains(text, footerLongText) {
			t.Errorf("%s: the footer does not show the whole text:\n%s", c.format, strings.Join(foot, "\n"))
		}
		if strings.Contains(text, "…") {
			t.Errorf("%s: the footer cuts a line:\n%s", c.format, strings.Join(foot, "\n"))
		}
		requireFooterHas(t, foot, c.format+" narrow", c.session, c.due, "repeat 1w", "Work · session in Plans")

		// A wide footer puts the dates, the rule and the note on one line.
		foot = footerLines(t, calScreen(a, tv, 200, 60), 60)
		want := strings.Join([]string{c.session, c.due, "repeat 1w", "Work · session in Plans"}, " · ")
		found := false
		for _, l := range foot {
			found = found || strings.TrimSpace(l) == want
		}
		if !found {
			t.Errorf("%s: no footer line reads %q:\n%s", c.format, want, strings.Join(foot, "\n"))
		}
	}
}

func TestFooterListsOverlapsInLevelOrder(t *testing.T) {
	var sb strings.Builder
	for _, n := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"} {
		sb.WriteString("- [ ] " + n + " due:2026-11-03T10:00-11:00\n")
	}
	files := map[string]string{"Work.md": sb.String()}

	a, tv, _ := calCursorApp(t, files, calDay, false)
	calDownTo(a, tv, 10)
	foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
	text := footerText(foot)
	for _, want := range []string{"▸ Alpha", "Bravo", "+3 more · Enter lists them"} {
		if !strings.Contains(text, want) {
			t.Errorf("the footer has no %q:\n%s", want, strings.Join(foot, "\n"))
		}
	}
	if strings.Index(text, "Alpha") > strings.Index(text, "Bravo") {
		t.Errorf("the footer does not list Alpha before Bravo:\n%s", strings.Join(foot, "\n"))
	}
	if strings.Contains(text, "Charlie") {
		t.Errorf("the footer lists an entry that does not fit:\n%s", strings.Join(foot, "\n"))
	}
	if last := strings.TrimSpace(foot[len(foot)-1]); last != "+3 more · Enter lists them" {
		t.Errorf("the last footer line is %q, want the count of the rest", last)
	}

	// Tab moves the mark, and the marked entry always shows.
	for i := 0; i < 3; i++ {
		calPress(a, tv, vim.KeyTab)
	}
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	if text = footerText(foot); !strings.Contains(text, "▸ Delta") || strings.Contains(text, "▸ Alpha") {
		t.Errorf("the mark is not on Delta:\n%s", strings.Join(foot, "\n"))
	}

	// Two entries fit and need no count.
	a, tv, _ = calCursorApp(t, map[string]string{"Work.md": "- [ ] Alpha due:2026-11-03T10:00\n- [ ] Bravo due:2026-11-03T10:00\n"}, calDay, false)
	calDownTo(a, tv, 10)
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	text = footerText(foot)
	if !strings.Contains(text, "Alpha") || !strings.Contains(text, "Bravo") || strings.Contains(text, "more") {
		t.Errorf("the footer does not list both entries whole:\n%s", strings.Join(foot, "\n"))
	}
}

func TestFooterBlankOnEmptySlotAndFollowsFocus(t *testing.T) {
	files := map[string]string{"Work.md": "- [ ] Nine due:2026-11-03T09:00\n- [ ] Whole day due:2026-11-03\n- [ ] Loose end\n"}
	a, tv, _ := calCursorApp(t, files, calDay, false)

	foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
	if text := footerText(foot); text != "" {
		t.Errorf("the footer is not blank on an empty slot: %q", text)
	}

	c := tv.calc()
	c.focus, c.cursor = calFocusItems, 0
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "all-day entry", "Whole day", "due 2026-11-03")

	c.focus, c.strip = calFocusStrip, 0
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "strip", "Loose end", "Work")
	if strings.Contains(footerText(foot), "due") {
		t.Errorf("the footer of a task with no date has a due date:\n%s", strings.Join(foot, "\n"))
	}
}

func TestFooterShowsEventDetails(t *testing.T) {
	files := map[string]string{"Plans.md": "- Spring break when:2027-03-13..2027-03-21\n" +
		"- Night shift when:2026-11-03T22:00..2026-11-04T02:00\n" +
		"- Dentist when:2026-11-03T14:00-15:00 @repeat:1w\n"}

	a, tv, _ := calCursorApp(t, files, calDay, false)
	calDownTo(a, tv, 14)
	foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "dentist", "Dentist", "2026-11-03 14:00–15:00", "repeat 1w", "Plans")

	calDownTo(a, tv, 8)
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "night shift", "Night shift", "2026-11-03 22:00 – 2026-11-04 02:00")
	a.prefs.TimeFormat = "12h"
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "night shift, 12h", "2026-11-03 10:00 PM – 2026-11-04 2:00 AM")

	tv.selectDay(a, calDate(2027, 3, 13))
	c := tv.calc()
	c.focus, c.cursor = calFocusItems, 0
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "spring break", "Spring break", "2027-03-13 – 2027-03-21")
}

func TestFooterLabelsSessionDate(t *testing.T) {
	files := map[string]string{
		"Work.md":  "- [ ] Draft report due:2026-11-06 ^ab12cd\n",
		"Plans.md": "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00-15:30\n",
	}
	a, tv, _ := calCursorApp(t, files, calDay, false)
	calDownTo(a, tv, 14)
	foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "session", "Write report", "session 2026-11-03 14:00–15:30", "due 2026-11-06", "Work · session in Plans")
}

func TestFooterShowsDanglingLink(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no note": {"Plans.md": "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00\n"},
		"plain bullet": {
			"Plans.md": "- Write report task:[[Work#^ab12cd]] when:2026-11-03T14:00\n",
			"Work.md":  "- A bullet, no task ^ab12cd\n",
		},
	} {
		a, tv, _ := calCursorApp(t, files, calDay, false)
		calDownTo(a, tv, 14)
		foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
		requireFooterHas(t, foot, name, "task not found: [[Work#^ab12cd]]")
	}
}

func TestFooterShowsRepeatUntil(t *testing.T) {
	files := map[string]string{
		"Work.md":  "- [ ] Standup due:2026-11-03T14:00 @repeat:1w until:2026-12-14\n",
		"Plans.md": "- Review when:2026-11-03T16:00 @repeat:1w until:2026-11-23\n",
	}
	a, tv, _ := calCursorApp(t, files, calDay, false)
	calDownTo(a, tv, 14)
	foot := footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "task", "Standup", "repeat 1w until 2026-12-14")
	calDownTo(a, tv, 2)
	foot = footerLines(t, calScreen(a, tv, 120, 60), 60)
	requireFooterHas(t, foot, "event", "Review", "repeat 1w until 2026-11-23")
}

func TestMonthViewHasNoFooter(t *testing.T) {
	a, tv, _ := calCursorApp(t, map[string]string{"Work.md": "- [ ] Nine due:2026-11-03T09:00\n"}, calMonth, false)
	lines := calScreen(a, tv, 120, 60)
	for _, l := range lines {
		if strings.Contains(l, "due 2026") || strings.Contains(l, "session in") {
			t.Errorf("the month view has a footer line %q", l)
		}
	}
}
