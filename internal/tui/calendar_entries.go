package tui

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// calEntry is one task placed on a calendar day by its due date.
type calEntry struct {
	task vault.Task
}

// clock is the entry's time of day as `HH:MM`, or "" when it is untimed. An
// entry shows the due time of its task.
func (e calEntry) clock() string {
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
