package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

const (
	calCreateWork  = "# Work\n\n- [ ] Write report due:2026-11-04T09:00\n- [ ] Errand due:2026-11-04\n- [ ] Loose end\n- [x] Finished job due:2026-11-04\n"
	calCreatePlans = "---\npage-type: event-list\n---\n\n# Plans\n"
)

var calCreateID = regexp.MustCompile(`\^([a-z0-9]{6})`)

func calCreateFiles() map[string]string {
	return map[string]string{"Work.md": calCreateWork, "Plans.md": calCreatePlans}
}

// calCreateApp opens the day view of 2026-11-04 over the files, with the
// cursor on the slot at the minute.
func calCreateApp(t *testing.T, files map[string]string, sub calSub, minute int) (*App, *tasksView, string) {
	t.Helper()
	a, tv, root := calEditApp(t, files, sub)
	tv.selectDay(a, calDate(2026, time.November, 4))
	tv.calc().setCursorRow(minute / tv.calc().hourStep())
	return a, tv, root
}

// calAnswerTask picks the task with the text in the open task picker.
func calAnswerTask(t *testing.T, a *App, text string) {
	t.Helper()
	if _, ok := a.overlay.(*palette); !ok {
		t.Fatalf("overlay = %T, want the task picker", a.overlay)
	}
	for _, r := range text {
		a.handleKey(vim.R(r))
	}
	a.handleKey(vim.KeyEnter)
}

func TestScheduleFromCalendarEntryAndPane(t *testing.T) {
	cases := []struct {
		name  string
		focus func(t *testing.T, a *App, tv *tasksView)
		when  string
	}{
		{"timed entry in the grid", func(t *testing.T, a *App, tv *tasksView) {
			tv.calc().setCursorRow(9)
			requireSelectedContent(t, tv, "Write report", "cursor on the 09 slot")
		}, "2026-11-04T10:00-11:00"},
		{"all-day entry", func(t *testing.T, a *App, tv *tasksView) {
			calSelectEntry(t, a, tv, calDate(2026, time.November, 4), "Errand")
		}, "2026-11-05"},
		{"task without a date", func(t *testing.T, a *App, tv *tasksView) {
			c := tv.calc()
			for i, u := range c.undated {
				if u.Content == "Loose end" {
					c.focus, c.strip = calFocusStrip, i
					return
				}
			}
			t.Fatal("Loose end is not in the No-date strip")
		}, "2026-11-06"},
	}
	for _, c := range cases {
		a, tv, root := calCreateApp(t, calCreateFiles(), calDay, 0)
		c.focus(t, a, tv)
		task := tv.selectedTask()
		if task == nil {
			t.Fatalf("%s: no task selected", c.name)
		}
		calPress(a, tv, vim.R('W'))
		schedPrompt(t, a)
		answer(t, a, c.when)
		pickLabel(t, a, "Plans")
		work := calEditNote(t, root, "Work.md")
		id := calCreateID.FindStringSubmatch(work)
		if id == nil {
			t.Fatalf("%s: Work.md has no block id:\n%s", c.name, work)
		}
		want := calCreatePlans + "- " + task.Content + " task:[[Work#^" + id[1] + "]] when:" + c.when + "\n"
		if got := calEditNote(t, root, "Plans.md"); got != want {
			t.Errorf("%s: Plans.md:\n%s\nwant:\n%s", c.name, got, want)
		}
	}
}

