package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

const (
	// newCategoryLabel is the picker entry that makes a new category note.
	newCategoryLabel = "New category note…"
	// localVaultRefusal is the message of a remote vault for a key that
	// writes an event.
	localVaultRefusal = "Events need a local vault"
	// whenHelp is the help line of a question about the time of an event.
	whenHelp = "YYYY-MM-DD[THH:MM[-HH:MM]], start/end, today, tomorrow, +3 or mon..sun"
)

// scheduleSession starts the flow that writes a work session for t: it asks
// when, then asks which category note receives the session line.
func (a *App) scheduleSession(t vault.Task) {
	a.scheduleSessionAt(t, a.sessionPrefill())
}

// scheduleSessionAt is scheduleSession with the text that the question about
// the time starts with.
func (a *App) scheduleSessionAt(t vault.Task, initial string) {
	local, ok := a.backend.(*backend.Local)
	if !ok {
		a.notify(localVaultRefusal)
		return
	}
	a.promptFor("Time of the work session", initial, whenHelp, func(a *App, text string) {
		when, err := parseSessionWhen(strings.TrimSpace(text), a.taskClock())
		if err != nil {
			a.scheduleSessionAt(t, text)
			a.notifyError(err.Error())
			return
		}
		a.pickCategoryNote(local.Vault(), a.sessionWrite(local.Vault(), t, when))
	})
}

// sessionPrefill is the next whole hour, for one hour.
func (a *App) sessionPrefill() string {
	now := a.taskClock()
	return hourPrefill(time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location()))
}

// hourPrefill is the `when:` value of one hour that starts at start. An hour
// that crosses midnight is a range of two days.
func hourPrefill(start time.Time) string {
	end := start.Add(time.Hour)
	if end.Day() != start.Day() {
		return start.Format("2006-01-02T15:04") + "/" + end.Format("2006-01-02T15:04")
	}
	return start.Format("2006-01-02T15:04") + "-" + end.Format("15:04")
}

// parseSessionWhen reads the answer to the when prompt into a `when:` value.
func parseSessionWhen(text string, now time.Time) (string, error) {
	if strings.Contains(text, "/") {
		value := strings.ToUpper(text)
		if _, _, _, _, problem := vault.ParseWhenValue(value); problem != "" {
			return "", fmt.Errorf("The time range is not correct: %s", problem)
		}
		return value, nil
	}
	value, err := parseDueInput(text, now)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("Enter a date")
	}
	if _, _, _, _, problem := vault.ParseWhenValue(value); problem != "" {
		return "", fmt.Errorf("The time is not correct: %s", problem)
	}
	return value, nil
}

// categoryNote is a note that lists events.
type categoryNote struct {
	path  string
	title string
}

// categoryNotes lists the category notes of the vault by title.
func (a *App) categoryNotes(v *vault.Vault) []categoryNote {
	var out []categoryNote
	for _, path := range v.EventListNotes() {
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if meta, ok := a.noteMeta(path); ok {
			title = meta.Title
		}
		out = append(out, categoryNote{path: path, title: title})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].title) < strings.ToLower(out[j].title)
	})
	return out
}

// categoryWrite is a line that goes into a category note. The line is made
// when the note is known, because a work session first writes a block id
// into the note of its task.
type categoryWrite struct {
	line func() (string, error)
	// done is the start of the message that says where the line went.
	done string
}

// sessionWrite is the write of the session line of t.
func (a *App) sessionWrite(v *vault.Vault, t vault.Task, when string) categoryWrite {
	return categoryWrite{done: "Work session written in ", line: func() (string, error) {
		link, err := a.sessionLink(v, t)
		if err != nil {
			return "", err
		}
		return vault.SessionLine(t.Content, link, when), nil
	}}
}

// eventWrite is the write of the line of an event that has no task.
func eventWrite(text, when string) categoryWrite {
	return categoryWrite{done: "Event written in ", line: func() (string, error) {
		return "- " + strings.TrimSpace(text) + " when:" + when, nil
	}}
}

// pickCategoryNote opens the picker of category notes. The note that a
// line went to last is selected.
func (a *App) pickCategoryNote(v *vault.Vault, w categoryWrite) {
	notes := a.categoryNotes(v)
	last := a.lastCategoryNote()
	items := make([]menuItem, 0, len(notes)+1)
	cursor := 0
	for i, n := range notes {
		n := n
		if n.path == last {
			cursor = i
		}
		key := ""
		if i < 9 {
			key = fmt.Sprint(i + 1)
		}
		items = append(items, menuItem{key: key, label: n.title, run: func(a *App) {
			a.writeCategoryLine(w, n.path)
		}})
	}
	items = append(items, menuItem{key: "+", label: newCategoryLabel, run: func(a *App) {
		a.newCategoryNote(w)
	}})
	a.showMenu("Category note", items)
	a.overlay.(*menu).cursor = cursor
}

