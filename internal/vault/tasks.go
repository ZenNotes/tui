package vault

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Task-line grammar. Group 1 is everything up to and including the `[`,
// group 2 the state character, group 3 the `]` and the rest. A leading
// `(?:>\s*)*` is a blockquote prefix, unrelated to the `>` state char. This
// is the app's TASK_LINE_RE, so the CLI counts task indexes the way the
// editor's toggles do and the two can never disagree about which line
// `inbox/Note.md#3` is.
var (
	taskFenceRe      = regexp.MustCompile("^(\\s*)(```|~~~)")
	taskLineRe       = regexp.MustCompile(`^(\s*(?:>\s*)*(?:[-+*]|\d+[.)])\s+\[)( |x|X|>|-|/)(\].*)$`)
	cliFenceRe       = regexp.MustCompile("^(\\s{0,3})(`{3,}|~{3,})")
	cliTaskLineRe    = regexp.MustCompile(`^(\s*[-*+]\s+\[)( |x|X|>|-|/)(\].*)$`)
	inlineDueRe      = regexp.MustCompile(`(?i)(?:^|\s)due:\s*(\S+)`)
	inlinePriorityRe = regexp.MustCompile(`(?i)(?:^|\s)!(high|med|medium|low|h|m|l)\b`)
	inlineWaitingRe  = regexp.MustCompile(`(?i)(?:^|\s)@waiting\b`)
	inlineFieldRe    = regexp.MustCompile(`(?i)(?:^|\s)@([a-z][a-z0-9_-]*):([\p{L}\d][\p{L}\d/_-]*)`)
	inlineTagRe      = regexp.MustCompile(`(?:^|\s)#([\p{L}\d][\p{L}\d/_-]*)`)
	isoDateRe        = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	priorityTokenRe  = regexp.MustCompile(`(?i)(^|\s)!(?:high|med|medium|low|h|m|l)\b`)
	dueTokenRe       = regexp.MustCompile(`(?i)(^|\s)due:\s*\S+`)
	waitingTokenRe   = regexp.MustCompile(`(?i)(^|\s)@waiting\b`)
	fieldKeyRe       = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	multiSpaceRe     = regexp.MustCompile(`\s{2,}`)
	trailingWsRe     = regexp.MustCompile(`\s+$`)
	leadingWsRe      = regexp.MustCompile(`^[ \t]*`)
)

func (d TaskDialect) fenceRe() *regexp.Regexp {
	if d == DialectCLI {
		return cliFenceRe
	}
	return taskFenceRe
}

func (d TaskDialect) lineRe() *regexp.Regexp {
	if d == DialectCLI {
		return cliTaskLineRe
	}
	return taskLineRe
}

// TaskFileTag is the frontmatter tag that marks a whole note as a task.
const TaskFileTag = "task"

var doneStatuses = map[string]bool{"done": true, "complete": true, "completed": true, "x": true}
var cancelledStatuses = map[string]bool{"cancelled": true, "canceled": true}
var inProgressStatuses = map[string]bool{
	"in-progress": true, "in progress": true, "inprogress": true,
	"doing": true, "started": true, "wip": true,
}

// IsValidISODate accepts `YYYY-MM-DD` that names a real day.
func IsValidISODate(s string) bool {
	if !isoDateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// NormalizePriority maps every accepted spelling onto high, med or low.
func NormalizePriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high", "h":
		return "high"
	case "med", "medium", "normal", "m":
		return "med"
	case "low", "l":
		return "low"
	}
	return ""
}

func normalizeDueDate(raw string) string {
	v := Unquote(strings.TrimSpace(raw))
	if IsValidISODate(v) {
		return v
	}
	return ""
}

// Note-level participation in the Tasks system, from the frontmatter
// `tasks:` key. Values are exact after lower-casing; anything unknown is
// "all", the behavior every vault had before the key existed.
const (
	tasksModeAll      = "all"
	tasksModeNoteOnly = "note-only"
	tasksModeNone     = "none"
)

// NoteTasksMode reads the frontmatter `tasks:` value.
func NoteTasksMode(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "false", "off":
		return tasksModeNone
	case "note":
		return tasksModeNoteOnly
	}
	return tasksModeAll
}

type noteDefaults struct {
	Due       string
	Priority  string
	Status    string
	TasksMode string
}

