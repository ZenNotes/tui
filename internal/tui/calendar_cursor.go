package tui

import (
	"fmt"
	"sort"
	"time"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// calStepDays is how many days Tab looks ahead or back for the next timed
// entry before it gives up.
const calStepDays = 366

// entryKey names an entry so that a choice of Tab survives a new layout of
// the same day.
func entryKey(e calEntry) string {
	return fmt.Sprintf("%d|%s|%s|%s|%d|%s|%s|%s", e.kind, e.task.ID, e.path, e.text, e.line, e.start, e.end, e.mark)
}

// curRow is the index of the slot that the cursor is on.
func (c *taskCalendar) curRow() int {
	return clampInt(c.curMin/c.hourStep(), 0, c.hourRowCount()-1)
}

// setCursorRow puts the cursor on the slot with the given index and drops the
// choice of Tab. The next layout scrolls the hour list to show the slot.
func (c *taskCalendar) setCursorRow(row int) {
	c.curMin = clampInt(row, 0, c.hourRowCount()-1) * c.hourStep()
	c.pin = ""
	c.follow = true
}

// moveSlot moves the cursor by delta slots. It stops at the first slot and
// at the last slot of the day.
func (c *taskCalendar) moveSlot(delta int) {
	c.setCursorRow(c.curRow() + delta)
}

// revealCursor scrolls the hour list, when it shows room rows, so that the
// slot of the cursor is on screen.
func (c *taskCalendar) revealCursor(room int) {
	if room <= 0 {
		return
	}
	step := c.hourStep()
	row := c.curRow()
	first := c.firstHourRow(room)
	switch {
	case row < first:
		c.scroll = row * step
	case row >= first+room:
		c.scroll = (row - room + 1) * step
	}
}

// covering gives the indexes of the blocks that cover a slot. The block with
// the lowest level comes first, and the earliest start breaks a tie.
func (col calColumn) covering(row int) []int {
	var out []int
	for i, b := range col.tt.blocks {
		if i < len(col.idx) && b.first <= row && row <= b.last {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return col.tt.blocks[out[i]].level < col.tt.blocks[out[j]].level
	})
	return out
}

// entryAt gives the index in the entries of the day of the entry that is
// selected with the cursor on a slot. It is the entry that Tab chose, if that
// entry covers the slot, else the first of the entries that cover it.
func (col calColumn) entryAt(row int, pin string) (int, bool) {
	cover := col.covering(row)
	if len(cover) == 0 {
		return 0, false
	}
	if pin != "" {
		for _, i := range cover {
			if entryKey(col.tt.blocks[i].entry) == pin {
				return col.idx[i], true
			}
		}
	}
	return col.idx[cover[0]], true
}

// gridEntry gives the index in the entries of the day of the entry that the
// cursor selects in the hour grid of a week or day view.
func (c *taskCalendar) gridEntry(day time.Time) (int, bool) {
	if c.sub == calMonth {
		return 0, false
	}
	return c.columnTable(day).entryAt(c.curRow(), c.pin)
}

// slotEntries gives the entries that cover the slot of the cursor, in level
// order.
func (c *taskCalendar) slotEntries(day time.Time) []calEntry {
	if c.sub == calMonth {
		return nil
	}
	col := c.columnTable(day)
	var out []calEntry
	for _, i := range col.covering(c.curRow()) {
		out = append(out, col.tt.blocks[i].entry)
	}
	return out
}

// markColumn tells a column which of its slots holds the cursor and which of
// its entries is selected, when the grid or the all-day entries have the
// focus.
func (c *taskCalendar) markColumn(col *calColumn) {
	switch c.focus {
	case calFocusItems:
		col.sel = c.cursor
	case calFocusGrid:
		if c.sub == calMonth {
			return
		}
		col.cursor = c.curRow()
		if i, ok := col.entryAt(col.cursor, c.pin); ok {
			col.sel = i
		}
	}
}

// stepTarget finds the timed entry that Tab (dir 1) or Shift-Tab (dir -1)
// selects, which can be on another day. Entries are in the order of their day
// and then of their start. With no entry selected, the step goes to the first
// entry that starts after the cursor, or the last that starts before it.
func (c *taskCalendar) stepTarget(day time.Time, dir int) (time.Time, calBlock, bool) {
	row := c.curRow()
	cur, hasCur := c.gridEntry(day)
	for n := 0; n <= calStepDays; n++ {
		d := day.AddDate(0, 0, dir*n)
		col := c.columnTable(d)
		blocks := col.tt.blocks
		pick := -1
		switch {
		case len(blocks) == 0 || len(col.idx) < len(blocks):
		case n > 0 && dir > 0:
			pick = 0
		case n > 0:
			pick = len(blocks) - 1
		default:
			pos := -1
			for i := range blocks {
				if hasCur && col.idx[i] == cur {
					pos = i
					break
				}
			}
			switch {
			case pos >= 0:
				pick = pos + dir
			case dir > 0:
				for i, b := range blocks {
					if b.first > row {
						pick = i
						break
					}
				}
			default:
				for i := len(blocks) - 1; i >= 0; i-- {
					if blocks[i].first < row {
						pick = i
						break
					}
				}
			}
		}
		if pick >= 0 && pick < len(blocks) {
			return d, blocks[pick], true
		}
	}
	return day, calBlock{}, false
}

// stepEntry selects the next (dir 1) or the previous (dir -1) timed entry.
// The cursor goes to the slot where the entry starts. When there is no such
// entry, nothing changes.
func (v *tasksView) stepEntry(a *App, dir int) {
	c := v.calc()
	d, b, ok := c.stepTarget(v.day, dir)
	if !ok {
		return
	}
	if !sameDay(d, v.day) {
		v.selectDay(a, d)
	}
	c.setCursorRow(b.first)
	c.pin = entryKey(b.entry)
}

// focusAllDay moves the focus to the all-day entries of the selected day. It
// does nothing when the day has none.
func (v *tasksView) focusAllDay() {
	c := v.calc()
	if c.sub == calMonth || c.untimedCount(v.day) == 0 {
		return
	}
	c.focus = calFocusItems
	c.cursor = 0
}

// leaveItems gives the focus back to the grid. In a week or day view the
// cursor goes to the slot of the entry that was selected, when it is timed.
func (c *taskCalendar) leaveItems(day time.Time) {
	if c.focus == calFocusItems && c.sub != calMonth {
		entries := c.dayEntries(day)
		if c.cursor >= c.untimedCount(day) && c.cursor < len(entries) {
			if first, _, ok := blockSlots(entries[c.cursor], c.hourStep()); ok {
				c.setCursorRow(first)
				c.pin = entryKey(entries[c.cursor])
			}
		}
	}
	c.focus = calFocusGrid
}

// gridEnter acts on Enter in the hour grid. One entry on the slot opens, and
// several entries open a picker. On an empty slot the focus moves to the
// all-day entries, then to the strip of tasks without a date, and when both
// are empty the daily note opens.
func (v *tasksView) gridEnter(a *App) {
	c := v.calc()
	cover := c.slotEntries(v.day)
	switch {
	case len(cover) == 1:
		a.openCalEntry(cover[0])
	case len(cover) > 1:
		v.slotPicker(a, cover)
	case c.untimedCount(v.day) > 0:
		c.focus = calFocusItems
		c.cursor = 0
	case len(c.undated) > 0:
		c.focus = calFocusStrip
		c.strip = 0
	default:
		a.openPeriodicOn(periodic.Daily, v.day)
	}
}

// openCalEntry opens the note of an entry. A due entry and a session open the
// line of their task. An inline event opens its line, and an event note opens
// the note.
func (a *App) openCalEntry(e calEntry) {
	switch {
	case e.hasTask():
		a.openTaskSource(e.task)
	case e.path != "":
		a.openNote(e.path, true)
		if buf := a.buffers[e.path]; buf != nil && e.inline {
			buf.ed.GotoLine(e.line + 1)
		}
	}
}

// taskActionKey runs a key that acts on a task of the calendar. It reports
// whether the key did.
func (v *tasksView) taskActionKey(a *App, k vim.Key, t *vault.Task) bool {
	switch {
	case a.bound(k, "nav.toggleTask"):
		a.toggleTask(*t)
	case a.bound(k, "nav.contextMenu"):
		a.taskMenu(*t)
	case k.IsRune('T'):
		// As in the desktop calendar: T moves the task to today, @ picks a date.
		a.setTaskDue(*t, vault.TodayISO(calendarNow()))
	case k.IsRune('@'):
		v.calendarDueMenu(a, *t)
	default:
		return v.taskEditKey(a, k, t)
	}
	return true
}

// calendarStepChord reports whether k is Ctrl-N or Ctrl-P with the hour grid
// of a week or day view in focus. Those keys step through the timed entries
// there, so the global chords of the same keys give way.
func (a *App) calendarStepChord(k vim.Key) bool {
	if !k.IsCtrl('n') && !k.IsCtrl('p') {
		return false
	}
	t := a.activeTab()
	if a.focus != focusPane || t == nil {
		return false
	}
	v, ok := t.view.(*tasksView)
	return ok && v.mode == "calendar" && v.calc().sub != calMonth && v.calc().focus == calFocusGrid
}
