package tui

import "strings"

// slotPicker lists the entries on the slot of the cursor, in level order, with
// the mark of each. A choice opens the note of that entry. Esc closes the
// picker and changes nothing.
func (v *tasksView) slotPicker(a *App, entries []calEntry) {
	c := v.calc()
	items := make([]menuItem, 0, len(entries))
	for _, e := range entries {
		e := e
		items = append(items, menuItem{
			label: e.label(a.prefs.TimeFormat),
			run:   func(a *App) { a.openCalEntry(e) },
		})
	}
	slot := strings.TrimSpace(hourLabel(a, c.curRow()*c.hourStep()))
	a.showMenu(v.day.Format("Mon Jan 2")+" · "+slot, items)
}
