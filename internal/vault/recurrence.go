package vault

import (
	"regexp"
	"sort"
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

// searchYears bounds how far past the base date NextOccurrence looks. A skip
// range that covers more than this leaves the rule with no occurrence instead
// of looping without end.
const searchYears = 400

const isoDay = "2006-01-02"

// NextOccurrence returns the first date of the rule's sequence that lies
// after base, after today and outside every skip period, as a `YYYY-MM-DD`
// date. base is the date the rule counts from: the task's own date, or today
// for a rule counted from completion. weekStart is the first day of the week
// that `Nw` periods use. It reports false when base or today is not a date,
// or when no date within the search bound qualifies.
//
// A rule without an anchor counts base plus k steps, each counted from base
// and clamped to the end of a shorter month. A rule with an anchor selects
// dates in the period that holds base, and in every N-th period after it.
// All arithmetic is on calendar days, so a daylight saving change never
// moves a date.
func NextOccurrence(rule RepeatRule, base, today string, weekStart time.Weekday, skips []SkipPeriod) (string, bool) {
	if !IsValidISODate(base) || !IsValidISODate(today) || rule.Every < 1 {
		return "", false
	}
	b, err := time.Parse(isoDay, base)
	if err != nil {
		return "", false
	}
	limit := b.Year() + searchYears
	after := base
	if today > after {
		after = today
	}
	for period := 0; ; period++ {
		dates, beyond := repeatPeriodDates(rule, b, period, weekStart, limit)
		if beyond {
			return "", false
		}
		for _, d := range dates {
			s := d.Format(isoDay)
			if s > after && !inSkipPeriod(s, skips) {
				return s, true
			}
		}
	}
}

func inSkipPeriod(day string, skips []SkipPeriod) bool {
	for _, p := range skips {
		if day >= p.Start && day <= p.End {
			return true
		}
	}
	return false
}

// repeatPeriodDates lists, in ascending order, the dates the rule selects in
// its period-th step after the base date. For an anchored rule that is the
// period N*period periods after the one that holds base. For a rule without
// an anchor it is the single date N*(period+1) units after base. It reports
// beyond when that lies past the year limit.
func repeatPeriodDates(rule RepeatRule, base time.Time, period int, weekStart time.Weekday, limit int) (dates []time.Time, beyond bool) {
	steps := period * rule.Every
	switch rule.Unit {
	case RepeatDays:
		d := civilDate(base.Year(), base.Month(), base.Day()+steps+rule.Every)
		return []time.Time{d}, d.Year() > limit
	case RepeatWeeks:
		if rule.Anchor == AnchorNone {
			d := civilDate(base.Year(), base.Month(), base.Day()+7*(steps+rule.Every))
			return []time.Time{d}, d.Year() > limit
		}
		first := civilDate(base.Year(), base.Month(), base.Day()-weekdayOffset(base.Weekday(), weekStart)+7*steps)
		if first.Year() > limit {
			return nil, true
		}
		for _, wd := range rule.Weekdays {
			dates = append(dates, first.AddDate(0, 0, weekdayOffset(wd, weekStart)))
		}
		sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
		return dates, false
	case RepeatMonths:
		return monthPeriodDates(rule, base, monthIndex(base)+steps, steps+rule.Every, limit)
	case RepeatQuarters:
		quarterStart := monthIndex(base) - (int(base.Month())-1)%3
		return monthPeriodDates(rule, base, quarterStart+3*steps, 3*(steps+rule.Every), limit)
	default:
		year := base.Year() + steps
		if rule.Anchor == AnchorNone {
			year += rule.Every
		}
		if year > limit {
			return nil, true
		}
		if rule.Anchor == AnchorMonthDay {
			return []time.Time{clampedDate(year, time.Month(rule.Month), rule.Day)}, false
		}
		return []time.Time{clampedDate(year, base.Month(), base.Day())}, false
	}
}

// monthPeriodDates handles the month and quarter units. first is the month
// index (year*12 + month-1) that the period starts in; unanchored is the
// number of months past the base date for a rule without an anchor.
func monthPeriodDates(rule RepeatRule, base time.Time, first, unanchored, limit int) ([]time.Time, bool) {
	if rule.Anchor == AnchorNone {
		d := clampedMonths(base, unanchored)
		return []time.Time{d}, d.Year() > limit
	}
	if first/12 > limit {
		return nil, true
	}
	months := 1
	if rule.Unit == RepeatQuarters {
		months = 3
	}
	y, m := first/12, time.Month(first%12+1)
	switch rule.Anchor {
	case AnchorDayOfMonth:
		return []time.Time{clampedDate(y, m, rule.Day)}, false
	case AnchorPeriodStart:
		return []time.Time{civilDate(y, m, 1)}, false
	case AnchorPeriodEnd:
		return []time.Time{civilDate(y, m+time.Month(months), 0)}, false
	case AnchorWeekdayOfMonth:
		return []time.Time{weekdayOfMonth(y, m, rule.Ordinal, rule.Weekday)}, false
	}
	return nil, true
}

// civilDate builds a calendar day at noon UTC, normalising month and day
// overflow. Noon in a fixed zone keeps every step on a whole day.
func civilDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func monthIndex(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

// clampedDate is day d of the month, or its last day when the month is
// shorter.
func clampedDate(y int, m time.Month, d int) time.Time {
	if last := civilDate(y, m+1, 0).Day(); d > last {
		d = last
	}
	return civilDate(y, m, d)
}

// clampedMonths is base moved forward by n months, keeping its day of the
// month unless the target month is shorter.
func clampedMonths(base time.Time, n int) time.Time {
	idx := monthIndex(base) + n
	return clampedDate(idx/12, time.Month(idx%12+1), base.Day())
}

// weekdayOffset is how many days wd falls after the week's first day.
func weekdayOffset(wd, weekStart time.Weekday) int {
	return (int(wd) - int(weekStart) + 7) % 7
}

// weekdayOfMonth is the ordinal-th weekday of the month, where ordinal -1
// selects the last one.
func weekdayOfMonth(y int, m time.Month, ordinal int, wd time.Weekday) time.Time {
	if ordinal < 0 {
		last := civilDate(y, m+1, 0)
		return civilDate(y, m, last.Day()-(int(last.Weekday())-int(wd)+7)%7)
	}
	firstOffset := (int(wd) - int(civilDate(y, m, 1).Weekday()) + 7) % 7
	return civilDate(y, m, 1+firstOffset+7*(ordinal-1))
}
