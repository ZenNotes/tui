package tui

import (
	"strings"
	"testing"
)

// helpText is the in-app manual as one text.
func helpText(t *testing.T) string {
	t.Helper()
	a := &App{}
	a.keymap = newTestResolver()
	return strings.Join(helpLines(a, ""), "\n")
}

func TestKeyHelpDescribesWeekAndDayKeys(t *testing.T) {
	help := helpText(t)
	for _, want := range []string{
		"Week and day: j k",
		"C-N",
		"C-P",
		"S-Tab",
		"all-day",
		"half-hourly",
		"quarter-hourly",
		"opens at the current time",
		"Calendar: W N",
		"Calendar event: T @ d D",
		"Calendar mouse",
		"task:[[",
		"where: session:",
		"Tasks: W",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("key help does not mention %q", want)
		}
	}
	if strings.Contains(help, "Scroll the all-day band") {
		t.Error("key help keeps the old zoom line")
	}
}

func TestKeyHelpAndHintsNeverMentionScheduled(t *testing.T) {
	help := strings.ToLower(helpText(t))
	if strings.Contains(help, "scheduled") {
		i := strings.Index(help, "scheduled")
		t.Errorf("key help mentions scheduled: %q", help[max(0, i-40):min(len(help), i+40)])
	}
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		calToSub(t, a, tv, sub)
		var labels []string
		for _, p := range tv.hintPairs(a) {
			labels = append(labels, p.id+" "+p.label)
		}
		if strings.Contains(strings.ToLower(strings.Join(labels, "|")), "scheduled") {
			t.Errorf("hints of sub %v mention scheduled: %v", sub, labels)
		}
	}
}

func TestCalendarHintsNameSessionAndEventKeys(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		calToSub(t, a, tv, sub)
		var got []string
		for _, p := range tv.hintPairs(a) {
			got = append(got, p.id+" "+p.label)
		}
		text := strings.Join(got, "|")
		for _, want := range []string{"key:W session", "key:N event"} {
			if !strings.Contains(text, want) {
				t.Errorf("hints of sub %v lack %q: %v", sub, want, got)
			}
		}
	}
}
