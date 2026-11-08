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

// RepeatRule is a parsed `@repeat:` rule.
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
	// task's own date (the `after-` prefix).
	FromCompletion bool
}

var (
	repeatCountRe  = regexp.MustCompile(`^(\d{1,3})([dwmqy])$`)
	repeatMDayRe   = regexp.MustCompile(`^\d{1,2}$`)
	repeatOrdDayRe = regexp.MustCompile(`^(1|2|3|4|last)-([a-z]{3})$`)
	repeatYearDay  = regexp.MustCompile(`^(\d{2})-(\d{2})$`)
)

var repeatWeekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// repeatIntervalNames are the named intervals. Each one is the same as a
// count of one unit.
var repeatIntervalNames = map[string]string{
	"daily": "1d", "weekly": "1w", "monthly": "1m",
	"quarterly": "1q", "yearly": "1y", "annually": "1y",
}

var repeatCountUnits = map[string]RepeatUnit{
	"d": RepeatDays, "w": RepeatWeeks, "m": RepeatMonths, "q": RepeatQuarters, "y": RepeatYears,
}

// ParseRepeatRule reads the value of a `@repeat:` field, ignoring case. It
// reports false for anything outside the grammar, so an out-of-range number
// or an unreal date never becomes a rule.
//
// A rule is an interval (`3d`, `2w`, `3m`, `1q`, `1y` or a name such as
// `weekly`), `after-` and an interval, `weekdays`, or a list of weekdays
// such as `mon/thu`. An interval may end with an anchor after a hyphen:
// `2w-mon/thu`, `monthly-15`, `monthly-2-tue`, `quarterly-end`,
// `yearly-03-14`.
func ParseRepeatRule(raw string) (RepeatRule, bool) {
	s := strings.ToLower(raw)
	if rest, found := strings.CutPrefix(s, "after-"); found {
		rule, ok := parseRepeatInterval(rest)
		rule.FromCompletion = ok
		return rule, ok
	}
	var rule RepeatRule
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
	interval, arg, hasArg := strings.Cut(s, "-")
	rule, ok := parseRepeatInterval(interval)
	if !ok {
		return RepeatRule{}, false
	}
	if !hasArg {
		return rule, true
	}
	switch rule.Unit {
	case RepeatWeeks:
		days, ok := parseRepeatWeekdays(arg)
		if !ok {
			return RepeatRule{}, false
		}
		rule.Anchor, rule.Weekdays = AnchorWeekdays, days
		return rule, true
	case RepeatMonths:
		return rule, parseRepeatMonthDay(&rule, arg)
	case RepeatQuarters:
		switch arg {
		case "start":
			rule.Anchor = AnchorPeriodStart
		case "end":
			rule.Anchor = AnchorPeriodEnd
		default:
			return RepeatRule{}, false
		}
		return rule, true
	case RepeatYears:
		return rule, parseRepeatYearDay(&rule, arg)
	}
	return RepeatRule{}, false
}

// parseRepeatInterval reads a count of units such as `3w`, or a name such as
// `weekly`, which is a count of one. The count is 1 to 999.
func parseRepeatInterval(s string) (RepeatRule, bool) {
	if count, ok := repeatIntervalNames[s]; ok {
		s = count
	}
	m := repeatCountRe.FindStringSubmatch(s)
	if m == nil {
		return RepeatRule{}, false
	}
	every, _ := strconv.Atoi(m[1])
	if every < 1 {
		return RepeatRule{}, false
	}
	return RepeatRule{Unit: repeatCountUnits[m[2]], Every: every}, true
}

// parseRepeatWeekdays reads weekday names that a slash separates.
func parseRepeatWeekdays(s string) ([]time.Weekday, bool) {
	var days []time.Weekday
	for _, name := range strings.Split(s, "/") {
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
		items, _, _ := ParseSkipItems(v)
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
// that `Nw` periods use. until is the last date the rule may select, or ""
// for a rule that never ends. It reports false when base or today is not a
// date, when no date up to until qualifies, or when no date within the search
// bound qualifies.
//
// A rule without an anchor counts base plus k steps, each counted from base
// and clamped to the end of a shorter month. A rule with an anchor selects
// dates in the period that holds base, and in every N-th period after it.
// All arithmetic is on calendar days, so a daylight saving change never
// moves a date.
func NextOccurrence(rule RepeatRule, base, today string, weekStart time.Weekday, skips []SkipPeriod, until string) (string, bool) {
	if rule.FromCompletion {
		base = today
	}
	if !IsValidISODate(base) || !IsValidISODate(today) || rule.Every < 1 {
		return "", false
	}
	b, err := time.Parse(isoDay, base)
	if err != nil {
		return "", false
	}
	after := base
	if today > after {
		after = today
	}
	if until != "" && until < base {
		return "", false
	}
	next, found := "", false
	eachRuleDate(rule, b, weekStart, 0, func(s string) bool {
		// The dates ascend, so the first one past until ends the search,
		// even when a skip period covers it.
		if until != "" && s > until {
			return false
		}
		if s > after && !inSkipPeriod(s, skips) {
			next, found = s, true
			return false
		}
		return true
	})
	return next, found
}

// eachRuleDate calls fn with the dates of the rule's sequence in ascending
// order, from the period with index firstPeriod. It stops when fn returns
// false or when the dates pass the search bound. A rule with an anchor can
// give dates before base in its first period.
func eachRuleDate(rule RepeatRule, base time.Time, weekStart time.Weekday, firstPeriod int, fn func(date string) bool) {
	limit := searchLimit(rule, base)
	for period := firstPeriod; ; period++ {
		dates, beyond := repeatPeriodDates(rule, base, period, weekStart, limit)
		if beyond {
			return
		}
		for _, d := range dates {
			if !fn(d.Format(isoDay)) {
				return
			}
		}
	}
}

// searchLimit is the last year that a search for dates of the rule may reach.
// It lies searchYears past the base date, and further by the length of one
// step of the rule, so a long step such as 999y still has a first date.
func searchLimit(rule RepeatRule, base time.Time) int {
	stepYears := rule.Every
	switch rule.Unit {
	case RepeatDays:
		stepYears = rule.Every/365 + 1
	case RepeatWeeks:
		stepYears = rule.Every*7/365 + 1
	case RepeatMonths:
		stepYears = rule.Every/12 + 1
	case RepeatQuarters:
		stepYears = rule.Every*3/12 + 1
	}
	return base.Year() + searchYears + stepYears
}

// periodNear returns a period index that is not later than the first period
// which holds a date on or after target. It lets a caller skip the periods
// before a window. It keeps a margin of three periods, so a clamped date or
// a week that starts on another day never lies before the returned period.
func periodNear(rule RepeatRule, base, target time.Time) int {
	days := int(target.Sub(base).Hours() / 24)
	months := monthIndex(target) - monthIndex(base)
	var p int
	switch rule.Unit {
	case RepeatDays:
		p = days / rule.Every
	case RepeatWeeks:
		p = days / (7 * rule.Every)
	case RepeatMonths:
		p = months / rule.Every
	case RepeatQuarters:
		p = months / (3 * rule.Every)
	default:
		p = (target.Year() - base.Year()) / rule.Every
	}
	if p -= 3; p < 0 {
		return 0
	}
	return p
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
