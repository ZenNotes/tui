package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

const (
	calEditPlans = "# Plans\n\n- Gym when:2026-11-04T07:00\n- Dentist when:2026-11-05T14:00-15:30\n- Walk when:2026-11-09\n"
	calEditRefus = "Repeating event: edit it in its note (Enter)"
)

// calEditApp opens the calendar over the note files with today fixed to
// 2026-11-02, so that every key reads the same day on every day of the year.
func calEditApp(t *testing.T, files map[string]string, sub calSub) (*App, *tasksView, string) {
	t.Helper()
	a, tv, root := calCursorApp(t, files, sub, false)
	calFixClock(t, a, time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local))
	return a, tv, root
}

// calSelectEntry puts the item cursor on the entry of the day with the text.
func calSelectEntry(t *testing.T, a *App, tv *tasksView, day time.Time, text string) {
	t.Helper()
	tv.selectDay(a, day)
	c := tv.calc()
	for i, e := range c.dayEntries(day) {
		if e.content() == text {
			c.focus, c.cursor = calFocusItems, i
			return
		}
	}
	t.Fatalf("no entry %q on %s", text, day.Format("2006-01-02"))
}

// calConfirm answers the open confirmation with the key.
func calConfirm(t *testing.T, a *App, key rune) {
	t.Helper()
	if _, ok := a.overlay.(*confirmDialog); !ok {
		t.Fatalf("overlay = %T, want a confirmation", a.overlay)
	}
	a.overlay.handleKey(a, vim.R(key))
}

