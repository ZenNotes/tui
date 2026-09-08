package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ZenNotes/zennotescli/internal/periodic"
	"github.com/ZenNotes/zennotescli/internal/search"
	"github.com/ZenNotes/zennotescli/internal/templates"
	"github.com/ZenNotes/zennotescli/internal/vault"
)

// index is the vault as the UI knows it: every note's metadata, the folder
// tree, tags, tasks and settings. It is rebuilt on file changes.
type index struct {
	notes    []vault.NoteMeta
	byPath   map[string]vault.NoteMeta
	folders  []vault.FolderEntry
	tags     []tagCount
	tasks    []vault.Task
	settings vault.VaultSettings
	primary  vault.PrimaryNotesLocation
	search   []search.Entry
	loadedAt time.Time
	tasksAt  time.Time
}

type tagCount struct {
	tag   string
	count int
}

type indexLoadedMsg struct {
	idx *index
	err error
}

type tasksLoadedMsg struct {
	tasks []vault.Task
	err   error
}

func (a *App) loadIndexCmd() func() interface{} {
	b := a.backend
	return func() interface{} {
		ctx := context.Background()
		idx := &index{byPath: map[string]vault.NoteMeta{}}
		notes, err := b.ListNotes(ctx)
		if err != nil {
			return indexLoadedMsg{err: err}
		}
		folders, err := b.ListFolders(ctx)
		if err != nil {
			return indexLoadedMsg{err: err}
		}
		settings, err := b.VaultSettings(ctx)
		if err != nil {
			settings = vault.ParseSettings(nil)
		}
		desc, err := b.Describe(ctx)
		if err != nil {
			return indexLoadedMsg{err: err}
		}
		idx.notes = notes
		for _, n := range notes {
			idx.byPath[n.Path] = n
		}
		idx.folders = folders
		idx.settings = settings
		idx.primary = desc.PrimaryNotesLocation
		idx.tags = countTags(notes)
		idx.search = search.Index(notes)
		idx.loadedAt = time.Now()
		return indexLoadedMsg{idx: idx}
	}
}

func (a *App) loadTasksCmd() func() interface{} {
	b := a.backend
	return func() interface{} {
		tasks, err := b.ScanTasks(context.Background(), vault.ParseTasksOptions{Dialect: vault.DialectApp})
		return tasksLoadedMsg{tasks: tasks, err: err}
	}
}

func countTags(notes []vault.NoteMeta) []tagCount {
	counts := map[string]int{}
	for _, n := range notes {
		if n.Folder == vault.FolderTrash {
			continue
		}
		for _, t := range n.Tags {
			counts[strings.ToLower(t)]++
		}
	}
	out := make([]tagCount, 0, len(counts))
	for tag, c := range counts {
		out = append(out, tagCount{tag, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].tag < out[j].tag
	})
	return out
}

func (a *App) primaryAtRoot() bool {
	return a.idx != nil && a.idx.primary == vault.PrimaryNotesRoot
}

func (a *App) folderPaths() map[string]string {
	if a.idx == nil {
		return nil
	}
	return a.idx.settings.SystemFolderPaths
}

// noteMeta looks a note up in the index.
func (a *App) noteMeta(path string) (vault.NoteMeta, bool) {
	if a.idx == nil {
		return vault.NoteMeta{}, false
	}
	m, ok := a.idx.byPath[path]
	return m, ok
}