func parseNoteDefaults(body string) noteDefaults {
	d := noteDefaults{TasksMode: tasksModeAll}
	block, _, ok := Frontmatter(body)
	if !ok {
		return d
	}
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(trimmed, ':')
		if colon < 1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:colon]))
		val := Unquote(strings.TrimSpace(trimmed[colon+1:]))
		switch key {
		case "due":
			if IsValidISODate(val) {
				d.Due = val
			}
		case "priority":
			if p := NormalizePriority(val); p != "" {
				d.Priority = p
			}
		case "status":
			d.Status = strings.ToLower(val)
		case "tasks":
			d.TasksMode = NoteTasksMode(val)
		}
	}
	return d
}

func firstScalar(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// parseTaskFile returns the whole-note task when the frontmatter `tags`
// include `task`. Metadata comes from frontmatter; the body is free-form.
func parseTaskFile(path, title string, folder NoteFolder, body string, dialect TaskDialect) (Task, bool) {
	block, _, ok := Frontmatter(body)
	if !ok {
		return Task{}, false
	}
	fm := ParseFrontmatterFields(block)
	tags := []string{}
	hasTaskTag := false
	for _, t := range fm["tags"] {
		tag := strings.ToLower(strings.TrimPrefix(t, "#"))
		if tag == TaskFileTag {
			hasTaskTag = true
			continue
		}
		tags = append(tags, tag)
	}
	if !hasTaskTag {
		return Task{}, false
	}
	// "open" is the effective state of a note that says nothing, not a
	// custom status the author chose: only an explicit status reaches Fields.
	status := "open"
	fields := map[string]string{}
	if s := firstScalar(fm["status"]); s != "" {
		status = strings.ToLower(s)
		fields["status"] = status
	}
	content := title
	if t := strings.TrimSpace(firstScalar(fm["title"])); t != "" {
		content = t
	}
	task := Task{
		ID:            path + "#task",
		SourcePath:    path,
		Link:          BuildOpenNoteDeepLink(path),
		NoteTitle:     title,
		NoteFolder:    folder,
		LineNumber:    0,
		TaskIndex:     -1,
		Content:       content,
		Checked:       doneStatuses[status],
		Cancelled:     cancelledStatuses[status],
		InProgress:    inProgressStatuses[status],
		Due:           normalizeDueDate(firstScalar(fm["due"])),
		Priority:      NormalizePriority(firstScalar(fm["priority"])),
		Waiting:       status == "waiting",
		Fields:        fields,
		Status:        status,
		Tags:          tags,
		Kind:          "file",
		Scheduled:     normalizeDueDate(firstScalar(fm["scheduled"])),
		CompletedDate: normalizeDueDate(firstScalar(fm["completeddate"])),
	}
	if dialect == DialectCLI {
		task.Fields, task.Status = nil, ""
	}
	return task, true
}

// ParseTasks walks a markdown body and returns every task: the note's own
// task file first (when it is one), then each checkbox line outside fenced
// code, with its inline metadata parsed out. `tasks: false` in the
// frontmatter emits nothing, `tasks: note` keeps only the file task, unless
// opts asks to scan past the opt-out.
func ParseTasks(path, title string, folder NoteFolder, body string, opts ParseTasksOptions) []Task {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	defaults := parseNoteDefaults(normalized)
	mode := defaults.TasksMode
	if opts.IncludeExcluded {
		mode = tasksModeAll
	}
	out := []Task{}
	if mode != tasksModeNone {
		if fileTask, ok := parseTaskFile(path, title, folder, normalized, opts.Dialect); ok {
			out = append(out, fileTask)
		}
	}
	if mode != tasksModeAll {
		return out
	}
	lines := strings.Split(normalized, "\n")
	taskIndex := 0
	inFence := false
	fenceMarker := ""
	fenceRe, lineRe := opts.Dialect.fenceRe(), opts.Dialect.lineRe()
	for i, line := range lines {
		if fm := fenceRe.FindStringSubmatch(line); fm != nil {
			marker := fm[2]
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
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
		checkedChar := m[2]
		tail := strings.TrimPrefix(m[3], "]")
		tokens := extractTaskTokens(tail, opts.Dialect)
		due := tokens.due
		if due == "" {
			due = defaults.Due
		}
		priority := tokens.priority
		if priority == "" {
			priority = defaults.Priority
		}
		fields := tokens.fields
		if opts.Dialect == DialectCLI {
			fields = nil
		} else if _, hasStatus := fields["status"]; !hasStatus && defaults.Status != "" {
			fields["status"] = defaults.Status
		}
		content := tokens.stripped
		if content == "" {
			content = strings.TrimSpace(tail)
		}
		out = append(out, Task{
			ID:         path + "#" + strconv.Itoa(taskIndex),
			SourcePath: path,
			Link:       BuildOpenNoteDeepLink(path),
			NoteTitle:  title,
			NoteFolder: folder,
			LineNumber: i,
			TaskIndex:  taskIndex,
			RawText:    line,
			Content:    content,
			Checked:    checkedChar == "x" || checkedChar == "X",
			Cancelled:  checkedChar == "-",
			InProgress: checkedChar == "/",
			Forwarded:  checkedChar == ">",
			Due:        due,
			Priority:   priority,
			Waiting:    tokens.waiting,
			Fields:     fields,
			Status:     fields["status"],
			Tags:       tokens.tags,
		})
		taskIndex++
	}
	return out
}

type taskTokens struct {
	due, priority string
	waiting       bool
	fields        map[string]string
	tags          []string
	stripped      string
}

func extractTaskTokens(tail string, dialect TaskDialect) taskTokens {
	t := taskTokens{fields: map[string]string{}, tags: []string{}}
	stripped := tail
	if dm := inlineDueRe.FindStringSubmatch(stripped); dm != nil {
		if IsValidISODate(dm[1]) {
			t.due = dm[1]
		}
		stripped = inlineDueRe.ReplaceAllString(stripped, " ")
	}
	if pm := inlinePriorityRe.FindStringSubmatch(stripped); pm != nil {
		t.priority = NormalizePriority(pm[1])
		stripped = inlinePriorityRe.ReplaceAllString(stripped, " ")
	}
	if inlineWaitingRe.MatchString(stripped) {
		t.waiting = true
		stripped = inlineWaitingRe.ReplaceAllString(stripped, " ")
	}
	// The CLI grammar leaves `@key:value` fields in the content.
	if dialect != DialectCLI {
		for _, fm := range inlineFieldRe.FindAllStringSubmatch(stripped, -1) {
			key := strings.ToLower(fm[1])
			if _, exists := t.fields[key]; !exists {
				t.fields[key] = strings.ToLower(fm[2])
			}
		}
		if len(t.fields) > 0 {
			stripped = inlineFieldRe.ReplaceAllString(stripped, " ")
		}
	}
	for _, tm := range inlineTagRe.FindAllStringSubmatch(tail, -1) {
		tag := strings.ToLower(tm[1])
		dupe := false
		for _, existing := range t.tags {
			if existing == tag {
				dupe = true
				break
			}
		}
		if !dupe {
			t.tags = append(t.tags, tag)
		}
	}
	t.stripped = strings.TrimSpace(wsCollapseRe.ReplaceAllString(stripped, " "))
	return t
}

// SplitTaskID splits `<path>#<index>` into its halves; indexStr is the
// literal `task` for a whole-note task.
func SplitTaskID(taskID string) (rel string, indexStr string, err error) {
	hash := strings.LastIndexByte(taskID, '#')
	if hash < 0 {
		return "", "", fmt.Errorf("Malformed task id: %s", taskID)
	}
	return taskID[:hash], taskID[hash+1:], nil
}

// ParseTaskIndex validates the `#<n>` half of a task id.
func ParseTaskIndex(taskID, indexStr string) (int, error) {
	n, err := strconv.Atoi(indexStr)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("Malformed task index in id: %s", taskID)
	}
	return n, nil
}

// editTaskAtIndex walks to the task line at taskIndex (fence-aware, counting
// exactly like ParseTasks) and hands its match to mutate. The markdown comes
// back unchanged when the index is out of range or mutate keeps the line.
func editTaskAtIndex(markdown string, taskIndex int, mutate func(m []string) (string, bool)) (string, bool) {
	if taskIndex < 0 {
		return markdown, false
	}
	lines := strings.Split(markdown, "\n")
	current := 0
	inFence := false
	fenceMarker := ""
	for i, line := range lines {
		if fm := taskFenceRe.FindStringSubmatch(line); fm != nil {
			marker := fm[2]
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		m := taskLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if current != taskIndex {
			current++
			continue
		}
		next, ok := mutate(m)
		if !ok || next == line {
			return markdown, false
		}
		lines[i] = next
		return strings.Join(lines, "\n"), true
	}
	return markdown, false
}

// taskLineNumbers lists the 0-based line of every task line, by task index.
func taskLineNumbers(lines []string) []int {
	return taskLineNumbersIn(lines, DialectApp)
}

func taskLineNumbersIn(lines []string, dialect TaskDialect) []int {
	fenceRe, lineRe := dialect.fenceRe(), dialect.lineRe()
	out := []int{}
	inFence := false
	fenceMarker := ""
	for i, line := range lines {
		if fm := fenceRe.FindStringSubmatch(line); fm != nil {
			marker := fm[2]
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		if lineRe.MatchString(line) {
			out = append(out, i)
		}
	}
	return out
}

// ToggleTaskInBody flips the nth checkbox the way `zn task toggle` and the
// MCP tool do: open and done flip, an in-progress `[/]` checks off, and the
// forwarded and cancelled record markers are left alone. ok is false when the
// body holds no task at that index.
func ToggleTaskInBody(body string, targetIndex int, dialect TaskDialect) (next string, ok bool) {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	numbers := taskLineNumbersIn(lines, dialect)
	if targetIndex < 0 || targetIndex >= len(numbers) {
		return body, false
	}
	lineNumber := numbers[targetIndex]
	m := dialect.lineRe().FindStringSubmatch(lines[lineNumber])
	if m == nil {
		return body, false
	}
	switch m[2] {
	case ">", "-":
		return normalized, true
	case "x", "X":
		lines[lineNumber] = m[1] + " " + m[3]
	default:
		lines[lineNumber] = m[1] + "x" + m[3]
	}
	return strings.Join(lines, "\n"), true
}

// SetTaskChecked is the editor's toggle: unchecking an in-progress `[/]`
// keeps the `/`, since it already is not done.
func SetTaskChecked(markdown string, taskIndex int, checked bool) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		if !checked && m[2] == "/" {
			return m[1] + "/" + m[3], true
		}
		if checked {
			return m[1] + "x" + m[3], true
		}
		return m[1] + " " + m[3], true
	})
	return out
}

