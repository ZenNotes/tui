package vault

import (
	"strings"
	"time"
)

// The planned day (ZenNotes/zennotes#1119) is the day a task is meant to be
// worked on, apart from the day it is due:
//
//	- [ ] Send the report due:2026-12-04 @scheduled:2026-12-02
//
// A checkbox task carries it as the `@scheduled:` field and a task note as
// frontmatter `scheduled:`. This file is the desktop's task-planning.ts at
// v2.94.0, so zn and the app answer "which day is this task on" the same way:
//
//   - A task sits on its planned day when it has one, on its deadline when it
//     has none.
//   - The deadline still wins once it arrives: a task due today or overdue
//     shows as that, whatever day it was planned for.
//   - A planned day that passed with the task still open is not lateness. The
//     task waits under today, and nothing is written.
//   - Lateness reads the deadline only.
//
// A date a task takes from its daily note (DueInferred) stands in for "the
// day this task is on"; a planned day says that outright, so it replaces the
// note's date and the task is not late by a day it was planned away from.

// PlannedDayField is the key of the planned day: `@scheduled:` on a checkbox
// task, frontmatter `scheduled:` on a task note.
const PlannedDayField = "scheduled"

// ParsePlannedDay is the day a `@scheduled:` value names, or "" for anything
// but a date: such a field stays on the line as an ordinary field and plans
// nothing.
func ParsePlannedDay(raw string) string {
	v := strings.TrimSpace(raw)
	if IsValidISODate(v) {
		return v
	}
	return ""
}

// PlannedDay is the day t is planned for, or "". A task note's plan is its
// frontmatter and a checkbox task's is its field, never the other way round.
// The CLI grammar reports no fields, so a checkbox task scanned with it reads
// its plan off the line itself.
func PlannedDay(t Task) string {
	if t.Kind == "file" {
		return ParsePlannedDay(t.Scheduled)
	}
	if t.Fields != nil {
		return ParsePlannedDay(t.Fields[PlannedDayField])
	}
	if t.RawText == "" {
		return ""
	}
	for _, fm := range inlineFieldRe.FindAllStringSubmatch(t.RawText, -1) {
		if strings.ToLower(fm[1]) == PlannedDayField {
			return ParsePlannedDay(fm[2])
		}
	}
	return ""
}

// TaskDeadline is the date t has to be done by: its `due:`, the note's
// frontmatter `due:`, or the date of the daily note it lives in. The last one
// gives way to a planned day. Lateness reads this and nothing else.
func TaskDeadline(t Task) string {
	if t.Due == "" {
		return ""
	}
	if t.DueInferred && PlannedDay(t) != "" {
		return ""
	}
	return t.Due
}

// TaskDay is the one day t sits on, for every surface that places tasks by
// date: the list's groups, the calendars, the board's date columns and Home.
// todayISO is the user's local day; "" means the task has no day.
func TaskDay(t Task, todayISO string) string {
	planned := PlannedDay(t)
	deadline := TaskDeadline(t)
	if planned == "" {
		return deadline
	}
	// The deadline has arrived or passed: the task is due today or late, and
	// says so, wherever it was planned.
	if deadline != "" && deadline <= todayISO {
		return deadline
	}
	// A missed plan follows the work to today. Only live work does: a done,
	// cancelled or forwarded line is a record, and stays on the day it names.
	if planned < todayISO && !t.Checked && !t.Cancelled && !t.Forwarded {
		return todayISO
	}
	return planned
}

// DeadlineApartFromDay is the deadline to show beside a task that sits on
// another day: its planned day, or today after a missed one. "" when the task
// sits on its deadline or has none.
func DeadlineApartFromDay(t Task, todayISO string) string {
	deadline := TaskDeadline(t)
	if deadline != "" && deadline != TaskDay(t, todayISO) {
		return deadline
	}
	return ""
}

// NudgedDueDate is the due date `<` and `>` give t: days from its deadline,
// or from baseISO when it has none. Counting from the day a planned task sits
// on would move its deadline relative to the plan: due Dec 4, planned Dec 1,
// `>` would write Dec 2 instead of Dec 5.
func NudgedDueDate(t Task, baseISO string, days int) string {
	from := TaskDeadline(t)
	if from == "" {
		from = baseISO
	}
	d, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		d = time.Now()
	}
	return TodayISO(d.AddDate(0, 0, days))
}

// CanPlanTask is false for a task that is done, cancelled or forwarded: it has
// no day left to plan.
func CanPlanTask(t Task) bool {
	return !t.Checked && !t.Cancelled && !t.Forwarded
}
