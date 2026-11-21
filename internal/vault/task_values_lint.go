package vault

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// TaskValueCheck names one kind of task value that no app reads.
type TaskValueCheck string

const (
	// TaskValueRepeat is an `@repeat:` rule that parseRepeatRule does not read.
	TaskValueRepeat TaskValueCheck = "repeat"
	// TaskValueDue is a `due:` value that is not a real `YYYY-MM-DD` day.
	TaskValueDue TaskValueCheck = "due"
	// TaskValueBlockID is a `^id` that ends more than one line of a note.
	TaskValueBlockID TaskValueCheck = "block-id"
)

var taskValueCheckOrder = map[TaskValueCheck]int{TaskValueRepeat: 0, TaskValueDue: 1, TaskValueBlockID: 2}

// TaskValueProblem is a value that the apps ignore, and what they do instead.
// Lines are 1-based. Only a block id has more than one.
type TaskValueProblem struct {
	Check  TaskValueCheck
	Path   string
	Lines  []int
	Value  string
	Reason string
}

const (
	repeatUnreadReason = "no app reads this rule, so the task does not repeat"
	dueUnreadReason    = "no app reads this as a date, so the task has no due date"
	dueNoSuchDayReason = "this day does not exist, so zn has no due date, and the desktop app can read another day"
	blockIDReason      = "links go to the first line with this id"
)

// lintWorkers is how many notes LintTaskValues reads at once, as ScanTasks does.
const lintWorkers = 32

// dayOutOfMonthRe is a day that every month may lack: the 29th to the 31st.
var dayOutOfMonthRe = regexp.MustCompile(`^\d{4}-(?:0[1-9]|1[0-2])-(?:29|30|31)$`)

// dueReason says what the apps do with a `due` value that is not a real day. The
// desktop app reads a 29th to a 31st that a month lacks as a day in the next
// month, and zn reads no date.
func dueReason(value string) string {
	if dayOutOfMonthRe.MatchString(value) {
		return dueNoSuchDayReason
	}
	return dueUnreadReason
}

// LintTaskValues reads the live notes and reports the task values that no app
// reads. The `@repeat:` and `due` checks read the notes that ScanTasks reads,
// with the same exclusions. The block id check reads every live note, as link
// lookup does. It changes no note. When ctx ends, it returns the problems found
// so far and the error of ctx.
func (v *Vault) LintTaskValues(ctx context.Context) ([]TaskValueProblem, error) {
	excluded, included := v.Settings().TasksFolderLists()
	files := []noteFile{}
	v.walkNotes(LiveFolders, false, func(f noteFile) { files = append(files, f) })
	groups := make([][]TaskValueProblem, len(files))
	sem := make(chan struct{}, lintWorkers)
	var wg sync.WaitGroup
	for i, f := range files {
		if ctx.Err() != nil {
			break
		}
		rel := v.relPosix(f.abs)
		wg.Add(1)
		go func(i int, f noteFile, rel string, tasksOut bool) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			body, err := os.ReadFile(f.abs)
			if err != nil {
				return
			}
			title := strings.TrimSuffix(filepath.Base(f.abs), filepath.Ext(f.abs))
			groups[i] = lintNoteTaskValues(rel, title, f.folder, string(body), tasksOut)
		}(i, f, rel, IsPathExcludedFromTasks(rel, excluded, included))
	}
	wg.Wait()
	out := []TaskValueProblem{}
	for _, g := range groups {
		out = append(out, g...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Check != b.Check {
			return taskValueCheckOrder[a.Check] < taskValueCheckOrder[b.Check]
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Lines[0] < b.Lines[0]
	})
	return out, ctx.Err()
}

// lintNoteTaskValues is the problems of one note. A note that the task scan
// leaves out (tasksOut) has block id problems only.
func lintNoteTaskValues(path, title string, folder NoteFolder, body string, tasksOut bool) []TaskValueProblem {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	out := []TaskValueProblem{}
	if !tasksOut {
		out = append(out, lintInlineAndFrontmatter(path, title, folder, normalized)...)
	}
	return append(out, lintBlockIDs(path, lines)...)
}

