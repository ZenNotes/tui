package vault

import (
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
// given grammar. An open or in-progress task that carries a valid `@repeat:`
// rule also gets its next occurrence, as the desktop app writes it: an open
// copy of the line and its indented block is inserted directly above, and the
// original becomes the finished record below it.
//
// The copy keeps the text, the priority, the tags and every field in their
// order. It drops `@waiting` and `@status:`, which belong to one occurrence.
// Its `due:` is replaced where it stood, and it keeps its `THH:MM` time and its
// range. With a work session, the new `due:` is the next date of the rule.
// Without one, the `due:` date is the base and goes to the next date. A task
// with no `due:` and no work session gets `due:` at the end of
// the copy. The copy holds the subtask lines as written, with every checkbox open and
// without the block ids that end a line, so an id stays on one line.
// A `scheduled:` token is plain text and stays as written.
//
// The record drops `@repeat:` and `@remind:`, so unticking it never starts a
// series again, and it cancels the subtasks that were still open. It keeps
// `@until:` and `@skip:`. A rule with no date left to land on, such as one that
// has reached its `@until:` date, gives no copy. The line is then finished and
// drops the same two fields.
//
// Every other case is the plain toggle or cancel, with nothing inserted: no
// rule, an invalid rule, unticking, reopening or a forwarded task.
//
// The next date counts from the earliest work session, else from `due:`, else
// from the `due` of the note, else from today. It counts from today when the
// rule starts with `after-`. It is the first date of the rule after that and
// after today that no `@skip:` period covers and that is not past the `@until:`
// date. now and weekStart are inputs so the caller chooses the clock and the
// first day of the week.
//
// Only local notes roll. A remote vault's server parses the note itself, so
// remote toggles use ToggleTaskInBody and never insert a line.
//
// inserted counts the task lines added above the original, which is how far
// the original's index, and every later task's, moved. Every line keeps the
// ending it has. ok is false when the body holds no task at that index.
func RollTaskInBody(body string, targetIndex int, dialect TaskDialect, action RollAction, now time.Time, weekStart time.Weekday) (next string, inserted int, ok bool) {
	return RollTaskInBodyWith(body, targetIndex, dialect, action, now, weekStart, nil)
}

// RollTaskInBodyWith is RollTaskInBody with a resolver for the names in
// `@skip:` fields. The days that names gives for a name count as skip periods.
// A nil names leaves these names without effect.
func RollTaskInBodyWith(body string, targetIndex int, dialect TaskDialect, action RollAction, now time.Time, weekStart time.Weekday, names SkipResolver) (next string, inserted int, ok bool) {
	res := rollTaskLines(body, targetIndex, dialect, action, now, weekStart, names, rollSessions{})
	return res.next, res.inserted, res.ok
}

// rollSessions tells a roll about the work sessions of the task and about the
// date of its note. The zero value is a task with none.
type rollSessions struct {
	// base is the start date of the earliest current session. The roll
	// counts from it when it is not empty.
	base string
	// fallbackDue is the date that the roll counts from when the task has no
	// session, no `due:` and no `due` in the frontmatter of the note.
	fallbackDue string
	// newID gives the block id of the inserted open line. It is called only
	// when the task rolls. It is nil for a task without sessions.
	newID func() string
	// moved maps the number of a session line under the task to its moved
	// copy. The new open line holds the copy in place of that session. Any
	// other session line under the task stays only under the record.
	moved map[int]string
}

// rollResult is the outcome of rollTaskLines.
type rollResult struct {
	next     string
	inserted int
	ok       bool
	// rolled is true when a new occurrence was inserted.
	rolled bool
	// ended is true when the task has a valid rule and no next date. The line
	// is finished and has lost its rule, and nothing is inserted.
	ended bool
	// days is how far each session of the new occurrence moves.
	days int
}

// rollTaskLines does the work of RollTaskInBodyWith. The sessions change the
// base date of the roll and give the inserted open line a new block id.
func rollTaskLines(body string, targetIndex int, dialect TaskDialect, action RollAction, now time.Time, weekStart time.Weekday, names SkipResolver, sessions rollSessions) rollResult {
	lines := strings.Split(body, "\n")
	bare := repeatBareLines(lines)
	numbers := taskLineNumbersIn(bare, dialect)
	if targetIndex < 0 || targetIndex >= len(numbers) {
		return rollResult{next: body}
	}
	at := numbers[targetIndex]
	lineRe := dialect.lineRe()
	m := lineRe.FindStringSubmatch(bare[at])
	if m == nil {
		return rollResult{next: body}
	}
	ending := lines[at][len(bare[at]):]

	plan := planRoll(m, bare, dialect, now, weekStart, names, sessions)
	mark := "x"
	if action == RollCancel {
		mark = "-"
	}
	switch {
	case plan.ended:
		lines[at] = m[1] + mark + "]" + recordText(m[3][1:]) + ending
		return rollResult{next: strings.Join(lines, "\n"), ok: true, ended: true}
	case !plan.rolled:
		if action == RollComplete {
			plain, found := ToggleTaskInBody(body, targetIndex, dialect)
			return rollResult{next: plain, ok: found}
		}
		lines[at] = m[1] + cancelMarker(m[2]) + m[3] + ending
		return rollResult{next: strings.Join(lines, "\n"), ok: true}
	}

	// The copy first, then the record, as the desktop app writes them.
	copied := []string{m[1] + " ]" + plan.copyText}
	if sessions.newID != nil {
		copied[0] = strings.TrimRight(copied[0], " \t") + " ^" + sessions.newID()
	}
	end := taskBlockEnd(bare, at, leadingIndentWidth(bare[at]))
	record := make([]string, 0, end-at)
	inserted := 1
	for i := at + 1; i < end; i++ {
		child := lineRe.FindStringSubmatch(bare[i])
		if repl, ok := sessions.moved[i]; ok {
			copied = append(copied, repl)
			record = append(record, bare[i])
			continue
		}
		if isSessionLine(bare[i]) {
			// A session line that is not moved stays only under the record.
			record = append(record, bare[i])
			continue
		}
		if child == nil {
			copied = append(copied, bare[i])
			record = append(record, bare[i])
			continue
		}
		inserted++
		copied = append(copied, stripBlockID(child[1]+" "+child[3]))
		if isOpenState(child[2]) {
			record = append(record, child[1]+"-"+child[3])
		} else {
			record = append(record, bare[i])
		}
	}

	out := make([]string, 0, len(lines)+len(copied))
	out = append(out, lines[:at]...)
	for _, line := range copied {
		out = append(out, line+ending)
	}
	out = append(out, m[1]+mark+"]"+recordText(m[3][1:])+ending)
	for k, line := range record {
		out = append(out, line+lines[at+1+k][len(bare[at+1+k]):])
	}
	out = append(out, lines[end:]...)
	return rollResult{next: strings.Join(out, "\n"), inserted: inserted, ok: true, rolled: true, days: plan.days}
}

// isSessionLine reports whether a line is a work session: an event line, by
// the rules of ParseEvents, with a `task:` link.
func isSessionLine(line string) bool {
	e, ok := eventFromLine(line)
	return ok && e.TaskLink != ""
}

// isOpenState reports whether a checkbox state is open or in progress.
func isOpenState(state string) bool { return state == " " || state == "/" }

// recordText is the text of a finished line of a repeating task: the text
// after the checkbox without its `@repeat:` and `@remind:` fields.
func recordText(rest string) string {
	rest = repeatRuleTokenRe.ReplaceAllString(rest, "${1}")
	return repeatTidy(repeatRemindRe.ReplaceAllString(rest, "${1}"))
}

// withDue gives line the `due:` date. A `due:` token the line already has,
// which holds no real date, gets its value replaced, so the parser reads the
// new date from it; otherwise the token is appended.
func withDue(line, date string) string {
	if loc := inlineDueRe.FindStringSubmatchIndex(line); loc != nil {
		return line[:loc[2]] + date + line[loc[3]:]
	}
	return appendTaskToken(line, "due:"+date)
}

// cancelMarker is the state a plain cancel gives a line: cancelled, or open
// again when it already was cancelled.
func cancelMarker(state string) string {
	if state == "-" {
		return " "
	}
	return "-"
}

// rollPlan is how a task line rolls.
type rollPlan struct {
	// rolled is true when the task gets a next occurrence, and then copyText
	// is the text of the copy after its checkbox, and days is how far each
	// session copy moves.
	rolled   bool
	copyText string
	days     int
	// ended is true when the task has a valid rule and no next date.
	ended bool
}

// planRoll decides whether the task line m rolls. bare holds the lines of the
// note, and the line of m is one of them.
//
// The rule counts from the first of these dates: the start of the earliest
// current work session, the `due:` of the line, the `due` of the note, the
// date that the sessions value gives for a daily note, and today. A task with
// a session gets no `due:` that it did not have.
func planRoll(m []string, bare []string, dialect TaskDialect, now time.Time, weekStart time.Weekday, names SkipResolver, sessions rollSessions) rollPlan {
	if !isOpenState(m[2]) {
		return rollPlan{}
	}
	rest := m[3][1:]
	tokens := extractTaskTokens(rest, dialect)
	rule, valid := ParseRepeatRule(tokens.repeat)
	if !valid {
		return rollPlan{}
	}
	_, periods, _ := ParseSkipItems(strings.Join(tokens.skip, ","))
	periods = append(periods, skipPeriodsFor(names, Event{}, tokens.skipNames)...)

	from := sessions.base
	if from == "" {
		from = tokens.due
	}
	if from == "" {
		from = parseNoteDefaults(strings.Join(bare, "\n")).Due
	}
	if from == "" {
		from = sessions.fallbackDue
	}
	days, following, found := nextRollDate(rule, from, periods, tokens.until, now, weekStart)
	if !found {
		return rollPlan{ended: true}
	}

	// The copy is the text without a block id, with `due:` replaced where it
	// stood. `@waiting` and `@status:` belong to one occurrence and do not carry.
	text, _ := splitBlockID(rest)
	if loc := inlineDueRe.FindStringSubmatchIndex(text); loc != nil {
		value := text[loc[2]:loc[3]]
		if date, _, _ := SplitDateTime(value); date != "" {
			value = rolledDateTime(value, days, following, sessions.base != "")
		} else {
			value = following
		}
		lead := ""
		if loc[0] < loc[1] && (text[loc[0]] == ' ' || text[loc[0]] == '\t') {
			lead = text[loc[0] : loc[0]+1]
		}
		text = text[:loc[0]] + lead + "due:" + value + text[loc[1]:]
	} else if sessions.base == "" {
		text = withDue(text, following)
	}
	text = repeatWaitingRe.ReplaceAllString(text, "${1}")
	text = repeatTidy(repeatStatusRe.ReplaceAllString(text, "${1}"))
	return rollPlan{rolled: true, copyText: text, days: days}
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

// rolledDateTime gives the due value of a new occurrence. A task with a
// session moves its due value to following, the next date of the rule, and
// keeps the time and the range. Otherwise the value moves by days.
func rolledDateTime(value string, days int, following string, session bool) string {
	date, _, _ := SplitDateTime(value)
	if session && date != "" {
		return following + value[len(date):]
	}
	return shiftDateTime(value, days)
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

// taskSubtree gives the line numbers of the task at targetIndex and the end of
// its block: the lines after the task up to end belong to it.
func taskSubtree(body string, targetIndex int, dialect TaskDialect) (at, end int, ok bool) {
	bare := repeatBareLines(strings.Split(body, "\n"))
	numbers := taskLineNumbersIn(bare, dialect)
	if targetIndex < 0 || targetIndex >= len(numbers) {
		return 0, 0, false
	}
	at = numbers[targetIndex]
	return at, taskBlockEnd(bare, at, leadingIndentWidth(bare[at])), true
}
