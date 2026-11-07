package vault

import "strings"

// SkipPeriod is an inclusive range of days, as `YYYY-MM-DD` dates, that a
// repeating task passes over when it looks for its next occurrence. A single
// day has the same Start and End.
type SkipPeriod struct {
	Start, End string
}

// ParseSkipItems reads the comma-separated value of a `skip:` token. An item
// is a day (`2027-03-13`), an inclusive range (`2027-03-13..2027-03-21`) or an
// `@name`. It returns the valid date items in their written form, the periods
// they cover, which is what the next-occurrence computation takes, and the
// names of the `@name` items without the `@`. An item that is not a real day,
// a reversed range, an empty `@` and anything unrecognised are ignored one at
// a time, so one bad item never hides the others.
func ParseSkipItems(value string) (items []string, periods []SkipPeriod, names []string) {
	for _, raw := range strings.Split(value, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if name, isName := strings.CutPrefix(item, "@"); isName {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
			continue
		}
		period, ok := parseSkipItem(item)
		if !ok {
			continue
		}
		items = append(items, item)
		periods = append(periods, period)
	}
	return items, periods, names
}

func parseSkipItem(item string) (SkipPeriod, bool) {
	start, end, isRange := strings.Cut(item, "..")
	if !isRange {
		end = start
	}
	// Dates are fixed-width ISO strings, so comparing them as text orders them
	// by day.
	if !IsValidISODate(start) || !IsValidISODate(end) || end < start {
		return SkipPeriod{}, false
	}
	return SkipPeriod{Start: start, End: end}, true
}
