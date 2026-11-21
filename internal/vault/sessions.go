package vault

import (
	"sort"
	"strings"
	"time"
)

// A session is an event with a `task:` link. It is a plan to work on the
// task that the link names. The task does not hold the session: the session
// holds the link.

// parseSessionLink splits a `task:` value into the note name and the block
// id. The value must be a wiki link, `[[Note]]` or `[[Note#^id]]`. An alias
// after `|` and a heading after `#` are dropped. The id is empty for a link
// to a whole note.
func parseSessionLink(link string) (note, id string, ok bool) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(link, "[[") || !strings.HasSuffix(link, "]]") {
		return "", "", false
	}
	inner := link[2 : len(link)-2]
	if i := strings.Index(inner, "|"); i >= 0 {
		inner = inner[:i]
	}
	if i := strings.Index(inner, "#"); i >= 0 {
		if strings.HasPrefix(inner[i+1:], "^") {
			id = inner[i+2:]
		}
		inner = inner[:i]
	}
	note = strings.TrimSpace(inner)
	if note == "" {
		return "", "", false
	}
	return note, id, true
}

// sessionIndex finds the task that a session names.
type sessionIndex struct {
	tasks []Task
	byID  map[string][]int
}

func newSessionIndex(tasks []Task) *sessionIndex {
	idx := &sessionIndex{tasks: tasks, byID: map[string][]int{}}
	for i, t := range tasks {
		if t.BlockID != "" {
			idx.byID[t.BlockID] = append(idx.byID[t.BlockID], i)
		}
	}
	return idx
}

// inNote reports whether the task is in the note that name gives. A name with
// a slash matches the path of the note, and any other name matches its title.
// Case is not important and the `.md` suffix is optional.
func inNote(t Task, name string) bool {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), ".md"))
	if strings.Contains(name, "/") {
		path := strings.ToLower(strings.TrimSuffix(t.SourcePath, ".md"))
		return path == name || strings.HasSuffix(path, "/"+name)
	}
	return strings.ToLower(t.NoteTitle) == name
}

// resolve gives the index of the task that the event names, or -1. A link to
// a note and an id finds the one task line in that note that ends with the
// id. When the note has none, it finds the one task line in the vault that
// ends with the id. A link to a note alone finds the task note of that name.
// A link that fits more than one task finds none.
func (x *sessionIndex) resolve(e Event) int {
	note, id, ok := parseSessionLink(e.TaskLink)
	if !ok {
		return -1
	}
	if id == "" {
		found := -1
		for i, t := range x.tasks {
			if t.Kind != "file" || !inNote(t, note) {
				continue
			}
			if found >= 0 {
				return -1
			}
			found = i
		}
		return found
	}
	candidates := x.byID[id]
	found := -1
	for _, i := range candidates {
		if !inNote(x.tasks[i], note) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	if found >= 0 {
		return found
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return -1
}

// ResolveSession gives the task that an event names with its `task:` link. It
// reports false when the event has no link or the link dangles.
func ResolveSession(tasks []Task, e Event) (Task, bool) {
	if i := newSessionIndex(tasks).resolve(e); i >= 0 {
		return tasks[i], true
	}
	return Task{}, false
}

// sessionEnd gives the last date of a session.
func sessionEnd(e Event) string {
	if e.EndDate != "" {
		return e.EndDate
	}
	return e.StartDate
}

// isCurrentSession reports whether a session counts for its task. A current
// session is written as an inline event without a `repeat:` of its own. For
// a task note it must also start after the `lastCompleted` date, because an
// earlier session belongs to an occurrence that is finished.
func isCurrentSession(e Event, t Task) bool {
	if e.Kind != EventKindInline || e.Repeat != "" {
		return false
	}
	return t.Kind != "file" || t.LastCompleted == "" || e.StartDate > t.LastCompleted
}

// lessSession orders sessions by start date, then by start time. A session
// without a time comes before one with a time.
func lessSession(a, b Event) bool {
	if a.StartDate != b.StartDate {
		return a.StartDate < b.StartDate
	}
	return a.StartTime < b.StartTime
}

// pickSession chooses the session that sets the session date of a task: the
// first one, by start, that ends on the day today or later. When all of them
// have ended, it chooses the last one.
func pickSession(sessions []Event, today string) Event {
	sort.SliceStable(sessions, func(i, j int) bool { return lessSession(sessions[i], sessions[j]) })
	for _, s := range sessions {
		if sessionEnd(s) >= today {
			return s
		}
	}
	return sessions[len(sessions)-1]
}

// JoinSessions gives a copy of tasks in which each task that has a current
// session carries its session date, start time and end time. A session that
// does not resolve changes nothing. The list that the caller gave stays as it
// was.
func JoinSessions(tasks []Task, events []Event, today time.Time) []Task {
	out := append([]Task(nil), tasks...)
	index := newSessionIndex(out)
	byTask := map[int][]Event{}
	for _, e := range events {
		if e.TaskLink == "" {
			continue
		}
		if i := index.resolve(e); i >= 0 && isCurrentSession(e, out[i]) {
			byTask[i] = append(byTask[i], e)
		}
	}
	todayISO := TodayISO(today)
	for i, sessions := range byTask {
		s := pickSession(sessions, todayISO)
		out[i].SessionDate, out[i].SessionTime, out[i].SessionEnd = s.StartDate, s.StartTime, s.EndTime
	}
	return out
}