func TestScheduleFromEmptySlotPicksTask(t *testing.T) {
	cases := []struct {
		name   string
		sub    calSub
		zoom   bool
		minute int
		prefix string
		want   string
	}{
		{"hourly", calDay, false, 15 * 60, "2026-11-04T15:00-16:00", "2026-11-04T15:00-16:00"},
		{"half-hourly", calWeek, true, 15*60 + 30, "2026-11-04T15:30-16:30", "2026-11-04T15:30-16:30"},
		{"across midnight", calDay, true, 23*60 + 30, "2026-11-04T23:30/2026-11-05T00:30", "2026-11-04T23:30/2026-11-05T00:30"},
	}
	for _, c := range cases {
		a, tv, root := calCreateApp(t, calCreateFiles(), c.sub, 0)
		if c.zoom {
			calPress(a, tv, vim.R('z'))
		}
		tv.calc().setCursorRow(c.minute / tv.calc().hourStep())
		requireSelectedContent(t, tv, "", c.name+": empty slot")
		calPress(a, tv, vim.R('W'))
		p, ok := a.overlay.(*palette)
		if !ok {
			t.Fatalf("%s: overlay = %T, want the task picker", c.name, a.overlay)
		}
		var labels []string
		for _, it := range p.items {
			labels = append(labels, it.label)
		}
		got := strings.Join(labels, "|")
		if strings.Contains(got, "Finished job") || !strings.Contains(got, "Write report") || !strings.Contains(got, "Loose end") {
			t.Errorf("%s: picker lists %q, want the open tasks only", c.name, got)
		}
		calAnswerTask(t, a, "Loose")
		if got := schedPrompt(t, a).input.String(); got != c.prefix {
			t.Errorf("%s: prompt prefill = %q, want %q", c.name, got, c.prefix)
		}
		answer(t, a, c.want)
		pickLabel(t, a, "Plans")
		id := calCreateID.FindStringSubmatch(calEditNote(t, root, "Work.md"))
		if id == nil {
			t.Fatalf("%s: Work.md has no block id", c.name)
		}
		want := calCreatePlans + "- Loose end task:[[Work#^" + id[1] + "]] when:" + c.want + "\n"
		if got := calEditNote(t, root, "Plans.md"); got != want {
			t.Errorf("%s: Plans.md:\n%s\nwant:\n%s", c.name, got, want)
		}
	}
}

func TestScheduleFromEmptySlotAsksNothingWhenCancelled(t *testing.T) {
	a, tv, root := calCreateApp(t, calCreateFiles(), calDay, 15*60)
	calPress(a, tv, vim.R('W'))
	a.handleKey(vim.Key{Name: "esc"})
	if a.overlay != nil {
		t.Fatalf("overlay = %T after Esc", a.overlay)
	}
	if got := calEditNote(t, root, "Work.md"); got != calCreateWork {
		t.Errorf("Work.md changed:\n%s", got)
	}
}

func TestNewEventFromSlot(t *testing.T) {
	cases := []struct{ answer, want string }{
		{"2026-11-04T12:00-13:00", "2026-11-04T12:00-13:00"},
		{"2026-11-04T22:00/2026-11-05T02:00", "2026-11-04T22:00/2026-11-05T02:00"},
		{"2026-11-07", "2026-11-07"},
	}
	for _, c := range cases {
		a, tv, root := calCreateApp(t, calCreateFiles(), calDay, 12*60)
		a.handleKey(vim.R('N'))
		p := schedPrompt(t, a)
		if p.input.String() != "" {
			t.Errorf("title prompt prefill = %q", p.input.String())
		}
		answer(t, a, "Lunch")
		if got := schedPrompt(t, a).input.String(); got != "2026-11-04T12:00-13:00" {
			t.Errorf("time prompt prefill = %q, want the slot for one hour", got)
		}
		answer(t, a, c.answer)
		pickLabel(t, a, "Plans")
		want := calCreatePlans + "- Lunch when:" + c.want + "\n"
		if got := calEditNote(t, root, "Plans.md"); got != want {
			t.Errorf("answer %q: Plans.md:\n%s\nwant:\n%s", c.answer, got, want)
		}
		if got := calEditNote(t, root, "Work.md"); got != calCreateWork {
			t.Errorf("answer %q: Work.md changed:\n%s", c.answer, got)
		}
		_ = tv
	}
}

func TestNewEventInNewCategoryNoteAndMonthView(t *testing.T) {
	a, tv, root := calCreateApp(t, map[string]string{"Work.md": calCreateWork}, calMonth, 0)
	calPress(a, tv, vim.R('N'))
	answer(t, a, "Trip")
	if got := schedPrompt(t, a).input.String(); got != "2026-11-04" {
		t.Errorf("month view prefill = %q, want the day", got)
	}
	answer(t, a, "2026-11-04")
	pickLabel(t, a, "New category note…")
	answer(t, a, "Travel")
	want := "---\npage-type: event-list\n---\n\n# Travel\n\n- Trip when:2026-11-04\n"
	found := false
	for _, name := range []string{"inbox/Travel.md", "Travel.md"} {
		if got := readRollNoteIfAny(root, name); got != "" {
			found = true
			if got != want {
				t.Errorf("%s:\n%q\nwant:\n%q", name, got, want)
			}
		}
	}
	if !found {
		t.Error("no category note was made")
	}
}

