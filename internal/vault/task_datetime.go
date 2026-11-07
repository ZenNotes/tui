package vault

import (
	"regexp"
	"strings"
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
// typo never hides the day. A value that goes on past the date with `-` or
// `..` keeps its date only. A value whose date is not a real `YYYY-MM-DD`
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
	case strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, ".."):
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

// splitFrontmatterDateTime reads a frontmatter scalar, which may be quoted,
// as a date, a start time and an end time.
func splitFrontmatterDateTime(raw string) (date, start, end string) {
	return SplitDateTime(Unquote(strings.TrimSpace(raw)))
}
