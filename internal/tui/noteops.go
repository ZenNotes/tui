package tui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/search"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// createNote makes a note and opens it.
func (a *App) createNote(folder vault.NoteFolder, title, subpath string, body *string, open bool) {
	meta, err := a.backend.CreateNote(context.Background(), folder, title, subpath, body)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.ignoreChange(meta.Path)
	a.addNoteToIndex(meta)
	if open {
		a.openNote(meta.Path, true)
		if buf := a.buffers[meta.Path]; buf != nil {
			buf.ed.GotoLine(buf.ed.LineCount() - 1)
			if a.prefs.VimMode {
				buf.ed.HandleKey(vim.R('A'))
			}
		}
	}
	a.refreshIndex()
}

// addNoteToIndex makes a fresh note visible before the next full reload.
func (a *App) addNoteToIndex(meta vault.NoteMeta) {
	if a.idx == nil {
		return
	}
	if _, ok := a.idx.byPath[meta.Path]; ok {
		return
	}
	a.idx.notes = append(a.idx.notes, meta)
	a.idx.byPath[meta.Path] = meta
	a.sidebar.rebuild(a)
}

func (a *App) newNoteHere() {
	folder, sub := a.currentFolderContext()
	if folder == vault.FolderTrash {
		folder, sub = vault.FolderInbox, ""
	}
	where := a.folderLabel(folder)
	if sub != "" {
		where += "/" + sub
	}
	a.promptFor("New note in "+where, "", "Title", func(a *App, title string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		a.createNote(folder, title, sub, nil, true)
	})
}

func (a *App) newQuickNote() {
	title := a.prefs.QuickNoteTitlePrefix
	if strings.TrimSpace(title) == "" {
		title = "Quick Note"
	}
	if a.prefs.QuickNoteDateTitle {
		title += " " + time.Now().Format("2006-01-02 15:04")
	}
	a.createNote(vault.FolderQuick, title, "", nil, true)
}

// mutateNote rewrites a note's body through its open buffer when it has
// one, else straight through the backend.
func (a *App) mutateNote(path string, mutate func(body string) (string, bool)) error {
	if buf, ok := a.buffers[path]; ok {
		next, changed := mutate(buf.ed.Text())
		if !changed {
			return nil
		}
		cur := buf.ed.Cursor()
		buf.ed.ReplaceText(next)
		buf.ed.SetCursor(cur)
		return a.saveBuffer(buf)
	}
	content, err := a.backend.ReadNote(context.Background(), path)
	if err != nil {
		return err
	}
	next, changed := mutate(content.Body)
	if !changed {
		return nil
	}
	if _, err := a.backend.WriteNote(context.Background(), path, next); err != nil {
		return err
	}
	a.ignoreChange(path)
	return nil
}

func (a *App) renameNote(path string) {
	meta, ok := a.noteMeta(path)
	if !ok {
		return
	}
	a.promptFor("Rename note", meta.Title, "New title", func(a *App, title string) {
		title = strings.TrimSpace(title)
		if title == "" || title == meta.Title {
			return
		}
		if buf, ok := a.buffers[path]; ok {
			if err := a.saveBuffer(buf); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
		next, err := a.backend.RenameNote(context.Background(), path, title)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.repointNote(path, next.Path)
		a.notify("Renamed to " + next.Title)
		a.refreshIndex()
	})
}

// repointNote follows a note that moved: tabs, buffers, recents and jumps.
func (a *App) repointNote(oldPath, newPath string) {
	if oldPath == newPath {
		return
	}
	if buf, ok := a.buffers[oldPath]; ok {
		delete(a.buffers, oldPath)
		buf.path = newPath
		if content, err := a.backend.ReadNote(context.Background(), newPath); err == nil {
			buf.meta = content.NoteMeta
			if !buf.dirty() {
				cur := buf.ed.Cursor()
				buf.ed.ReplaceText(content.Body)
				buf.ed.SetCursor(cur)
				buf.savedText = content.Body
				buf.ed.MarkSaved()
			} else {
				buf.savedText = content.Body
			}
		}
		buf.ed.SetHooks(a.editorHooks(buf))
		a.buffers[newPath] = buf
	}
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if t.path == oldPath {
				t.path = newPath
			}
		}
	}
	for i, r := range a.recent {
		if r == oldPath {
			a.recent[i] = newPath
		}
	}
	for i := range a.jumps {
		if a.jumps[i].path == oldPath {
			a.jumps[i].path = newPath
		}
	}
	if mode, ok := a.noteModes[oldPath]; ok {
		delete(a.noteModes, oldPath)
		a.noteModes[newPath] = mode
	}
	a.ignoreChange(newPath)
	a.markSessionDirty()
}

