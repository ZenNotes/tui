package vault

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TaskTokenProblem is one token that the parsers set aside: a time that is
// not a valid `HH:MM`, a `@repeat:` rule outside the grammar, a `@skip:` item
// that is not a real day, a forward range or a name that an event note
// answers, an `@until:` value that does not end a rule, a `when:` value that
// is not valid, a `task:` link that no task answers, or a block id that ends
// more than one line. Line is 1-based, and Token is the token as written, for
// example `due:2026-11-03T25:00`. Reason says why the parsers ignore the
// token.
type TaskTokenProblem struct {
	Path   string
	Line   int
	Token  string
	Reason string
}

// TaskTokenProblems lists the bad tokens of one note: those on checkbox lines
// outside fenced code and, in a task note, those in the frontmatter. A date
// that is not real is not reported, because it was never a date to begin
// with. A skip name is not checked here, because one note cannot tell which
// event notes exist. TaskTokenProblemsWithEvents checks it.
func TaskTokenProblems(path, body string) []TaskTokenProblem {
	return lintNote(path, body, nil)
}

// TaskTokenProblemsWithEvents lists the bad tokens of one note, and also the
// skip names that no single event note answers. events are the events of the
// whole vault.
func TaskTokenProblemsWithEvents(path, body string, events []Event) []TaskTokenProblem {
	return lintNote(path, body, newSkipNameIndex(events))
}

