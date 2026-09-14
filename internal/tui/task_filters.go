package tui

import (
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
	"sort"
	"strings"
)

// Shared saved queries have the desktop's substring semantics. The terminal's
// structured operators remain available behind an explicit where: prefix.
func filterTaskQuery(tasks []vault.Task, query string) []vault.Task {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.HasPrefix(q, "where:") {
		return filterTasks(tasks, strings.TrimSpace(q[len("where:"):]))
	}
	if q == "" {
		return tasks
	}
	out := []vault.Task{}
	for _, t := range tasks {
		match := strings.Contains(strings.ToLower(t.Content), q) || strings.Contains(strings.ToLower(t.NoteTitle), q) || (t.Priority != "" && strings.Contains("!"+t.Priority, q))
		for _, tag := range t.Tags {
			match = match || strings.Contains("#"+strings.ToLower(tag), q)
		}
		for k, v := range t.Fields {
			match = match || strings.Contains(strings.ToLower("@"+k+":"+v), q)
		}
		if match {
			out = append(out, t)
		}
	}
	return out
}
func (a *App) activeTasksView() *tasksView {
	a.openTasks()
	if t := a.activeTab(); t != nil {
		v, _ := t.view.(*tasksView)
		return v
	}
	return nil
}
func (a *App) savedTaskFilters() {
	items := []paletteItem{}
	names := []string{}
	for n := range a.prefs.SavedTaskFilters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		items = append(items, paletteItem{label: n, detail: a.prefs.SavedTaskFilters[n], id: n})
	}
	a.overlay = &palette{title: "Saved task filters", placeholder: "Select a filter · delete manages it", items: items, filtered: items, emptyHint: "Use :savefilter to save the current task query", onSelect: func(a *App, it paletteItem) {
		query := a.prefs.SavedTaskFilters[it.id]
		if v := a.activeTasksView(); v != nil {
			v.filter = query
			v.refresh(a)
			a.markSessionDirty()
		}
	}, onDelete: func(a *App, it paletteItem) { a.filterMenu(it.id) }}
}
func (a *App) saveTaskFilter() {
	v := a.activeTasksView()
	if v == nil {
		return
	}
	a.promptFor("Name this task filter", "", "Query: "+v.filter, func(a *App, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		save := func() {
			if err := config.SetValue("saved_filters", name, v.filter); err != nil {
				a.notifyError(err.Error())
				return
			}
			_ = a.reloadPreferences()
			a.notify("Saved filter " + name)
		}
		if _, exists := a.prefs.SavedTaskFilters[name]; exists {
			a.confirm("Replace filter "+name+"?", save)
		} else {
			save()
		}
	})
}
func (a *App) filterMenu(name string) {
	a.showMenu("Filter · "+name, []menuItem{
		{key: "r", label: "Rename", run: func(a *App) {
			a.promptFor("Rename filter", name, "", func(a *App, next string) {
				next = strings.TrimSpace(next)
				if next == "" || next == name {
					return
				}
				if _, exists := a.prefs.SavedTaskFilters[next]; exists {
					a.notifyError("A filter already has that name")
					return
				}
				err := config.UpdateConfig(func(doc map[string]any) error {
					m, _ := doc["saved_filters"].(map[string]any)
					if m != nil {
						m[next] = m[name]
						delete(m, name)
					}
					return nil
				})
				if err != nil {
					a.notifyError(err.Error())
					return
				}
				_ = a.reloadPreferences()
				a.savedTaskFilters()
			})
		}},
		{key: "d", label: "Delete", run: func(a *App) {
			a.confirm("Delete filter "+name+"?", func() {
				if err := config.SetValue("saved_filters", name, nil); err != nil {
					a.notifyError(err.Error())
					return
				}
				_ = a.reloadPreferences()
				a.savedTaskFilters()
			})
		}},
	})
}
func (v *tasksView) boardMenu(a *App) {
	a.showMenu("Board options", []menuItem{
		{key: "g", label: "Group by", run: func(a *App) {
			items := []paletteItem{}
			for _, g := range v.groupOptions() {
				items = append(items, paletteItem{label: g, id: g})
			}
			a.overlay = &palette{title: "Group tasks", items: items, filtered: items, onSelect: func(a *App, it paletteItem) { v.groupBy = it.id; a.prefs.KanbanGroupBy = it.id; v.refresh(a) }}
		}},
		{key: "r", label: "Folder root", run: func(a *App) {
			a.promptFor("Folder board root", a.prefs.KanbanFolderRoot, "Relative to primary notes; empty means all folders", func(a *App, s string) {
				a.prefs.KanbanFolderRoot = strings.Trim(s, "/")
				v.groupBy = "folder"
				a.prefs.KanbanGroupBy = "folder"
				v.refresh(a)
			})
		}},
		{key: "t", label: "Rename selected column", run: func(a *App) {
			if v.col >= len(v.columns) {
				return
			}
			c := v.columns[v.col]
			a.promptFor("Column title", c.title, "Empty restores default", func(a *App, s string) {
				var value any = s
				if strings.TrimSpace(s) == "" {
					value = nil
				}
				if err := config.SetValue("kanban_column_titles", c.id, value); err != nil {
					a.notifyError(err.Error())
					return
				}
				_ = a.reloadPreferences()
				v.refresh(a)
			})
		}},
		{key: "s", label: "Custom status columns", run: func(a *App) {
			a.promptFor("Status values", strings.Join(a.prefs.KanbanStatuses, ", "), "Comma-separated values for field:status", func(a *App, s string) {
				values := []string{}
				for _, v := range strings.Split(s, ",") {
					if v = strings.TrimSpace(v); v != "" {
						values = append(values, v)
					}
				}
				if err := config.SetValue("view", "kanban_statuses", values); err != nil {
					a.notifyError(err.Error())
					return
				}
				_ = a.reloadPreferences()
				v.refresh(a)
			})
		}},
		{key: "u", label: "Move card up", run: func(a *App) { v.reorderCard(a, -1) }},
		{key: "d", label: "Move card down", run: func(a *App) { v.reorderCard(a, 1) }},
	})
}
func (v *tasksView) reorderCard(a *App, delta int) {
	if v.col >= len(v.columns) {
		return
	}
	cards := v.columns[v.col].cards
	next := v.card + delta
	if v.card < 0 || v.card >= len(cards) || next < 0 || next >= len(cards) {
		return
	}
	cards[v.card], cards[next] = cards[next], cards[v.card]
	v.card = next
	if a.boardOrder == nil {
		a.boardOrder = map[string][]string{}
	}
	key := v.groupBy + "|" + v.columns[v.col].id
	ids := []string{}
	for _, t := range cards {
		ids = append(ids, t.ID)
	}
	a.boardOrder[key] = ids
	a.markSessionDirty()
}