// dropNote forgets a note that left the vault (trashed, deleted) from every
// pane, keeping the buffer for trash restores.
func (a *App) dropNote(path string) {
	for _, p := range a.panes.leaves() {
		for i := len(p.tabs) - 1; i >= 0; i-- {
			if p.tabs[i].path == path {
				p.tabs = append(p.tabs[:i], p.tabs[i+1:]...)
				if p.active >= len(p.tabs) {
					p.active = max(0, len(p.tabs)-1)
				} else if p.active > i {
					p.active--
				}
			}
		}
	}
	delete(a.buffers, path)
	if len(a.activePane.tabs) == 0 && len(a.panes.leaves()) > 1 {
		a.panes.remove(a.activePane)
		a.activePane = a.panes.leaves()[0]
	}
	a.markSessionDirty()
}

func (a *App) trashNote(path string) {
	meta, ok := a.noteMeta(path)
	if !ok {
		return
	}
	if meta.Folder == vault.FolderTrash {
		a.deleteNoteForever(path)
		return
	}
	do := func() {
		if buf, ok := a.buffers[path]; ok {
			_ = a.saveBuffer(buf)
		}
		next, err := a.backend.MoveToTrash(context.Background(), path)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.dropNote(path)
		a.ignoreChange(next.Path)
		a.notify("Moved to trash: " + meta.Title)
		a.refreshIndex()
	}
	a.confirm("Move \""+meta.Title+"\" to trash?", do)
}

func (a *App) deleteNoteForever(path string) {
	meta, _ := a.noteMeta(path)
	a.confirm("Delete \""+meta.Title+"\" permanently?", func() {
		if err := a.backend.DeleteNote(context.Background(), path); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.dropNote(path)
		a.notify("Deleted " + meta.Title)
		a.refreshIndex()
	})
}

func (a *App) restoreNote(path string) {
	next, err := a.backend.RestoreFromTrash(context.Background(), path)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.repointNote(path, next.Path)
	a.notify("Restored " + next.Title)
	a.refreshIndex()
}

func (a *App) archiveNote(path string) {
	if buf, ok := a.buffers[path]; ok {
		_ = a.saveBuffer(buf)
	}
	next, err := a.backend.ArchiveNote(context.Background(), path)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.repointNote(path, next.Path)
	a.notify("Archived " + next.Title)
	a.refreshIndex()
}

func (a *App) unarchiveNote(path string) {
	next, err := a.backend.UnarchiveNote(context.Background(), path)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.repointNote(path, next.Path)
	a.notify("Unarchived " + next.Title)
	a.refreshIndex()
}

func (a *App) moveNote(path string, folder vault.NoteFolder, subpath string) {
	if buf, ok := a.buffers[path]; ok {
		_ = a.saveBuffer(buf)
	}
	next, err := a.backend.MoveNote(context.Background(), path, folder, subpath)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.repointNote(path, next.Path)
	where := a.folderLabel(folder)
	if subpath != "" {
		where += "/" + subpath
	}
	a.notify("Moved to " + where)
	a.refreshIndex()
}

func (a *App) duplicateNote(path string) {
	if buf, ok := a.buffers[path]; ok {
		_ = a.saveBuffer(buf)
	}
	next, err := a.backend.DuplicateNote(context.Background(), path)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.ignoreChange(next.Path)
	a.addNoteToIndex(next)
	a.openNote(next.Path, true)
	a.refreshIndex()
}