// SetTaskInProgress marks the line `[/]`, or back to `[ ]`.
func SetTaskInProgress(markdown string, taskIndex int, inProgress bool) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		if inProgress {
			return m[1] + "/" + m[3], true
		}
		return m[1] + " " + m[3], true
	})
	return out
}

// SetTaskCancelled marks the line `[-]`, or back to `[ ]`.
func SetTaskCancelled(markdown string, taskIndex int, cancelled bool) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		if cancelled {
			return m[1] + "-" + m[3], true
		}
		return m[1] + " " + m[3], true
	})
	return out
}

func taskTail(m []string) (string, bool) {
	if !strings.HasPrefix(m[3], "]") {
		return "", false
	}
	return m[3][1:], true
}

func tidyTail(tail string) string {
	return trailingWsRe.ReplaceAllString(multiSpaceRe.ReplaceAllString(tail, " "), "")
}

// SetTaskWaiting adds or removes the `@waiting` marker.
func SetTaskWaiting(markdown string, taskIndex int, waiting bool) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		tail, ok := taskTail(m)
		if !ok {
			return "", false
		}
		has := waitingTokenRe.MatchString(tail)
		next := tail
		if waiting && !has {
			next = trailingWsRe.ReplaceAllString(tail, "") + " @waiting"
		} else if !waiting && has {
			next = tidyTail(waitingTokenRe.ReplaceAllString(tail, "$1"))
		}
		return m[1] + m[2] + "]" + next, true
	})
	return out
}

