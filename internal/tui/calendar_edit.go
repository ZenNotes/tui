package tui

import (
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

const (
	repeatingEventRefusal = "Edit a repeating event in its note. Press Enter"
	eventTaskKeyRefusal   = "An event has no state, no priority and no status"
	doneSessionRefusal    = "The task of this work session is done"

	cancelledSessionRefusal = "The task of this work session is cancelled"
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
		v.eventAct(a, e, k.Rune)
	case e.kind == calEvent && eventTaskKey(a, k):
		a.notify(eventTaskKeyRefusal)
	case e.finishedRefusal() != "" && (a.bound(k, "nav.toggleTask") || k.IsRune('c')):
		a.notify(e.finishedRefusal())
	default:
		return false
	}
	return true
}

// eventAct edits an event or a session with the action of a key: `T`, `@`,
// `d` or `D`.
func (v *tasksView) eventAct(a *App, e calEntry, act rune) {
	if !a.eventEditable(e) {
		return
	}
	switch act {
	case 'T':
		a.moveEventTo(e, vault.TodayISO(calendarNow()))
	case '@':
		v.eventDayMenu(a, e)
	case 'd':
		a.eventWhenPrompt(e, e.when)
	default:
		a.deleteEvent(e)
	}
}

// eventMenu opens the menu of an event or a session. It has the actions of
// the keys, and a session also leads to the menu of its task.
func (v *tasksView) eventMenu(a *App, e calEntry) {
	title := e.content()
	if title == "" {
		title = e.note
	}
	open := "Open note"
	if e.hasTask() {
		open = "Open task"
	}
	act := func(r rune) func(a *App) {
		return func(a *App) { v.eventAct(a, e, r) }
	}
	items := []menuItem{
		{key: "o", label: open, run: func(a *App) { a.openCalEntry(e) }},
		{key: "T", label: "Move to today", run: act('T')},
		{key: "@", label: "Move to a day", run: act('@')},
		{key: "d", label: "Edit time", run: act('d')},
		{key: "D", label: "Delete", run: act('D')},
	}
	if e.kind == calSession {
		items = append(items, menuItem{sep: true}, menuItem{key: "t", label: "Task actions", run: func(a *App) { a.taskMenuFor(e.task, e.finishedRefusal()) }})
	}
	a.showMenu(truncateCells(title, 40), items)
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
		a.notifyError("The date of the event is not correct")
		return
	}
	days := int(to.Sub(from).Hours() / 24)
	if days == 0 {
		a.notify("The event is already on " + day)
		return
	}
	when := vault.ShiftWhenValue(e.when, days)
	if when == e.when {
		a.notifyError("The time of the event is not correct: " + e.when)
		return
	}
	a.writeEventWhen(e, when)
}

// eventWhenPrompt asks for a new time of an event. A wrong answer keeps the
// prompt open with the text and the error.
func (a *App) eventWhenPrompt(e calEntry, initial string) {
	a.promptFor("Time of the event", initial, whenHelp, func(a *App, text string) {
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
		a.notify("The event is now at " + when)
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
	noun := "event"
	if e.kind == calSession {
		noun = "work session"
	}
	a.confirm("Delete the "+noun+" \""+title+"\"?", func() {
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
			a.notify("The event is deleted: " + title)
			a.afterTaskChange()
		}
	})
}
