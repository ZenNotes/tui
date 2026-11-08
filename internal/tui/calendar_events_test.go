package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// calEventApp opens the calendar over the tasks and events that the given note
// bodies hold. The notes are not written to disk.
func calEventApp(t *testing.T, notes map[string]string) (*App, *tasksView) {
	t.Helper()
	a, _ := newFeatureTestApp(t)
	names := make([]string, 0, len(notes))
	for name := range notes {
		names = append(names, name)
	}
	sort.Strings(names)
	var tasks []vault.Task
	var events []vault.Event
	for _, name := range names {
		title := strings.TrimSuffix(name, ".md")
		tasks = append(tasks, vault.ParseTasks(name, title, vault.FolderInbox, notes[name], vault.ParseTasksOptions{Dialect: vault.DialectApp})...)
		events = append(events, vault.ParseEvents(name, title, vault.FolderInbox, notes[name])...)
	}
	a.idx.tasks, a.idx.events = tasks, events
	a.prefs.CalendarWeekStart = "monday"
	a.prefs.TimeFormat = "24h"
	a, tv := calOpen(t, a)
	calToSub(t, a, tv, calMonth)
	return a, tv
}

// calLabelsOn gives the label of each entry that the calendar lists on a day.
func calLabelsOn(tv *tasksView, year int, month time.Month, day int) []string {
	var out []string
	for _, e := range tv.calc().dayEntries(calDate(year, month, day)) {
		out = append(out, e.label("24h"))
	}
	return out
}

