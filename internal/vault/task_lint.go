package vault

import (
	"os"
	"sort"
	"strings"
)

// TaskTokenProblem is one task token that the parsers set aside: a time that
// is not a valid `HH:MM`, a `@repeat:` rule outside the grammar, a `@skip:`
// item that is not a real day, a forward range or a name that an event note
// answers, or an `@until:` value that does not end a rule. Line is 1-based,
// and Token is the token as written, for example `due:2026-11-03T25:00`.
// Reason says why the parsers ignore the token.
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
	if _, _, ok := Frontmatter(body); ok {
		if _, isTask := parseTaskFile(path, "", FolderInbox, body, DialectApp); isTask {
			lintFrontmatter(path, lines, names, add, addWhy)
		}
	}
	lintCheckboxes(lines, names, add, addWhy)
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
	untilBeforeStart = "the until date is before the due date, so the rule has no next date"
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

func lintCheckboxes(lines []string, names *skipNameIndex, add func(line int, token string), addWhy func(line int, token, reason string)) {
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
			continue
		}
		tail := strings.TrimPrefix(m[3], "]")
		for _, tm := range inlineDueRe.FindAllStringSubmatch(tail, -1) {
			if badDateTime(tm[1]) {
				add(i+1, "due:"+tm[1])
			}
		}
		for _, tm := range repeatFieldRe.FindAllStringSubmatch(tail, -1) {
			if badRepeat(tm[1]) {
				add(i+1, "@repeat:"+tm[1])
			}
		}
		lintInlineSkip(i+1, tail, names, addWhy)
		lintInlineUntil(i+1, tail, addWhy)
	}
}

// lintInlineUntil checks the `@until:` fields of one checkbox line. The first
// one is read against the line's own `@repeat:` rule and `due:` date, and each
// later one is set aside.
func lintInlineUntil(line int, tail string, addWhy func(line int, token, reason string)) {
	tokens := untilFieldRe.FindAllStringSubmatch(tail, -1)
	if len(tokens) == 0 {
		return
	}
	ruleOK := false
	if rm := repeatFieldRe.FindStringSubmatch(tail); rm != nil {
		_, ruleOK = ParseRepeatRule(rm[1])
	}
	due := ""
	if dm := inlineDueRe.FindStringSubmatch(tail); dm != nil {
		due, _, _ = SplitDateTime(dm[1])
	}
	if reason := untilProblem(tokens[0][1], due, ruleOK); reason != "" {
		addWhy(line, "@until:"+tokens[0][1], reason)
	}
	for _, tm := range tokens[1:] {
		addWhy(line, "@until:"+tm[1], untilSecond)
	}
}

// lintFrontmatter checks the `due`, `repeat`, `skip` and `until` keys of
// the frontmatter at the top of lines. A list under a key, one item per line,
// is read as that key's values.
func lintFrontmatter(path string, lines []string, names *skipNameIndex, add func(line int, token string), addWhy func(line int, token, reason string)) {
	key := ""
	var due, repeat, until string
	untilLine := 0
	defer func() {
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
		case "due":
			if v := Unquote(value); badDateTime(v) {
				add(i+1, key+":"+v)
			} else if due == "" {
				due = v
			}
		case "repeat":
			if v := Unquote(value); v != "" && badRepeat(strings.ToLower(v)) {
				add(i+1, key+":"+v)
			} else if repeat == "" {
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

// ScanTaskTokenProblems lints every live note that the Tasks views read, in
// path order.
func (v *Vault) ScanTaskTokenProblems() ([]TaskTokenProblem, error) {
	// A vault whose events cannot be read still gets every other check.
	events, _ := v.ScanEvents()
	names := newSkipNameIndex(events)
	excluded, included := v.Settings().TasksFolderLists()
	var out []TaskTokenProblem
	v.walkNotes(LiveFolders, false, func(f noteFile) {
		rel := v.relPosix(f.abs)
		if IsPathExcludedFromTasks(rel, excluded, included) {
			return
		}
		body, err := os.ReadFile(f.abs)
		if err != nil {
			return
		}
		out = append(out, lintNote(rel, string(body), names)...)
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