// lintInlineAndFrontmatter checks the `@repeat:` and `due` values. The note
// parses once for its tasks. The frontmatter `due` counts only when a task reads
// it: a task note, or an inline task that has no due date of its own.
func lintInlineAndFrontmatter(path, title string, folder NoteFolder, normalized string) []TaskValueProblem {
	out := []TaskValueProblem{}
	hasFileTask, inherits := false, false
	for _, task := range ParseTasks(path, title, folder, normalized, ParseTasksOptions{Dialect: DialectCLI}) {
		if task.Kind == "file" {
			hasFileTask = true
			continue
		}
		m := taskLineRe.FindStringSubmatch(task.RawText)
		if m == nil {
			continue
		}
		tail := strings.TrimPrefix(m[3], "]")
		line := task.LineNumber + 1
		if field := repeatFieldRe.FindStringSubmatch(tail); field != nil {
			if _, ok := parseRepeatRule(field[1]); !ok {
				out = append(out, TaskValueProblem{TaskValueRepeat, path, []int{line}, "@repeat:" + field[1], repeatUnreadReason})
			}
		}
		due := inlineDueRe.FindStringSubmatch(tail)
		if due == nil || !IsValidISODate(due[1]) {
			inherits = true
		}
		if due != nil && !IsValidISODate(due[1]) {
			out = append(out, TaskValueProblem{TaskValueDue, path, []int{line}, "due:" + due[1], dueReason(due[1])})
		}
	}
	if hasFileTask || inherits {
		out = append(out, lintFrontmatterDue(path, normalized, hasFileTask, inherits)...)
	}
	return out
}

// lintFrontmatterDue is the frontmatter `due` that a reader loses. An inline
// task reads the last `due` line that is a real day, as parseNoteDefaults does.
// A task note reads the first scalar of the key, as parseTaskFile does.
func lintFrontmatterDue(path, normalized string, fileTask, inline bool) []TaskValueProblem {
	block, _, ok := Frontmatter(normalized)
	if !ok {
		return nil
	}
	type dueLine struct {
		line int
		val  string
		// top is true for a key at the start of its line, the only form that
		// ParseFrontmatterFields reads.
		top bool
	}
	dues := []dueLine{}
	anyValid := false
	for i, text := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(trimmed, ':')
		if colon < 1 || strings.ToLower(strings.TrimSpace(trimmed[:colon])) != "due" {
			continue
		}
		val := Unquote(strings.TrimSpace(trimmed[colon+1:]))
		if val == "" {
			continue
		}
		anyValid = anyValid || IsValidISODate(val)
		// The block starts on the second line of the note.
		kv := fmKvRe.FindStringSubmatch(text)
		dues = append(dues, dueLine{i + 2, val, kv != nil && strings.ToLower(kv[1]) == "due"})
	}
	out := []TaskValueProblem{}
	reported := map[int]bool{}
	report := func(line int, val string) {
		if !reported[line] {
			reported[line] = true
			out = append(out, TaskValueProblem{TaskValueDue, path, []int{line}, "due:" + val, dueReason(val)})
		}
	}
	if inline && !anyValid {
		for _, d := range dues {
			report(d.line, d.val)
		}
	}
	if fileTask && len(dues) > 0 {
		scalar := firstScalar(ParseFrontmatterFields(block)["due"])
		if scalar != "" && normalizeDueDate(scalar) == "" {
			// A later key takes the place of an earlier one, so the value is on
			// the last top-level `due` line.
			line := dues[0].line
			for _, d := range dues {
				if d.top {
					line = d.line
				}
			}
			report(line, scalar)
		}
	}
	return out
}

// lintBlockIDs is each `^id` that ends more than one line. Ids match without
// regard to case, as link lookup matches them.
func lintBlockIDs(path string, lines []string) []TaskValueProblem {
	owned := markdownOwnedLines(lines)
	seen := map[string][]int{}
	first := map[string]string{}
	order := []string{}
	for i, text := range lines {
		if !owned[i] {
			continue
		}
		m := blockIDRe.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		id := text[m[2]:m[3]]
		key := strings.ToLower(id)
		if _, ok := seen[key]; !ok {
			first[key] = id
			order = append(order, key)
		}
		seen[key] = append(seen[key], i+1)
	}
	out := []TaskValueProblem{}
	for _, key := range order {
		if len(seen[key]) > 1 {
			out = append(out, TaskValueProblem{TaskValueBlockID, path, seen[key], "^" + first[key], blockIDReason})
		}
	}
	return out
}