// SetTaskPriority replaces, inserts or removes the `!priority` token.
func SetTaskPriority(markdown string, taskIndex int, priority string) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		tail, ok := taskTail(m)
		if !ok {
			return "", false
		}
		cleaned := multiSpaceRe.ReplaceAllString(priorityTokenRe.ReplaceAllString(tail, "$1"), " ")
		next := trailingWsRe.ReplaceAllString(cleaned, "")
		if priority != "" {
			next += " !" + priority
		}
		return m[1] + m[2] + "]" + next, true
	})
	return out
}

// SetTaskDue replaces, inserts or removes the `due:YYYY-MM-DD` token.
func SetTaskDue(markdown string, taskIndex int, due string) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		tail, ok := taskTail(m)
		if !ok {
			return "", false
		}
		cleaned := multiSpaceRe.ReplaceAllString(dueTokenRe.ReplaceAllString(tail, "$1"), " ")
		next := trailingWsRe.ReplaceAllString(cleaned, "")
		if due != "" {
			next += " due:" + due
		}
		return m[1] + m[2] + "]" + next, true
	})
	return out
}

// SetTaskField replaces, inserts or removes an inline `@key:value` token.
func SetTaskField(markdown string, taskIndex int, key, value string) string {
	if !fieldKeyRe.MatchString(key) {
		return markdown
	}
	tokenRe := regexp.MustCompile(`(?i)(^|\s)@` + regexp.QuoteMeta(key) + `:\S+`)
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		tail, ok := taskTail(m)
		if !ok {
			return "", false
		}
		cleaned := multiSpaceRe.ReplaceAllString(tokenRe.ReplaceAllString(tail, "$1"), " ")
		next := trailingWsRe.ReplaceAllString(cleaned, "")
		if value != "" {
			next += " @" + key + ":" + value
		}
		return m[1] + m[2] + "]" + next, true
	})
	return out
}