func (a *App) emptyTrash() {
	a.confirm("Empty the trash? This cannot be undone.", func() {
		if err := a.backend.EmptyTrash(context.Background()); err != nil {
			a.notifyError(err.Error())
			return
		}
		for _, n := range a.notesIn(vault.FolderTrash) {
			a.dropNote(n.Path)
		}
		a.notify("Trash emptied")
		a.refreshIndex()
	})
}

// moveNotePicker offers every folder as a destination.
func (a *App) moveNotePicker(path string) {
	items := []paletteItem{}
	for _, folder := range []vault.NoteFolder{vault.FolderInbox, vault.FolderQuick, vault.FolderArchive} {
		items = append(items, paletteItem{label: a.folderLabel(folder), id: string(folder), data: ""})
	}
	if a.idx != nil {
		for _, f := range a.idx.folders {
			if f.Folder == vault.FolderTrash {
				continue
			}
			items = append(items, paletteItem{label: a.folderLabel(f.Folder) + "/" + f.Subpath, id: string(f.Folder), data: f.Subpath})
		}
	}
	a.overlay = &palette{title: "Move note to", placeholder: "Folder", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		a.moveNote(path, vault.NoteFolder(it.id), it.data.(string))
	}}
}

// --- active note conveniences ---

func (a *App) activePath() string {
	if buf := a.activeBuffer(); buf != nil {
		return buf.path
	}
	return ""
}

func (a *App) renameActiveNote() {
	if p := a.activePath(); p != "" {
		a.renameNote(p)
	} else {
		a.notify("No note is active")
	}
}

func (a *App) moveActiveNotePrompt() {
	if p := a.activePath(); p != "" {
		a.moveNotePicker(p)
	} else {
		a.notify("No note is active")
	}
}

func (a *App) trashActiveNote() {
	if p := a.activePath(); p != "" {
		a.trashNote(p)
	} else {
		a.notify("No note is active")
	}
}

func (a *App) archiveActiveNote() {
	p := a.activePath()
	if p == "" {
		a.notify("No note is active")
		return
	}
	if meta, ok := a.noteMeta(p); ok && meta.Folder == vault.FolderArchive {
		a.unarchiveNote(p)
		return
	}
	a.archiveNote(p)
}

func (a *App) toggleActiveFavorite() {
	p := a.activePath()
	if p == "" {
		if a.focus == focusSidebar {
			if folder, sub, ok := a.sidebar.selectedFolder(); ok {
				a.toggleFavorite(string(folder) + ":" + sub)
				return
			}
		}
		a.notify("No note is active")
		return
	}
	a.toggleFavorite(p)
}

func (a *App) toggleFavorite(key string) {
	err := a.backend.UpdateVaultSettings(context.Background(), func(raw map[string]any) {
		current := []string{}
		if list, ok := raw["favorites"].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok {
					current = append(current, s)
				}
			}
		}
		next := []any{}
		found := false
		for _, f := range current {
			if f == key {
				found = true
				continue
			}
			next = append(next, f)
		}
		if !found {
			next = append(next, key)
		}
		raw["favorites"] = next
	})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if a.isFavorite(key) {
		a.notify("Removed from favorites")
	} else {
		a.notify("Added to favorites")
	}
	a.refreshIndex()
}

func (a *App) copyActiveNote() {
	buf := a.activeBuffer()
	if buf == nil {
		a.notify("No note is active")
		return
	}
	if err := clipboard.WriteAll(buf.ed.Text()); err != nil {
		a.notifyError("Clipboard unavailable: " + err.Error())
		return
	}
	a.notify("Copied note as Markdown")
}

func (a *App) copyActiveLink() {
	p := a.activePath()
	if p == "" {
		a.notify("No note is active")
		return
	}
	meta, _ := a.noteMeta(p)
	link := meta.Link
	if link == "" {
		link = vault.BuildOpenNoteDeepLink(p)
	}
	if err := clipboard.WriteAll(link); err != nil {
		a.notifyError("Clipboard unavailable: " + err.Error())
		return
	}
	a.notify("Copied " + link)
}

