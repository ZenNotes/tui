package backend

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
)

// UpdateTaskInput is what to change on one task; a nil field stays as it is.
type UpdateTaskInput struct {
	ID string
	// Due is YYYY-MM-DD; "" clears it.
	Due *string
	// Priority is high, med or low; "" clears it.
	Priority *string
	Waiting  *bool
	// State is open, done, in-progress or cancelled.
	State *string
	// Status is the Kanban column (`@status:` on a line, `status:` on a task
	// note); "" clears it.
	Status *string
	// Text replaces everything after a line's checkbox, tokens included.
	Text *string
	// ExpectedText is the task's content as the caller last saw it. An id counts
	// the task's position, so after an edit above it the id names another task;
	// when this no longer matches, nothing changes and the call fails.
	ExpectedText *string
}

// TaskRoller is a backend that rolls a repeating task with the local roll. A
// local backend is one. A remote backend is not, and keeps the roll of the
// released version.
type TaskRoller interface {
	// RollEditedTask rolls the task at index in the note rel. Its line is
	// edited in body and not written. Applied in the result is false when the
	// task has no rule that rolls: nothing is written then.
	RollEditedTask(ctx context.Context, rel, body string, index int, action vault.RollAction, canWrite func(rel string) bool) (vault.RollEdit, error)
}

// taskFieldValueRe is what the task parser reads as an `@key:value` value.
var taskFieldValueRe = regexp.MustCompile(`^[\p{L}\d][\p{L}\d/_-]*$`)

func (in UpdateTaskInput) validate() error {
	if in.Due == nil && in.Priority == nil && in.Waiting == nil && in.State == nil && in.Status == nil && in.Text == nil {
		return errors.New("update_task needs at least one of due, priority, waiting, state, status, text")
	}
	if in.Due != nil && *in.Due != "" && !vault.IsValidISODate(*in.Due) {
		return errors.New("due must be YYYY-MM-DD, or empty to clear it")
	}
	if in.Priority != nil {
		switch *in.Priority {
		case "", "high", "med", "low":
		default:
			return errors.New("priority must be high, med or low, or empty to clear it")
		}
	}
	if in.State != nil {
		switch *in.State {
		case "open", "done", "in-progress", "cancelled":
		default:
			return errors.New("state must be open, done, in-progress or cancelled")
		}
	}
	if in.Status != nil && *in.Status != "" && !taskFieldValueRe.MatchString(*in.Status) {
		return errors.New("status must be one word (letters, digits, /, _ or -), or empty to clear it")
	}
	if in.Text != nil && (strings.TrimSpace(*in.Text) == "" || strings.ContainsAny(*in.Text, "\r\n")) {
		return errors.New("text must be one line, and not empty")
	}
	return nil
}

// UpdateTask edits one task through the backend's own read and write, so it
// takes a scope and a server the way any other edit does. It returns the task
// as it now reads, or nil when the id no longer matches one.
//
// The edit is applied to the task's line on its own: list_tasks numbers tasks
// by the scan that produced the id (a server's own parser, or the CLI dialect),
// and the line setters count with the editor's, which reads an unusual fence
// differently. Editing the line a scan pointed at cannot land on another task.
func UpdateTask(ctx context.Context, b Backend, in UpdateTaskInput) (*vault.Task, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	rel, index, err := vault.SplitTaskID(in.ID)
	if err != nil {
		return nil, err
	}
	task, err := findTask(ctx, b, rel, index)
	if err != nil || task == nil {
		return nil, err
	}
	if in.ExpectedText != nil && strings.TrimSpace(*in.ExpectedText) != strings.TrimSpace(task.Content) {
		return nil, fmt.Errorf("Task %s now reads %q, not %q: the note changed. Run list_tasks again for its id.", in.ID, task.Content, *in.ExpectedText)
	}
	note, err := b.ReadNote(ctx, task.SourcePath)
	if err != nil {
		return nil, err
	}
	var roller func(edited string, action vault.RollAction) (vault.RollEdit, error)
	if tr, ok := b.(TaskRoller); ok && b.Kind() == KindLocal {
		roller = func(edited string, action vault.RollAction) (vault.RollEdit, error) {
			return tr.RollEditedTask(ctx, task.SourcePath, edited, task.TaskIndex, action, nil)
		}
	}
	var next string
	finalLine := task.LineNumber
	var rolled vault.RollEdit
	if task.Kind == "file" {
		if next, err = updateFileTask(note.Body, *task, in); err != nil {
			return nil, err
		}
	} else {
		var found bool
		if next, finalLine, found, rolled, err = updateTaskLine(note.Body, *task, in, roller); err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
	}
	if rolled.Applied {
		// The roll wrote the note, and the record sits below the new occurrence.
		return findTaskByIndex(ctx, b, task.SourcePath, task.TaskIndex+rolled.Inserted)
	}
	if next != note.Body {
		if _, err := b.WriteNote(ctx, task.SourcePath, next); err != nil {
			return nil, err
		}
	}
	if finalLine != task.LineNumber {
		return findTaskOnLine(ctx, b, task.SourcePath, finalLine)
	}
	return findTask(ctx, b, task.SourcePath, index)
}

func taskScan(ctx context.Context, b Backend, rel string) ([]vault.Task, error) {
	return b.ScanTasksForPath(ctx, rel, vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectCLI})
}

