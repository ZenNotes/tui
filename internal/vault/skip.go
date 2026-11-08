package vault

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// skipFieldRe and untilFieldRe find the `@skip:` and `@until:` fields of a
	// line. They read a value with the same character class as any other
	// `@key:value` field, so a value stops at the first dot: in
	// `@skip:2027-03-13..2027-03-21` the value is `2027-03-13`. A bare `skip:`
	// or `until:` is no field and is plain text.
	skipFieldRe  = regexp.MustCompile(`(?i)(?:^|\s)@skip:([\p{L}\d][\p{L}\d/_-]*)`)
	untilFieldRe = regexp.MustCompile(`(?i)(?:^|\s)@until:([\p{L}\d][\p{L}\d/_-]*)`)
)

// SkipPeriod is an inclusive range of days, as `YYYY-MM-DD` dates, that a
// repeating task passes over when it looks for its next occurrence. A single
// day has the same Start and End.
type SkipPeriod struct {
	Start, End string
}

// ParseSkipItems reads the value of a `@skip:` field, or one value of a
// `skip` key in frontmatter. An item is a day (`2027-03-13`), an inclusive
// range (`2027-03-13/2027-03-21`) or a name, which starts with a letter. A
// value holds one item. A comma between items is read as a separator, for a
// frontmatter value that lists several. It returns the valid date items in
// their written form, the periods they cover, which is what the
// next-occurrence computation takes, and the names. An item that is not a
// real day, a reversed range and anything unrecognised are ignored one at a
// time, so one bad item never hides the others.
func ParseSkipItems(value string) (items []string, periods []SkipPeriod, names []string) {
	for _, raw := range strings.Split(value, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if isSkipName(item) {
			names = append(names, item)
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

// isSkipName reports whether an item is a name: it starts with a letter.
func isSkipName(item string) bool {
	for _, r := range item {
		return unicode.IsLetter(r)
	}
	return false
}

func parseSkipItem(item string) (SkipPeriod, bool) {
	start, end, isRange := strings.Cut(item, "/")
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