func (a *App) openActiveInDesktop() {
	p := a.activePath()
	if p == "" {
		a.notify("No note is active")
		return
	}
	link := vault.BuildOpenNoteDeepLink(p)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	if err := cmd.Start(); err != nil {
		a.notifyError("Could not hand off to the desktop app: " + err.Error())
		return
	}
	a.notify("Opened in ZenNotes")
}

var (
	trailingWSRe = regexp.MustCompile(`[ \t]+$`)
	manyBlanksRe = regexp.MustCompile(`\n{3,}`)
	listMarkerRe = regexp.MustCompile(`^(\s*)[*+]\s+`)
	headingSpace = regexp.MustCompile(`^(#{1,6})([^#\s])`)
)

// formatActiveNote tidies the note: trailing whitespace, blank-line runs,
// list markers and heading spacing, the way the desktop's formatter does.
func (a *App) formatActiveNote() {
	buf := a.activeBuffer()
	if buf == nil {
		a.notify("No note is active")
		return
	}
	text := buf.ed.Text()
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = trailingWSRe.ReplaceAllString(line, "")
		line = listMarkerRe.ReplaceAllString(line, "${1}- ")
		line = headingSpace.ReplaceAllString(line, "$1 $2")
		lines[i] = line
	}
	next := strings.Join(lines, "\n")
	next = manyBlanksRe.ReplaceAllString(next, "\n\n")
	next = strings.TrimRight(next, "\n") + "\n"
	if next == text {
		a.notify("Note is already formatted")
		return
	}
	cur := buf.ed.Cursor()
	buf.ed.ReplaceText(next)
	buf.ed.SetCursor(cur)
	a.notify("Formatted note")
}

var fenceRe = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")

// --- periodic notes ---

// openPeriodic opens today's (or an offset's) daily, weekly or monthly
// note, creating it from its template when missing.
func (a *App) openPeriodic(kind periodic.Kind, offset int) {
	a.openPeriodicOn(kind, shiftPeriod(kind, time.Now(), offset))
}

func shiftPeriod(kind periodic.Kind, date time.Time, offset int) time.Time {
	switch kind {
	case periodic.Weekly:
		return date.AddDate(0, 0, 7*offset)
	case periodic.Monthly:
		return date.AddDate(0, offset, 0)
	}
	return date.AddDate(0, 0, offset)
}

func (a *App) openPeriodicOn(kind periodic.Kind, date time.Time) {
	if a.idx == nil {
		return
	}
	if !a.periodicEnabled(kind) && kind != periodic.Daily {
		a.notify(string(kind) + " notes are off in this vault's settings")
		return
	}
	folder, sub, title, rel := a.periodicLocation(kind, date)
	if _, ok := a.noteMeta(rel); ok {
		a.openNote(rel, true)
		return
	}
	body := a.periodicBody(kind, title, date)
	meta, err := a.backend.CreateNote(context.Background(), folder, title, sub, &body)
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.ignoreChange(meta.Path)
	a.addNoteToIndex(meta)
	if kind == periodic.Daily {
		a.rolloverUnfinishedTasks(meta.Path, date)
	}
	a.openNote(meta.Path, true)
	if buf := a.buffers[meta.Path]; buf != nil {
		buf.ed.GotoLine(buf.ed.LineCount() - 1)
	}
	a.refreshIndex()
}

func (a *App) periodicBody(kind periodic.Kind, title string, date time.Time) string {
	id := a.periodicTemplateID(kind)
	if id != "" {
		for _, t := range a.allTemplates() {
			if t.ID == id || t.BuiltinID == id {
				return templates.Render(t.Body, title, date).Body
			}
		}
	}
	return "# " + title + "\n\n"
}

// --- templates ---

func (a *App) openTemplatePicker(insert bool) {
	all := a.allTemplates()
	items := []paletteItem{}
	for _, t := range all {
		items = append(items, paletteItem{label: t.Name, detail: t.Description, hint: t.Category, id: t.ID, data: t})
	}
	title := "New note from template"
	if insert {
		title = "Insert template"
	}
	a.overlay = &palette{title: title, placeholder: "Template", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		t := it.data.(templates.Template)
		if insert {
			a.insertTemplate(t)
			return
		}
		a.createFromTemplate(t)
	}}
}