func TestNewEventAsksAgainOnBadAnswers(t *testing.T) {
	a, tv, root := calCreateApp(t, calCreateFiles(), calDay, 12*60)
	calPress(a, tv, vim.R('N'))
	answer(t, a, "   ")
	if _, ok := a.overlay.(*prompt); !ok || !a.messageErr {
		t.Fatal("an empty title did not keep the prompt open with an error")
	}
	answer(t, a, "Lunch when:2026-11-04")
	if _, ok := a.overlay.(*prompt); !ok || !a.messageErr {
		t.Fatal("a text with a time did not keep the prompt open with an error")
	}
	answer(t, a, "Lunch")
	answer(t, a, "soon")
	if _, ok := a.overlay.(*prompt); !ok || !a.messageErr {
		t.Fatal("a bad time did not keep the prompt open with an error")
	}
	if got := schedPrompt(t, a).input.String(); got != "soon" {
		t.Errorf("prompt text = %q, want the answer kept", got)
	}
	if got := calEditNote(t, root, "Plans.md"); got != calCreatePlans {
		t.Errorf("Plans.md changed:\n%s", got)
	}
}

func TestNewEventRefusesOnRemoteVault(t *testing.T) {
	for _, key := range []rune{'N', 'W'} {
		a, tv, _ := calCreateApp(t, calCreateFiles(), calDay, 15*60)
		a.backend = stubBackend{}
		calPress(a, tv, vim.R(key))
		if a.overlay != nil {
			t.Errorf("%c: an overlay opened: %T", key, a.overlay)
		}
		if a.message != "Events need a local vault" {
			t.Errorf("%c: message = %q", key, a.message)
		}
	}
}

func TestNewEventKeyReachesTheCalendar(t *testing.T) {
	for _, sub := range []calSub{calMonth, calWeek, calDay} {
		a, tv, _ := calCreateApp(t, calCreateFiles(), sub, 12*60)
		a.focus = focusPane
		a.handleKey(vim.R('N'))
		if p, ok := a.overlay.(*prompt); !ok || !strings.Contains(p.title, "event") {
			t.Errorf("layout %v: overlay = %T after N, want the event prompt", sub, a.overlay)
		}
		_ = tv
	}
}

func TestNoWritePathAddsScheduled(t *testing.T) {
	a, tv, root := calCreateApp(t, calCreateFiles(), calDay, 12*60)
	calPress(a, tv, vim.R('N'))
	answer(t, a, "Lunch")
	answer(t, a, "2026-11-04T12:00-13:00")
	pickLabel(t, a, "Plans")
	tv.calc().setCursorRow(15)
	calPress(a, tv, vim.R('W'))
	calAnswerTask(t, a, "Loose")
	answer(t, a, "2026-11-04T15:00-16:00")
	pickLabel(t, a, "Plans")
	all := calEditNote(t, root, "Plans.md") + calEditNote(t, root, "Work.md")
	for _, bad := range []string{"scheduled", "repeat:", "until:", "skip:"} {
		if strings.Contains(all, bad) {
			t.Errorf("the notes hold %q:\n%s", bad, all)
		}
	}
	if !strings.Contains(all, "Lunch when:") || !strings.Contains(all, "Loose end task:") {
		t.Errorf("the notes lack the new lines:\n%s", all)
	}
}

// readRollNoteIfAny gives the text of a note, or "" when it is not there.
func readRollNoteIfAny(root, rel string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestScheduleFromMonthViewStartsWithTheDay(t *testing.T) {
	a, tv, _ := calCreateApp(t, calCreateFiles(), calMonth, 0)
	calPress(a, tv, vim.R('W'))
	calAnswerTask(t, a, "Loose")
	if got := schedPrompt(t, a).input.String(); got != "2026-11-04" {
		t.Errorf("prompt prefill = %q, want the selected day", got)
	}
}

func TestScheduleKeyOnEventSaysItHasNoTask(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	calPress(a, tv, vim.R('W'))
	if a.overlay != nil || a.message == "" {
		t.Errorf("overlay = %T, message = %q", a.overlay, a.message)
	}
	if got := calEditNote(t, root, "Plans.md"); got != calEditPlans {
		t.Errorf("Plans.md changed:\n%s", got)
	}
}
