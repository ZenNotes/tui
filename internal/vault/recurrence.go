package vault

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RepeatUnit is the period a repeat rule counts in.
type RepeatUnit int

const (
	RepeatDays RepeatUnit = iota + 1
	RepeatWeeks
	RepeatMonths
	RepeatQuarters
	RepeatYears
)

// RepeatAnchor says where in its period a repeat rule lands. The zero value
// means the rule counts steps from the base date instead.
type RepeatAnchor int

const (
	AnchorNone RepeatAnchor = iota
	// AnchorWeekdays selects the listed weekdays of each week.
	AnchorWeekdays
	// AnchorDayOfMonth selects day Day of each month, clamped to its end.
	AnchorDayOfMonth
	// AnchorPeriodStart and AnchorPeriodEnd select the first or last day of a
	// month or a calendar quarter.
	AnchorPeriodStart
	AnchorPeriodEnd
	// AnchorWeekdayOfMonth selects the Ordinal-th Weekday of each month, where
	// Ordinal -1 is the last one.
	AnchorWeekdayOfMonth
	// AnchorMonthDay selects Month and Day of each year, clamped to its end.
	AnchorMonthDay
)

// RepeatRule is a parsed `repeat:` rule.
type RepeatRule struct {
	Unit   RepeatUnit
	Every  int
	Anchor RepeatAnchor
	// Weekdays lists the days of an AnchorWeekdays rule, in written order and
	// without duplicates.
	Weekdays []time.Weekday
	Day      int
	Month    int
	Ordinal  int
	Weekday  time.Weekday
	// FromCompletion counts from the date of completion instead of the
	// task's own date (the `/done` suffix).
	FromCompletion bool
}

var (
	repeatCountRe  = regexp.MustCompile(`^([1-9]\d{0,2})([dwmqy])(?::(.+))?$`)
	repeatMDayRe   = regexp.MustCompile(`^\d{1,2}$`)
	repeatOrdDayRe = regexp.MustCompile(`^(1|2|3|4|last)-([a-z]{3})$`)
	repeatYearDay  = regexp.MustCompile(`^(\d{2})-(\d{2})$`)
)

var repeatWeekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseRepeatRule reads a `repeat:` rule, ignoring case. It reports false for
// anything outside the grammar, so an out-of-range number or an unreal date
// never becomes a rule.
func ParseRepeatRule(raw string) (RepeatRule, bool) {
	s := strings.ToLower(raw)
	var rule RepeatRule
	if base, ok := strings.CutSuffix(s, "/done"); ok {
		s = base
		rule.FromCompletion = true
	}
	if s == "" {
		return RepeatRule{}, false
	}
	if s == "weekdays" {
		rule.Unit, rule.Every, rule.Anchor = RepeatWeeks, 1, AnchorWeekdays
		rule.Weekdays = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
		return rule, true
	}
	if days, ok := parseRepeatWeekdays(s); ok {
		rule.Unit, rule.Every, rule.Anchor = RepeatWeeks, 1, AnchorWeekdays
		rule.Weekdays = days
		return rule, true
	}
	m := repeatCountRe.FindStringSubmatch(s)
	if m == nil {
		return RepeatRule{}, false
	}
	rule.Every, _ = strconv.Atoi(m[1])
	arg := m[3]
	hasArg := strings.Contains(s, ":")
	if hasArg && arg == "" {
		return RepeatRule{}, false
	}
	switch m[2] {
	case "d":
		rule.Unit = RepeatDays
		return rule, !hasArg
	case "w":
		rule.Unit = RepeatWeeks
		if !hasArg {
			return rule, true
		}
		days, ok := parseRepeatWeekdays(arg)
		if !ok {
			return RepeatRule{}, false
		}
		rule.Anchor, rule.Weekdays = AnchorWeekdays, days
		return rule, true
	case "m":
		rule.Unit = RepeatMonths
		if !hasArg {
			return rule, true
		}
		return rule, parseRepeatMonthDay(&rule, arg)
	case "q":
		rule.Unit = RepeatQuarters
		if !hasArg {
			return rule, true
		}
		switch arg {
		case "start":
			rule.Anchor = AnchorPeriodStart
		case "end":
			rule.Anchor = AnchorPeriodEnd
		default:
			return RepeatRule{}, false
		}
		return rule, true
	default:
		rule.Unit = RepeatYears
		if !hasArg {
			return rule, true
		}
		return rule, parseRepeatYearDay(&rule, arg)
	}
}

func parseRepeatWeekdays(s string) ([]time.Weekday, bool) {
	var days []time.Weekday
	for _, name := range strings.Split(s, ",") {
		wd, ok := repeatWeekdayNames[name]
		if !ok {
			return nil, false
		}
		dupe := false
		for _, have := range days {
			dupe = dupe || have == wd
		}
		if !dupe {
			days = append(days, wd)
		}
	}
	return days, true
}

// parseRepeatMonthDay fills the anchor of an `Nm:<arg>` rule, and reports
// false when arg is not a day 1-31, start, end or "<1-4|last>-<weekday>".
func parseRepeatMonthDay(rule *RepeatRule, arg string) bool {
	switch {
	case arg == "start":
		rule.Anchor = AnchorPeriodStart
	case arg == "end":
		rule.Anchor = AnchorPeriodEnd
	case repeatMDayRe.MatchString(arg):
		day, _ := strconv.Atoi(arg)
		if day < 1 || day > 31 {
			return false
		}
		rule.Anchor, rule.Day = AnchorDayOfMonth, day
	default:
		m := repeatOrdDayRe.FindStringSubmatch(arg)
		if m == nil {
			return false
		}
		wd, ok := repeatWeekdayNames[m[2]]
		if !ok {
			return false
		}
		rule.Anchor, rule.Weekday = AnchorWeekdayOfMonth, wd
		if m[1] == "last" {
			rule.Ordinal = -1
		} else {
			rule.Ordinal, _ = strconv.Atoi(m[1])
		}
	}
	return true
}

// parseRepeatYearDay fills the anchor of an `Ny:MM-DD` rule. The day must
// exist in some year, so 02-29 is accepted and 02-30 is not.
func parseRepeatYearDay(rule *RepeatRule, arg string) bool {
	m := repeatYearDay.FindStringSubmatch(arg)
	if m == nil {
		return false
	}
	month, _ := strconv.Atoi(m[1])
	day, _ := strconv.Atoi(m[2])
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	// 2000 is a leap year, so it is the year whose calendar holds every
	// valid month and day.
	if day > time.Date(2000, time.Month(month)+1, 0, 12, 0, 0, 0, time.UTC).Day() {
		return false
	}
	rule.Anchor, rule.Month, rule.Day = AnchorMonthDay, month, day
	return true
}

// frontmatterRepeat reads a task note's `repeat` value. It returns the
// lower-cased rule, or "" when the value does not parse.
func frontmatterRepeat(raw string) string {
	v := strings.ToLower(Unquote(strings.TrimSpace(raw)))
	if _, ok := ParseRepeatRule(v); !ok {
		return ""
	}
	return v
}

// frontmatterSkip reads a task note's `skip` value, a scalar or a list, into
// its valid items. A scalar may also hold several comma-separated items.
func frontmatterSkip(values []string) []string {
	var out []string
	for _, v := range values {
		items, _ := ParseSkipItems(v)
		out = append(out, items...)
	}
	return out
}
