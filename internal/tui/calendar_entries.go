package tui

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// calEntry is one task placed on a calendar day. scheduled is true when the
// task's scheduled date placed it rather than its due date.
type calEntry struct {
	task      vault.Task
	scheduled bool
}

// clock is the entry's time of day as `HH:MM`, or "" when it is untimed. An
// entry shows the time of the field that placed it. When both dates fall on
// one day the task has a single entry, and its scheduled time wins over the
// due time.
func (e calEntry) clock() string {
	if e.scheduled {
		return e.task.ScheduledTime
	}
	if e.task.Scheduled == e.task.Due && e.task.ScheduledTime != "" {
		return e.task.ScheduledTime
	}
	return e.task.DueTime
}

// label is the entry's text in the calendar: the task's content, led by its
// time of day when it has one, in the `time_format` the user chose.
func (e calEntry) label(timeFormat string) string {
	if c := e.clock(); c != "" {
		return formatTaskClock(c, timeFormat) + " " + e.task.Content
	}
	return e.task.Content
}

// calPriorityRank orders the strip: high, medium, low, then none.
var calPriorityRank = map[string]int{"high": 0, "med": 1, "low": 2}

func calRank(priority string) int {
	if r, ok := calPriorityRank[priority]; ok {
		return r
	}
	return len(calPriorityRank)
}

// bucketCalendarEntries places open tasks on the days of their due and
// scheduled dates. A task whose two dates are the same day appears once, as a
// due entry. On each day due entries come before scheduled ones, and each
// group keeps the input order; timed entries then move ahead of the untimed
// ones, earliest first, ties keeping that order. Tasks with neither date are returned as
// undated, ordered by priority and then by lower-cased text, ties keeping
// the input order. Closed, cancelled and forwarded tasks are left out.
func bucketCalendarEntries(tasks []vault.Task) (map[string][]calEntry, []vault.Task) {
	due := map[string][]calEntry{}
	sched := map[string][]calEntry{}
	var undated []vault.Task
	for _, t := range tasks {
		if !vault.IsTaskOpen(t) || t.Forwarded {
			continue
		}
		if t.Due == "" && t.Scheduled == "" {
			undated = append(undated, t)
			continue
		}
		if t.Due != "" {
			due[t.Due] = append(due[t.Due], calEntry{task: t})
		}
		if t.Scheduled != "" && t.Scheduled != t.Due {
			sched[t.Scheduled] = append(sched[t.Scheduled], calEntry{task: t, scheduled: true})
		}
	}
	byDay := make(map[string][]calEntry, len(due)+len(sched))
	for day, entries := range due {
		byDay[day] = append(byDay[day], entries...)
	}
	for day, entries := range sched {
		byDay[day] = append(byDay[day], entries...)
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