// lintNote does the work of both functions above. names is nil when skip
// names are not checked.
func lintNote(path, body string, names *skipNameIndex) []TaskTokenProblem {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []TaskTokenProblem
	add := func(line int, token string) {
		out = append(out, TaskTokenProblem{Path: path, Line: line, Token: token, Reason: tokenReason(token)})
	}
	addWhy := func(line int, token, reason string) {
		out = append(out, TaskTokenProblem{Path: path, Line: line, Token: token, Reason: reason})
	}
	lines := strings.Split(body, "\n")
	eventsFrom := 0
	if block, after, ok := Frontmatter(body); ok {
		eventsFrom = strings.Count(body[:len(body)-len(after)], "\n")
		_, isTask := parseTaskFile(path, "", FolderInbox, body, DialectApp)
		isEvent := hasPageType(block, EventNotePageType)
		if isTask || isEvent {
			lintFrontmatter(path, lines, isEvent && !isTask, names, add, addWhy)
		}
		switch strings.ToLower(Unquote(firstScalar(ParseFrontmatterFields(block)["events"]))) {
		case "false", "off":
			eventsFrom = -1
		}
	}
	lintCheckboxes(lines, eventsFrom, names, add, addWhy)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// tokenReason says why the parsers ignore the token, which has the form
// `key:value`.
func tokenReason(token string) string {
	key, _, _ := strings.Cut(token, ":")
	switch key {
	case "due":
		return dueReason(token)
	case "repeat", "@repeat":
		return "the rule is not valid"
	case "skip", "@skip":
		return "the item is not a real day, a forward range or a name"
	case "until", "@until":
		return "the value is not a real date without a time"
	}
	return "the value is not valid"
}

// dueReason says why a `due:` token is set aside. The token has the form
// `due:value`.
func dueReason(token string) string {
	_, value, _ := strings.Cut(token, ":")
	date, start, _ := SplitDateTime(value)
	switch {
	case strings.HasPrefix(value[len(date):], "/"), strings.HasPrefix(value[len(date):], ".."):
		return "a due value holds one day, so only its start date is used"
	case start == "" && strings.HasPrefix(value[len(date):], "-"),
		start == "" && strings.HasPrefix(value[len(date):], "T-"):
		return "an end time needs a start time, so the time is ignored"
	case start != "" && strings.Contains(value[len(date):], "-"):
		return "the end time is not a valid HH:MM later than the start time, so it is ignored"
	}
	return "the time is not a valid HH:MM"
}

// badDateTime reports whether value has a real date followed by something
// that is not a valid time or a valid time range.
func badDateTime(value string) bool {
	date, _, _, ok := parseDateTimeRange(value)
	return date != "" && !ok
}

func badRepeat(value string) bool {
	_, ok := ParseRepeatRule(value)
	return !ok
}

// badSkipItems returns the items of a skip value that are neither a real day,
// a forward range nor a name. A name is not checked here.
func badSkipItems(value string) []string {
	var bad []string
	for _, raw := range strings.Split(value, ",") {
		item := strings.TrimSpace(raw)
		if item == "" || isSkipName(item) {
			continue
		}
		if valid, _, _ := ParseSkipItems(item); len(valid) == 0 {
			bad = append(bad, item)
		}
	}
	return bad
}

// skipDotsReason is the reason for a `@skip:` value that dots follow.
const skipDotsReason = "a range is written start/end with a slash, so only the first day is used"

// nameProblem says why a skip name gives no period, or "" when it does or when
// names is nil. path is the note that holds the name.
func nameProblem(names *skipNameIndex, name, path string) string {
	if names == nil {
		return ""
	}
	return names.problem(name, path)
}

// lintInlineSkip checks the `@skip:` fields of one checkbox line. A value that
// dots follow is a range in a form that is not read. A name is checked when
// names is not nil.
func lintInlineSkip(line int, tail string, names *skipNameIndex, addWhy func(line int, token, reason string)) {
	for _, m := range skipFieldRe.FindAllStringSubmatchIndex(tail, -1) {
		value := tail[m[2]:m[3]]
		if rest := tail[m[3]:]; strings.HasPrefix(rest, "..") {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			addWhy(line, "@skip:"+value+rest[:end], skipDotsReason)
			continue
		}
		if isSkipName(value) {
			if reason := nameProblem(names, value, ""); reason != "" {
				addWhy(line, "@skip:"+value, reason)
			}
			continue
		}
		for _, item := range badSkipItems(value) {
			addWhy(line, "@skip:"+item, tokenReason("skip:"+item))
		}
	}
}

// Reasons for an `until` value that is a real date but still ends no rule.
const (
	untilNeedsRule   = "an until date needs a valid repeat rule, so it is ignored"
	untilBeforeStart = "the until date is before the first date, so the rule has no next date"
	untilSecond      = "only the first until value is used, so this one is ignored"
)

// untilProblem says why an `until` value does not end a rule, or "" when it
// does. due is the task's own date, or "" when it has none, and ruleOK says
// whether the task has a valid `repeat` rule.
func untilProblem(value, due string, ruleOK bool) string {
	switch {
	case validUntil(value) == "":
		return tokenReason("until:" + value)
	case !ruleOK:
		return untilNeedsRule
	case due != "" && value < due:
		return untilBeforeStart
	}
	return ""
}

func lintCheckboxes(lines []string, eventsFrom int, names *skipNameIndex, add func(line int, token string), addWhy func(line int, token, reason string)) {
	fenceRe, lineRe := taskFenceRe, DialectApp.lineRe()
	inFence, fenceMarker := false, ""
	for i, line := range lines {
		if fm := fenceRe.FindStringSubmatch(line); fm != nil {
			if !inFence {
				inFence, fenceMarker = true, fm[2]
			} else if fm[2] == fenceMarker {
				inFence, fenceMarker = false, ""
			}
			continue
		}
		if inFence {
			continue
		}
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			if eventsFrom >= 0 && i >= eventsFrom {
				lintEventLine(i+1, line, names, add, addWhy)
			}
			continue
		}
		tail := strings.TrimPrefix(m[3], "]")
		for _, tm := range inlineDueRe.FindAllStringSubmatch(tail, -1) {
			if badDateTime(tm[1]) {
				add(i+1, "due:"+tm[1])
			}
		}
		for _, tm := range eventWhenRe.FindAllStringSubmatch(maskInlineCode(tail), -1) {
			if looksDated(tm[1]) {
				addWhy(i+1, "when:"+tm[1], checkboxWhenReason)
			}
		}
		lintRepeatFields(i+1, tail, repeatFieldRe, false, add, addWhy)
		lintInlineSkip(i+1, tail, names, addWhy)
		lintInlineUntil(i+1, tail, "", addWhy)
	}
}

