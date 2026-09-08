package tui

import (
	"strings"

	"github.com/ZenNotes/zennotescli/internal/vim"
)

// view is a built-in tab: Tasks, Tags, Trash and friends.
type view interface {
	title() string
	hint(a *App) string
	refresh(a *App)
	render(a *App, w, h int, focused bool) string
	handleKey(a *App, k vim.Key) bool
}

// refreshViews tells every open view the index changed.
func (a *App) refreshViews() {
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if t.view != nil {
				t.view.refresh(a)
			}
		}
	}
	a.refreshPanels()
}

func (a *App) refreshPanels() {
	if a.outlineOpen {
		a.outline.refresh(a)
	}
	if a.connectionsOpen {
		a.connections.refresh(a)
	}
	if a.calendarOpen {
		a.calendar.refresh(a)
	}
}

// openVirtualByPath reopens a built-in view from its tab path.
func (a *App) openVirtualByPath(path string) {
	switch {
	case path == tabTasks:
		a.openTasks()
	case path == tabTags:
		a.openTags("")
	case path == tabQuickNotes:
		a.openNoteList(tabQuickNotes)
	case path == tabArchive:
		a.openNoteList(tabArchive)
	case path == tabTrash:
		a.openNoteList(tabTrash)
	case path == tabHelp:
		a.openHelp()
	case path == tabHome:
		a.openHome()
	case path == tabSettings:
		a.openSettings()
	case strings.HasPrefix(path, tabDatabase):
		a.openDatabase(strings.TrimPrefix(path, tabDatabase))
	}
}

func (a *App) openTasks() {
	a.openVirtual(tabTasks, func() view { return newTasksView(a) })
}

func (a *App) openTags(tag string) {
	a.openVirtual(tabTags, func() view { return newTagsView() })
	if t := a.activeTab(); t != nil {
		if tv, ok := t.view.(*tagsView); ok && tag != "" {
			tv.selectTag(a, tag)
		}
	}
}

func (a *App) openNoteList(path string) {
	a.openVirtual(path, func() view { return newNoteListView(path) })
}

func (a *App) openHelp() {
	a.openVirtual(tabHelp, func() view { return newHelpView() })
}

func (a *App) openHome() {
	a.openVirtual(tabHome, func() view { return &homeView{} })
}

func (a *App) openSettings() {
	a.openVirtual(tabSettings, func() view { return &settingsView{} })
}

func (a *App) openDatabase(name string) {
	a.openVirtual(tabDatabase+name, func() view { return newDatabaseView(name) })
}

// listCursor is the shared cursor and scroll state of the list views.
type listCursor struct {
	cursor int
	scroll int
}

func (c *listCursor) clamp(n int) {
	if n == 0 {
		c.cursor, c.scroll = 0, 0
		return
	}
	if c.cursor >= n {
		c.cursor = n - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

func (c *listCursor) move(delta, n int) {
	c.cursor += delta
	c.clamp(n)
}

// ensureVisible scrolls so the cursor row is on screen.
func (c *listCursor) ensureVisible(rows int) {
	if rows <= 0 {
		return
	}
	if c.cursor < c.scroll {
		c.scroll = c.cursor
	}
	if c.cursor >= c.scroll+rows {
		c.scroll = c.cursor - rows + 1
	}
	if c.scroll < 0 {
		c.scroll = 0
	}
}

// listNav applies the shared movement keys to a cursor. It returns true
// when the key was a movement.
func (a *App) listNav(c *listCursor, k vim.Key, n, pageRows int) bool {
	if k.Is("down") {
		c.move(1, n)
		return true
	}
	if k.Is("up") {
		c.move(-1, n)
		return true
	}
	if k.Is("pgdn") {
		c.move(pageRows, n)
		return true
	}
	if k.Is("pgup") {
		c.move(-pageRows, n)
		return true
	}
	if k.Is("home") {
		c.cursor = 0
		c.clamp(n)
		return true
	}
	if k.Is("end") {
		c.cursor = n - 1
		c.clamp(n)
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.moveDown", "nav.moveUp", "nav.jumpTop", "nav.jumpBottom", "nav.halfPageDown", "nav.halfPageUp")
	if pending {
		return true
	}
	switch id {
	case "nav.moveDown":
		c.move(1, n)
	case "nav.moveUp":
		c.move(-1, n)
	case "nav.jumpTop":
		c.cursor = 0
		c.clamp(n)
	case "nav.jumpBottom":
		c.cursor = n - 1
		c.clamp(n)
	case "nav.halfPageDown":
		c.move(max(1, pageRows/2), n)
	case "nav.halfPageUp":
		c.move(-max(1, pageRows/2), n)
	default:
		return false
	}
	return true
}
