package tui

import (
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

const (
	repeatingEventRefusal = "Repeating event: edit it in its note (Enter)"
	eventTaskKeyRefusal   = "Events have no completion, priority or status"
)

// selectedEvent gives the event or session that the cursor selects in the
// hour grid or in the items of the day. A due entry is not an event.
func (c *taskCalendar) selectedEvent(day time.Time) (calEntry, bool) {
	entries := c.dayEntries(day)
	i := -1
	switch c.focus {
	case calFocusGrid:
		if g, ok := c.gridEntry(day); ok {
			i = g
		}
	case calFocusItems:
		i = c.cursor
	}
	if i < 0 || i >= len(entries) || entries[i].kind == calDue {
		return calEntry{}, false
	}
	return entries[i], true
}

// eventKey runs a key on the selected event or session. `T` moves it to
// today, `@` opens the menu of days, `d` edits its time and `D` deletes it.
// On an event, the keys of a task say that an event has none of those. On a
// session, all other keys are left to the task. It reports whether the key
// did anything.
func (v *tasksView) eventKey(a *App, k vim.Key) bool {
	e, ok := v.calc().selectedEvent(v.day)
	if !ok {
		return false
	}
	switch {
	case k.IsRune('T'), k.IsRune('@'), k.IsRune('d'), k.IsRune('D'):
		if !a.eventEditable(e) {
			return true
		}
		switch {
		case k.IsRune('T'):
			a.moveEventTo(e, vault.TodayISO(calendarNow()))
		case k.IsRune('@'):
			v.eventDayMenu(a, e)
		case k.IsRune('d'):
			a.eventWhenPrompt(e, e.when)
		default:
			a.deleteEvent(e)
		}
	case e.kind == calEvent && eventTaskKey(a, k):
		a.notify(eventTaskKeyRefusal)
	default:
		return false
	}
	return true
}

// eventTaskKey reports whether k is a key that completes, cancels, ranks or
// changes the status of a task.
func eventTaskKey(a *App, k vim.Key) bool {
	if a.bound(k, "nav.toggleTask") {
		return true
	}
	for _, r := range "c/pws<>" {
		if k.IsRune(r) {
			return true
		}
	}
	return false
}

// eventEditable tells whether a key may change the event. A remote vault
// shows no events, and an event that repeats is edited in its note.
func (a *App) eventEditable(e calEntry) bool {
	if _, ok := a.backend.(*backend.Local); !ok {
		a.notify("Events need a local vault")
		return false
	}
	if e.repeat != "" {
		a.notify(repeatingEventRefusal)
		return false
	}
	return true
}

// eventDayMenu offers the days that an event can move to at once. It has no
// entry that clears the date, because an event always has one.
func (v *tasksView) eventDayMenu(a *App, e calEntry) {
	now := calendarNow()
	move := func(d time.Time) func(a *App) {
		return func(a *App) { a.moveEventTo(e, vault.TodayISO(d)) }
	}
	a.showMenu("Move event", []menuItem{
		{label: "Today (" + vault.TodayISO(now) + ")", run: move(now)},
		{label: "Tomorrow (" + vault.TodayISO(now.AddDate(0, 0, 1)) + ")", run: move(now.AddDate(0, 0, 1))},
		{label: "Next week (" + vault.TodayISO(now.AddDate(0, 0, 7)) + ")", run: move(now.AddDate(0, 0, 7))},
		{label: "Selected day (" + vault.TodayISO(v.day) + ")", run: move(v.day)},
		{label: "Other…", run: func(a *App) { a.eventWhenPrompt(e, e.when) }},
	})
}

// moveEventTo moves an event so that it starts on a day. Its times and its
// length stay the same.
func (a *App) moveEventTo(e calEntry, day string) {
	from, errFrom := time.Parse(calISODay, e.occ.StartDate)
	to, errTo := time.Parse(calISODay, day)
	if errFrom != nil || errTo != nil {
		a.notifyError("The event has no valid date")
		return
	}
	days := int(to.Sub(from).Hours() / 24)
	if days == 0 {
		a.notify("The event is already on " + day)
		return
	}
	when := vault.ShiftWhenValue(e.when, days)
	if when == e.when {
		a.notifyError("The time of the event is not valid: " + e.when)
		return
	}
	a.writeEventWhen(e, when)
}

// eventWhenPrompt asks for a new time of an event. A wrong answer keeps the
// prompt open with the text and the error.
func (a *App) eventWhenPrompt(e calEntry, initial string) {
	help := "YYYY-MM-DD[THH:MM[-HH:MM]], start/end, today, tomorrow, +3 or mon..sun"
	a.promptFor("Time of the event", initial, help, func(a *App, text string) {
		when, err := parseSessionWhen(strings.TrimSpace(text), a.taskClock())
		if err != nil {
			a.eventWhenPrompt(e, text)
			a.notifyError(err.Error())
			return
		}
		if when != e.when {
			a.writeEventWhen(e, when)
		}
	})
}

// eventAt is the place of an event in its note, as the vault edits it.
func eventAt(e calEntry) vault.Event {
	kind := vault.EventKindNote
	if e.inline {
		kind = vault.EventKindInline
	}
	return vault.Event{Path: e.path, Kind: kind, Line: e.line, When: e.when}
}

// writeEventWhen writes a new `when` value into the note of an event.
func (a *App) writeEventWhen(e calEntry, when string) {
	found := false
	err := a.mutateNote(e.path, func(body string) (string, bool) {
		next, ok := vault.SetEventWhen(body, eventAt(e), when)
		found = ok
		return next, ok && next != body
	})
	switch {
	case err != nil:
		a.notifyError(err.Error())
	case !found:
		a.notifyError("The event is no longer in " + e.path)
	default:
		a.notify("Event now at " + when)
		a.afterTaskChange()
	}
}

// deleteEvent asks, and then removes the line of an inline event or moves an
// event note to the trash.
func (a *App) deleteEvent(e calEntry) {
	title := e.content()
	if title == "" {
		title = e.note
	}
	a.confirm("Delete event \""+title+"\"?", func() {
		if !e.inline {
			a.moveNoteToTrash(e.path, e.note)
			return
		}
		found := false
		err := a.mutateNote(e.path, func(body string) (string, bool) {
			next, ok := vault.DeleteEventLine(body, eventAt(e))
			found = ok
			return next, ok
		})
		switch {
		case err != nil:
			a.notifyError(err.Error())
		case !found:
			a.notifyError("The event is no longer in " + e.path)
		default:
			a.notify("Deleted event " + title)
			a.afterTaskChange()
		}
	})
}