// lintInlineUntil checks the `@until:` fields of one checkbox line. The first
// one is read against the line's own `@repeat:` rule and `due:` date, and each
// later one is set aside. start is the first date of an event line, or "" for
// a task line, which has its `due:` date instead.
func lintInlineUntil(line int, tail, start string, addWhy func(line int, token, reason string)) {
	tokens := untilFieldRe.FindAllStringSubmatch(tail, -1)
	if len(tokens) == 0 {
		return
	}
	ruleOK := false
	if rm := repeatFieldRe.FindStringSubmatch(tail); rm != nil {
		_, ruleOK = ParseRepeatRule(rm[1])
	}
	due := start
	if dm := inlineDueRe.FindStringSubmatch(tail); due == "" && dm != nil {
		due, _, _ = SplitDateTime(dm[1])
	}
	if reason := untilProblem(tokens[0][1], due, ruleOK); reason != "" {
		addWhy(line, "@until:"+tokens[0][1], reason)
	}
	for _, tm := range tokens[1:] {
		addWhy(line, "@until:"+tm[1], untilSecond)
	}
}

// Reasons for the event kinds of problem.
const (
	checkboxWhenReason = "a task line does not read when:, so the token is plain text; write an event as a list item without a checkbox"
	manyWhenReason     = "an event line needs exactly one when: token, so this line is not an event"
	afterReason        = "an event does not use after-, so the rule is ignored"
	noWhenReason       = "the value does not start with a real date in the form YYYY-MM-DD, so there is no event"
	noteNoWhenReason   = "an event note needs a when value that starts with a real date in the form YYYY-MM-DD, so there is no event"
)

// looksDated reports whether a `when:` value starts like a date, so that a
// value such as `tomorrow` in prose is not a mistake.
func looksDated(value string) bool {
	return len(value) > 5 && value[4] == '-' && value[0] >= '0' && value[0] <= '9' &&
		value[1] >= '0' && value[1] <= '9' && value[2] >= '0' && value[2] <= '9' &&
		value[3] >= '0' && value[3] <= '9' && value[5] >= '0' && value[5] <= '9'
}

