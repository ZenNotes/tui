package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/zennotescli/internal/vim"
)

// Click handling for the built-in views. Coordinates are relative to the
// view's content area; row 0 is the view's own header line.

func (v *tasksView) click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool) {
	right := m.Button == tea.MouseButtonRight
	switch v.mode {
	case "kanban":
		v.pointColumn(a, p, x)
		ci := v.col
		if ci >= len(v.columns) {
			return
		}
		start := 0
		if ci < len(v.boardStarts) {
			start = v.boardStarts[ci]
		}
		rel := y - 1 - 2
		if rel < 0 {
			return
		}
		card := start + rel/3
		if card < 0 || card >= len(v.columns[ci].cards) {
			return
		}
		v.card = card
		t := v.columns[ci].cards[card]
		switch {
		case right:
			a.taskMenu(t)
		case double:
			a.openTaskSource(t)
		}
	case "calendar":
		gridX := x - 1
		gridY := y - 1
		if day, ok := dayAtCell(v.calFirst, v.calWeeks, gridX, gridY, a.prefs.CalendarShowWeekNumbers); ok {
			v.selectDay(a, day)
			v.dayFocus = false
			if double {
				v.calendarKey(a, vim.KeyEnter)
			}
			return
		}
		listTop := 1 + 2 + v.calWeeks + 1
		idx := y - listTop
		if idx >= 0 && idx < len(v.dayTasks) {
			v.dayFocus = true
			v.dayCursor = idx
			t := v.dayTasks[idx]
			switch {
			case right:
				a.taskMenu(t)
			case double:
				a.openTaskSource(t)
			}
		}
	default:
		idx := v.list.scroll + y - 1
		if idx < 0 || idx >= len(v.rows) || v.rows[idx].task == nil {
			return
		}
		v.list.cursor = idx
		t := *v.rows[idx].task
		switch {
		case right:
			a.taskMenu(t)
		case double:
			a.openTaskSource(t)
		case x <= 2:
			a.toggleTask(t)
		}
	}
}

// pointColumn selects the board column under an x offset.
func (v *tasksView) pointColumn(a *App, p *pane, x int) {
	if len(v.columns) == 0 {
		return
	}
	colW := max(12, (a.contentRect(p).w-len(v.columns)+1)/len(v.columns))
	ci := x / (colW + 1)
	if ci >= 0 && ci < len(v.columns) && ci != v.col {
		v.col = ci
		v.card = 0
	}
}

func (v *tagsView) click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool) {
	leftW := min(32, max(18, a.contentRect(p).w/3))
	if x < leftW {
		idx := v.tagList.scroll + y - 1
		if idx < 0 || idx >= len(v.tags) {
			return
		}
		v.side = 0
		v.tagList.cursor = idx
		if double {
			t := v.tags[idx].tag
			v.selected[t] = !v.selected[t]
			if !v.selected[t] {
				delete(v.selected, t)
			}
		}
		if m.Button == tea.MouseButtonRight {
			v.handleKey(a, vim.R('m'))
			return
		}
		v.refresh(a)
		return
	}
	idx := v.noteList.scroll + y - 1
	if idx < 0 || idx >= len(v.notes) {
		return
	}
	v.side = 1
	v.noteList.cursor = idx
	if m.Button == tea.MouseButtonRight {
		a.noteContextMenu(v.notes[idx].Path)
		return
	}
	if double {
		a.openNote(v.notes[idx].Path, true)
	}
}

func (v *noteListView) click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool) {
	if y < 1 {
		return
	}
	idx := v.list.scroll + (y-1)/2
	if idx < 0 || idx >= len(v.notes) {
		return
	}
	v.list.cursor = idx
	if m.Button == tea.MouseButtonRight {
		a.noteContextMenu(v.notes[idx].Path)
		return
	}
	if double {
		a.openNote(v.notes[idx].Path, true)
	}
}

var _ = time.Now

// Screen positions of selections, the inverse of the click geometry, so
// keyboard menus open next to the row they act on.

func (v *tasksView) selectionPos(a *App, p *pane) (int, int, bool) {
	top := a.contentRect(p).y
	switch v.mode {
	case "kanban":
		if v.col >= len(v.columns) {
			return 0, 0, false
		}
		colW := max(12, (a.contentRect(p).w-len(v.columns)+1)/len(v.columns))
		start := 0
		if v.col < len(v.boardStarts) {
			start = v.boardStarts[v.col]
		}
		return a.contentRect(p).x + v.col*(colW+1) + 2, top + 1 + 2 + (v.card-start)*3 + 1, true
	case "calendar":
		if v.dayFocus {
			return a.contentRect(p).x + 4, top + 1 + 2 + v.calWeeks + 1 + v.dayCursor + 1, true
		}
		return a.contentRect(p).x + 8, top + 1 + 2 + 1, true
	}
	return a.contentRect(p).x + 4, top + 1 + (v.list.cursor - v.list.scroll) + 1, true
}

func (v *tagsView) selectionPos(a *App, p *pane) (int, int, bool) {
	top := a.contentRect(p).y
	leftW := min(32, max(18, a.contentRect(p).w/3))
	if v.side == 1 {
		return a.contentRect(p).x + leftW + 4, top + 1 + (v.noteList.cursor - v.noteList.scroll) + 1, true
	}
	return a.contentRect(p).x + leftW + 1, top + 1 + (v.tagList.cursor - v.tagList.scroll), true
}

func (v *noteListView) selectionPos(a *App, p *pane) (int, int, bool) {
	top := a.contentRect(p).y
	return a.contentRect(p).x + 4, top + 1 + (v.list.cursor-v.list.scroll)*2 + 1, true
}
