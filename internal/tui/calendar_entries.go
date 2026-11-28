package tui

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// calEntry is one task placed on a calendar day. scheduled is true when the
// task's planned day placed it rather than its deadline.
type calEntry struct {
	task      vault.Task
	scheduled bool
}

// calPriorityRank orders the strip: high, medium, low, then none.
var calPriorityRank = map[string]int{"high": 0, "med": 1, "low": 2}

func calRank(priority string) int {
	if r, ok := calPriorityRank[priority]; ok {
		return r
	}
	return len(calPriorityRank)
}

// bucketCalendarEntries places each open task on the one day it sits on
// (vault.TaskDay): its planned day when it has one, its deadline otherwise,
// today for a planned day it missed, and its deadline again once that arrives.
// todayISO is the calendar's today. On each day the entries that sit on their
// deadline come before those a plan put there, and each group keeps the input
// order. Tasks with no day are returned as undated, ordered by priority and
// then by lower-cased text, ties keeping the input order. Closed, cancelled
// and forwarded tasks are left out.
func bucketCalendarEntries(tasks []vault.Task, todayISO string) (map[string][]calEntry, []vault.Task) {
	due := map[string][]calEntry{}
	sched := map[string][]calEntry{}
	var undated []vault.Task
	for _, t := range tasks {
		if !vault.IsTaskOpen(t) || t.Forwarded {
			continue
		}
		day := vault.TaskDay(t, todayISO)
		switch {
		case day == "":
			undated = append(undated, t)
		case vault.PlannedDay(t) != "" && day != vault.TaskDeadline(t):
			sched[day] = append(sched[day], calEntry{task: t, scheduled: true})
		default:
			due[day] = append(due[day], calEntry{task: t})
		}
	}
	byDay := make(map[string][]calEntry, len(due)+len(sched))
	for day, entries := range due {
		byDay[day] = append(byDay[day], entries...)
	}
	for day, entries := range sched {
		byDay[day] = append(byDay[day], entries...)
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
