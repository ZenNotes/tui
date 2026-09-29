package tui

import (
	"fmt"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
	"path"
	"sort"
	"strings"
)

func (a *App) markPath(path string) {
	if path == "" {
		return
	}
	if a.markedNotes == nil {
		a.markedNotes = map[string]bool{}
	}
	if a.markedNotes[path] {
		delete(a.markedNotes, path)
		delete(a.markedMoveDirs, path)
	} else {
		a.markedNotes[path] = true
	}
}
func (a *App) markSidebarRow(r sidebarRow) {
	a.selectSidebarRow(r, false)
}
func (a *App) selectSidebarRow(r sidebarRow, addOnly bool) {
	if r.kind == "note" || r.kind == "favorite" {
		if addOnly && a.markedNotes[r.path] {
			return
		}
		a.markPath(r.path)
		return
	}
	if r.kind != "folder" && r.kind != "favorite-folder" {
		return
	}
	if a.idx == nil {
		return
	}
	paths := []string{}
	all := true
	for _, n := range a.idx.notes {
		f, sub := a.folderOfPath(n.Path)
		if f == r.folder && (sub == r.subpath || r.subpath == "" || strings.HasPrefix(sub, r.subpath+"/")) {
			paths = append(paths, n.Path)
			all = all && a.markedNotes[n.Path]
		}
	}
	if a.markedNotes == nil {
		a.markedNotes = map[string]bool{}
	}
	if a.markedMoveDirs == nil {
		a.markedMoveDirs = map[string]string{}
	}
	for _, p := range paths {
		if all && !addOnly {
			delete(a.markedNotes, p)
			delete(a.markedMoveDirs, p)
		} else {
			a.markedNotes[p] = true
			_, sub := a.folderOfPath(p)
			parent := path.Dir(r.subpath)
			if parent != "." && parent != "" {
				sub = strings.TrimPrefix(sub, parent+"/")
			}
			a.markedMoveDirs[p] = sub
		}
	}
}
func (a *App) bulkMenu() {
	paths := []string{}
	for p := range a.markedNotes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		a.notify("Mark notes with Ctrl+Space; Shift+Up/Down extends selection")
		return
	}
	a.showMenu(fmt.Sprintf("%d selected notes (including filtered-out marks)", len(paths)), []menuItem{
		{key: "o", label: "Open selected", run: func(a *App) {
			for _, p := range paths {
				a.openNote(p, true)
			}
		}},
		{key: "m", label: "Move selected", run: func(a *App) {
			a.promptFor("Move selected to folder", "", "Path within primary notes", func(a *App, sub string) { a.applyBulkNotes(paths, "move", sub) })
		}},
		{key: "a", label: "Archive selected", run: func(a *App) { a.applyBulkNotes(paths, "archive", "") }},
		{key: "t", label: "Trash selected", run: func(a *App) {
			a.confirm(fmt.Sprintf("Move %d selected notes to trash?", len(paths)), func() { a.applyBulkNotes(paths, "trash", "") })
		}},
		{key: "r", label: "Restore / unarchive selected", run: func(a *App) { a.applyBulkNotes(paths, "restore", "") }},
		{key: "c", label: "Clear selection", run: func(a *App) { a.markedNotes = nil; a.markedMoveDirs = nil }},
	})
}
func (a *App) applyBulkNotes(paths []string, action, sub string) {
	failures := []string{}
	done := 0
	for _, p := range paths {
		if buf := a.buffers[p]; buf != nil {
			if err := a.saveBuffer(buf); err != nil {
				failures = append(failures, p+": "+err.Error())
				continue
			}
		}
		var next vault.NoteMeta
		var err error
		switch action {
		case "move":
			dest := sub
			if rel := a.markedMoveDirs[p]; rel != "" {
				dest = strings.TrimSuffix(sub, "/") + "/" + rel
				dest = strings.TrimPrefix(dest, "/")
			}
			next, err = a.backend.MoveNote(a.ctx, p, vault.FolderInbox, dest)
		case "archive":
			next, err = a.backend.ArchiveNote(a.ctx, p)
		case "trash":
			next, err = a.backend.MoveToTrash(a.ctx, p)
		case "restore":
			f, _ := a.folderOfPath(p)
			if f == vault.FolderTrash {
				next, err = a.backend.RestoreFromTrash(a.ctx, p)
			} else if f == vault.FolderArchive {
				next, err = a.backend.UnarchiveNote(a.ctx, p)
			} else {
				failures = append(failures, p+": not archived or trashed")
				continue
			}
		default:
			err = fmt.Errorf("unknown bulk action %q", action)
		}
		if err != nil {
			failures = append(failures, p+": "+err.Error())
			continue
		}
		a.repointNote(p, next.Path)
		delete(a.markedNotes, p)
		delete(a.markedMoveDirs, p)
		done++
	}
	a.refreshIndex()
	if len(failures) > 0 {
		a.notifyError(fmt.Sprintf("%d completed; %d failed: %s", done, len(failures), strings.Join(failures, "; ")))
		a.overlay = &textReader{title: fmt.Sprintf("%d completed · %d failed", done, len(failures)), body: strings.Join(failures, "\n\n")}
	} else {
		a.notify(fmt.Sprintf("%s: %d notes", action, done))
	}
}
func isMarkKey(k vim.Key) bool { return k.IsCtrl(' ') || k.IsCtrl('@') || k.Is("ctrl+space") }