// SetTaskText replaces everything after the checkbox.
func SetTaskText(markdown string, taskIndex int, text string) string {
	out, _ := editTaskAtIndex(markdown, taskIndex, func(m []string) (string, bool) {
		if !strings.HasPrefix(m[3], "]") {
			return "", false
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			trimmed = " " + trimmed
		}
		return m[1] + m[2] + "]" + trimmed, true
	})
	return out
}

func leadingIndentWidth(line string) int {
	return len(leadingWsRe.FindString(line))
}

// taskBlockEnd is the exclusive end of the indented block belonging to the
// task line at start: wrapped text, children and loose sub-paragraphs. A
// blank line does not end the block when what follows is still deeper than
// the task; a dedent, a fence or the end of the note closes it.
func taskBlockEnd(lines []string, start, baseIndent int) int {
	end := start + 1
	for end < len(lines) {
		next := lines[end]
		if strings.TrimSpace(next) == "" {
			j := end + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j >= len(lines) || taskFenceRe.MatchString(lines[j]) || leadingIndentWidth(lines[j]) <= baseIndent {
				break
			}
			end = j
			continue
		}
		if taskFenceRe.MatchString(next) || leadingIndentWidth(next) <= baseIndent {
			break
		}
		end++
	}
	return end
}

// ForwardTaskSubtree flips the task at taskIndex to `[>]`, appends linkToken
// to it, and flips its open subtasks to `[>]` too. childLines is the subtree
// as it read before the flip, re-based to the parent's indent, so the caller
// can place it under a copy in the destination note.
func ForwardTaskSubtree(markdown string, taskIndex int, linkToken string) (body string, childLines []string) {
	if taskIndex < 0 {
		return markdown, nil
	}
	lines := strings.Split(markdown, "\n")
	numbers := taskLineNumbers(lines)
	if taskIndex >= len(numbers) {
		return markdown, nil
	}
	parentAt := numbers[taskIndex]
	parentLine := lines[parentAt]
	m := taskLineRe.FindStringSubmatch(parentLine)
	if m == nil || !strings.HasPrefix(m[3], "]") {
		return markdown, nil
	}
	tail := trailingWsRe.ReplaceAllString(m[3][1:], "")
	nextTail := tail
	if linkToken != "" && !strings.Contains(tail, linkToken) {
		nextTail = tail + " " + linkToken
	}
	nextParent := m[1] + ">]" + nextTail
	if nextParent == parentLine {
		return markdown, nil
	}
	parentIndent := leadingWsRe.FindString(parentLine)
	end := taskBlockEnd(lines, parentAt, leadingIndentWidth(parentLine))
	childLines = []string{}
	for _, l := range lines[parentAt+1 : end] {
		if strings.HasPrefix(l, parentIndent) {
			childLines = append(childLines, l[len(parentIndent):])
			continue
		}
		ws := leadingWsRe.FindString(l)
		childLines = append(childLines, ws[min(len(ws), len(parentIndent)):]+l[len(ws):])
	}
	lines[parentAt] = nextParent
	for i := parentAt + 1; i < end; i++ {
		if cm := taskLineRe.FindStringSubmatch(lines[i]); cm != nil && (cm[2] == " " || cm[2] == "/") {
			lines[i] = cm[1] + ">" + cm[3]
		}
	}
	return strings.Join(lines, "\n"), childLines
}

// ForwardTaskLines is ForwardTaskSubtree plus what the destination note
// receives: the task itself as a copy with its indent dropped and no link,
// then its subtree. A task without subtasks still yields its own line, so
// forwarding never leaves a `[>]` record with nothing on the other side.
func ForwardTaskLines(markdown string, taskIndex int, linkToken string) (body string, moved []string) {
	lines := strings.Split(markdown, "\n")
	numbers := taskLineNumbers(lines)
	if taskIndex < 0 || taskIndex >= len(numbers) {
		return markdown, nil
	}
	parentLine := lines[numbers[taskIndex]]
	// Only open work travels: a done, cancelled or already forwarded task
	// stays where it is, as in the desktop's task menu.
	if m := taskLineRe.FindStringSubmatch(parentLine); m == nil || (m[2] != " " && m[2] != "/") {
		return markdown, nil
	}
	parent := strings.TrimLeft(parentLine, " \t")
	next, children := ForwardTaskSubtree(markdown, taskIndex, linkToken)
	if next == markdown {
		return markdown, nil
	}
	return next, append([]string{parent}, children...)
}

