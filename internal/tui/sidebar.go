package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/backend"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// sidebarRow is one visible line of the sidebar tree.
type sidebarRow struct {
	kind       string // section, home, favorite, folder, note, quick, archive, trash, tags, tag, tasks, databases, database, help, settings
	key        string
	label      string
	depth      int
	folder     vault.NoteFolder
	subpath    string
	path       string
	tag        string
	expandable bool
	expanded   bool
	count      int
	dbName     string
}

type sidebarState struct {
	rows      []sidebarRow
	cursor    int
	scroll    int
	collapsed map[string]bool
	// favOpen holds favorite folders expanded in place; favorites start
	// collapsed, unlike the bucket tree.
	favOpen map[string]bool
	filter  string
}

func newSidebar(a *App) *sidebarState {
	s := &sidebarState{collapsed: map[string]bool{}, favOpen: map[string]bool{}}
	s.collapsed["section:tags"] = true
	s.collapsed["folder:archive:"] = true
	return s
}

// rebuild recomputes rows from the index, keeping the cursor on its key.
func (s *sidebarState) rebuild(a *App) {
	selectedKey := ""
	if s.cursor < len(s.rows) {
		selectedKey = s.rows[s.cursor].key
	}
	s.rows = s.buildRows(a)
	s.cursor = 0
	for i, r := range s.rows {
		if r.key == selectedKey {
			s.cursor = i
			break
		}
	}
}

func (s *sidebarState) expanded(key string) bool { return !s.collapsed[key] }

func (s *sidebarState) buildRows(a *App) []sidebarRow {
	rows := []sidebarRow{}
	rows = append(rows, sidebarRow{kind: "home", key: "home", label: "Home"})
	rows = append(rows, sidebarRow{kind: "gap"})
	if favs := a.favorites(); len(favs) > 0 {
		key := "section:favorites"
		rows = append(rows, sidebarRow{kind: "section", key: key, label: "Favorites", expandable: true, expanded: s.expanded(key)})
		if s.expanded(key) {
			for _, f := range favs {
				if i := strings.Index(f, ":"); i >= 0 {
					folder := vault.NoteFolder(f[:i])
					sub := f[i+1:]
					label := a.folderLabel(folder)
					if sub != "" {
						label = sub
						if j := strings.LastIndex(sub, "/"); j >= 0 {
							label = sub[j+1:]
						}
					}
					key := "fav:" + f
					rows = append(rows, sidebarRow{kind: "favorite-folder", key: key, label: label, depth: 1, folder: folder, subpath: sub, expandable: true, expanded: s.favOpen[key]})
					if s.favOpen[key] {
						rows = append(rows, s.subtreeRows(a, folder, sub, 2, a.notesIn(folder), "fav:")...)
					}
					continue
				}
				if meta, ok := a.noteMeta(f); ok {
					rows = append(rows, sidebarRow{kind: "note", key: "fav:" + f, label: meta.Title, depth: 1, path: f, folder: meta.Folder})
				}
			}
		}
	}
	if len(a.favorites()) > 0 {
		rows = append(rows, sidebarRow{kind: "gap"})
	}
	rows = append(rows, s.folderRows(a, vault.FolderInbox)...)
	quick := a.notesIn(vault.FolderQuick)
	rows = append(rows, sidebarRow{kind: "quick", key: "quick", label: a.folderLabel(vault.FolderQuick), count: len(quick)})
	rows = append(rows, s.folderRows(a, vault.FolderArchive)...)
	rows = append(rows, sidebarRow{kind: "trash", key: "trash", label: a.folderLabel(vault.FolderTrash), count: len(a.notesIn(vault.FolderTrash))})
	rows = append(rows, sidebarRow{kind: "gap"})
	// Tasks expands into its three layouts, so the board and the calendar
	// are one Enter away instead of a mode key inside the view.
	rows = append(rows, sidebarRow{kind: "tasks", key: "tasks", label: "Tasks", count: a.openTaskCount(), expandable: true, expanded: !s.collapsed["tasks"]})
	if !s.collapsed["tasks"] {
		for _, mode := range []string{"list", "kanban", "calendar"} {
			rows = append(rows, sidebarRow{kind: "taskmode", key: "tasks:" + mode, label: mode, depth: 1, subpath: mode})
		}
	}
	if a.idx != nil && len(a.idx.tags) > 0 {
		key := "section:tags"
		rows = append(rows, sidebarRow{kind: "tags", key: key, label: "Tags", expandable: true, expanded: s.expanded(key), count: len(a.idx.tags)})
		if s.expanded(key) {
			rows = append(rows, s.tagRows(a)...)
		}
	}
	if dbs := a.databaseNames(); len(dbs) > 0 {
		key := "section:databases"
		rows = append(rows, sidebarRow{kind: "databases", key: key, label: "Databases", expandable: true, expanded: s.expanded(key), count: len(dbs)})
		if s.expanded(key) {
			for _, name := range dbs {
				rows = append(rows, sidebarRow{kind: "database", key: "db:" + name, label: name, depth: 1, dbName: name})
			}
		}
	}
	rows = append(rows, sidebarRow{kind: "gap"})
	rows = append(rows, sidebarRow{kind: "help", key: "help", label: "Help"})
	rows = append(rows, sidebarRow{kind: "settings", key: "settings", label: "Settings"})
	return rows
}