// maskInlineCode blanks the inline code of text and keeps every offset.
func maskInlineCode(text string) string {
	return inlineCodeRe.ReplaceAllStringFunc(text, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
}

// whenProblem says why a `when:` value is not read in full, or "" when it is.
// inline is true for a list line and false for the frontmatter of an event
// note. It also gives false when the value makes no event at all.
func whenProblem(when string, inline bool) (reason string, isEvent bool) {
	date, _, _, _, problem := ParseWhenValue(when)
	if date == "" || inline && !startsWithRealDate(when) {
		return noWhenReason, false
	}
	return problem, true
}

// lintRepeatFields checks the `@repeat:` fields of one line that re finds. A
// value that the reader cuts at a character it does not read is listed with
// the full token. An event does not use `after-`.
func lintRepeatFields(line int, tail string, re *regexp.Regexp, event bool, add func(line int, token string), addWhy func(line int, token, reason string)) {
	for _, m := range re.FindAllStringSubmatchIndex(tail, -1) {
		value := tail[m[2]:m[3]]
		rest := tail[m[3]:]
		if end := strings.IndexAny(rest, " \t"); end >= 0 {
			rest = rest[:end]
		}
		switch {
		case strings.ContainsAny(rest, "0123456789") || strings.IndexFunc(rest, unicode.IsLetter) >= 0:
			cut, _ := utf8.DecodeRuneInString(rest)
			addWhy(line, "@repeat:"+value+rest, fmt.Sprintf(
				"the rule ends at %q, so only the part before it is read; join the days with a slash, for example mon/thu", string(cut)))
		case badRepeat(value):
			add(line, "@repeat:"+value)
		case event && strings.HasPrefix(strings.ToLower(value), "after-"):
			addWhy(line, "@repeat:"+value, afterReason)
		}
	}
}

// lintEventLine checks one line that is not a task. A list item with one
// `when:` token that starts with a real date is an event, and its `when:`,
// `@repeat:`, `@skip:` and `@until:` tokens are checked. A list item with
// several `when:` tokens is not an event.
func lintEventLine(line int, text string, names *skipNameIndex, add func(line int, token string), addWhy func(line int, token, reason string)) {
	m := eventListItemRe.FindStringSubmatch(text)
	if m == nil || eventCheckboxRe.MatchString(m[1]) {
		return
	}
	body, _ := splitBlockID(m[1])
	masked := maskInlineCode(body)
	whens := eventWhenRe.FindAllStringSubmatch(masked, -1)
	dated := false
	for _, w := range whens {
		dated = dated || looksDated(w[1])
	}
	if !dated {
		return
	}
	if len(whens) > 1 {
		addWhy(line, "when:"+whens[0][1], manyWhenReason)
		return
	}
	when := whens[0][1]
	reason, isEvent := whenProblem(when, true)
	if reason != "" {
		addWhy(line, "when:"+when, reason)
	}
	if !isEvent {
		return
	}
	start, _, _, _, _ := ParseWhenValue(when)
	lintRepeatFields(line, masked, eventRepeatRe, true, add, addWhy)
	lintInlineSkip(line, masked, names, addWhy)
	lintInlineUntil(line, masked, start, addWhy)
}

// lintFrontmatter checks the keys of the frontmatter at the top of lines: the
// `due`, `repeat`, `skip` and `until` keys of a task note, and the `when`,
// `repeat`, `skip` and `until` keys of an event note. A list under a key, one
// item per line, is read as that key's values.
func lintFrontmatter(path string, lines []string, event bool, names *skipNameIndex, add func(line int, token string), addWhy func(line int, token, reason string)) {
	key := ""
	var due, repeat, until, when string
	untilLine, whenLine, typeLine := 0, 0, 0
	defer func() {
		if event {
			switch reason, _ := whenProblem(when, false); {
			case whenLine == 0 && typeLine > 0:
				addWhy(typeLine, "page-type:"+EventNotePageType, noteNoWhenReason)
			case whenLine > 0 && reason != "":
				if reason == noWhenReason {
					reason = noteNoWhenReason
				}
				addWhy(whenLine, "when:"+when, reason)
			}
			due, _, _, _, _ = ParseWhenValue(when)
		}
		if untilLine == 0 {
			return
		}
		_, ruleOK := ParseRepeatRule(strings.ToLower(repeat))
		date, _, _ := SplitDateTime(due)
		if reason := untilProblem(until, date, ruleOK); reason != "" {
			addWhy(untilLine, "until:"+until, reason)
		}
	}()
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "---" && !fmLeadWsRe.MatchString(line) {
			return
		}
		value := ""
		if li := fmListItemRe.FindStringSubmatch(line); key != "" && li != nil && fmLeadWsRe.MatchString(line) {
			value = li[1]
		} else if kv := fmKvRe.FindStringSubmatch(line); kv != nil {
			key, value = strings.ToLower(kv[1]), kv[2]
		} else {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		switch key {
		case "page-type":
			if typeLine == 0 {
				typeLine = i + 1
			}
		case "when":
			if event && whenLine == 0 {
				when, whenLine = Unquote(value), i+1
			}
		case "due":
			if event {
				continue
			}
			if v := Unquote(value); badDateTime(v) {
				add(i+1, key+":"+v)
			} else if due == "" {
				due = v
			}
		case "repeat":
			v := Unquote(value)
			switch {
			case v != "" && badRepeat(strings.ToLower(v)):
				add(i+1, key+":"+v)
			case event && strings.HasPrefix(strings.ToLower(v), "after-"):
				addWhy(i+1, key+":"+v, afterReason)
			case repeat == "":
				repeat = v
			}
		case "until":
			if v := Unquote(value); v != "" && untilLine == 0 {
				until, untilLine = v, i+1
			}
		case "skip":
			for _, part := range strings.Split(value, ",") {
				item := Unquote(strings.TrimSpace(part))
				if isSkipName(item) {
					if reason := nameProblem(names, item, path); reason != "" {
						addWhy(i+1, key+":"+item, reason)
					}
					continue
				}
				for _, bad := range badSkipItems(item) {
					add(i+1, key+":"+bad)
				}
			}
		}
	}
}

// Reasons for a link or a block id that the whole vault decides.
const (
	danglingLinkReason = "no single task answers this link, so the session has no task"
	duplicateIDReason  = "more than one line ends with this block id, so a link to it can name the wrong line"
)

// linkProblems lists the events of one note whose `task:` link no task
// answers. lines holds the note, to find the `task` key of an event note.
func linkProblems(path string, lines []string, events []Event, index *sessionIndex) []TaskTokenProblem {
	var out []TaskTokenProblem
	for _, e := range events {
		if e.TaskLink == "" || index.resolve(e) >= 0 {
			continue
		}
		line := e.Line + 1
		if e.Kind == EventKindNote {
			line = 1
			for i := 1; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
				if kv := fmKvRe.FindStringSubmatch(lines[i]); kv != nil && strings.ToLower(kv[1]) == "task" {
					line = i + 1
					break
				}
			}
		}
		out = append(out, TaskTokenProblem{Path: path, Line: line, Token: "task:" + e.TaskLink, Reason: danglingLinkReason})
	}
	return out
}