// MoveTaskLine relocates the task line at fromTaskIndex before or after the
// task line at targetTaskIndex.
func MoveTaskLine(markdown string, fromTaskIndex, targetTaskIndex int, before bool) string {
	if fromTaskIndex < 0 || targetTaskIndex < 0 || fromTaskIndex == targetTaskIndex {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	numbers := taskLineNumbers(lines)
	if fromTaskIndex >= len(numbers) || targetTaskIndex >= len(numbers) {
		return markdown
	}
	fromLine, targetLine := numbers[fromTaskIndex], numbers[targetTaskIndex]
	content := lines[fromLine]
	lines = append(lines[:fromLine], lines[fromLine+1:]...)
	shifted := targetLine
	if fromLine < targetLine {
		shifted--
	}
	insertAt := shifted
	if !before {
		insertAt = shifted + 1
	}
	lines = append(lines[:insertAt], append([]string{content}, lines[insertAt:]...)...)
	return strings.Join(lines, "\n")
}

// RemoveTaskLine deletes the task line at taskIndex, returning it too.
func RemoveTaskLine(markdown string, taskIndex int) (line string, body string, ok bool) {
	if taskIndex < 0 {
		return "", markdown, false
	}
	lines := strings.Split(markdown, "\n")
	numbers := taskLineNumbers(lines)
	if taskIndex >= len(numbers) {
		return "", markdown, false
	}
	at := numbers[taskIndex]
	removed := lines[at]
	lines = append(lines[:at], lines[at+1:]...)
	return removed, strings.Join(lines, "\n"), true
}

var (
	tasksHeadingRe   = regexp.MustCompile(`(?i)^ {0,3}(#{1,6})\s+Tasks\s*$`)
	anyHeadingRe     = regexp.MustCompile(`^ {0,3}(#{1,6})\s+`)
	thematicBreakRe  = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	frontmatterOnlyR = regexp.MustCompile(`(?s)\A---\n.*?\n---\n?`)
)

// InsertTasksUnderTasksHeading places task lines at the end of a `# Tasks`
// section when the note has one (before the next heading of the same or a
// higher level, or a horizontal rule), else appends them to the note.
func InsertTasksUnderTasksHeading(body string, taskLines []string) string {
	if len(taskLines) == 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	headingIdx, headingLevel := -1, 0
	inFence, fenceMarker := false, ""
	for i, line := range lines {
		if fm := taskFenceRe.FindStringSubmatch(line); fm != nil {
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
		if m := tasksHeadingRe.FindStringSubmatch(line); m != nil {
			headingIdx, headingLevel = i, len(m[1])
			break
		}
	}
	block := strings.Join(taskLines, "\n")
	if headingIdx == -1 {
		trimmed := trailingWsRe.ReplaceAllString(body, "")
		if trimmed == "" {
			return block + "\n"
		}
		return trimmed + "\n" + block + "\n"
	}
	sectionEnd := len(lines)
	inFence, fenceMarker = false, ""
	for i := headingIdx + 1; i < len(lines); i++ {
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
		if hm := anyHeadingRe.FindStringSubmatch(lines[i]); hm != nil && len(hm[1]) <= headingLevel {
			sectionEnd = i
			break
		}
		if thematicBreakRe.MatchString(lines[i]) {
			sectionEnd = i
			break
		}
	}
	lastContent := -1
	for i := headingIdx + 1; i < sectionEnd; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			lastContent = i
		}
	}
	insertAt := headingIdx + 1
	if lastContent != -1 {
		insertAt = lastContent + 1
	} else if insertAt < sectionEnd && strings.TrimSpace(lines[insertAt]) == "" {
		insertAt++
	}
	out := append([]string{}, lines[:insertAt]...)
	out = append(out, taskLines...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n")
}

// ExtractOpenTaskBlocks pulls every open task line (`[ ]` and `[/]`) with its
// indented children out of markdown, for rolling unfinished work forward
// into today's daily note.
func ExtractOpenTaskBlocks(markdown string) (moved []string, rest string) {
	lines := strings.Split(markdown, "\n")
	consumed := make([]bool, len(lines))
	moved = []string{}
	inFence, fenceMarker := false, ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if fm := taskFenceRe.FindStringSubmatch(line); fm != nil {
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
		m := taskLineRe.FindStringSubmatch(line)
		if m == nil || (m[2] != " " && m[2] != "/") {
			continue
		}
		moved = append(moved, line)
		consumed[i] = true
		end := taskBlockEnd(lines, i, leadingIndentWidth(line))
		for j := i + 1; j < end; j++ {
			moved = append(moved, lines[j])
			consumed[j] = true
		}
		i = end - 1
	}
	kept := []string{}
	for i, line := range lines {
		if !consumed[i] {
			kept = append(kept, line)
		}
	}
	return moved, strings.Join(kept, "\n")
}

// TodayISO is the local calendar date as `YYYY-MM-DD`.
func TodayISO(now time.Time) string {
	return now.Format("2006-01-02")
}

// ToggleFileTaskInBody flips a task file's frontmatter `status` (and its
// `completedDate`).
func ToggleFileTaskInBody(body string, currentlyChecked bool, now time.Time) string {
	if currentlyChecked {
		return SetTaskFileStatus(body, false, now)
	}
	return SetTaskFileStatus(body, true, now)
}

// SetTaskFileStatus writes `status: done` plus `completedDate` when checking,
// and back to `open` (clearing `completedDate`) when unchecking.
func SetTaskFileStatus(body string, done bool, now time.Time) string {
	if done {
		return UpdateFrontmatterFields(body, []FrontmatterUpdate{
			{Key: "status", Value: StrPtr("done")},
			{Key: "completedDate", Value: StrPtr(TodayISO(now))},
		})
	}
	return UpdateFrontmatterFields(body, []FrontmatterUpdate{
		{Key: "status", Value: StrPtr("open")},
		{Key: "completedDate", Value: nil},
	})
}

// SetTaskFileCancelled writes `status: cancelled`, or back to `open`.
func SetTaskFileCancelled(body string, cancelled bool) string {
	status := "open"
	if cancelled {
		status = "cancelled"
	}
	return UpdateFrontmatterFields(body, []FrontmatterUpdate{
		{Key: "status", Value: StrPtr(status)},
		{Key: "completedDate", Value: nil},
	})
}

// SetTaskFileInProgress writes `status: in-progress`, or back to `open`.
func SetTaskFileInProgress(body string, inProgress bool) string {
	status := "open"
	if inProgress {
		status = "in-progress"
	}
	return UpdateFrontmatterFields(body, []FrontmatterUpdate{
		{Key: "status", Value: StrPtr(status)},
		{Key: "completedDate", Value: nil},
	})
}

// SetTaskFileField sets any frontmatter scalar (`due`, `priority` in
// TaskNotes vocabulary, `status`); an empty value removes the key.
func SetTaskFileField(body, key, value string) string {
	var v *string
	if value != "" {
		v = StrPtr(value)
	}
	return UpdateFrontmatterFields(body, []FrontmatterUpdate{{Key: key, Value: v}})
}

// TaskFilePriorityValue maps a ZenNotes priority onto TaskNotes' vocabulary.
func TaskFilePriorityValue(priority string) string {
	switch priority {
	case "high":
		return "high"
	case "med":
		return "normal"
	case "low":
		return "low"
	}
	return ""
}

// ComposeTaskFileInput describes a new whole-note task.
type ComposeTaskFileInput struct {
	Title       string
	Status      string
	Priority    string
	Due         string
	Scheduled   string
	Tags        []string
	DateCreated string
	Body        string
}

// ComposeTaskFile builds a task-file note in the TaskNotes shape.
func ComposeTaskFile(in ComposeTaskFileInput) string {
	tags := []string{TaskFileTag}
	for _, t := range in.Tags {
		if t != "" && t != TaskFileTag {
			tags = append(tags, t)
		}
	}
	status := in.Status
	if status == "" {
		status = "open"
	}
	lines := []string{"---", "title: " + YAMLValue(in.Title), "status: " + status}
	if p := TaskFilePriorityValue(in.Priority); p != "" {
		lines = append(lines, "priority: "+p)
	}
	if in.Due != "" {
		lines = append(lines, "due: "+in.Due)
	}
	if in.Scheduled != "" {
		lines = append(lines, "scheduled: "+in.Scheduled)
	}
	lines = append(lines, "tags: ["+strings.Join(tags, ", ")+"]")
	if in.DateCreated != "" {
		lines = append(lines, "dateCreated: "+in.DateCreated)
	}
	lines = append(lines, "---", "")
	return strings.Join(lines, "\n") + "\n" + strings.TrimLeft(in.Body, "\n")
}

// GroupTasks buckets tasks for the list view. Waiting overrides everything
// but Done; tasks without a due date land in Today.
func GroupTasks(tasks []Task, today time.Time) TaskGroups {
	todayISO := TodayISO(today)
	g := TaskGroups{}
	for _, task := range tasks {
		switch {
		case task.Cancelled:
			g.Cancelled = append(g.Cancelled, task)
		case task.Forwarded:
			g.Forwarded = append(g.Forwarded, task)
		case task.Checked:
			g.Done = append(g.Done, task)
		case task.Waiting:
			g.Waiting = append(g.Waiting, task)
		case task.Due == "":
			g.Today = append(g.Today, task)
		case task.Due < todayISO:
			g.Today = append(g.Today, task)
			g.OverdueCount++
		case task.Due == todayISO:
			g.Today = append(g.Today, task)
		default:
			g.Upcoming = append(g.Upcoming, task)
		}
	}
	byDueThenPath := func(a, b Task) bool {
		ad, bd := a.Due, b.Due
		if ad == "" {
			ad = "9999-99-99"
		}
		if bd == "" {
			bd = "9999-99-99"
		}
		if ad != bd {
			return ad < bd
		}
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		return a.TaskIndex < b.TaskIndex
	}
	rank := map[string]int{"high": 0, "med": 1, "low": 2, "": 3}
	byPriorityThenDue := func(a, b Task) bool {
		if rank[a.Priority] != rank[b.Priority] {
			return rank[a.Priority] < rank[b.Priority]
		}
		return byDueThenPath(a, b)
	}
	byPath := func(a, b Task) bool {
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		return a.TaskIndex < b.TaskIndex
	}
	sortTasks(g.Today, byPriorityThenDue)
	sortTasks(g.Upcoming, byDueThenPath)
	sortTasks(g.Waiting, byPriorityThenDue)
	sortTasks(g.Done, byPath)
	sortTasks(g.Forwarded, byDueThenPath)
	sortTasks(g.Cancelled, byPath)
	return g
}

func sortTasks(tasks []Task, less func(a, b Task) bool) {
	sort.SliceStable(tasks, func(i, j int) bool { return less(tasks[i], tasks[j]) })
}

// FilterTasksForDisplay drops tasks from archived notes unless showArchived.
func FilterTasksForDisplay(tasks []Task, showArchived bool) []Task {
	if showArchived {
		return tasks
	}
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.NoteFolder != FolderArchive {
			out = append(out, t)
		}
	}
	return out
}

