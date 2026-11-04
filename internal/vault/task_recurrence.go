package vault

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Recurring tasks: the `@repeat:<rule>` field and the edit checking one off
// makes. Port of packages/shared-domain/src/task-recurrence.ts; both run
// testdata/task-recurrence.cases.json. A check-off turns
//
//	- [ ] Water the plants @repeat:weekly due:2026-11-01
//
// into the next occurrence above a finished record, which drops `@repeat` and
// `@remind` so unchecking it never spawns a copy:
//
//	- [ ] Water the plants @repeat:weekly due:2026-11-08
//	- [x] Water the plants due:2026-11-01
//
// A task's subtree repeats with it, every checkbox reset to open, and the record
// cancels the subtasks it left open. Dates are whole days on UTC calendar fields,
// so a daylight-saving change can never shift one.

type repeatUnit int

const (
	repeatDay repeatUnit = iota
	repeatWeek
	repeatMonth
	repeatYear
)

// repeatRule is every N units, counted from the due date, or from the day the
// task is finished when afterDone; or, with days set, on those weekdays
// (0 = Sunday, ascending).
type repeatRule struct {
	unit      repeatUnit
	every     int
	afterDone bool
	days      []int
}

var (
	repeatIntervalRe = regexp.MustCompile(`^(\d{1,3})([dwmy])$`)
	repeatISODateRe  = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

	repeatFieldRe     = regexp.MustCompile(`(?i)(?:^|\s)@repeat:([\p{L}\d][\p{L}\d/_-]*)`)
	repeatRuleTokenRe = regexp.MustCompile(`(?i)(^|\s)@repeat:[\p{L}\d][\p{L}\d/_-]*`)
	repeatRemindRe    = regexp.MustCompile(`(?i)(^|\s)@remind:[\p{L}\d][\p{L}\d/_-]*`)
	repeatStatusRe    = regexp.MustCompile(`(?i)(^|\s)@status:[\p{L}\d][\p{L}\d/_-]*`)
	repeatWaitingRe   = regexp.MustCompile(`(?i)(^|\s)@waiting\b`)
	repeatDueRe       = regexp.MustCompile(`(?i)(^|\s)due:\s*(\S+)`)
	tidyRunRe         = regexp.MustCompile(`[ \t]{2,}`)
	tidyEndRe         = regexp.MustCompile(`\s+$`)
)

var repeatNamed = map[string]repeatRule{
	"daily":    {unit: repeatDay, every: 1},
	"weekly":   {unit: repeatWeek, every: 1},
	"monthly":  {unit: repeatMonth, every: 1},
	"yearly":   {unit: repeatYear, every: 1},
	"annually": {unit: repeatYear, every: 1},
}

var repeatUnitLetters = map[string]repeatUnit{"d": repeatDay, "w": repeatWeek, "m": repeatMonth, "y": repeatYear}

var repeatDayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// parseRepeatRule reads a `@repeat:` value. An unknown rule leaves the task an
// ordinary one rather than guessing.
func parseRepeatRule(raw string) (repeatRule, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return repeatRule{}, false
	}
	if value == "weekdays" {
		return repeatRule{days: []int{1, 2, 3, 4, 5}}, true
	}
	afterDone := strings.HasPrefix(value, "after-")
	value = strings.TrimPrefix(value, "after-")
	if rule, ok := repeatNamed[value]; ok {
		rule.afterDone = afterDone
		return rule, true
	}
	if m := repeatIntervalRe.FindStringSubmatch(value); m != nil {
		every, _ := strconv.Atoi(m[1])
		if every < 1 {
			return repeatRule{}, false
		}
		return repeatRule{unit: repeatUnitLetters[m[2]], every: every, afterDone: afterDone}, true
	}
	if afterDone {
		return repeatRule{}, false
	}
	seen := [7]bool{}
	for _, part := range strings.Split(value, "/") {
		day := -1
		for i, name := range repeatDayNames {
			if name == part {
				day = i
			}
		}
		if day < 0 {
			return repeatRule{}, false
		}
		seen[day] = true
	}
	days := []int{}
	for i, on := range seen {
		if on {
			days = append(days, i)
		}
	}
	return repeatRule{days: days}, len(days) > 0
}

