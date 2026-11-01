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
// group keeps the input order. Tasks with neither date are returned as
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
	sort.SliceStable(undated, func(i, j int) bool {
		ri, rj := calRank(undated[i].Priority), calRank(undated[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(undated[i].Content) < strings.ToLower(undated[j].Content)
	})
	return byDay, undated
}