// folderRows renders a bucket as a tree: subfolders first, then notes.
func (s *sidebarState) folderRows(a *App, folder vault.NoteFolder) []sidebarRow {
	key := fmt.Sprintf("folder:%s:", folder)
	notes := a.notesIn(folder)
	rows := []sidebarRow{{kind: "folder", key: key, label: a.folderLabel(folder), folder: folder, subpath: "", expandable: true, expanded: s.expanded(key), count: len(notes)}}
	if !s.expanded(key) {
		return rows
	}
	rows = append(rows, s.subtreeRows(a, folder, "", 1, notes, "")...)
	return rows
}

// subtreeRows lists the subfolders and notes under a folder. The key
// prefix keeps a folder shown twice (in Favorites and in its bucket) with
// separate expansion state.
func (s *sidebarState) subtreeRows(a *App, folder vault.NoteFolder, parent string, depth int, notes []vault.NoteMeta, prefix string) []sidebarRow {
	rows := []sidebarRow{}
	children := []string{}
	if a.idx != nil {
		for _, f := range a.idx.folders {
			if f.Folder != folder {
				continue
			}
			sub := f.Subpath
			if parent == "" {
				if !strings.Contains(sub, "/") {
					children = append(children, sub)
				}
			} else if strings.HasPrefix(sub, parent+"/") && !strings.Contains(sub[len(parent)+1:], "/") {
				children = append(children, sub)
			}
		}
	}
	sort.Slice(children, func(i, j int) bool { return strings.ToLower(children[i]) < strings.ToLower(children[j]) })
	for _, sub := range children {
		if isDatabaseDir(sub) {
			continue
		}
		key := fmt.Sprintf("%sfolder:%s:%s", prefix, folder, sub)
		label := sub
		if i := strings.LastIndex(sub, "/"); i >= 0 {
			label = sub[i+1:]
		}
		rows = append(rows, sidebarRow{kind: "folder", key: key, label: label, depth: depth, folder: folder, subpath: sub, expandable: true, expanded: s.expanded(key)})
		if s.expanded(key) {
			rows = append(rows, s.subtreeRows(a, folder, sub, depth+1, notes, prefix)...)
		}
	}
	direct := []vault.NoteMeta{}
	for _, n := range notes {
		_, sub := a.folderOfPath(n.Path)
		if sub == parent && !isDatabaseDir(sub) {
			direct = append(direct, n)
		}
	}
	sortNotes(direct, a.prefs.NoteSortOrder)
	for _, n := range direct {
		rows = append(rows, sidebarRow{kind: "note", key: prefix + "note:" + n.Path, label: n.Title, depth: depth, path: n.Path, folder: folder})
	}
	return rows
}

