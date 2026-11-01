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

// SplitDateTime splits a `due:` or `scheduled:` value into its date and its
// time. A bare date gives an empty time. A `YYYY-MM-DDTHH:MM` value gives
// both. When the date is real but what follows `T` is not a valid `HH:MM`, the
// date is kept and the time is empty, so a typo never hides the day. A value
// whose date is not a real `YYYY-MM-DD` gives an empty date.
func SplitDateTime(s string) (date, clock string) {
	if len(s) < 10 || !IsValidISODate(s[:10]) {
		return "", ""
	}
	date = s[:10]
	if len(s) == 10 {
		return date, ""
	}
	if s[10] != 'T' {
		return "", ""
	}
	if rest := s[11:]; IsValidClockTime(rest) {
		return date, rest
	}
	return date, ""
}

// splitFrontmatterDateTime reads a frontmatter scalar, which may be quoted,
// as a date and an optional time.
func splitFrontmatterDateTime(raw string) (date, clock string) {
	return SplitDateTime(Unquote(strings.TrimSpace(raw)))
}
