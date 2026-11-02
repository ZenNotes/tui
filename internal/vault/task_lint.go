package vault

import (
	"os"
	"sort"
	"strings"
)

// TaskTokenProblem is one task token that the parsers set aside: a time that
// is not a valid `HH:MM`, a `repeat:` rule outside the grammar, or a `skip:`
// item that is not a real day or a forward range. Line is 1-based, and Token
// is the token as written, for example `due:2026-11-03T25:00`.
type TaskTokenProblem struct {
	Path  string
	Line  int
	Token string
}

// TaskTokenProblems lists the bad tokens of one note: those on checkbox lines
// outside fenced code and, in a task note, those in the frontmatter. A date
// that is not real is not reported, because it was never a date to begin
// with. The reserved `@name` skip items are never reported.
func TaskTokenProblems(path, body string) []TaskTokenProblem {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []TaskTokenProblem
	add := func(line int, token string) {
		out = append(out, TaskTokenProblem{Path: path, Line: line, Token: token})
	}
	lines := strings.Split(body, "\n")
	if _, _, ok := Frontmatter(body); ok {
		if _, isTask := parseTaskFile(path, "", FolderInbox, body, DialectApp); isTask {
			lintFrontmatter(lines, add)
		}
	}
	lintCheckboxes(lines, add)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// badDateTime reports whether value has a real date followed by something
// that is not a valid `HH:MM` time.
func badDateTime(value string) bool {
	date, clock := SplitDateTime(value)
	return date != "" && len(value) > len(date) && clock == ""
}

func badRepeat(value string) bool {
	_, ok := ParseRepeatRule(value)
	return !ok
}

// badSkipItems returns the items of a comma-separated skip value that
// ParseSkipItems does not accept, leaving the reserved `@name` items out.
func badSkipItems(value string) []string {
	var bad []string
	for _, raw := range strings.Split(value, ",") {
		item := strings.TrimSpace(raw)
		if item == "" || strings.HasPrefix(item, "@") {
			continue
		}
		if valid, _ := ParseSkipItems(item); len(valid) == 0 {
			bad = append(bad, item)
		}
	}
	return bad
}

func lintCheckboxes(lines []string, add func(line int, token string)) {
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
		for _, tm := range inlineScheduledRe.FindAllStringSubmatch(tail, -1) {
			if badDateTime(tm[1]) {
				add(i+1, "scheduled:"+tm[1])
			}
		}
		for _, tm := range inlineRepeatRe.FindAllStringSubmatch(tail, -1) {
			if badRepeat(tm[1]) {
				add(i+1, "repeat:"+tm[1])
			}
		}
		for _, tm := range inlineSkipRe.FindAllStringSubmatch(tail, -1) {
			for _, item := range badSkipItems(tm[1]) {
				add(i+1, "skip:"+item)
			}
		}
	}
}

// lintFrontmatter checks the `due`, `scheduled`, `repeat` and `skip` keys of
// the frontmatter at the top of lines. A list under a key, one item per line,
// is read as that key's values.
func lintFrontmatter(lines []string, add func(line int, token string)) {
	key := ""
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
		case "due", "scheduled":
			if v := Unquote(value); badDateTime(v) {
				add(i+1, key+":"+v)
			}
		case "repeat":
			if v := Unquote(value); v != "" && badRepeat(strings.ToLower(v)) {
				add(i+1, key+":"+v)
			}
		case "skip":
			for _, part := range strings.Split(value, ",") {
				for _, item := range badSkipItems(Unquote(strings.TrimSpace(part))) {
					add(i+1, key+":"+item)
				}
			}
		}
	}
}

// ScanTaskTokenProblems lints every live note that the Tasks views read, in
// path order.
func (v *Vault) ScanTaskTokenProblems() ([]TaskTokenProblem, error) {
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
		out = append(out, TaskTokenProblems(rel, string(body))...)
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
