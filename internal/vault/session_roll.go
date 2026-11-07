package vault

import (
	"math/rand"
	"sort"
	"strings"
	"time"
)

// TaskRollInput is what PlanTaskRoll needs to roll one task.
type TaskRollInput struct {
	// Path is the note that holds the task and Body is its text.
	Path string
	Body string
	// File is true for a task note. Index is the task index of a checkbox
	// task in the note, and is not read for a task note.
	File    bool
	Index   int
	Dialect TaskDialect
	Action  RollAction
	Now     time.Time
	// WeekStart is the first day of the week.
	WeekStart time.Weekday
	// Names resolves the `@name` items of a `skip:` token. It can be nil.
	Names SkipResolver
	// Tasks and Events are every task and every event of the vault. They tell
	// the roll which sessions the task has.
	Tasks  []Task
	Events []Event
	// Read gives the text of a note by its path. The roll reads the lines of
	// the sessions with it.
	Read func(path string) (string, bool)
	// IDs are the block ids that the vault uses already, and Rand is the
	// source for a new id. Rand can be nil.
	IDs  map[string]bool
	Rand *rand.Rand
}

// SessionCopy is one new line for a note other than the task note: the copy
// of a work session for the new occurrence.
type SessionCopy struct {
	Path string
	// After is the zero-based number of the line that the copy follows, in
	// the note as Read gave it.
	After int
	Line  string
}

// TaskRollPlan is the result of PlanTaskRoll.
type TaskRollPlan struct {
	// Body is the new text of the task note. It holds the copies of the
	// sessions that are in the task note itself.
	Body string
	// Inserted counts the task lines added above the original.
	Inserted int
	// Rolled is true when a new occurrence was inserted.
	Rolled bool
	// Copies are the session copies for other notes, in path and line order.
	Copies []SessionCopy
	// InOwnNote counts the work sessions that are event notes. A roll does
	// not copy them.
	InOwnNote int
}

// PlanTaskRoll plans how a task rolls, without a write. When the task is
// open and its rule gives a next date, the plan holds the new text of the
// task note and a copy of each current work session of the task.
//
// The roll counts from the start of the earliest current session, else from
// `due`, else from today. The `due` date and each session move by the same
// number of days. A session is copied directly below its original, with its
// `when:` moved and its `task:` link on the new occurrence. A link to a whole
// note stays as it is. The copy has no `until:` and no block id of its own.
// The inserted open line gets a new block id only when the task has current
// sessions.
//
// A session is not copied when it is an event note, when it repeats, or when
// its link does not name the task. Event notes are counted in InOwnNote. A
// task that does not roll has no copies and gets no new id.
//
// ok is false when the note has no task at that index.
func PlanTaskRoll(in TaskRollInput) (plan TaskRollPlan, ok bool) {
	var current []Event
	ownNote := 0
	if target, found := rollTarget(in); found {
		current, ownNote = sessionsOfTask(target, in.Tasks, in.Events)
	}
	base := ""
	for _, e := range current {
		if base == "" || e.StartDate < base {
			base = e.StartDate
		}
	}

	var id string
	newID := func() string {
		if id == "" {
			ids := in.IDs
			if ids == nil {
				ids = map[string]bool{}
			}
			id = NewBlockID(ids, in.Rand)
		}
		return id
	}
	roll := func(body string) (string, int, bool, int, bool) {
		if in.File {
			next, rolled, days := rollTaskFile(body, in.Action, in.Now, in.WeekStart, in.Names, base)
			return next, 0, rolled, days, true
		}
		s := rollSessions{base: base}
		if base != "" {
			s.newID = newID
		}
		res := rollTaskLines(body, in.Index, in.Dialect, in.Action, in.Now, in.WeekStart, in.Names, s)
		return res.next, res.inserted, res.rolled, res.days, res.ok
	}

	next, inserted, rolled, days, ok := roll(in.Body)
	if !ok {
		return TaskRollPlan{Body: in.Body}, false
	}
	plan = TaskRollPlan{Body: next, Inserted: inserted, Rolled: rolled}
	if !rolled {
		return plan, true
	}
	plan.InOwnNote = ownNote

	link := ""
	if !in.File && len(current) > 0 {
		link = newID()
	}
	var copies []SessionCopy
	for _, e := range current {
		body, read := "", false
		if in.Read != nil {
			body, read = in.Read(e.Path)
		}
		if !read {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
		if e.Line < 0 || e.Line >= len(lines) {
			continue
		}
		line, good := sessionCopyLine(lines[e.Line], e.When, days, link)
		if !good {
			continue
		}
		copies = append(copies, SessionCopy{Path: e.Path, After: e.Line, Line: line})
	}
	sort.SliceStable(copies, func(i, j int) bool {
		if copies[i].Path != copies[j].Path {
			return copies[i].Path < copies[j].Path
		}
		return copies[i].After < copies[j].After
	})

	var own, other []SessionCopy
	for _, c := range copies {
		if c.Path == in.Path {
			own = append(own, c)
		} else {
			other = append(other, c)
		}
	}
	plan.Copies = other
	if len(own) > 0 {
		// The copies in the task note go in before the roll. They are not
		// task lines, so the task index stays the same.
		withCopies := InsertSessionCopies(in.Body, own)
		plan.Body, plan.Inserted, _, _, _ = roll(withCopies)
	}
	return plan, true
}

// InsertSessionCopies puts each copy directly after the line it names. The
// copies must all be for the note that body holds. Line endings are kept as
// the note has them.
func InsertSessionCopies(body string, copies []SessionCopy) string {
	crlf := strings.Contains(body, "\r\n")
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	ordered := append([]SessionCopy(nil), copies...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].After > ordered[j].After })
	for _, c := range ordered {
		if c.After < 0 || c.After >= len(lines) {
			continue
		}
		lines = append(lines[:c.After+1], append([]string{c.Line}, lines[c.After+1:]...)...)
	}
	out := strings.Join(lines, "\n")
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out
}