// newCategoryNote asks for a title and writes the line into a new category
// note, in the folder of the note used last.
func (a *App) newCategoryNote(w categoryWrite) {
	a.promptFor("New category note", "", "Title of the note", func(a *App, text string) {
		title := vault.SanitizeTitle(strings.TrimSpace(text))
		fail := func(msg string) {
			a.newCategoryNote(w)
			a.overlay.(*prompt).input = newTextInput(text)
			a.notifyError(msg)
		}
		if strings.TrimSpace(text) == "" || title == "" {
			fail("Enter a title")
			return
		}
		folder, sub := vault.FolderInbox, ""
		if meta, ok := a.noteMeta(a.lastCategoryNote()); ok {
			folder = meta.Folder
			sub = vault.FolderSubpathOf(meta.Path, meta.Folder, a.primaryAtRoot(), a.folderPaths())
		}
		for _, n := range a.idx.notes {
			if n.Folder == folder && strings.EqualFold(n.Title, title) &&
				vault.FolderSubpathOf(n.Path, n.Folder, a.primaryAtRoot(), a.folderPaths()) == sub {
				fail("A note with the name " + title + " exists")
				return
			}
		}
		line, err := w.line()
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		body := "---\npage-type: " + vault.EventListPageType + "\n---\n\n# " + title + "\n\n" + line + "\n"
		meta, err := a.backend.CreateNote(context.Background(), folder, title, sub, &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		a.rememberCategoryNote(meta.Path)
		a.finishSchedule(w, meta.Title)
	})
}

// sessionLink gives the link of a session to t. A checkbox task gets a block
// id on its line when it has none. A task note is linked by its title.
func (a *App) sessionLink(v *vault.Vault, t vault.Task) (string, error) {
	if t.Kind == "file" {
		return "[[" + t.NoteTitle + "]]", nil
	}
	id := ""
	err := a.mutateNote(t.SourcePath, func(body string) (string, bool) {
		next, got, ok := vault.SetTaskBlockID(body, t.TaskIndex, func() string {
			ids, _ := v.VaultBlockIDs()
			return vault.NewBlockID(ids, v.Rand)
		})
		if !ok {
			return body, false
		}
		id = got
		return next, next != body
	})
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("The task is no longer in %s", t.SourcePath)
	}
	return "[[" + t.NoteTitle + "#^" + id + "]]", nil
}

// writeCategoryLine adds the line at the end of a category note.
func (a *App) writeCategoryLine(w categoryWrite, path string) {
	line, err := w.line()
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	err = a.mutateNote(path, func(body string) (string, bool) {
		return vault.AppendLine(body, line), true
	})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.rememberCategoryNote(path)
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if meta, ok := a.noteMeta(path); ok {
		title = meta.Title
	}
	a.finishSchedule(w, title)
}

func (a *App) finishSchedule(w categoryWrite, title string) {
	a.notify(w.done + title)
	a.afterTaskChange()
}

// lastCategoryNote is the category note that a session went to last in this
// vault. It is empty when there is none or when the note is gone.
func (a *App) lastCategoryNote() string {
	path := readSessionFile().LastCategory[a.sessionKey()]
	if path == "" {
		return ""
	}
	if _, ok := a.noteMeta(path); !ok {
		return ""
	}
	return path
}

// rememberCategoryNote saves the category note in the session file. The file
// keeps one note for each saved vault and for this vault. It drops the notes
// of other vaults.
func (a *App) rememberCategoryNote(path string) {
	f := readSessionFile()
	keep := map[string]bool{filepath.Clean(a.sessionKey()): true}
	for _, saved := range config.LoadWorkspaces().Vaults {
		keep[filepath.Clean(saved.Root)] = true
	}
	next := map[string]string{}
	for key, note := range f.LastCategory {
		if keep[filepath.Clean(key)] {
			next[key] = note
		}
	}
	next[a.sessionKey()] = path
	f.LastCategory = next
	if err := writeSessionFile(f); err != nil {
		a.notifyError("Save workspace: " + err.Error())
	}
}
