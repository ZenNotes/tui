package tui

import (
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// createKey runs `W` and `N` in the calendar. `W` writes a work session for
// the selected task. With the cursor on an empty slot, it asks for the task
// first. `N` writes an event that has no task. It reports whether the key was
// one of these.
func (v *tasksView) createKey(a *App, k vim.Key) bool {
	switch {
	case k.IsRune('N'):
		v.newEvent(a)
	case k.IsRune('W'):
		v.scheduleKey(a)
	default:
		return false
	}
	return true
}

// cursorPrefill is the answer that the question about a time starts with. In
// the week and day views it is the slot of the cursor for one hour. In the
// month view it is the selected day.
func (v *tasksView) cursorPrefill() string {
	c := v.calc()
	if c.sub == calMonth {
		return v.day.Format(calISODay)
	}
	minute := c.curRow() * c.hourStep()
	return hourPrefill(time.Date(v.day.Year(), v.day.Month(), v.day.Day(), minute/60, minute%60, 0, 0, v.day.Location()))
}

// scheduleKey writes a work session for the selected task, or for a task that
// the developer picks when the cursor is on a slot with no entry.
func (v *tasksView) scheduleKey(a *App) {
	if t := v.selectedTask(); t != nil {
		a.scheduleSession(*t)
		return
	}
	c := v.calc()
	if _, ok := c.selectedEvent(v.day); ok {
		a.notify("An event has no task for a work session")
		return
	}
	if c.focus != calFocusGrid {
		return
	}
	if _, ok := a.backend.(*backend.Local); !ok {
		a.notify(localVaultRefusal)
		return
	}
	a.pickTaskToSchedule(v.cursorPrefill())
}

// pickTaskToSchedule opens the list of open tasks. The task that is chosen
// goes to the question about the time, which starts with initial.
func (a *App) pickTaskToSchedule(initial string) {
	var items []paletteItem
	for _, t := range a.idx.tasks {
		if !vault.IsTaskOpen(t) || t.Forwarded {
			continue
		}
		detail := t.NoteTitle
		if t.Due != "" {
			detail += " · " + t.Due
		}
		items = append(items, paletteItem{label: t.Content, detail: detail, id: t.ID, data: t})
	}
	if len(items) == 0 {
		a.notify("There is no open task for a work session")
		return
	}
	a.overlay = &palette{title: "Select a task", placeholder: "Task", items: items, filtered: items,
		onSelect: func(a *App, it paletteItem) {
			a.scheduleSessionAt(it.data.(vault.Task), initial)
		}}
}

// newEvent asks for the text, then the time, of an event with no task. Then it
// asks for the category note that receives the line.
func (v *tasksView) newEvent(a *App) {
	local, ok := a.backend.(*backend.Local)
	if !ok {
		a.notify(localVaultRefusal)
		return
	}
	a.eventTextPrompt(local.Vault(), v.cursorPrefill(), "")
}

// eventTextPrompt asks for the text of a new event.
func (a *App) eventTextPrompt(v *vault.Vault, when, initial string) {
	a.promptFor("New event", initial, "Text of the event", func(a *App, text string) {
		text = strings.TrimSpace(text)
		problem := ""
		switch {
		case text == "":
			problem = "Enter the text of the event"
		case strings.Contains(strings.ToLower(text), "when:"):
			problem = "Write the time in the next prompt. Do not write it in the text"
		}
		if problem != "" {
			a.eventTextPrompt(v, when, text)
			a.notifyError(problem)
			return
		}
		a.eventWhenAsk(v, text, when)
	})
}

// eventWhenAsk asks for the time of a new event. A wrong answer keeps the
// prompt open with the text and the error.
func (a *App) eventWhenAsk(v *vault.Vault, text, initial string) {
	a.promptFor("Time of "+truncateCells(text, 30), initial, whenHelp, func(a *App, answer string) {
		when, err := parseSessionWhen(strings.TrimSpace(answer), a.taskClock())
		if err != nil {
			a.eventWhenAsk(v, text, answer)
			a.notifyError(err.Error())
			return
		}
		a.pickCategoryNote(v, eventWrite(text, when))
	})
}
