package tui

// Key help for the built-in views, expressed as catalog actions where one
// exists so a rebinding in config.toml shows up in the hints.

func (v *tasksView) hintPairs(a *App) []hintPair {
	common := []hintPair{{"key:v", "view"}, {"key:f", "filter"}, {"key:F", "clear filter"}, {"key:n", "new task"}, {"key:A", "archived"}}
	switch v.mode {
	case "kanban":
		return append([]hintPair{{"key:h/l", "column"}, {"key:j/k", "card"}, {"key:H/L", "move card"}, {"nav.toggleTask", "toggle"}, {"key:Enter", "open"}, {"key:g", "group by"}, {"key:m", "menu"}}, common...)
	case "calendar":
		return append([]hintPair{{"key:h/j/k/l", "day"}, {"key:[/]", "month"}, {"key:t", "today"}, {"key:Enter", "day tasks"}, {"key:o", "daily note"}}, common...)
	}
	return append([]hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"nav.toggleTask", "toggle"}, {"nav.openResult", "open"}, {"key:p", "priority"}, {"key:d", "due"}, {"key:w", "waiting"}, {"key:c", "cancel"}, {"key:/", "in progress"}, {"key:e", "edit"}, {"key:s", "field"}, {"key:</>", "shift due"}, {"tasks.moveTaskUp|tasks.moveTaskDown", "reorder"}, {"nav.contextMenu", "menu"}}, common...)
}

func (v *tagsView) hintPairs(a *App) []hintPair {
	return []hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"key:Tab", "select tag"}, {"key:a", "any/all"}, {"key:c", "clear"}, {"nav.openSideItem|nav.back", "switch side"}, {"key:Enter", "open"}, {"key:r", "rename tag"}, {"nav.delete", "remove tag"}}
}

func (v *noteListView) hintPairs(a *App) []hintPair {
	base := []hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"key:Enter", "open"}}
	switch v.path {
	case tabArchive:
		return append(base, hintPair{"nav.unarchive", "unarchive"}, hintPair{"nav.delete", "trash"}, hintPair{"nav.restore", "rename"}, hintPair{"nav.contextMenu", "menu"}, hintPair{"nav.filter", "filter"})
	case tabTrash:
		return append(base, hintPair{"nav.restore", "restore"}, hintPair{"nav.delete", "delete forever"}, hintPair{"key:E", "empty trash"}, hintPair{"nav.filter", "filter"})
	}
	return append(base, hintPair{"nav.newQuickNote", "new quick note"}, hintPair{"nav.delete", "trash"}, hintPair{"nav.restore", "rename"}, hintPair{"nav.contextMenu", "menu"}, hintPair{"nav.filter", "filter"})
}

func (v *helpView) hintPairs(a *App) []hintPair {
	return []hintPair{{"nav.moveDown|nav.moveUp", "scroll"}, {"nav.halfPageDown|nav.halfPageUp", "page"}, {"nav.filter", "filter"}, {"nav.jumpTop|nav.jumpBottom", "top/bottom"}, {"key::help <topic>", "search the manual"}}
}

func (v *settingsView) hintPairs(a *App) []hintPair {
	return []hintPair{{"nav.moveDown|nav.moveUp", "scroll"}, {"key::theme", "theme"}, {"key::style", "preview style"}, {"key::vim", "vim mode"}, {"key::wrap", "wrap"}, {"key::nu", "line numbers"}}
}

// Mouse wheel scrolling for the list views.

func (v *tasksView) scrollBy(a *App, delta int) {
	switch v.mode {
	case "kanban":
		if v.col < len(v.columns) {
			v.card = max(0, min(max(0, len(v.columns[v.col].cards)-1), v.card+delta))
		}
	case "calendar":
		v.selectDay(a, v.day.AddDate(0, 0, 7*sign(delta)))
	default:
		v.list.move(delta, len(v.rows))
	}
}

func (v *tagsView) scrollBy(a *App, delta int) {
	if v.side == 1 {
		v.noteList.move(delta, len(v.notes))
		return
	}
	v.tagList.move(delta, len(v.tags))
	if len(v.selected) == 0 {
		v.refresh(a)
	}
}

func (v *noteListView) scrollBy(a *App, delta int) { v.list.move(delta, len(v.notes)) }
func (v *helpView) scrollBy(a *App, delta int) {
	v.scroll = max(0, min(max(0, len(v.lines)-v.rows), v.scroll+delta))
}
func (v *settingsView) scrollBy(a *App, delta int) {
	v.scroll = max(0, min(max(0, len(v.lines)-v.rows), v.scroll+delta))
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}