// notesIn lists the notes of one bucket, newest first.
func (a *App) notesIn(folder vault.NoteFolder) []vault.NoteMeta {
	if a.idx == nil {
		return nil
	}
	out := []vault.NoteMeta{}
	for _, n := range a.idx.notes {
		if n.Folder == folder {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// folderLabel is a bucket's display name, honoring `[folder_labels]`.
func (a *App) folderLabel(folder vault.NoteFolder) string {
	if custom := a.prefs.SystemFolderLabels[string(folder)]; strings.TrimSpace(custom) != "" {
		return custom
	}
	switch folder {
	case vault.FolderInbox:
		if a.primaryAtRoot() {
			return "Notes"
		}
		return "Inbox"
	case vault.FolderQuick:
		return "Quick Notes"
	case vault.FolderArchive:
		return "Archive"
	case vault.FolderTrash:
		return "Trash"
	}
	return string(folder)
}

// relDirFor is the vault-relative directory of a bucket subpath.
func (a *App) relDirFor(folder vault.NoteFolder, subpath string) string {
	sub := strings.Trim(subpath, "/")
	top := ""
	if !(folder == vault.FolderInbox && a.primaryAtRoot()) {
		top = vault.ResolveFolderPath(folder, a.folderPaths())
	}
	switch {
	case top == "":
		return sub
	case sub == "":
		return top
	}
	return top + "/" + sub
}

// folderOfPath classifies a vault-relative path with the index's layout.
func (a *App) folderOfPath(rel string) (vault.NoteFolder, string) {
	folder, ok := vault.FolderForRelativePath(rel, a.folderPaths())
	if !ok {
		return vault.FolderInbox, ""
	}
	sub := vault.FolderSubpathOf(rel, folder, a.primaryAtRoot(), a.folderPaths())
	return folder, sub
}

// periodicSettings is what the daily, weekly and monthly commands need.
func (a *App) periodicLocation(kind periodic.Kind, date time.Time) (folder vault.NoteFolder, subpath, title, relPath string) {
	settings := vault.ParseSettings(nil)
	if a.idx != nil {
		settings = a.idx.settings
	}
	loc := periodic.LocationFor(kind, date, settings, a.primaryAtRoot())
	rel := periodic.RelPathFor(kind, date, settings, a.primaryAtRoot(), a.folderPaths())
	return vault.FolderInbox, loc.Subpath, loc.Title, rel
}

func (a *App) periodicEnabled(kind periodic.Kind) bool {
	if a.idx == nil {
		return false
	}
	switch kind {
	case periodic.Weekly:
		return a.idx.settings.WeeklyNotes.Enabled
	case periodic.Monthly:
		return a.idx.settings.MonthlyNotes.Enabled
	}
	return a.idx.settings.DailyNotes.Enabled
}

func (a *App) periodicTemplateID(kind periodic.Kind) string {
	if a.idx == nil {
		return ""
	}
	switch kind {
	case periodic.Weekly:
		return a.idx.settings.WeeklyNotes.TemplateID
	case periodic.Monthly:
		return a.idx.settings.MonthlyNotes.TemplateID
	}
	return a.idx.settings.DailyNotes.TemplateID
}

// dailyDueByPath maps daily notes to their dates so undated tasks inside
// them land on the calendar for that day.
func (a *App) dailyDueByPath() map[string]string {
	out := map[string]string{}
	if a.idx == nil || !a.idx.settings.DailyNotes.Enabled || !a.idx.settings.DailyNotes.TasksDueOnNoteDate {
		return out
	}
	for _, n := range a.idx.notes {
		if n.Folder != vault.FolderInbox {
			continue
		}
		_, sub := a.folderOfPath(n.Path)
		if date, ok := periodic.DateOf(periodic.Daily, sub, n.Title, a.idx.settings, a.primaryAtRoot()); ok {
			out[n.Path] = date.Format("2006-01-02")
		}
	}
	return out
}

// displayTasks are the tasks the Tasks surfaces show: daily-note due
// inference applied, archived notes retired unless the preference keeps them.
func (a *App) displayTasks() []vault.Task {
	if a.idx == nil {
		return nil
	}
	tasks := vault.InferDailyTaskDueDates(a.idx.tasks, a.dailyDueByPath())
	return vault.FilterTasksForDisplay(tasks, a.prefs.ShowArchivedTasks)
}

// allTemplates merges built-ins with the vault's custom templates.
func (a *App) allTemplates() []templates.Template {
	custom, err := a.backend.ListTemplates(context.Background())
	if err != nil {
		custom = nil
	}
	return templates.All(custom, a.prefs.HideBuiltinTemplates)
}

// favoriteKeys are the pinned notes and folders.
func (a *App) favorites() []string {
	if a.idx == nil {
		return nil
	}
	return a.idx.settings.Favorites
}

func (a *App) isFavorite(key string) bool {
	for _, f := range a.favorites() {
		if f == key {
			return true
		}
	}
	return false
}
