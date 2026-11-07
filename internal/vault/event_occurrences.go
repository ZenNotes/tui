package vault

import (
	"strings"
	"time"
)

// EventOccurrence is one time an event takes place. EndDate and the times
// are empty when the event has none, as in Event.
type EventOccurrence struct {
	StartDate string
	StartTime string
	EndDate   string
	EndTime   string
}

// EventOccurrences lists each occurrence of an event that overlaps the days
// from to to, both included, in ascending order of start. The first date of
// the event always counts. Later dates come from the repeat rule, counted
// from the first date. An occurrence whose start date is in a skip period is
// dropped. An occurrence that starts after the until date is not shown, but
// one that starts on or before it is shown whole. Each occurrence keeps the
// times and the length in days of the event.
//
// names gives the skip periods of an `@name` item of the event. It can be nil,
// and then these items skip nothing.
//
// The walk starts near from, so the cost depends on the size of the window
// and not on the age of the event. The function reads only its arguments.
func EventOccurrences(e Event, from, to string, weekStart time.Weekday, names func(name string) []SkipPeriod) []EventOccurrence {
	start, err := time.Parse(isoDay, e.StartDate)
	if err != nil || !IsValidISODate(from) || !IsValidISODate(to) {
		return nil
	}
	length := 0
	if e.EndDate != "" {
		if end, err := time.Parse(isoDay, e.EndDate); err == nil && !end.Before(start) {
			length = int(end.Sub(start).Hours() / 24)
		}
	}
	// An occurrence overlaps the window when it starts on or before to and
	// ends on or after from, so its start is not before earliest.
	earliest := shiftISODate(from, -length)
	var out []EventOccurrence
	add := func(date string) {
		if date < earliest || date > to {
			return
		}
		out = append(out, EventOccurrence{
			StartDate: date,
			StartTime: e.StartTime,
			EndDate:   endDateAfter(e, date, length),
			EndTime:   e.EndTime,
		})
	}
	add(e.StartDate)
	rule, ok := ParseRepeatRule(e.Repeat)
	if !ok || e.Repeat == "" {
		return out
	}
	skips := eventSkipPeriods(e, names)
	target, _ := time.Parse(isoDay, earliest)
	first := 0
	if target.After(start) {
		first = periodNear(rule, start, target)
	}
	eachRuleDate(rule, start, weekStart, first, func(date string) bool {
		if date > to || (e.Until != "" && date > e.Until) {
			return false
		}
		if date > e.StartDate && !inSkipPeriod(date, skips) {
			add(date)
		}
		return true
	})
	return out
}

// endDateAfter gives the end date of the occurrence that starts on date. It
// is empty when the event has no end date.
func endDateAfter(e Event, date string, length int) string {
	if e.EndDate == "" {
		return ""
	}
	return shiftISODate(date, length)
}

// eventSkipPeriods gathers the skip periods of an event: the date items it
// writes and the periods that names gives for its `@name` items.
func eventSkipPeriods(e Event, names func(name string) []SkipPeriod) []SkipPeriod {
	var out []SkipPeriod
	for _, item := range e.Skip {
		_, periods := ParseSkipItems(item)
		out = append(out, periods...)
	}
	if names != nil {
		for _, name := range e.SkipNames {
			out = append(out, names(strings.TrimSpace(name))...)
		}
	}
	return out
}