func (a *App) createFromTemplate(t templates.Template) {
	suggested := templates.RenderTitle(t.TitleTemplate, "", time.Now())
	a.promptFor("Title for "+t.Name, suggested, "Enter to create", func(a *App, title string) {
		title = strings.TrimSpace(title)
		if title == "" {
			title = t.Name
		}
		rendered := templates.Render(t.Body, title, time.Now())
		body := rendered.Body
		folder, sub := t.TargetFolder, t.TargetSubpath
		if folder == "" {
			folder, sub = a.currentFolderContext()
			if folder == vault.FolderTrash {
				folder, sub = vault.FolderInbox, ""
			}
		}
		meta, err := a.backend.CreateNote(context.Background(), folder, title, sub, &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		a.openNote(meta.Path, true)
		if buf := a.buffers[meta.Path]; buf != nil && rendered.CursorOffset >= 0 {
			buf.ed.SetCursor(posForOffset(buf.ed.Text(), rendered.CursorOffset))
		}
		a.refreshIndex()
	})
}

func (a *App) insertTemplate(t templates.Template) {
	buf := a.activeBuffer()
	if buf == nil {
		a.notify("Open a note to insert into")
		return
	}
	meta, _ := a.noteMeta(buf.path)
	rendered := templates.Render(t.Body, meta.Title, time.Now())
	body := strings.TrimPrefix(rendered.Body, "# "+meta.Title+"\n")
	buf.ed.InsertAtCursor(strings.TrimRight(body, "\n"))
	a.notify("Inserted " + t.Name)
}

// posForOffset maps a rune offset to a position.
func posForOffset(text string, offset int) vim.Pos {
	line, col := 0, 0
	for i, r := range []rune(text) {
		if i == offset {
			break
		}
		if r == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return vim.Pos{Line: line, Col: col}
}

// --- folders ---

func (a *App) createFolderPrompt(folder vault.NoteFolder, parent string) {
	a.promptFor("New folder in "+a.folderLabel(folder)+pathSuffix(parent), "", "Folder name", func(a *App, name string) {
		name = strings.TrimSpace(strings.Trim(name, "/"))
		if name == "" {
			return
		}
		sub := name
		if parent != "" {
			sub = parent + "/" + name
		}
		if err := a.backend.CreateFolder(context.Background(), folder, sub); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.notify("Created folder " + sub)
		a.refreshIndex()
	})
}

func pathSuffix(sub string) string {
	if sub == "" {
		return ""
	}
	return "/" + sub
}

func (a *App) renameFolderPrompt(folder vault.NoteFolder, sub string) {
	base := sub
	parent := ""
	if i := strings.LastIndex(sub, "/"); i >= 0 {
		parent, base = sub[:i], sub[i+1:]
	}
	a.promptFor("Rename folder", base, "New name", func(a *App, name string) {
		name = strings.TrimSpace(strings.Trim(name, "/"))
		if name == "" || name == base {
			return
		}
		next := name
		if parent != "" {
			next = parent + "/" + name
		}
		for _, buf := range a.buffers {
			_ = a.saveBuffer(buf)
		}
		if _, err := a.backend.RenameFolder(context.Background(), folder, sub, next); err != nil {
			a.notifyError(err.Error())
			return
		}
		oldDir := a.relDirFor(folder, sub)
		newDir := a.relDirFor(folder, next)
		for path := range a.buffers {
			if strings.HasPrefix(path, oldDir+"/") {
				a.repointNote(path, newDir+path[len(oldDir):])
			}
		}
		a.notify("Renamed folder to " + next)
		a.refreshIndex()
	})
}

func (a *App) deleteFolderConfirm(folder vault.NoteFolder, sub string) {
	a.confirm("Delete folder \""+sub+"\" and everything in it?", func() {
		if err := a.backend.DeleteFolder(context.Background(), folder, sub); err != nil {
			a.notifyError(err.Error())
			return
		}
		dir := a.relDirFor(folder, sub)
		for path := range a.buffers {
			if strings.HasPrefix(path, dir+"/") {
				a.dropNote(path)
			}
		}
		a.notify("Deleted folder " + sub)
		a.refreshIndex()
	})
}

// --- pickers ---

func (a *App) openBufferPicker() {
	items := []paletteItem{}
	seen := map[string]bool{}
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if seen[t.path] {
				continue
			}
			seen[t.path] = true
			label := a.tabTitle(t)
			detail := t.path
			if t.view != nil {
				detail = "view"
			}
			items = append(items, paletteItem{label: label, detail: detail, id: t.path})
		}
	}
	for _, r := range a.recent {
		if seen[r] {
			continue
		}
		if meta, ok := a.noteMeta(r); ok {
			seen[r] = true
			items = append(items, paletteItem{label: meta.Title, detail: r, hint: "recent", id: r})
		}
	}
	a.overlay = &palette{title: "Open buffers", placeholder: "Buffer", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		if isVirtualPath(it.id) {
			a.openVirtualByPath(it.id)
			return
		}
		a.openNote(it.id, true)
	}}
}