func findTask(ctx context.Context, b Backend, rel, index string) (*vault.Task, error) {
	tasks, err := taskScan(ctx, b, rel)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if strings.HasSuffix(tasks[i].ID, "#"+index) {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

func findTaskByIndex(ctx context.Context, b Backend, rel string, index int) (*vault.Task, error) {
	tasks, err := taskScan(ctx, b, rel)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].Kind == "" && tasks[i].TaskIndex == index {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

func findTaskOnLine(ctx context.Context, b Backend, rel string, line int) (*vault.Task, error) {
	tasks, err := taskScan(ctx, b, rel)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].Kind == "" && tasks[i].LineNumber == line {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

// updateTaskLine returns the note and the line the task now stands on, which
// moves when finishing a recurring task writes its next occurrence above it.
//
// roller is nil on a backend that keeps the released roll. Otherwise it rolls
// an open task that is finished or cancelled. When the roll applies, it has
// written the note already, and the last result says how far the task moved.
func updateTaskLine(body string, task vault.Task, in UpdateTaskInput, roller func(edited string, action vault.RollAction) (vault.RollEdit, error)) (string, int, bool, vault.RollEdit, error) {
	if task.Forwarded && in.State != nil {
		return "", 0, false, vault.RollEdit{}, errors.New("That task was forwarded to another note; change it there.")
	}
	// Split on the bare newline, so every line keeps its own ending.
	lines := strings.Split(body, "\n")
	if task.LineNumber < 0 || task.LineNumber >= len(lines) || strings.TrimRight(lines[task.LineNumber], " \t\r") != strings.TrimRight(task.RawText, " \t\r") {
		return "", 0, false, vault.RollEdit{}, nil
	}
	ending := ""
	if strings.HasSuffix(lines[task.LineNumber], "\r") {
		ending = "\r"
	}
	line := strings.TrimSuffix(lines[task.LineNumber], "\r")
	// An open task that is finished or cancelled may roll. The roll sets the
	// marker itself, so the line keeps its marker until the roll is known.
	rollable := roller != nil && in.State != nil && (*in.State == "done" || *in.State == "cancelled") && !task.Checked && !task.Cancelled
	if in.Text != nil {
		line = vault.SetTaskText(line, 0, *in.Text)
	}
	if in.State != nil {
		switch *in.State {
		case "open":
			line = vault.SetTaskInProgress(vault.SetTaskChecked(line, 0, false), 0, false)
		case "in-progress":
			line = vault.SetTaskInProgress(line, 0, true)
		case "cancelled":
			if !rollable {
				line = vault.SetTaskCancelled(line, 0, true)
			}
		}
	}
	if in.Due != nil {
		line = vault.SetTaskDue(line, 0, *in.Due)
	}
	if in.Priority != nil {
		line = vault.SetTaskPriority(line, 0, *in.Priority)
	}
	if in.Waiting != nil {
		line = vault.SetTaskWaiting(line, 0, *in.Waiting)
	}
	if in.Status != nil {
		line = vault.SetTaskField(line, 0, "status", *in.Status)
	}
	lines[task.LineNumber] = line + ending
	if rollable {
		action := vault.RollComplete
		if *in.State == "cancelled" {
			action = vault.RollCancel
		}
		// Last, so a due date changed in the same call is what the series counts from.
		rolled, err := roller(strings.Join(lines, "\n"), action)
		if err != nil {
			return "", 0, false, vault.RollEdit{}, err
		}
		if rolled.Applied {
			return "", task.LineNumber, true, rolled, nil
		}
		if action == vault.RollCancel {
			lines[task.LineNumber] = vault.SetTaskCancelled(line, 0, true) + ending
		}
	}
	if in.State != nil && *in.State == "done" {
		// Last, so a due date changed in the same call is what the series counts from.
		joined := strings.Join(lines, "\n")
		if done, recurring := vault.CompleteRecurringTaskOnLine(joined, task.LineNumber, vault.DialectApp, vault.RecurringOptions{Today: vault.TodayISO(time.Now())}); recurring {
			return done.Body, task.LineNumber + done.InsertedLines, true, vault.RollEdit{}, nil
		}
		lines[task.LineNumber] = vault.SetTaskChecked(line, 0, true) + ending
	}
	return strings.Join(lines, "\n"), task.LineNumber, true, vault.RollEdit{}, nil
}

// updateFileTask edits a whole-note task, whose fields are its frontmatter. Its
// state, its column and whether it is waiting are all one `status`, so asking for
// two of them in one call would have to drop one.
func updateFileTask(body string, task vault.Task, in UpdateTaskInput) (string, error) {
	if in.Text != nil {
		return "", fmt.Errorf("A task note has no line of text to change; use rename_note for its title.")
	}
	asked := 0
	for _, set := range []bool{in.State != nil, in.Status != nil, in.Waiting != nil} {
		if set {
			asked++
		}
	}
	if asked > 1 {
		return "", errors.New("A task note keeps state, status and waiting in its one frontmatter status: pass only one of them.")
	}
	now := time.Now()
	if in.State != nil {
		switch *in.State {
		case "open":
			body = vault.SetTaskFileStatus(body, false, now)
		case "done":
			// Already finished keeps the day it was finished.
			if !task.Checked {
				body = vault.SetTaskFileStatus(body, true, now)
			}
		case "in-progress":
			body = vault.SetTaskFileInProgress(body, true)
		case "cancelled":
			body = vault.SetTaskFileCancelled(body, true)
		}
	}
	if in.Due != nil {
		body = vault.SetTaskFileField(body, "due", *in.Due)
	}
	if in.Priority != nil {
		body = vault.SetTaskFileField(body, "priority", vault.TaskFilePriorityValue(*in.Priority))
	}
	if in.Waiting != nil {
		if *in.Waiting {
			body = vault.SetTaskFileStatusValue(body, "waiting", task.Checked, now)
		} else if task.Waiting {
			body = vault.SetTaskFileStatusValue(body, "open", task.Checked, now)
		}
	}
	if in.Status != nil {
		body = vault.SetTaskFileStatusValue(body, *in.Status, task.Checked, now)
	}
	return body, nil
}
