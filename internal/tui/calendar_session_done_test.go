package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

const (
	doneGymNote = "---\npage-type: task\ndue: 2026-11-11T18:00\nrepeat: mon/wed/fri\nlastCompleted: 2026-11-09\n---\n# Gym\n"
	doneGymPlan = "---\npage-type: event-list\n---\n- Workout when:2026-11-09T18:00-19:00 task:[[Gym]]\n" +
		"- Workout when:2026-11-11T18:00-19:00 task:[[Gym]]\n"
	doneBoxNote = "- [x] Water plants ^ab12cd\n"
	doneBoxPlan = "- Water plants when:2026-11-09T09:00-09:15 task:[[Work#^ab12cd]]\n"
)

func doneSessionApp(t *testing.T, sub calSub) (*App, *tasksView, string) {
	t.Helper()
	files := map[string]string{"inbox/Gym.md": doneGymNote, "inbox/Plans.md": doneGymPlan, "inbox/Work.md": doneBoxNote, "inbox/Boxes.md": doneBoxPlan}
	a, tv, root := calEditApp(t, files, sub)
	calFixClock(t, a, time.Date(2026, time.November, 9, 12, 0, 0, 0, time.Local))
	return a, tv, root
}

func TestCompleteAndCancelDoNothingOnDoneSession(t *testing.T) {
	day := calDate(2026, time.November, 9)
	for _, name := range []string{"Workout", "Water plants"} {
		for _, key := range []rune{'x', 'c'} {
			a, tv, root := doneSessionApp(t, calMonth)
			before := map[string]string{}
			for _, n := range []string{"inbox/Gym.md", "inbox/Plans.md", "inbox/Work.md", "inbox/Boxes.md"} {
				before[n] = calEditNote(t, root, n)
			}
			calSelectEntry(t, a, tv, day, name)
			a.toasts = nil
			calPress(a, tv, vim.R(key))
			for n, want := range before {
				if got := calEditNote(t, root, n); got != want {
					t.Errorf("%s %c changed %s:\n%s\nwant:\n%s", name, key, n, got, want)
				}
			}
			if !strings.Contains(a.message, "done") {
				t.Errorf("%s %c: message = %q, want a note that the occurrence is done", name, key, a.message)
			}
		}
	}
}

func TestDoneSessionIsNotCompletedFromTheHourGridOrMenu(t *testing.T) {
	a, tv, root := doneSessionApp(t, calDay)
	before := calEditNote(t, root, "inbox/Gym.md")
	tv.selectDay(a, calDate(2026, time.November, 9))
	tv.calc().setCursorRow(18)
	calPress(a, tv, vim.R('x'))
	if got := calEditNote(t, root, "inbox/Gym.md"); got != before {
		t.Errorf("x in the grid changed Gym.md:\n%s", got)
	}
	e, ok := tv.calc().selectedEvent(tv.day)
	if !ok {
		t.Fatal("no session under the cursor")
	}
	tv.eventMenu(a, e)
	a.overlay.handleKey(a, vim.R('t'))
	a.message = ""
	a.overlay.handleKey(a, vim.R('x'))
	if got := calEditNote(t, root, "inbox/Gym.md"); got != before {
		t.Errorf("the menu changed Gym.md:\n%s", got)
	}
	if !strings.Contains(a.message, "done") {
		t.Errorf("message = %q, want a note that the occurrence is done", a.message)
	}
}