// sortNotes orders a folder's notes the way the desktop's note_sort_order
// names it; "none" (manual) keeps the file order.
func sortNotes(notes []vault.NoteMeta, order string) {
	byTitle := func(i, j int) bool {
		return strings.ToLower(notes[i].Title) < strings.ToLower(notes[j].Title)
	}
	switch order {
	case "title", "alpha", "name", "name-asc":
		sort.SliceStable(notes, byTitle)
	case "name-desc":
		sort.SliceStable(notes, func(i, j int) bool { return byTitle(j, i) })
	case "created", "created-desc":
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].CreatedAt > notes[j].CreatedAt })
	case "created-asc":
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].CreatedAt < notes[j].CreatedAt })
	case "updated-asc":
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt < notes[j].UpdatedAt })
	case "none", "manual":
	default:
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].UpdatedAt > notes[j].UpdatedAt })
	}
}

func (s *sidebarState) tagRows(a *App) []sidebarRow {
	rows := []sidebarRow{}
	if !a.prefs.NestedTags {
		for _, t := range a.idx.tags {
			rows = append(rows, sidebarRow{kind: "tag", key: "tag:" + t.tag, label: t.tag, depth: 1, tag: t.tag, count: t.count})
		}
		return rows
	}
	type node struct {
		name     string
		full     string
		count    int
		children map[string]*node
	}
	root := &node{children: map[string]*node{}}
	for _, t := range a.idx.tags {
		parts := strings.Split(t.tag, "/")
		cur := root
		full := ""
		for _, p := range parts {
			if full == "" {
				full = p
			} else {
				full += "/" + p
			}
			child, ok := cur.children[p]
			if !ok {
				child = &node{name: p, full: full, children: map[string]*node{}}
				cur.children[p] = child
			}
			child.count += t.count
			cur = child
		}
	}
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		names := make([]string, 0, len(n.children))
		for name := range n.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := n.children[name]
			key := "tag:" + child.full
			expandable := len(child.children) > 0
			rows = append(rows, sidebarRow{kind: "tag", key: key, label: child.name, depth: depth, tag: child.full, count: child.count, expandable: expandable, expanded: s.expanded(key)})
			if expandable && s.expanded(key) {
				walk(child, depth+1)
			}
		}
	}
	walk(root, 1)
	return rows
}

func (a *App) openTaskCount() int {
	n := 0
	for _, t := range a.displayTasks() {
		if vault.IsTaskOpen(t) {
			n++
		}
	}
	return n
}

// selectedFolder is the folder the cursor row belongs to.
func (s *sidebarState) selectedFolder() (vault.NoteFolder, string, bool) {
	if s.cursor >= len(s.rows) {
		return "", "", false
	}
	r := s.rows[s.cursor]
	switch r.kind {
	case "folder", "favorite-folder":
		return r.folder, r.subpath, true
	case "note":
		return r.folder, parentSubpath(r.path, r.folder), true
	case "quick":
		return vault.FolderQuick, "", true
	}
	return "", "", false
}

func parentSubpath(path string, folder vault.NoteFolder) string {
	return ""
}

func (s *sidebarState) row() *sidebarRow {
	if s.cursor < len(s.rows) {
		return &s.rows[s.cursor]
	}
	return nil
}

func (s *sidebarState) selectKey(key string) {
	for i, r := range s.rows {
		if r.key == key {
			s.cursor = i
			return
		}
	}
}

// --- rendering ---