// calDaysListing gives the days of a month, as day numbers, on which an entry
// label contains text.
func calDaysListing(tv *tasksView, year int, month time.Month, text string) []int {
	var out []int
	for d := 1; d <= 31; d++ {
		if calDate(year, month, d).Month() != month {
			break
		}
		for _, l := range calLabelsOn(tv, year, month, d) {
			if strings.Contains(l, text) {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

func TestMonthViewListsEventOccurrences(t *testing.T) {
	a, tv := calEventApp(t, map[string]string{
		"Lecture.md": "---\npage-type: event\nwhen: 2026-11-02T09:00-10:30\nrepeat: 1w\nskip: [2026-11-16]\n---\nBody\n",
		"Plans.md": "- Gym when:2026-11-02T07:00 @repeat:after-1w\n" +
			"- Trip when:2026-11-05/2026-11-07\n" +
			"- Course when:2026-11-02T18:00 @repeat:1w @until:2026-11-23\n" +
			"- Conference when:2027-03-13/2027-03-14 @repeat:1y\n",
	})
	cases := []struct {
		text string
		want []int
	}{
		{"Lecture", []int{2, 9, 23, 30}},
		{"Gym", []int{2, 9, 16, 23, 30}},
		{"Trip", []int{5, 6, 7}},
		{"Course", []int{2, 9, 16, 23}},
	}
	for _, c := range cases {
		if got := calDaysListing(tv, 2026, time.November, c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s is listed on days %v, want %v", c.text, got, c.want)
		}
	}
	if got := calLabelsOn(tv, 2026, time.November, 5); !reflect.DeepEqual(got, []string{"◆Trip"}) {
		t.Errorf("2026-11-05 lists %q, want the whole-day trip", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 2); !reflect.DeepEqual(got, []string{"◆07:00 Gym", "◆09:00 Lecture", "◆18:00 Course"}) {
		t.Errorf("2026-11-02 lists %q", got)
	}
	// A yearly event shows years later, on both of its days.
	tv.selectDay(a, calDate(2028, time.March, 15))
	if got := calDaysListing(tv, 2028, time.March, "Conference"); !reflect.DeepEqual(got, []int{13, 14}) {
		t.Errorf("Conference is listed on days %v of 2028-03, want [13 14]", got)
	}
}

func TestMonthViewShowsEventOnScreen(t *testing.T) {
	a, tv := calEventApp(t, map[string]string{"Plans.md": "- Trip when:2026-11-18\n"})
	if text := calScreenText(calScreen(a, tv, 160, 40)); !strings.Contains(text, "◆Trip") {
		t.Errorf("the month does not show the event:\n%s", text)
	}
}

func TestCalendarMarksSessionsByTaskState(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{
		"Work.md": "- [ ] Write report ^aaaaaa\n- [x] Pay bills ^bbbbbb\n- [/] Draft ^cccccc\n- [-] Old plan ^dddddd\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-03T09:00\n" +
			"- task:[[Work#^bbbbbb]] when:2026-11-03T10:00\n" +
			"- task:[[Work#^cccccc]] when:2026-11-03T11:00\n" +
			"- task:[[Work#^dddddd]] when:2026-11-03T12:00\n" +
			"- Lost task:[[Work#^zzzzzz]] when:2026-11-03T13:00\n",
	})
	want := []string{"☐09:00 Write report", "☑10:00 Pay bills", "◩11:00 Draft", "☒12:00 Old plan", "?13:00 Lost"}
	if got := calLabelsOn(tv, 2026, time.November, 3); !reflect.DeepEqual(got, want) {
		t.Errorf("2026-11-03 lists %q, want %q", got, want)
	}
	for _, e := range tv.calc().dayEntries(calDate(2026, time.November, 3)) {
		if got, want := e.done, e.mark == "☑"; got != want {
			t.Errorf("%q: done = %v, want %v", e.label("24h"), got, want)
		}
	}
}

func TestCalendarMarksForwardedAndFinishedRepeatSessions(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{
		"Work.md":   "- [>] Moved ^aaaaaa\n",
		"Report.md": "---\npage-type: task\nstatus: open\nrepeat: 1w\nlastCompleted: 2026-11-02\n---\n# Report\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-03T09:00\n" +
			"- task:[[Report]] when:2026-11-02T09:00\n" +
			"- task:[[Report]] when:2026-11-05T09:00\n",
	})
	if got := calLabelsOn(tv, 2026, time.November, 3); !reflect.DeepEqual(got, []string{"→09:00 Moved"}) {
		t.Errorf("2026-11-03 lists %q", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 2); !reflect.DeepEqual(got, []string{"☑09:00 Report"}) {
		t.Errorf("2026-11-02 lists %q", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 5); !reflect.DeepEqual(got, []string{"☐09:00 Report"}) {
		t.Errorf("2026-11-05 lists %q", got)
	}
}

func TestSessionShowsOwnTextElseTaskContent(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{
		"Work.md": "- [ ] Write report ^aaaaaa\n",
		"Plans.md": "- Outline task:[[Work#^aaaaaa]] when:2026-11-03T09:00\n" +
			"- task:[[Work#^aaaaaa]] when:2026-11-04T09:00\n",
	})
	if got := calLabelsOn(tv, 2026, time.November, 3); !reflect.DeepEqual(got, []string{"☐09:00 Outline"}) {
		t.Errorf("a session with text lists %q", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 4); !reflect.DeepEqual(got, []string{"☐09:00 Write report"}) {
		t.Errorf("a session with no text lists %q", got)
	}
}

func TestMonthViewListsSessionWithTime(t *testing.T) {
	a, tv := calEventApp(t, map[string]string{
		"Work.md":  "- [ ] X due:2026-11-10 ^aaaaaa\n",
		"Plans.md": "- task:[[Work#^aaaaaa]] when:2026-11-05T09:30\n",
	})
	if got := calLabelsOn(tv, 2026, time.November, 5); !reflect.DeepEqual(got, []string{"☐09:30 X"}) {
		t.Errorf("2026-11-05 lists %q", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 10); !reflect.DeepEqual(got, []string{"X"}) {
		t.Errorf("2026-11-10 lists %q, want the due entry alone", got)
	}
	a.prefs.TimeFormat = "12h"
	tv.selectDay(a, calDate(2026, time.November, 5))
	if text := calScreenText(calScreen(a, tv, 160, 40)); !strings.Contains(text, "9:30 AM") {
		t.Errorf("the month does not show the session time:\n%s", text)
	}
}

func TestEventAcrossDaysIsBlockOnFirstAndLastDay(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{"Plans.md": "- Shift when:2026-11-03T22:00/2026-11-05T02:00\n"})
	type shape struct{ clock, end string }
	want := map[int]shape{3: {"22:00", "24:00"}, 4: {"", ""}, 5: {"00:00", "02:00"}}
	for d, w := range want {
		entries := tv.calc().dayEntries(calDate(2026, time.November, d))
		if len(entries) != 1 {
			t.Fatalf("2026-11-%02d lists %d entries, want 1", d, len(entries))
		}
		if got := (shape{entries[0].clock(), entries[0].end}); got != w {
			t.Errorf("2026-11-%02d entry = %+v, want %+v", d, got, w)
		}
	}
	if got := calLabelsOn(tv, 2026, time.November, 4); !reflect.DeepEqual(got, []string{"◆Shift"}) {
		t.Errorf("2026-11-04 lists %q, want the event whole", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 5); !reflect.DeepEqual(got, []string{"◆Shift"}) {
		t.Errorf("2026-11-05 lists %q", got)
	}
}

func TestCalendarNeverWritesNotes(t *testing.T) {
	notes := map[string]string{
		"work.md":  "- [ ] Report ^aaaaaa\n",
		"Plans.md": "- Standup when:2026-11-02T09:00-09:15 @repeat:weekdays\n- Write task:[[work#^aaaaaa]] when:2026-11-03T10:00\n",
	}
	a, root := newFeatureTestApp(t)
	for name, body := range notes {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	msg := a.loadTasksCmd()().(tasksLoadedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	a.idx.tasks, a.idx.events = msg.tasks, msg.events
	a, tv := calOpen(t, a)
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		calToSub(t, a, tv, sub)
		for d := 2; d <= 10; d++ {
			tv.selectDay(a, calDate(2026, time.November, d))
			calScreen(a, tv, 160, 40)
		}
	}
	for name, body := range notes {
		if got := calReadFile(t, root, name); got != body {
			t.Errorf("%s changed:\n%s", name, got)
		}
	}
	if got := calLabelsOn(tv, 2026, time.November, 10); len(got) != 1 {
		t.Errorf("2026-11-10 lists %q, want the weekday standup", got)
	}
}

func TestCalendarShowsNoEventsOnRemoteVault(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.idx.tasks = vault.ParseTasks("Plans.md", "Plans", vault.FolderInbox, "- [ ] Report due:2026-11-03\n", vault.ParseTasksOptions{Dialect: vault.DialectApp})
	a.idx.events = loadEvents(stubBackend{})
	a, tv := calOpen(t, a)
	calToSub(t, a, tv, calMonth)
	if got := calLabelsOn(tv, 2026, time.November, 3); !reflect.DeepEqual(got, []string{"Report"}) {
		t.Errorf("2026-11-03 lists %q, want the due task alone", got)
	}
	if len(a.idx.events) != 0 {
		t.Errorf("a remote vault gave %d events", len(a.idx.events))
	}
}

func TestProseWhenIsNoCalendarEvent(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{"Plans.md": "- ask them when: tomorrow\n- meeting when:soon\n- see `when:2026-11-03`\n"})
	for d := 1; d <= 30; d++ {
		if got := calLabelsOn(tv, 2026, time.November, d); len(got) != 0 {
			t.Errorf("2026-11-%02d lists %q, want no event", d, got)
		}
	}
}

func TestMonthViewEventFields(t *testing.T) {
	_, tv := calEventApp(t, map[string]string{
		"Plans.md": "- Lecture when:2026-11-02T09:00-10:30 @repeat:weekly @until:2026-11-23 @skip:2026-11-16\n" +
			"- Class when:2026-11-02T11:00 repeat:1w\n",
	})
	if got := calDaysListing(tv, 2026, time.November, "Lecture"); !reflect.DeepEqual(got, []int{2, 9, 23}) {
		t.Errorf("Lecture is listed on days %v, want [2 9 23]", got)
	}
	if got := calLabelsOn(tv, 2026, time.November, 2); !reflect.DeepEqual(got, []string{"◆09:00 Lecture", "◆11:00 Class repeat:1w"}) {
		t.Errorf("2026-11-02 lists %q", got)
	}
	if got := calDaysListing(tv, 2026, time.November, "Class"); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("Class is listed on days %v, want [2]", got)
	}
}
