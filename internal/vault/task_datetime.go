package vault

import (
	"regexp"
	"strings"
	"time"
)

var clockTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// IsValidClockTime accepts a 24-hour `HH:MM` with two digits each.
func IsValidClockTime(s string) bool {
	return clockTimeRe.MatchString(s)
}

// SplitDateTime splits a `due:` value into its date, its start time and its
// end time. A bare date gives empty times. A `YYYY-MM-DDTHH:MM` value gives
// the date and the start. A `YYYY-MM-DDTHH:MM-HH:MM` value also gives the end,
// but only when the end is a valid `HH:MM` later than the start. Otherwise the
// end is empty and the start is kept. When the date is real but what follows
// `T` is not a valid `HH:MM`, the date is kept and the times are empty, so a
// typo never hides the day. A value that goes on past the date with `-`, `/`
// or `..` keeps its date only. A value whose date is not a real `YYYY-MM-DD`
// gives an empty date.
func SplitDateTime(s string) (date, start, end string) {
	date, start, end, _ = parseDateTimeRange(s)
	return date, start, end
}

// parseDateTimeRange reads a value as SplitDateTime does and also says whether
// the whole value is well formed: a date, a date with a start time, or a date
// with a start time and a later end time.
func parseDateTimeRange(s string) (date, start, end string, ok bool) {
	if len(s) < 10 || !IsValidISODate(s[:10]) {
		return "", "", "", false
	}
	date = s[:10]
	rest := s[10:]
	switch {
	case rest == "":
		return date, "", "", true
	case strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, ".."):
		return date, "", "", false
	case rest[0] != 'T':
		return "", "", "", false
	}
	rest = rest[1:]
	if IsValidClockTime(rest) {
		return date, rest, "", true
	}
	if len(rest) >= 6 && IsValidClockTime(rest[:5]) && rest[5] == '-' {
		start = rest[:5]
		if tail := rest[6:]; IsValidClockTime(tail) && tail > start {
			return date, start, tail, true
		}
		return date, start, "", false
	}
	return date, "", "", false
}

// ParseWhenValue reads a `when:` value into a start date, a start time, an
// end date and an end time. It also returns a problem text for `zn doctor`,
// which is empty when the whole value is well formed.
//
// The same-day form is `start` or `start-HH:MM`, read as SplitDateTime reads
// it. The cross-day form is `start/end`, where both ends are a date or both
// are a date-time. A date end must not be before the start date. A date-time
// end must be after the start. A `/` with both ends on one day gives the same
// result as the same-day form. A `..` is not a separator, so a value with `..`
// keeps its start and has a problem. An end that is not valid gives no end, and the
// start is kept with its time. The end date is empty when the value has no
// end, and for a date range that ends on its start day.
func ParseWhenValue(s string) (startDate, startTime, endDate, endTime, problem string) {
	cut := strings.Index(s, "/")
	if cut < 10 {
		return parseSameDayWhen(s)
	}
	date, start, left, leftOK := parseDateTimeRange(s[:cut])
	if date == "" {
		return "", "", "", "", "the start is not a date in the form YYYY-MM-DD"
	}
	toDate, toTime, toEnd, toOK := parseDateTimeRange(s[cut+1:])
	switch {
	case !leftOK || left != "":
		return date, start, "", "", "the start of the range is not a date or a date with a time"
	case !toOK || toEnd != "":
		return date, start, "", "", "the end of the range is not a date or a date with a time"
	case (start == "") != (toTime == ""):
		return date, start, "", "", "both ends of the range must be dates or both must be date-times"
	case start == "" && toDate < date:
		return date, start, "", "", "the end date is before the start date"
	case start != "" && toDate+"T"+toTime <= date+"T"+start:
		return date, start, "", "", "the end is not after the start"
	case start == "" && toDate == date:
		return date, "", "", "", ""
	}
	return date, start, toDate, toTime, ""
}

// parseSameDayWhen reads a value that has no `/`. A timed end is on the start
// date.
func parseSameDayWhen(s string) (startDate, startTime, endDate, endTime, problem string) {
	date, start, end, ok := parseDateTimeRange(s)
	switch {
	case ok && end != "":
		return date, start, date, end, ""
	case ok:
		return date, start, "", "", ""
	case date == "":
		return "", "", "", "", "the value does not start with a date in the form YYYY-MM-DD"
	}
	if len(s) == 22 && s[10] == 'T' && s[16] == '-' && IsValidClockTime(s[11:16]) && IsValidClockTime(s[17:]) {
		// The end is a valid time that is not after the start. Suggest the
		// range that goes on into the next day.
		if next := shiftISODate(date, 1); next != "" {
			return date, start, "", "", "the end is not after the start. To end on the next day, write " +
				date + "T" + start + "/" + next + "T" + s[17:]
		}
	}
	rest := s[10:]
	if len(rest) >= 6 && rest[0] == 'T' && IsValidClockTime(rest[1:6]) {
		rest = rest[6:]
		start = s[11:16]
	}
	if strings.HasPrefix(rest, "..") {
		// A range across days is written with a slash, not with dots.
		return date, start, "", "", "write a range across days as start/end with a slash, so zn uses only the start"
	}
	return date, start, "", "", "the time or the end of the value is not correct"
}

// FormatWhenValue renders a parsed `when:` value for display with 24-hour
// times: `2027-03-13 – 2027-03-21` for a range of dates, `2026-11-03 22:00 –
// 2026-11-04 02:00` for a range across days and `2026-11-03 14:00–15:30` for a
// range inside one day.
func FormatWhenValue(startDate, startTime, endDate, endTime string) string {
	from := strings.TrimSpace(startDate + " " + startTime)
	switch {
	case endTime != "" && endDate == startDate:
		return from + "–" + endTime
	case endDate != "":
		return from + " – " + strings.TrimSpace(endDate+" "+endTime)
	}
	return from
}

// ShiftWhenValue moves both ends of a `when:` value by a number of days. The
// times and the length of the range stay the same. A value with a problem is
// returned unchanged.
func ShiftWhenValue(s string, days int) string {
	startDate, startTime, endDate, endTime, problem := ParseWhenValue(s)
	if problem != "" {
		return s
	}
	out := shiftISODate(startDate, days)
	if startTime != "" {
		out += "T" + startTime
	}
	switch {
	case endDate == "":
		return out
	case endDate == startDate:
		return out + "-" + endTime
	}
	out += "/" + shiftISODate(endDate, days)
	if endTime != "" {
		out += "T" + endTime
	}
	return out
}

// shiftISODate moves a `YYYY-MM-DD` date by a number of days. It returns an
// empty string when the date is not valid.
func shiftISODate(date string, days int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// splitFrontmatterDateTime reads a frontmatter scalar, which may be quoted,
// as a date, a start time and an end time.
func splitFrontmatterDateTime(raw string) (date, start, end string) {
	return SplitDateTime(Unquote(strings.TrimSpace(raw)))
}
