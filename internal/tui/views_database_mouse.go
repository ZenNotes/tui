package tui

import tea "github.com/charmbracelet/bubbletea"

// click maps a pointer position inside the database pane: the view tabs on
// the first line, the header row, the gutter, cells, or board cards. Right
// clicks open the matching menu; double clicks edit.
func (v *databaseView) click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool) {
	if v.doc == nil || v.raw {
		return
	}
	right := m.Button == tea.MouseButtonRight
	if y == 0 {
		for _, s := range v.viewSpans {
			if x >= s.x0 && x < s.x1 {
				v.switchView(a, s.id)
				return
			}
		}
		if right {
			v.viewsMenu(a)
		}
		return
	}
	if v.isBoard() {
		v.boardClick(a, x, y, m, double)
		return
	}
	if y == 1 {
		for i, span := range v.colSpans {
			if x >= span[0]-1 && x < span[1]+2 {
				v.col = v.hscroll + i
				v.header = true
				switch {
				case right:
					v.fieldMenu(a, v.currentField())
				case double:
					v.renameFieldPrompt(a, v.currentField())
				}
				return
			}
		}
		if right {
			v.fieldMenu(a, v.currentField())
		}
		return
	}
	idx := v.scroll + y - 3
	if idx < 0 || idx >= len(v.rows) {
		if y >= 3 && !right {
			v.header = false
		}
		return
	}
	v.header = false
	v.row = idx
	if x < dbGutter {
		if !right {
			v.toggleSelectID(v.rows[idx].ID)
			return
		}
	}
	for i, span := range v.colSpans {
		if x >= span[0] && x < span[1]+2 {
			v.col = v.hscroll + i
		}
	}
	if right {
		v.rowMenu(a)
		return
	}
	if double {
		v.editCell(a)
	}
}
