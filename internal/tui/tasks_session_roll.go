package tui

import (
	"fmt"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/vault"
)

// rollEffects is what a roll still has to write after the note of the task:
// the copies of its work sessions for other notes, and the number of sessions
// that are event notes and are not copied.
type rollEffects struct {
	copies  []vault.SessionCopy
	ownNote int
}

// planTaskRoll plans the roll of t in body on a local vault. planned is false
// for any other target, and the caller makes the plain roll. ok is false when
// body has no task at the index of t.
func (a *App) planTaskRoll(body string, t vault.Task, action vault.RollAction) (plan vault.TaskRollPlan, ok, planned bool) {
	local, isLocal := a.backend.(*backend.Local)
	if !isLocal {
		return plan, false, false
	}
	v := local.Vault()
	now := a.taskClock()
	in := vault.TaskRollInput{
		Path: t.SourcePath, Body: body, File: t.Kind == "file", Index: t.TaskIndex,
		Dialect: vault.DialectApp, Action: action, Now: now, WeekStart: a.taskWeekStart(),
		Names: v.SkipResolverAt(now), Rand: v.Rand,
		FallbackDue: a.dailyNoteDate(t.SourcePath, t.NoteTitle),
	}
	if t.Repeat != "" {
		v.LoadRollContext(&in)
		a.useNoteText(&in, t, body)
	}
	plan, ok = vault.PlanTaskRoll(in)
	return plan, ok, true
}

// useNoteText makes the roll read the task note and the open notes as the
// editor holds them, not as the disk holds them.
func (a *App) useNoteText(in *vault.TaskRollInput, t vault.Task, body string) {
	opts := vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectApp}
	tasks := in.Tasks[:0:0]
	for _, other := range in.Tasks {
		if other.SourcePath != t.SourcePath {
			tasks = append(tasks, other)
		}
	}
	in.Tasks = append(tasks, vault.ParseTasks(t.SourcePath, t.NoteTitle, t.NoteFolder, body, opts)...)
	events := in.Events[:0:0]
	for _, other := range in.Events {
		if other.Path != t.SourcePath {
			events = append(events, other)
		}
	}
	in.Events = append(events, vault.ParseEvents(t.SourcePath, t.NoteTitle, t.NoteFolder, body)...)
	disk := in.Read
	in.Read = func(path string) (string, bool) {
		if path == t.SourcePath {
			return body, true
		}
		if buf, open := a.buffers[path]; open && !buf.loading {
			return buf.ed.Text(), true
		}
		return disk(path)
	}
}

// endRoll finishes a roll after the write of the task note. When that write
// failed, the roll writes nothing else.
func (a *App) endRoll(err error) {
	effects := a.rollPending
	a.rollPending = rollEffects{}
	if err != nil {
		return
	}
	var messages []string
	failed := false
	var paths []string
	byPath := map[string][]vault.SessionCopy{}
	for _, c := range effects.copies {
		if _, seen := byPath[c.Path]; !seen {
			paths = append(paths, c.Path)
		}
		byPath[c.Path] = append(byPath[c.Path], c)
	}
	for _, path := range paths {
		copies := byPath[path]
		err := a.mutateNote(path, func(body string) (string, bool) {
			return vault.InsertSessionCopies(body, copies), true
		})
		if err != nil {
			failed = true
			messages = append(messages, fmt.Sprintf("Could not copy a work session into %s: %v", path, err))
		}
	}
	switch {
	case effects.ownNote == 1:
		messages = append(messages, "1 work session in its own note was not copied")
	case effects.ownNote > 1:
		messages = append(messages, fmt.Sprintf("%d work sessions in their own notes were not copied", effects.ownNote))
	}
	switch {
	case len(messages) == 0:
	case failed:
		a.notifyError(strings.Join(messages, ". "))
	default:
		a.notify(strings.Join(messages, ". "))
	}
}