// IsTaskOpen is true for a task that is neither done nor cancelled.
func IsTaskOpen(t Task) bool {
	return !t.Checked && !t.Cancelled
}

// IsOverdue is true for an open, non-waiting task whose due date has passed.
func IsOverdue(t Task, today time.Time) bool {
	if t.Checked || t.Waiting || t.Due == "" {
		return false
	}
	return t.Due < TodayISO(today)
}

// InferDailyTaskDueDates gives undated tasks living in a daily note an
// implicit due date equal to that note's date. Forwarded records are exempt.
func InferDailyTaskDueDates(tasks []Task, dueByPath map[string]string) []Task {
	if len(dueByPath) == 0 {
		return tasks
	}
	out := make([]Task, len(tasks))
	for i, task := range tasks {
		out[i] = task
		if task.Due != "" || task.Forwarded {
			continue
		}
		if iso, ok := dueByPath[task.SourcePath]; ok {
			out[i].Due = iso
			out[i].DueInferred = true
		}
	}
	return out
}

// BucketTasksByDueDate groups open tasks by due date; undated ones land under
// "unscheduled". An undated forwarded record is a record of a move, not
// unscheduled work, and is skipped.
func BucketTasksByDueDate(tasks []Task) map[string][]Task {
	out := map[string][]Task{}
	for _, task := range tasks {
		if !IsTaskOpen(task) {
			continue
		}
		if task.Forwarded && task.Due == "" {
			continue
		}
		key := task.Due
		if key == "" {
			key = "unscheduled"
		}
		out[key] = append(out[key], task)
	}
	return out
}

// ErrTaskGone says a task id no longer names a task in its note.
var ErrTaskGone = errors.New("task no longer exists at that location")