func (s *sidebarState) render(a *App, w, h int) string {
	th := a.theme
	focused := a.focus == focusSidebar
	name := a.vaultLabel()
	glyph := "◈"
	if a.opts.Target.Kind == backend.KindRemote {
		glyph = "⇅"
	}
	header := " " + th.Bold.Render(glyph+" "+truncateCells(name, w-4))
	rule := th.Border.Render(strings.Repeat("─", w))
	if focused {
		rule = th.BorderFocus.Render(strings.Repeat("─", w))
	}
	lines := []string{padRight(header, w), rule}
	rows := h - len(lines)
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	if s.cursor >= s.scroll+rows {
		s.scroll = s.cursor - rows + 1
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	activePath := a.activePath()
	for i := s.scroll; i < len(s.rows) && len(lines) < h; i++ {
		r := s.rows[i]
		if r.kind == "gap" {
			lines = append(lines, strings.Repeat(" ", w))
			continue
		}
		indent := strings.Repeat("  ", r.depth)
		icon := sidebarGlyph(r)
		label := r.label
		count := ""
		if r.count > 0 && r.kind != "note" {
			count = fmt.Sprint(r.count)
		}
		avail := w - 3 - cellWidth(indent) - cellWidth(icon) - len(count)
		if count != "" {
			avail--
		}
		// Built-in rows are lowercase chrome: group headings dim, entries
		// in the base color. Note titles and folder names keep their case.
		builtin := r.kind != "note" && r.kind != "tag" && r.kind != "database" && !(r.kind == "folder" && r.depth > 0) && r.kind != "favorite-folder"
		if builtin {
			label = strings.ToLower(label)
		}
		text := indent + icon + truncateCells(label, max(3, avail))
		style := th.Base
		switch r.kind {
		case "section", "tags", "databases":
			style = th.Dim
		case "home", "quick", "trash", "tasks", "taskmode", "help", "settings", "archive":
			style = th.Base
		case "folder", "favorite-folder":
			if r.depth == 0 {
				style = th.Dim
			}
		case "tag":
			style = th.Tag
		case "note":
			if r.path == activePath {
				style = th.Base.Foreground(th.Accent).Bold(true)
			}
		}
		gutter := " "
		if r.kind == "note" && r.path == activePath {
			gutter = th.BorderFocus.Render("▍")
		}
		plain := text + strings.Repeat(" ", max(1, w-2-cellWidth(text)-len(count))) + count
		var line string
		switch {
		case i == s.cursor && focused:
			line = gutter + th.CursorRow.Render(padRight(plain, w-1))
		case i == s.cursor:
			line = gutter + th.Selected.Render(padRight(plain, w-1))
		default:
			line = gutter + style.Render(text) + strings.Repeat(" ", max(1, w-2-cellWidth(text)-len(count))) + th.Muted.Render(count)
		}
		lines = append(lines, padRight(line, w))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

// sidebarGlyph is the marker drawn before a row.
func sidebarGlyph(r sidebarRow) string {
	switch r.kind {
	case "home":
		return "⌂ "
	case "section":
		if r.expanded {
			return "▾ "
		}
		return "▸ "
	case "tags":
		if r.expanded {
			return "▾ "
		}
		return "▸ "
	case "databases":
		if r.expanded {
			return "▾ "
		}
		return "▸ "
	case "folder", "favorite-folder":
		if !r.expandable {
			return "  "
		}
		if r.expanded {
			return "▾ "
		}
		return "▸ "
	case "tag":
		if r.expandable {
			if r.expanded {
				return "▾ #"
			}
			return "▸ #"
		}
		return "  #"
	case "quick":
		return "✎ "
	case "trash":
		return "⌫ "
	case "tasks":
		if r.expanded {
			return "▾ "
		}
		return "▸ "
	case "taskmode":
		switch r.subpath {
		case "kanban":
			return "▥ "
		case "calendar":
			return "▣ "
		}
		return "≡ "
	case "help":
		return "? "
	case "settings":
		return "⚙ "
	case "database":
		return "▦ "
	case "note":
		return "  "
	}
	return "  "
}

// --- keys ---

func (s *sidebarState) handleKey(a *App, k vim.Key) {
	n := len(s.rows)
	if k.Is("esc") {
		if len(a.navKeys) > 0 {
			a.navKeys = nil
			return
		}
		a.focus = focusPane
		return
	}
	if k.Is("enter") {
		s.activate(a, false)
		return
	}
	if k.Is("right") {
		s.activate(a, true)
		return
	}
	if k.Is("left") {
		s.collapseOrParent(a)
		return
	}
	cur := listCursor{cursor: s.cursor, scroll: s.scroll}
	before := s.cursor
	if a.listNav(&cur, k, n, max(1, a.height-4)) {
		s.cursor, s.scroll = cur.cursor, cur.scroll
		dir := 1
		if s.cursor < before {
			dir = -1
		}
		for s.cursor >= 0 && s.cursor < n && s.rows[s.cursor].kind == "gap" {
			next := s.cursor + dir
			if next < 0 || next >= n {
				dir = -dir
				next = s.cursor + dir
			}
			s.cursor = next
		}
		return
	}
	if !a.listKeyAllowed(k) {
		return
	}
	id, pending := a.resolveAction(k, "nav.openSideItem", "nav.back", "nav.toggleFolder", "nav.filter", "nav.contextMenu", "nav.delete", "nav.localEx", "nav.newQuickNote", "nav.peekPreview")
	if pending {
		return
	}
	switch id {
	case "nav.openSideItem":
		s.activate(a, true)
	case "nav.back":
		s.collapseOrParent(a)
	case "nav.toggleFolder", "nav.newQuickNote":
		if id == "nav.newQuickNote" && s.row() != nil && s.row().kind == "quick" {
			a.newQuickNote()
			return
		}
		if id == "nav.toggleFolder" {
			s.toggle(a)
		} else {
			s.newHere(a)
		}
	case "nav.filter":
		a.openNoteSearch("")
	case "nav.contextMenu":
		s.contextMenu(a)
	case "nav.delete":
		s.deleteSelected(a)
	case "nav.localEx":
		a.openLocalEx()
	case "nav.peekPreview":
		if r := s.row(); r != nil && r.kind == "note" {
			a.openNote(r.path, false)
			if t := a.activeTab(); t != nil {
				t.mode = modePreview
				if t.preview == nil {
					t.preview = &previewState{}
				}
			}
		}
	default:
		switch {
		case k.IsRune('r'):
			s.renameSelected(a)
		case k.IsRune('s'):
			s.favoriteSelected(a)
		case k.IsRune('N'):
			s.newFolderHere(a)
		case k.IsRune('?'):
			a.openKeyHelp()
		}
	}
}

func (s *sidebarState) newHere(a *App) {
	if folder, sub, ok := s.selectedFolder(); ok {
		if folder == vault.FolderQuick {
			a.newQuickNote()
			return
		}
		a.promptFor("New note in "+a.folderLabel(folder)+pathSuffix(sub), "", "Title", func(a *App, title string) {
			if strings.TrimSpace(title) != "" {
				a.createNote(folder, strings.TrimSpace(title), sub, nil, true)
			}
		})
		return
	}
	a.newNoteHere()
}

func (s *sidebarState) newFolderHere(a *App) {
	folder, sub, ok := s.selectedFolder()
	if !ok || folder == vault.FolderQuick || folder == vault.FolderTrash {
		folder, sub = vault.FolderInbox, ""
	}
	if r := s.row(); r != nil && r.kind == "note" {
		_, sub = a.folderOfPath(r.path)
	}
	a.createFolderPrompt(folder, sub)
}

// activate opens what the cursor is on. open true expands folders instead
// of toggling.
func (s *sidebarState) activate(a *App, open bool) {
	r := s.row()
	if r == nil {
		return
	}
	switch r.kind {
	case "home":
		a.openHome()
	case "section", "tags", "databases":
		if open {
			s.collapsed[r.key] = false
		} else {
			s.collapsed[r.key] = !s.collapsed[r.key]
		}
		s.rebuild(a)
	case "folder":
		if open {
			s.collapsed[r.key] = false
		} else {
			s.collapsed[r.key] = !s.collapsed[r.key]
		}
		s.rebuild(a)
		a.markSessionDirty()
	case "favorite-folder":
		if open {
			s.favOpen[r.key] = true
		} else {
			s.favOpen[r.key] = !s.favOpen[r.key]
		}
		s.rebuild(a)
		a.markSessionDirty()
	case "note":
		a.openNote(r.path, true)
	case "quick":
		a.openNoteList(tabQuickNotes)
	case "trash":
		a.openNoteList(tabTrash)
	case "tasks":
		_ = a.openTasksMode("")
	case "taskmode":
		_ = a.openTasksMode(r.subpath)
	case "tag":
		if r.expandable && open {
			s.collapsed[r.key] = false
			s.rebuild(a)
			return
		}
		a.openTags(r.tag)
	case "database":
		a.openDatabase(r.dbName)
	case "help":
		a.openHelp()
	case "settings":
		a.openSettings()
	}
}

func (s *sidebarState) toggle(a *App) {
	r := s.row()
	if r == nil || !r.expandable {
		return
	}
	if r.kind == "favorite-folder" {
		s.favOpen[r.key] = !s.favOpen[r.key]
		s.rebuild(a)
		a.markSessionDirty()
		return
	}
	s.collapsed[r.key] = !s.collapsed[r.key]
	s.rebuild(a)
	a.markSessionDirty()
}

// collapseOrParent collapses an open folder, else jumps to the parent row.
func (s *sidebarState) collapseOrParent(a *App) {
	r := s.row()
	if r == nil {
		return
	}
	if r.expandable && r.expanded {
		if r.kind == "favorite-folder" {
			s.favOpen[r.key] = false
		} else {
			s.collapsed[r.key] = true
		}
		s.rebuild(a)
		return
	}
	for i := s.cursor - 1; i >= 0; i-- {
		if s.rows[i].depth < r.depth {
			s.cursor = i
			return
		}
	}
}

func (s *sidebarState) renameSelected(a *App) {
	r := s.row()
	if r == nil {
		return
	}
	switch r.kind {
	case "note":
		a.renameNote(r.path)
	case "folder":
		if r.subpath != "" {
			a.renameFolderPrompt(r.folder, r.subpath)
		}
	case "tag":
		a.renameTagPrompt(r.tag)
	}
}

func (s *sidebarState) favoriteSelected(a *App) {
	r := s.row()
	if r == nil {
		return
	}
	switch r.kind {
	case "note":
		a.toggleFavorite(r.path)
	case "folder", "favorite-folder":
		a.toggleFavorite(string(r.folder) + ":" + r.subpath)
	}
}

func (s *sidebarState) deleteSelected(a *App) {
	r := s.row()
	if r == nil {
		return
	}
	switch r.kind {
	case "note":
		a.trashNote(r.path)
	case "folder":
		if r.subpath != "" {
			a.deleteFolderConfirm(r.folder, r.subpath)
		}
	case "tag":
		a.deleteTagConfirm(r.tag)
	}
}

func (s *sidebarState) contextMenu(a *App) {
	r := s.row()
	if r == nil {
		return
	}
	switch r.kind {
	case "note":
		a.noteContextMenu(r.path)
	case "folder", "favorite-folder":
		folder, sub := r.folder, r.subpath
		items := []menuItem{
			{key: "n", label: "New note here", run: func(a *App) {
				a.promptFor("New note in "+a.folderLabel(folder)+pathSuffix(sub), "", "Title", func(a *App, title string) {
					if strings.TrimSpace(title) != "" {
						a.createNote(folder, strings.TrimSpace(title), sub, nil, true)
					}
				})
			}},
			{key: "N", label: "New folder here", run: func(a *App) { a.createFolderPrompt(folder, sub) }},
			{key: "s", label: map[bool]string{true: "Remove from favorites", false: "Add to favorites"}[a.isFavorite(string(folder)+":"+sub)], run: func(a *App) { a.toggleFavorite(string(folder) + ":" + sub) }},
		}
		if r.kind == "favorite-folder" {
			items = append(items, menuItem{key: "g", label: "Reveal in tree", run: func(a *App) { s.revealFolder(a, folder, sub) }})
		}
		if sub != "" {
			items = append(items,
				menuItem{sep: true},
				menuItem{key: "r", label: "Rename folder", run: func(a *App) { a.renameFolderPrompt(folder, sub) }},
				menuItem{key: "x", label: "Delete folder", run: func(a *App) { a.deleteFolderConfirm(folder, sub) }},
			)
		}
		a.showMenu(r.label, items)
	case "tag":
		tag := r.tag
		a.showMenu("#"+tag, []menuItem{
			{key: "o", label: "Show notes", run: func(a *App) { a.openTags(tag) }},
			{key: "r", label: "Rename tag everywhere", run: func(a *App) { a.renameTagPrompt(tag) }},
			{key: "x", label: "Remove tag everywhere", run: func(a *App) { a.deleteTagConfirm(tag) }},
		})
	case "trash":
		a.showMenu("Trash", []menuItem{
			{key: "o", label: "Open", run: func(a *App) { a.openNoteList(tabTrash) }},
			{key: "E", label: "Empty trash", run: func(a *App) { a.emptyTrash() }},
		})
	case "quick":
		a.showMenu("Quick Notes", []menuItem{
			{key: "o", label: "Open", run: func(a *App) { a.openNoteList(tabQuickNotes) }},
			{key: "n", label: "New quick note", run: func(a *App) { a.newQuickNote() }},
		})
	case "database":
		name := r.dbName
		a.showMenu(name, []menuItem{
			{key: "o", label: "Open", run: func(a *App) { a.openDatabase(name) }},
			{key: "r", label: "Rename", run: func(a *App) { a.renameDatabasePrompt(name) }},
		})
	}
}

// hintTargets for the sidebar: every visible row.
func (s *sidebarState) hintTargets(a *App, x, y0, h int) []hintTarget {
	out := []hintTarget{}
	for i := s.scroll; i < len(s.rows) && i-s.scroll < h-2; i++ {
		if s.rows[i].kind == "gap" {
			continue
		}
		idx := i
		out = append(out, hintTarget{x: x, y: y0 + 2 + (i - s.scroll), run: func(a *App) {
			s.cursor = idx
			a.focus = focusSidebar
			s.activate(a, false)
		}})
	}
	return out
}

// isDatabaseDir is true for a subpath inside a `.base` database folder,
// which the Databases section owns.
func isDatabaseDir(sub string) bool {
	for _, seg := range strings.Split(sub, "/") {
		if strings.HasSuffix(strings.ToLower(seg), ".base") {
			return true
		}
	}
	return false
}

// noteContextMenu is the right-click and `m` menu for a note, shared by
// the sidebar, the list views and the editor.
func (a *App) noteContextMenu(path string) {
	meta, _ := a.noteMeta(path)
	items := []menuItem{
		{key: "o", label: "Open", run: func(a *App) { a.openNote(path, true) }},
		{key: "v", label: "Open in split", run: func(a *App) { a.splitPane(true); a.openNote(path, true) }},
		{key: "p", label: "Preview", run: func(a *App) {
			a.openNote(path, true)
			a.setPaneMode(modePreview)
		}},
		{sep: true},
		{key: "r", label: "Rename", run: func(a *App) { a.renameNote(path) }},
		{key: "m", label: "Move to folder", run: func(a *App) { a.moveNotePicker(path) }},
		{key: "d", label: "Duplicate", run: func(a *App) { a.duplicateNote(path) }},
		{key: "s", label: map[bool]string{true: "Remove from favorites", false: "Add to favorites"}[a.isFavorite(path)], run: func(a *App) { a.toggleFavorite(path) }},
		{key: "y", label: "Copy link", run: func(a *App) {
			a.openNoteQuiet(path)
			a.copyActiveLink()
		}},
		{sep: true},
	}
	if meta.Folder == vault.FolderArchive {
		items = append(items, menuItem{key: "u", label: "Unarchive", run: func(a *App) { a.unarchiveNote(path) }})
	} else if meta.Folder != vault.FolderTrash {
		items = append(items, menuItem{key: "a", label: "Archive", run: func(a *App) { a.archiveNote(path) }})
	}
	if meta.Folder == vault.FolderTrash {
		items = append(items, menuItem{key: "R", label: "Restore", run: func(a *App) { a.restoreNote(path) }})
		items = append(items, menuItem{key: "x", label: "Delete permanently", run: func(a *App) { a.deleteNoteForever(path) }})
	} else {
		items = append(items, menuItem{key: "x", label: "Move to trash", run: func(a *App) { a.trashNote(path) }})
	}
	a.showMenu(meta.Title, items)
}

// revealFolder expands the bucket tree down to a folder and selects it.
func (s *sidebarState) revealFolder(a *App, folder vault.NoteFolder, sub string) {
	s.collapsed[fmt.Sprintf("folder:%s:", folder)] = false
	parts := strings.Split(sub, "/")
	for i := range parts {
		s.collapsed[fmt.Sprintf("folder:%s:%s", folder, strings.Join(parts[:i+1], "/"))] = false
	}
	s.rebuild(a)
	s.selectKey(fmt.Sprintf("folder:%s:%s", folder, sub))
}
