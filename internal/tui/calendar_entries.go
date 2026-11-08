package tui

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// calEntryKind tells what a calendar entry is.
type calEntryKind int

const (
	// calDue is a task placed on the day of its due date.
	calDue calEntryKind = iota
	// calEvent is one occurrence of an event.
	calEvent
	// calSession is one occurrence of a work session of a task.
	calSession
)

// calEntry is one item on a calendar day: a task by its due date, an event, or
// a work session.
type calEntry struct {
	// task is the task of a due entry. For a session it is the task that the
	// session names, and it is empty when the link dangles.
	task vault.Task
	kind calEntryKind
	// mark leads the label of an event or a session. A due entry has none.
	mark string
	// done says that the entry is drawn in the done style.
	done bool
	// text is the own text of an event or a session.
	text string
	// start and end are the times of day of the block of an event or a session
	// on this day. An end of 24:00 is the end of the day. An entry with no
	// start covers the day whole.
	start, end string
	// continued says that the block carries on from the day before, so its
	// start is midnight and not a time the user wrote.
	continued bool
	// path and line say where an event or a session is written. The line is
	// zero-based. inline is true for an event that is one list line, and false
	// for an event that is a whole note.
	path   string
	line   int
	inline bool
	// when is the `when` value of the event as written.
	when string
	// occ is the whole occurrence of an event or a session, with the dates and
	// times that the user wrote, not the part that falls on this day.
	occ vault.EventOccurrence
	// repeat and until are the repeat rule of the event, and its last date.
	repeat, until string
	// note is the title of the note that holds the event or the session.
	note string
	// link is the `task:` link of a session as written.
	link string
}

// hasTask reports whether the entry stands for a task that a key or a click
// can act on. An event and a dangling session do not.
func (e calEntry) hasTask() bool {
	return e.kind == calDue || (e.kind == calSession && e.task.SourcePath != "")
}

// clock is the entry's time of day as `HH:MM`, or "" when it is untimed. A due
// entry shows the due time of its task.
func (e calEntry) clock() string {
	if e.kind == calDue {
		return e.task.DueTime
	}
	return e.start
}

// content is the text of the entry without its mark and time.
func (e calEntry) content() string {
	if e.kind == calDue {
		return e.task.Content
	}
	return e.text
}

// label is the entry's text in the calendar: its mark and its text, with the
// time of day between them when it has one, in the `time_format` the user
// chose.
func (e calEntry) label(timeFormat string) string {
	if c := e.clock(); c != "" && !e.continued {
		return e.mark + formatTaskClock(c, timeFormat) + " " + e.content()
	}
	return e.mark + e.content()
}

// calPriorityRank orders the strip: high, medium, low, then none.
var calPriorityRank = map[string]int{"high": 0, "med": 1, "low": 2}

func calRank(priority string) int {
	if r, ok := calPriorityRank[priority]; ok {
		return r
	}
	return len(calPriorityRank)
}

// bucketCalendarEntries places open tasks on the day of their due date. Each
// day keeps the input order, except that timed entries move ahead of the
// untimed ones, earliest first, ties keeping that order. Tasks with no due date
// are returned as undated, ordered by priority and then by lower-cased text,
// ties keeping the input order. Closed, cancelled and forwarded tasks are left
// out.
func bucketCalendarEntries(tasks []vault.Task) (map[string][]calEntry, []vault.Task) {
	byDay := map[string][]calEntry{}
	var undated []vault.Task
	for _, t := range tasks {
		if !vault.IsTaskOpen(t) || t.Forwarded {
			continue
		}
		if t.Due == "" {
			undated = append(undated, t)
			continue
		}
		byDay[t.Due] = append(byDay[t.Due], calEntry{task: t})
	}
	for _, entries := range byDay {
		sort.SliceStable(entries, func(i, j int) bool {
			ci, cj := entries[i].clock(), entries[j].clock()
			if (ci == "") != (cj == "") {
				return ci != ""
			}
			return ci < cj
		})
	}
	sort.SliceStable(undated, func(i, j int) bool {
		ri, rj := calRank(undated[i].Priority), calRank(undated[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(undated[i].Content) < strings.ToLower(undated[j].Content)
	})
	return byDay, undated
}
