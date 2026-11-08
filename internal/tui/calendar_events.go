package tui

import (
	"sort"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

const calISODay = "2006-01-02"

// calSources holds what the calendar needs to place events and sessions: the
// tasks that sessions name, the events, and the first day of the week. It
// keeps the entries of one window of days, and builds the window again when a
// day outside of it is asked for.
type calSources struct {
	tasks     []vault.Task
	events    []vault.Event
	weekStart time.Weekday

	built    bool
	from, to string
	byDay    map[string][]calEntry
}

// setSources gives the calendar the tasks and the events to place, and drops
// the entries that it built before.
func (c *taskCalendar) setSources(tasks []vault.Task, events []vault.Event, weekStart time.Weekday) {
	c.src = calSources{tasks: tasks, events: events, weekStart: weekStart}
}

// extraEntries gives the event and session entries of a day. A month view
// shows days of the months next to it, so the window covers the month of the
// day and one month on each side of it.
func (c *taskCalendar) extraEntries(day time.Time) []calEntry {
	s := &c.src
	if len(s.events) == 0 {
		return nil
	}
	key := day.Format(calISODay)
	if !s.built || key < s.from || key > s.to {
		first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
		s.from = first.AddDate(0, -1, 0).Format(calISODay)
		s.to = first.AddDate(0, 2, -1).Format(calISODay)
		s.byDay = buildEventEntries(s.tasks, s.events, s.from, s.to, s.weekStart)
		s.built = true
	}
	return s.byDay[key]
}

// dayStored gives the entries of a day in the order that a month lists them:
// timed entries first, earliest first, and then the untimed ones. Due entries
// come before events and sessions that start at the same time.
func (c *taskCalendar) dayStored(day time.Time) []calEntry {
	stored := c.byDay[day.Format(calISODay)]
	extra := c.extraEntries(day)
	if len(extra) == 0 {
		return stored
	}
	out := make([]calEntry, 0, len(stored)+len(extra))
	out = append(out, stored...)
	out = append(out, extra...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i].clock(), out[j].clock()
		if (ci == "") != (cj == "") {
			return ci != ""
		}
		return ci < cj
	})
	return out
}

// buildEventEntries places each occurrence of each event on the days from
// from to to that it covers. An event with a `task:` link is a session, and it
// takes the mark of the state of its task.
func buildEventEntries(tasks []vault.Task, events []vault.Event, from, to string, weekStart time.Weekday) map[string][]calEntry {
	out := map[string][]calEntry{}
	names := vault.NamedSkips(events, weekStart, from, to)
	for _, e := range events {
		var task vault.Task
		linked := false
		if e.TaskLink != "" {
			task, linked = vault.ResolveSession(tasks, e)
		}
		for _, occ := range vault.EventOccurrences(e, from, to, weekStart, names) {
			last := occ.StartDate
			if occ.EndDate > last {
				last = occ.EndDate
			}
			for day := occ.StartDate; day <= last; day = nextISODay(day) {
				if day < from || day > to {
					continue
				}
				entry := calEntry{kind: calEvent, mark: "◆", text: e.Text}
				if e.TaskLink != "" {
					entry.kind = calSession
					entry.mark, entry.done = sessionMark(task, linked, occ.StartDate)
					entry.task = task
					switch {
					case e.Text != "":
					case linked:
						entry.text = task.Content
					default:
						entry.text = e.TaskLink
					}
				}
				entry.start, entry.end, entry.continued = dayBlock(occ, day, last)
				out[day] = append(out[day], entry)
			}
		}
	}
	return out
}

// dayBlock gives the times of the block of an occurrence on one day. A day
// strictly inside a timed event across days is covered whole and has no
// times. The first day runs from the start to the end of the day, and the
// last day from midnight to the end time, when the event has one.
func dayBlock(occ vault.EventOccurrence, day, last string) (start, end string, continued bool) {
	if occ.StartTime == "" {
		return "", "", false
	}
	switch {
	case last == occ.StartDate:
		return occ.StartTime, occ.EndTime, false
	case day == occ.StartDate:
		return occ.StartTime, "24:00", false
	case day == last && occ.EndTime != "":
		return "00:00", occ.EndTime, true
	}
	return "", "", false
}

// nextISODay gives the day after an `YYYY-MM-DD` day.
func nextISODay(day string) string {
	t, err := time.Parse(calISODay, day)
	if err != nil {
		return "9999-12-31"
	}
	return t.AddDate(0, 0, 1).Format(calISODay)
}

// sessionMark gives the mark of a session and whether it is drawn in the done
// style. The mark shows the state of the task: `☐` open, `☑` done, `◩` in
// progress, `☒` cancelled, `→` forwarded, and `?` when the link names no task.
// A session of a task note that repeats is done when it starts on or before
// the day that the task note was last completed.
func sessionMark(t vault.Task, linked bool, date string) (string, bool) {
	switch {
	case !linked:
		return "?", false
	case t.Forwarded:
		return "→", false
	case t.Cancelled:
		return "☒", false
	case t.Checked, t.Kind == "file" && t.LastCompleted != "" && date <= t.LastCompleted:
		return "☑", true
	case t.InProgress:
		return "◩", false
	}
	return "☐", false
}
