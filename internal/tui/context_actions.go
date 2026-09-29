package tui

import (
	"github.com/ZenNotes/tui/internal/vim"
	"time"
)

// F2 is a terminal-safe route to actions in both Vim and plain-editor modes.
func (a *App) contextActions() {
	if a.focus == focusSidebar {
		a.sidebar.contextMenu(a)
		return
	}
	if a.focus == focusComments && a.comments != nil {
		p := a.comments
		if p.list.cursor < len(p.threads) {
			p.menu(a, p.threads[p.list.cursor])
		}
		return
	}
	t := a.activeTab()
	if t == nil {
		a.openCommandPalette()
		return
	}
	switch v := t.view.(type) {
	case *tasksView:
		items := []menuItem{{key: "v", label: "Switch view", run: func(a *App) {
			modes := []paletteItem{{label: "List", id: "list"}, {label: "Board", id: "kanban"}, {label: "Calendar", id: "calendar"}}
			a.overlay = &palette{title: "Task view", items: modes, filtered: modes, onSelect: func(a *App, it paletteItem) { v.setMode(it.id); a.prefs.TasksViewMode = v.mode; v.refresh(a) }}
		}},
			{key: "f", label: "Filter tasks", run: func(a *App) {
				a.promptFor("Filter tasks", v.filter, "Text or metadata · advanced: where: is:open", func(a *App, s string) { v.filter = s; v.refresh(a) })
			}},
			{key: "s", label: "Saved filters", run: func(a *App) { a.savedTaskFilters() }},
			{key: "b", label: "Board options", run: func(a *App) { v.boardMenu(a) }},
			{key: "n", label: "New task", run: func(a *App) {
				a.promptFor("New task", "", "Task text and optional due:YYYY-MM-DD", func(a *App, s string) {
					if s != "" {
						a.addTaskToDaily(s, time.Now())
					}
				})
			}},
		}
		if task := v.selectedTask(); task != nil {
			copy := *task
			items = append([]menuItem{{key: "e", label: "Edit selected task", run: func(a *App) { a.taskMenu(copy) }}}, items...)
		}
		a.showMenu("Tasks", items)
	case *noteListView:
		if n := v.selected(); n != nil {
			a.noteContextMenu(n.Path)
		}
	case *filesView:
		v.handleKey(a, vim.KeyEnter)
	case *templatesView:
		v.handleKey(a, vim.KeyEnter)
	case *settingsView:
		v.handleKey(a, vim.KeyEnter)
	default:
		if t.view == nil {
			a.noteContextMenu(t.path)
		} else {
			a.openCommandPalette()
		}
	}
}
func (a *App) selectionActions() { a.bulkMenu() }