// blockIDLines gives the 1-based lines of a note that end with a block id,
// by id in lower case. Fenced code and frontmatter are not read.
func blockIDLines(lines []string, from int, into map[string][]int) {
	inFence, fenceMarker := false, ""
	for i := from; i < len(lines); i++ {
		if fm := taskFenceRe.FindStringSubmatch(lines[i]); fm != nil {
			if !inFence {
				inFence, fenceMarker = true, fm[2]
			} else if fm[2] == fenceMarker {
				inFence, fenceMarker = false, ""
			}
			continue
		}
		if inFence {
			continue
		}
		if _, id := splitBlockID(strings.TrimRight(lines[i], "\r")); id != "" {
			into[strings.ToLower(id)] = append(into[strings.ToLower(id)], i+1)
		}
	}
}

// ScanTaskTokenProblems lints every live note that the Tasks views read, in
// path order. Besides the tokens of each note, it lists the `task:` links
// that no task answers and the block ids that end more than one line.
func (v *Vault) ScanTaskTokenProblems() ([]TaskTokenProblem, error) {
	// A vault whose events cannot be read still gets every other check.
	events, _ := v.ScanEvents()
	tasks, _ := v.ScanTasks(ParseTasksOptions{})
	index := newSessionIndex(tasks)
	names := newSkipNameIndex(events)
	eventsByPath := map[string][]Event{}
	for _, e := range events {
		eventsByPath[e.Path] = append(eventsByPath[e.Path], e)
	}
	excluded, included := v.Settings().TasksFolderLists()
	var out []TaskTokenProblem
	type idLine struct {
		path string
		line int
	}
	ids := map[string][]idLine{}
	v.walkNotes(LiveFolders, false, func(f noteFile) {
		rel := v.relPosix(f.abs)
		if IsPathExcludedFromTasks(rel, excluded, included) {
			return
		}
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return
		}
		body := string(raw)
		out = append(out, lintNote(rel, body, names)...)
		lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
		out = append(out, linkProblems(rel, lines, eventsByPath[rel], index)...)
		from := 0
		if _, after, ok := Frontmatter(strings.ReplaceAll(body, "\r\n", "\n")); ok {
			from = len(lines) - strings.Count(after, "\n") - 1
		}
		found := map[string][]int{}
		blockIDLines(lines, from, found)
		for id, at := range found {
			for _, n := range at {
				ids[id] = append(ids[id], idLine{rel, n})
			}
		}
	})
	for id, at := range ids {
		if len(at) < 2 {
			continue
		}
		for _, p := range at {
			out = append(out, TaskTokenProblem{Path: p.path, Line: p.line, Token: "^" + id, Reason: duplicateIDReason})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