func (a *App) openNoteSearch(initial string) {
	p := &palette{title: "Search notes", placeholder: "Type to search titles, tags and text", input: newTextInput(initial), minQuery: 0, highlight: true}
	p.source = func(a *App, query string) []paletteItem {
		if a.idx == nil {
			return nil
		}
		var notes []vault.NoteMeta
		if strings.TrimSpace(query) == "" {
			seen := map[string]bool{}
			for _, r := range a.recent {
				if meta, ok := a.noteMeta(r); ok && !seen[r] {
					seen[r] = true
					notes = append(notes, meta)
				}
			}
			for _, n := range a.notesIn(vault.FolderInbox) {
				if len(notes) >= 30 {
					break
				}
				if !seen[n.Path] {
					seen[n.Path] = true
					notes = append(notes, n)
				}
			}
		} else {
			notes = search.Notes(a.idx.search, query, 50, false)
		}
		items := make([]paletteItem, 0, len(notes))
		for _, n := range notes {
			items = append(items, paletteItem{label: n.Title, detail: n.Path, hint: shortFolder(a, n), id: n.Path, data: n})
		}
		return items
	}
	p.onSelect = func(a *App, it paletteItem) { a.openNote(it.id, true) }
	p.onEmpty = func(a *App, query string) {
		folder, sub := a.currentFolderContext()
		if folder == vault.FolderTrash {
			folder, sub = vault.FolderInbox, ""
		}
		a.createNote(folder, query, sub, nil, true)
	}
	p.onDelete = func(a *App, it paletteItem) { a.trashNote(it.id) }
	p.emptyHint = "No matches. Enter creates a note with this title."
	p.refilter(a)
	a.overlay = p
}

func shortFolder(a *App, n vault.NoteMeta) string {
	if n.Folder == vault.FolderInbox {
		return ""
	}
	return a.folderLabel(n.Folder)
}

func (a *App) openTextSearch(initial string) {
	p := &palette{title: "Search vault text", placeholder: "Text to find (2+ characters)", input: newTextInput(initial), minQuery: 2}
	p.source = func(a *App, query string) []paletteItem {
		matches, err := a.backend.SearchText(context.Background(), query, 200)
		if err != nil {
			return nil
		}
		items := make([]paletteItem, 0, len(matches))
		for _, m := range matches {
			items = append(items, paletteItem{label: strings.TrimSpace(m.LineText), detail: m.Title + ":" + fmt.Sprint(m.LineNumber), id: m.Path, data: m})
		}
		return items
	}
	p.onSelect = func(a *App, it paletteItem) {
		m := it.data.(vault.TextSearchMatch)
		a.openNote(m.Path, true)
		if buf := a.buffers[m.Path]; buf != nil {
			buf.ed.GotoLine(max(0, m.LineNumber-1))
		}
	}
	p.emptyHint = "No matching lines"
	p.refilter(a)
	a.overlay = p
}

// openNoteQuiet opens a note without recording a jump.
func (a *App) openNoteQuiet(path string) {
	pane := a.activePane
	if idx := pane.findTab(path); idx >= 0 {
		pane.active = idx
		return
	}
	if _, err := a.openBuffer(path); err != nil {
		a.notifyError(err.Error())
		return
	}
	mode := paneMode(a.prefs.DefaultPaneMode)
	if mode == "" {
		mode = modeEdit
	}
	pane.tabs = append(pane.tabs, &tab{path: path, mode: mode})
	pane.active = len(pane.tabs) - 1
	a.touchRecent(path)
	a.markSessionDirty()
}

