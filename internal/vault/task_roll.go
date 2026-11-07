package vault

import (
	"regexp"
	"strings"
	"time"
)

// RollAction is how a repeating checkbox leaves the open state.
type RollAction int

const (
	// RollComplete ticks the task, or unticks it when it is already done.
	RollComplete RollAction = iota
	// RollCancel marks the task `[-]`, or reopens it when it is already
	// cancelled.
	RollCancel
)

// RollTaskInBody completes or cancels the task at targetIndex, counted in the
// given grammar. An open, in-progress or waiting task that carries a valid
// `repeat:` rule also gets its next occurrence: an open copy of the line and
// its indented block is inserted directly above it, with `due:` moved to the
// next date and the original keeping its own dates. A `scheduled:` token is
// plain text and stays as written.
// Every other case is the plain toggle or cancel, with nothing inserted: no
// rule, an invalid rule, unticking, reopening, a forwarded task, or a rule
// with no date left to land on, such as one that has reached its `until:`
// date.
//
// The next date counts from `due:`, else today, and from today when the rule
// ends in `/done`. It is the first date of the rule after that and after today
// that no `skip:` period covers and that is not past the `until:` date. The copied subtasks' dates move by the same
// number of days as the task's `due:`, and each keeps its `THH:MM`. A task
// with no `due:` gets `due:` on the copy. now and weekStart are inputs so the
// caller chooses the clock and the first day of the week.
//
// Only local notes roll. A remote vault's server parses the note itself, so
// remote toggles use ToggleTaskInBody and never insert a line.
//
// inserted counts the task lines added above the original, which is how far
// the original's index, and every later task's, moved. Line endings are
// normalised to LF, as ToggleTaskInBody does. ok is false when the body holds
// no task at that index.
func RollTaskInBody(body string, targetIndex int, dialect TaskDialect, action RollAction, now time.Time, weekStart time.Weekday) (next string, inserted int, ok bool) {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	numbers := taskLineNumbersIn(lines, dialect)
	if targetIndex < 0 || targetIndex >= len(numbers) {
		return body, 0, false
	}
	at := numbers[targetIndex]
	m := dialect.lineRe().FindStringSubmatch(lines[at])
	if m == nil {
		return body, 0, false
	}

	shift, due, rolled := planRoll(m, dialect, now, weekStart)
	if !rolled {
		if action == RollComplete {
			plain, found := ToggleTaskInBody(body, targetIndex, dialect)
			return plain, 0, found
		}
		lines[at] = m[1] + cancelMarker(m[2]) + m[3]
		return strings.Join(lines, "\n"), 0, true
	}

	end := taskBlockEnd(lines, at, leadingIndentWidth(lines[at]))
	copied := make([]string, 0, end-at)
	for i := at; i < end; i++ {
		line := lines[i]
		if cm := dialect.lineRe().FindStringSubmatch(line); cm != nil {
			inserted++
			line = cm[1] + " " + shift(cm[3])
			if i == at && due != "" {
				line = withDue(line, due)
			}
		}
		copied = append(copied, line)
	}

	original := m[1] + "x" + m[3]
	if action == RollCancel {
		original = m[1] + "-" + m[3]
	}
	out := make([]string, 0, len(lines)+len(copied))
	out = append(out, lines[:at]...)
	out = append(out, copied...)
	out = append(out, original)
	out = append(out, lines[at+1:]...)
	return strings.Join(out, "\n"), inserted, true
}

// withDue gives line the `due:` date. A `due:` token the line already has,
// which holds no real date, gets its value replaced, so the parser reads the
// new date from it; otherwise the token is appended.
func withDue(line, date string) string {
	if loc := inlineDueRe.FindStringSubmatchIndex(line); loc != nil {
		return line[:loc[2]] + date + line[loc[3]:]
	}
	return strings.TrimRight(line, " \t") + " due:" + date
}

// cancelMarker is the state a plain cancel gives a line: cancelled, or open
// again when it already was cancelled.
func cancelMarker(state string) string {
	if state == "-" {
		return " "
	}
	return "-"
}

// planRoll decides whether the task line m rolls. If it does, shift moves the
// `due:` date in the text after a line's `[ ` (which starts with its `]`) by
// the offset of the new occurrence, and due is the `due:` date to add to the
// copy of a task that has none.
func planRoll(m []string, dialect TaskDialect, now time.Time, weekStart time.Weekday) (shift func(string) string, due string, rolled bool) {
	switch m[2] {
	case " ", "/":
	default:
		return nil, "", false
	}
	tokens := extractTaskTokens(strings.TrimPrefix(m[3], "]"), dialect)
	rule, valid := ParseRepeatRule(tokens.repeat)
	if !valid {
		return nil, "", false
	}
	_, periods := ParseSkipItems(strings.Join(tokens.skip, ","))

	field := tokens.due
	days, following, found := nextRollDate(rule, field, periods, tokens.until, now, weekStart)
	if !found {
		return nil, "", false
	}

	shift = func(text string) string {
		return shiftTokenDate(text, inlineDueRe, days)
	}
	if field == "" {
		due = following
	}
	return shift, due, true
}

// nextRollDate finds where a repeating task goes next. field is the date the
// rule counts from, `due:`, or empty for a task with none. It returns that
// date, and how many days the task's dates move to get there: the distance
// from field, or from today when field is empty. until is the last date the
// rule may land on, or "" for none.
func nextRollDate(rule RepeatRule, field string, periods []SkipPeriod, until string, now time.Time, weekStart time.Weekday) (days int, following string, found bool) {
	today := TodayISO(now)
	base := field
	if base == "" || rule.FromCompletion {
		base = today
	}
	following, found = NextOccurrence(rule, base, today, weekStart, periods, until)
	if !found {
		return 0, "", false
	}
	from := field
	if from == "" {
		from = today
	}
	return daysBetween(from, following), following, true
}

// shiftTokenDate moves the date of the first token re matches by days. The
// token's `THH:MM` suffix stays as written. A value whose date is not real is
// left alone.
func shiftTokenDate(text string, re *regexp.Regexp, days int) string {
	loc := re.FindStringSubmatchIndex(text)
	if loc == nil || loc[2] < 0 {
		return text
	}
	return text[:loc[2]] + shiftDateTime(text[loc[2]:loc[3]], days) + text[loc[3]:]
}

// shiftDateTime moves the date of a `YYYY-MM-DD[THH:MM]` value by days and
// keeps whatever follows the date as written. A value whose date is not real
// is returned unchanged.
func shiftDateTime(value string, days int) string {
	date, _, _ := SplitDateTime(value)
	if date == "" {
		return value
	}
	moved, err := time.Parse(isoDay, date)
	if err != nil {
		return value
	}
	return moved.AddDate(0, 0, days).Format(isoDay) + value[len(date):]
}

// daysBetween counts whole calendar days from one `YYYY-MM-DD` date to a
// later one. Both are read as UTC dates, so a daylight saving change cannot
// add or drop a day.
func daysBetween(from, to string) int {
	a, errA := time.Parse(isoDay, from)
	b, errB := time.Parse(isoDay, to)
	if errA != nil || errB != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}