func calEditNote(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEventTKeyMovesToToday(t *testing.T) {
	cases := []struct{ before, after string }{
		{"- Dentist when:2026-11-05T14:00-15:30", "- Dentist when:2026-11-02T14:00-15:30"},
		{"- Dentist when:2026-11-05/2026-11-07", "- Dentist when:2026-11-02/2026-11-04"},
		{"- Dentist when:2026-11-05T22:00/2026-11-06T02:00", "- Dentist when:2026-11-02T22:00/2026-11-03T02:00"},
	}
	for _, c := range cases {
		note := "# Plans\n\n- Gym when:2026-11-04T07:00\n" + c.before + "\n- Walk when:2026-11-09\n"
		a, tv, root := calEditApp(t, map[string]string{"Plans.md": note}, calMonth)
		calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
		calPress(a, tv, vim.R('T'))
		want := "# Plans\n\n- Gym when:2026-11-04T07:00\n" + c.after + "\n- Walk when:2026-11-09\n"
		if got := calEditNote(t, root, "Plans.md"); got != want {
			t.Errorf("T on %s:\n%s\nwant:\n%s", c.before, got, want)
		}
	}
}

func TestEventTKeyMovesFromTheHourGrid(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calDay)
	tv.selectDay(a, calDate(2026, time.November, 5))
	tv.calc().setCursorRow(14)
	calPress(a, tv, vim.R('T'))
	want := strings.Replace(calEditPlans, "Dentist when:2026-11-05", "Dentist when:2026-11-02", 1)
	if got := calEditNote(t, root, "Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestEventDueMenuHasNoClear(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	calPress(a, tv, vim.R('@'))
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay = %T, want the date menu", a.overlay)
	}
	labels := strings.ToLower(strings.Join(menuLabels(m), "|"))
	for _, want := range []string{"today", "tomorrow", "next week", "2026-11-05", "other"} {
		if !strings.Contains(labels, want) {
			t.Errorf("menu %q lacks %q", labels, want)
		}
	}
	if strings.Contains(labels, "clear") {
		t.Errorf("menu %q offers to clear the date", labels)
	}
	calPick(t, a, m, "tomorrow")
	want := strings.Replace(calEditPlans, "Dentist when:2026-11-05", "Dentist when:2026-11-03", 1)
	if got := calEditNote(t, root, "Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestEventDPromptEditsWhen(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	calPress(a, tv, vim.R('d'))
	p := schedPrompt(t, a)
	if got := p.input.String(); got != "2026-11-05T14:00-15:30" {
		t.Errorf("prompt prefill = %q", got)
	}
	answer(t, a, "2026-11-06T09:00-10:00")
	want := strings.Replace(calEditPlans, "when:2026-11-05T14:00-15:30", "when:2026-11-06T09:00-10:00", 1)
	if got := calEditNote(t, root, "Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestEventDPromptRefusesBadValue(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	calPress(a, tv, vim.R('d'))
	answer(t, a, "soon")
	schedPrompt(t, a)
	if got := calEditNote(t, root, "Plans.md"); got != calEditPlans {
		t.Errorf("Plans.md changed:\n%s", got)
	}
}

func TestEventDeleteAsksFirst(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
	a.focus = focusPane
	a.handleKey(vim.R('D'))
	c, ok := a.overlay.(*confirmDialog)
	if !ok {
		t.Fatalf("overlay = %T, want a confirmation", a.overlay)
	}
	if !strings.Contains(c.message, `Delete event "Dentist"`) {
		t.Errorf("message = %q", c.message)
	}
	if got := calEditNote(t, root, "Plans.md"); got != calEditPlans {
		t.Errorf("Plans.md changed before the answer:\n%s", got)
	}
	a.handleKey(vim.R('n'))
	if got := calEditNote(t, root, "Plans.md"); got != calEditPlans {
		t.Errorf("Plans.md changed after n:\n%s", got)
	}
	a.handleKey(vim.R('D'))
	a.handleKey(vim.R('y'))
	want := strings.Replace(calEditPlans, "- Dentist when:2026-11-05T14:00-15:30\n", "", 1)
	if got := calEditNote(t, root, "Plans.md"); got != want {
		t.Errorf("Plans.md:\n%s\nwant:\n%s", got, want)
	}
}

const calEditEventNote = "---\npage-type: event\nwhen: 2026-11-03T14:00-15:30\n---\n\nBring the card.\n"

func TestEventNoteTKeyRewritesFrontmatter(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Dentist.md": calEditEventNote}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Dentist")
	calPress(a, tv, vim.R('T'))
	want := strings.Replace(calEditEventNote, "2026-11-03", "2026-11-02", 1)
	if got := calEditNote(t, root, "Dentist.md"); got != want {
		t.Errorf("Dentist.md:\n%s\nwant:\n%s", got, want)
	}
}

func TestEventNoteDeleteMovesToTrash(t *testing.T) {
	a, tv, root := calEditApp(t, map[string]string{"Dentist.md": calEditEventNote}, calMonth)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Dentist")
	calPress(a, tv, vim.R('D'))
	if _, ok := a.overlay.(*confirmDialog); !ok {
		t.Fatalf("overlay = %T, want a confirmation", a.overlay)
	}
	calConfirm(t, a, 'y')
	if _, err := os.Stat(filepath.Join(root, "Dentist.md")); !os.IsNotExist(err) {
		t.Errorf("Dentist.md is still in place: %v", err)
	}
}

func TestRepeatingEventRefusesEdits(t *testing.T) {
	files := map[string]string{
		"Plans.md":   "- Gym when:2026-11-03T07:00 @repeat:1w\n",
		"Lecture.md": "---\npage-type: event\nwhen: 2026-11-03T09:00-10:30\nrepeat: 1w\n---\n",
	}
	for _, name := range []string{"Gym", "Lecture"} {
		for _, key := range []rune{'T', '@', 'd', 'D'} {
			a, tv, root := calEditApp(t, files, calMonth)
			calSelectEntry(t, a, tv, calDate(2026, time.November, 10), name)
			calPress(a, tv, vim.R(key))
			if a.overlay != nil {
				t.Errorf("%s %c opened %T", name, key, a.overlay)
			}
			if a.message != calEditRefus {
				t.Errorf("%s %c: message = %q", name, key, a.message)
			}
			for file, body := range files {
				if got := calEditNote(t, root, file); got != body {
					t.Errorf("%s %c changed %s:\n%s", name, key, file, got)
				}
			}
		}
	}
}

const (
	calEditWork  = "intro\n- [ ] Water plants @repeat:mon/thu ^ab12cd\noutro\n"
	calEditSess  = "- Water plants task:[[Work#^ab12cd]] when:2026-11-03T09:00-09:15\n"
	calEditOther = "- [ ] Write report due:2026-11-06 ^zz99yy\n"
	calEditPlan2 = "- Write report task:[[Work#^zz99yy]] when:2026-11-03T14:00-15:30\n"
)

func TestSessionKeysSplitBetweenTaskAndSession(t *testing.T) {
	files := map[string]string{"Work.md": calEditOther, "Plans.md": calEditPlan2}
	a, tv, root := calEditApp(t, files, calDay)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Write report")
	calPress(a, tv, vim.R('T'))
	if got := calEditNote(t, root, "Work.md"); got != calEditOther {
		t.Errorf("T changed the task:\n%s", got)
	}
	want := strings.Replace(calEditPlan2, "2026-11-03", "2026-11-02", 1)
	if got := calEditNote(t, root, "Plans.md"); got != want {
		t.Errorf("T on the session:\n%s\nwant:\n%s", got, want)
	}

	a, tv, root = calEditApp(t, files, calDay)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Write report")
	calPress(a, tv, vim.R('p'), vim.R('h'))
	if _, ok := a.overlay.(*menu); !ok {
		t.Fatalf("p opened %T, want the priority menu of the task", a.overlay)
	}
	if got := calEditNote(t, root, "Plans.md"); got != calEditPlan2 {
		t.Errorf("p changed the session:\n%s", got)
	}

	a, tv, root = calEditApp(t, files, calDay)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Write report")
	calPress(a, tv, vim.R('D'))
	calConfirm(t, a, 'y')
	if got := calEditNote(t, root, "Plans.md"); got != "" {
		t.Errorf("D left the session line:\n%s", got)
	}
	if got := calEditNote(t, root, "Work.md"); got != calEditOther {
		t.Errorf("D changed the task:\n%s", got)
	}
}

func TestSessionOfRepeatingTaskRollsWithX(t *testing.T) {
	files := map[string]string{"Work.md": calEditWork, "Plans.md": calEditSess}
	a, tv, root := calEditApp(t, files, calDay)
	calSelectEntry(t, a, tv, calDate(2026, time.November, 3), "Water plants")
	calPress(a, tv, vim.R('x'))
	work := calEditNote(t, root, "Work.md")
	if !strings.Contains(work, "- [x] Water plants ^ab12cd") || !strings.Contains(work, "- [ ] Water plants @repeat:mon/thu ^") {
		t.Errorf("the task did not roll:\n%s", work)
	}
	if got := calEditNote(t, root, "Plans.md"); strings.Count(got, "task:[[Work#^") != 2 {
		t.Errorf("the session was not copied:\n%s", got)
	}
}

func TestEventRefusesTaskKeys(t *testing.T) {
	for _, key := range []rune{'x', 'c', '/', 'p', 'w', 's', '<', '>'} {
		a, tv, root := calEditApp(t, map[string]string{"Plans.md": calEditPlans}, calMonth)
		calSelectEntry(t, a, tv, calDate(2026, time.November, 5), "Dentist")
		calPress(a, tv, vim.R(key))
		if a.message != "Events have no completion, priority or status" {
			t.Errorf("%c: message = %q", key, a.message)
		}
		if a.overlay != nil {
			t.Errorf("%c opened %T", key, a.overlay)
		}
		if got := calEditNote(t, root, "Plans.md"); got != calEditPlans {
			t.Errorf("%c changed Plans.md:\n%s", key, got)
		}
	}
}