func repeatParseISO(iso string) (time.Time, bool) {
	m := repeatISODateRe.FindStringSubmatch(iso)
	if m == nil {
		return time.Time{}, false
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
}

func repeatFormatISO(t time.Time) string { return t.Format("2006-01-02") }

func repeatAddDays(iso string, n int) string {
	t, ok := repeatParseISO(iso)
	if !ok {
		return iso
	}
	return repeatFormatISO(t.AddDate(0, 0, n))
}

// repeatAddMonths clamps to the month's length: Jan 31 plus a month is Feb 28.
func repeatAddMonths(iso string, n int) string {
	t, ok := repeatParseISO(iso)
	if !ok {
		return iso
	}
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return repeatFormatISO(time.Date(first.Year(), first.Month(), min(t.Day(), last), 0, 0, 0, 0, time.UTC))
}

func repeatAddUnits(iso string, n int, unit repeatUnit) string {
	switch unit {
	case repeatDay:
		return repeatAddDays(iso, n)
	case repeatWeek:
		return repeatAddDays(iso, n*7)
	case repeatYear:
		return repeatAddMonths(iso, n*12)
	}
	return repeatAddMonths(iso, n)
}

// nextRepeatDate is the first date of the rule's series after both the due date
// and today, so a task finished late skips what it missed: a daily task done
// five days late comes back tomorrow, not as five overdue copies. Months count
// from base each time, so one completion never compounds a month-end clamp.
func nextRepeatDate(rule repeatRule, base, today string) string {
	if len(rule.days) > 0 {
		from := base
		if today > from {
			from = today
		}
		date := repeatAddDays(from, 1)
		for i := 0; i < 7; i++ {
			t, _ := repeatParseISO(date)
			for _, day := range rule.days {
				if int(t.Weekday()) == day {
					return date
				}
			}
			date = repeatAddDays(date, 1)
		}
		return date
	}
	if rule.afterDone {
		return repeatAddUnits(today, rule.every, rule.unit)
	}
	if rule.unit == repeatDay || rule.unit == repeatWeek {
		step := rule.every
		if rule.unit == repeatWeek {
			step *= 7
		}
		baseT, okBase := repeatParseISO(base)
		todayT, okToday := repeatParseISO(today)
		if !okBase || !okToday {
			return repeatAddDays(base, step)
		}
		behind := int(todayT.Sub(baseT).Hours() / 24)
		k := 1
		if behind >= 0 {
			k = behind/step + 1
		}
		return repeatAddDays(base, k*step)
	}
	months := rule.every
	if rule.unit == repeatYear {
		months *= 12
	}
	for k := 1; k < 100_000; k++ {
		if date := repeatAddMonths(base, k*months); date > today {
			return date
		}
	}
	return repeatAddMonths(base, months)
}

func repeatTidy(text string) string {
	return tidyEndRe.ReplaceAllString(tidyRunRe.ReplaceAllString(text, " "), "")
}

// RecurringOptions says how a recurring task is being finished.
type RecurringOptions struct {
	// Today is the day it is finished, YYYY-MM-DD in the user's time zone.
	Today string
	// FallbackDue is the date a task with no `due:` counts from when the note's
	// frontmatter names none either.
	FallbackDue string
	// Skip writes the record cancelled instead of done: "skip this time".
	Skip bool
}

// RecurringCompletion is the note after a recurring task was finished.
type RecurringCompletion struct {
	Body    string
	NextDue string
	// InsertedLines and InsertedTasks count what went in above the finished
	// task, so its line and its index moved down by that much.
	InsertedLines int
	InsertedTasks int
}

// CompleteRecurringTaskAtIndex finishes the open recurring task at taskIndex,
// counted in the dialect. ok is false when that task is not an open recurring
// one, and the caller then treats it like any other task. A Windows note keeps
// its `\r\n` endings, the inserted lines included.
func CompleteRecurringTaskAtIndex(markdown string, taskIndex int, dialect TaskDialect, opts RecurringOptions) (RecurringCompletion, bool) {
	lines := strings.Split(markdown, "\n")
	numbers := taskLineNumbersIn(repeatBareLines(lines), dialect)
	if taskIndex < 0 || taskIndex >= len(numbers) {
		return RecurringCompletion{}, false
	}
	return CompleteRecurringTaskOnLine(markdown, numbers[taskIndex], dialect, opts)
}

// CompleteRecurringTaskOnLine is CompleteRecurringTaskAtIndex for a caller that
// knows the line, as one working from a scan's answer does.
func CompleteRecurringTaskOnLine(markdown string, at int, dialect TaskDialect, opts RecurringOptions) (RecurringCompletion, bool) {
	lines := strings.Split(markdown, "\n")
	if at < 0 || at >= len(lines) {
		return RecurringCompletion{}, false
	}
	bare := repeatBareLines(lines)
	lineRe := dialect.lineRe()
	m := lineRe.FindStringSubmatch(bare[at])
	if m == nil || (m[2] != " " && m[2] != "/") || !strings.HasPrefix(m[3], "]") {
		return RecurringCompletion{}, false
	}
	rest := m[3][1:]
	field := repeatFieldRe.FindStringSubmatch(rest)
	if field == nil {
		return RecurringCompletion{}, false
	}
	rule, ok := parseRepeatRule(field[1])
	if !ok {
		return RecurringCompletion{}, false
	}

	base := opts.Today
	if d := parseNoteDefaults(strings.ReplaceAll(markdown, "\r\n", "\n")).Due; d != "" {
		base = d
	} else if opts.FallbackDue != "" {
		base = opts.FallbackDue
	}
	due := repeatDueRe.FindStringSubmatchIndex(rest)
	if due != nil {
		if written := rest[due[4]:due[5]]; repeatISODateRe.MatchString(written) {
			base = written
		}
	}
	nextDue := nextRepeatDate(rule, base, opts.Today)

	// The occurrence keeps the text, priority, tags and every other field, in
	// their order, with `due:` replaced where it stood. `@waiting` and the board's
	// `@status:` belong to one occurrence and do not carry.
	var nextRest string
	if due != nil {
		nextRest = rest[:due[0]] + rest[due[2]:due[3]] + "due:" + nextDue + rest[due[1]:]
	} else {
		nextRest = tidyEndRe.ReplaceAllString(rest, "") + " due:" + nextDue
	}
	nextRest = repeatWaitingRe.ReplaceAllString(nextRest, "${1}")
	nextRest = repeatTidy(repeatStatusRe.ReplaceAllString(nextRest, "${1}"))
	next := []string{m[1] + " ]" + nextRest}

	// The subtree comes along with every checkbox open again, and the record
	// cancels the subtasks it left open, so only the fresh copies read as live.
	end := taskBlockEnd(bare, at, leadingIndentWidth(bare[at]))
	recordChildren := []string{}
	insertedTasks := 1
	for i := at + 1; i < end; i++ {
		child := lineRe.FindStringSubmatch(bare[i])
		if child == nil {
			next = append(next, bare[i])
			recordChildren = append(recordChildren, bare[i])
			continue
		}
		insertedTasks++
		next = append(next, child[1]+" "+child[3])
		if child[2] == " " || child[2] == "/" {
			recordChildren = append(recordChildren, child[1]+"-"+child[3])
		} else {
			recordChildren = append(recordChildren, bare[i])
		}
	}

	recordRest := repeatRuleTokenRe.ReplaceAllString(rest, "${1}")
	recordRest = repeatTidy(repeatRemindRe.ReplaceAllString(recordRest, "${1}"))
	mark := "x"
	if opts.Skip {
		mark = "-"
	}

	ending := func(i int) string {
		if strings.HasSuffix(lines[i], "\r") {
			return "\r"
		}
		return ""
	}
	out := make([]string, 0, len(lines)+len(next)+1)
	out = append(out, lines[:at]...)
	for _, line := range next {
		out = append(out, line+ending(at))
	}
	out = append(out, m[1]+mark+"]"+recordRest+ending(at))
	for k, line := range recordChildren {
		out = append(out, line+ending(at+1+k))
	}
	out = append(out, lines[end:]...)
	return RecurringCompletion{
		Body:          strings.Join(out, "\n"),
		NextDue:       nextDue,
		InsertedLines: len(next),
		InsertedTasks: insertedTasks,
	}, true
}

func repeatBareLines(lines []string) []string {
	bare := make([]string, len(lines))
	for i, line := range lines {
		bare[i] = strings.TrimSuffix(line, "\r")
	}
	return bare
}

// ToggleTaskRepeating is ToggleTaskInBody with recurring tasks: finishing an
// open `@repeat:` task writes its next occurrence above it. index is where the
// toggled task now is, which moved down past that occurrence.
func ToggleTaskRepeating(body string, targetIndex int, dialect TaskDialect, now time.Time) (next string, index int, ok bool) {
	if done, recurring := CompleteRecurringTaskAtIndex(body, targetIndex, dialect, RecurringOptions{Today: TodayISO(now)}); recurring {
		return done.Body, targetIndex + done.InsertedTasks, true
	}
	next, ok = ToggleTaskInBody(body, targetIndex, dialect)
	return next, targetIndex, ok
}
