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
	note, err := b.ReadNote(ctx, task.SourcePath)
	if err != nil {
		return nil, err
	}
	var next string
	if task.Kind == "file" {
		if next, err = updateFileTask(note.Body, in); err != nil {
			return nil, err
		}
	} else {
		var found bool
		if next, found, err = updateTaskLine(note.Body, *task, in); err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
	}
	if next != note.Body {
		if _, err := b.WriteNote(ctx, task.SourcePath, next); err != nil {
			return nil, err
		}
	}
	return findTask(ctx, b, task.SourcePath, index)
}

func findTask(ctx context.Context, b Backend, rel, index string) (*vault.Task, error) {
	tasks, err := b.ScanTasksForPath(ctx, rel, vault.ParseTasksOptions{IncludeExcluded: true, Dialect: vault.DialectCLI})
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

func updateTaskLine(body string, task vault.Task, in UpdateTaskInput) (string, bool, error) {
	if task.Forwarded && in.State != nil {
		return "", false, errors.New("That task was forwarded to another note; change it there.")
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if task.LineNumber < 0 || task.LineNumber >= len(lines) || strings.TrimRight(lines[task.LineNumber], " \t") != strings.TrimRight(task.RawText, " \t") {
		return "", false, nil
	}
	line := lines[task.LineNumber]
	if in.Text != nil {
		line = vault.SetTaskText(line, 0, *in.Text)
	}
	if in.State != nil {
		switch *in.State {
		case "open":
			line = vault.SetTaskInProgress(vault.SetTaskChecked(line, 0, false), 0, false)
		case "done":
			line = vault.SetTaskChecked(line, 0, true)
		case "in-progress":
			line = vault.SetTaskInProgress(line, 0, true)
		case "cancelled":
			line = vault.SetTaskCancelled(line, 0, true)
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
	lines[task.LineNumber] = line
	return strings.Join(lines, "\n"), true, nil
}

// updateFileTask edits a whole-note task, whose fields are its frontmatter.
func updateFileTask(body string, in UpdateTaskInput) (string, error) {
	if in.Text != nil {
		return "", fmt.Errorf("A task note has no line of text to change; use rename_note for its title.")
	}
	now := time.Now()
	if in.State != nil {
		switch *in.State {
		case "open":
			body = vault.SetTaskFileStatus(body, false, now)
		case "done":
			body = vault.SetTaskFileStatus(body, true, now)
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
		value := ""
		if *in.Waiting {
			value = "true"
		}
		body = vault.SetTaskFileField(body, "waiting", value)
	}
	if in.Status != nil {
		body = vault.SetTaskFileField(body, "status", *in.Status)
	}
	return body, nil
}