// rollTarget finds the task that the roll is for in the vault's tasks.
func rollTarget(in TaskRollInput) (Task, bool) {
	for _, t := range in.Tasks {
		if t.SourcePath != in.Path {
			continue
		}
		if in.File && t.Kind == "file" {
			return t, true
		}
		if !in.File && t.Kind == "" && t.TaskIndex == in.Index {
			return t, true
		}
	}
	return Task{}, false
}

// sessionsOfTask gives the current inline sessions of a task, and the number
// of its sessions that are event notes.
func sessionsOfTask(target Task, tasks []Task, events []Event) (current []Event, ownNote int) {
	index := newSessionIndex(tasks)
	for _, e := range events {
		if e.TaskLink == "" {
			continue
		}
		i := index.resolve(e)
		if i < 0 {
			continue
		}
		t := tasks[i]
		if t.SourcePath != target.SourcePath || t.Kind != target.Kind ||
			(t.Kind != "file" && t.TaskIndex != target.TaskIndex) {
			continue
		}
		if e.Kind == EventKindNote {
			if t.Kind != "file" || t.LastCompleted == "" || e.StartDate > t.LastCompleted {
				ownNote++
			}
			continue
		}
		if isCurrentSession(e, t) {
			current = append(current, e)
		}
	}
	return current, ownNote
}

// sessionCopyLine makes the copy of a session line. The `when:` value moves
// by days. The link moves to newID when newID is not empty and the link names
// a block. The copy loses `until:` tokens and a block id. It gives false when
// the line is no longer the session that when names.
func sessionCopyLine(line, when string, days int, newID string) (string, bool) {
	line = strings.TrimRight(line, "\r")
	line = stripBlockID(line)
	masked := inlineCodeRe.ReplaceAllStringFunc(line, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
	whens := eventWhenRe.FindAllStringSubmatchIndex(masked, -1)
	if len(whens) != 1 || masked[whens[0][2]:whens[0][3]] != when {
		return "", false
	}
	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{{whens[0][2], whens[0][3], ShiftWhenValue(when, days)}}
	for _, m := range eventUntilRe.FindAllStringSubmatchIndex(masked, -1) {
		edits = append(edits, edit{m[0], m[1], ""})
	}
	if tasks := eventTaskRe.FindAllStringSubmatchIndex(masked, -1); len(tasks) > 0 && newID != "" {
		m := tasks[0]
		edits = append(edits, edit{m[2], m[3], withLinkID(line[m[2]:m[3]], newID)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		line = line[:e.start] + e.text + line[e.end:]
	}
	return line, true
}

// withLinkID puts id in a link to a block, such as `[[Work#^ab12cd]]`. A link
// to a whole note is returned as it is. An alias stays.
func withLinkID(link, id string) string {
	if !strings.HasPrefix(link, "[[") || !strings.HasSuffix(link, "]]") {
		return link
	}
	inner := link[2 : len(link)-2]
	alias := ""
	if i := strings.Index(inner, "|"); i >= 0 {
		inner, alias = inner[:i], inner[i:]
	}
	i := strings.Index(inner, "#^")
	if i < 0 {
		return link
	}
	return "[[" + inner[:i] + "#^" + id + alias + "]]"
}
