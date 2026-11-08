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

// newCategoryLabel is the picker entry that makes a new category note.
const newCategoryLabel = "New category note…"

// scheduleSession starts the flow that writes a work session for t: it asks
// when, then asks which category note receives the session line.
func (a *App) scheduleSession(t vault.Task) {
	local, ok := a.backend.(*backend.Local)
	if !ok {
		a.notify("Events need a local vault")
		return
	}
	a.promptFor("Schedule a work session", a.sessionPrefill(), "YYYY-MM-DD[THH:MM[-HH:MM]], start/end, today, tomorrow, +3 or mon..sun", func(a *App, text string) {
		when, err := parseSessionWhen(strings.TrimSpace(text), a.taskClock())
		if err != nil {
			a.scheduleSession(t)
			a.overlay.(*prompt).input = newTextInput(text)
			a.notifyError(err.Error())
			return
		}
		a.pickCategoryNote(local.Vault(), t, when)
	})
}

// sessionPrefill is the next whole hour, for one hour.
func (a *App) sessionPrefill() string {
	now := a.taskClock()
	start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
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
			return "", fmt.Errorf("not a time range: %s", problem)
		}
		return value, nil
	}
	value, err := parseDueInput(text, now)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("enter a date")
	}
	if _, _, _, _, problem := vault.ParseWhenValue(value); problem != "" {
		return "", fmt.Errorf("not a time: %s", problem)
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

// pickCategoryNote opens the picker of category notes. The note that a
// session went to last is selected.
func (a *App) pickCategoryNote(v *vault.Vault, t vault.Task, when string) {
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
			a.writeSession(v, t, when, n.path)
		}})
	}
	items = append(items, menuItem{key: "+", label: newCategoryLabel, run: func(a *App) {
		a.newCategoryNote(v, t, when)
	}})
	a.showMenu("Category note", items)
	a.overlay.(*menu).cursor = cursor
}

// newCategoryNote asks for a title and writes the session into a new
// category note, in the folder of the note used last.
func (a *App) newCategoryNote(v *vault.Vault, t vault.Task, when string) {
	a.promptFor("New category note", "", "Title of the note", func(a *App, text string) {
		title := vault.SanitizeTitle(strings.TrimSpace(text))
		fail := func(msg string) {
			a.newCategoryNote(v, t, when)
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
				fail("A note named " + title + " exists")
				return
			}
		}
		link, err := a.sessionLink(v, t)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		line := vault.SessionLine(t.Content, link, when)
		body := "---\npage-type: " + vault.EventListPageType + "\n---\n\n# " + title + "\n\n" + line + "\n"
		meta, err := a.backend.CreateNote(context.Background(), folder, title, sub, &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		a.rememberCategoryNote(meta.Path)
		a.finishSchedule(meta.Title)
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
		return "", fmt.Errorf("the task is no longer in %s", t.SourcePath)
	}
	return "[[" + t.NoteTitle + "#^" + id + "]]", nil
}

// writeSession adds the session line at the end of a category note.
func (a *App) writeSession(v *vault.Vault, t vault.Task, when, path string) {
	link, err := a.sessionLink(v, t)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	line := vault.SessionLine(t.Content, link, when)
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
	a.finishSchedule(title)
}

func (a *App) finishSchedule(title string) {
	a.notify("Scheduled in " + title)
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