// maybeWikilinkCompletion opens the note picker when `[[` was just typed
// in insert mode, the way the desktop's link autocomplete does. Picking a
// note inserts its title and steps past the closing brackets auto-pairs
// added.
func (a *App) maybeWikilinkCompletion(buf *noteBuffer) {
	if buf.ed.Mode() != vim.ModeInsert {
		return
	}
	cur := buf.ed.Cursor()
	line := buf.ed.LineRunes(cur.Line)
	if cur.Col < 2 || line[cur.Col-1] != '[' || line[cur.Col-2] != '[' {
		return
	}
	a.openWikilinkPicker(buf)
}

// openWikilinkPicker lets the user pick a note to link to at the cursor.
func (a *App) openWikilinkPicker(buf *noteBuffer) {
	p := &palette{title: "Link to note", placeholder: "Note title (Enter inserts [[title]])", highlight: true}
	p.source = a.notePaletteSource(buf.path)
	insert := func(a *App, title string) {
		cur := buf.ed.Cursor()
		line := buf.ed.LineRunes(cur.Line)
		opened := cur.Col >= 2 && line[cur.Col-1] == '[' && line[cur.Col-2] == '['
		closed := opened && cur.Col+1 < len(line) && line[cur.Col] == ']' && line[cur.Col+1] == ']'
		switch {
		case opened && closed:
			buf.ed.InsertAtCursor(title)
			c := buf.ed.Cursor()
			buf.ed.SetCursor(vim.Pos{Line: c.Line, Col: c.Col + 2})
		case opened:
			buf.ed.InsertAtCursor(title + "]]")
		default:
			buf.ed.InsertAtCursor("[[" + title + "]]")
		}
	}
	p.onSelect = func(a *App, it paletteItem) { insert(a, it.data.(vault.NoteMeta).Title) }
	p.onEmpty = func(a *App, query string) { insert(a, query) }
	p.emptyHint = "No note matches. Enter links the typed title anyway."
	p.refilter(a)
	a.overlay = p
}

// rolloverUnfinishedTasks moves open tasks from the newest earlier daily
// note into a freshly created one, when the vault asks for it.
func (a *App) rolloverUnfinishedTasks(newPath string, today time.Time) {
	if a.idx == nil || !a.idx.settings.DailyNotes.RolloverUnfinishedTasks {
		return
	}
	a.rolloverInto(newPath, today)
}

// rolloverInto moves the open tasks of the newest daily note before today
// into newPath and reports how many moved.
func (a *App) rolloverInto(newPath string, today time.Time) int {
	if a.idx == nil {
		return 0
	}
	var prevPath string
	var prevDate time.Time
	for _, n := range a.idx.notes {
		if n.Folder != vault.FolderInbox || n.Path == newPath {
			continue
		}
		_, sub := a.folderOfPath(n.Path)
		d, ok := periodic.DateOf(periodic.Daily, sub, n.Title, a.idx.settings, a.primaryAtRoot())
		if !ok || !d.Before(today) {
			continue
		}
		if prevPath == "" || d.After(prevDate) {
			prevPath, prevDate = n.Path, d
		}
	}
	if prevPath == "" {
		return 0
	}
	var moved []string
	err := a.mutateNote(prevPath, func(body string) (string, bool) {
		lines, rest := vault.ExtractOpenTaskBlocks(body)
		moved = lines
		return rest, len(lines) > 0
	})
	if err != nil || len(moved) == 0 {
		return 0
	}
	if err := a.mutateNote(newPath, func(body string) (string, bool) {
		return vault.InsertTasksUnderTasksHeading(body, moved), true
	}); err != nil {
		a.notifyError(err.Error())
		return 0
	}
	a.notify(fmt.Sprintf("Rolled over %d open tasks from %s", len(moved), prevDate.Format("2006-01-02")))
	return len(moved)
}
