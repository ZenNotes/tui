package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// calColumn is the layout of one day column in the week and day views: the
// timetable of the day and, for each block, the index of its entry in the
// entries of the day.
type calColumn struct {
	tt  calTimetable
	idx []int
	// sel is the index in the entries of the day of the selected entry, and
	// cursor the slot that holds the cursor. Both are -1 when the column is not
	// the selected day or when nothing is selected.
	sel, cursor int
}

// columnTable lays out a day for the current zoom of the hour rows.
func (c *taskCalendar) columnTable(day time.Time) calColumn {
	tt := c.timetable(day, c.hourStep())
	return calColumn{tt: tt, idx: c.blockIndexes(day, tt), sel: -1, cursor: -1}
}

// blockIndexes gives, for each block of the timetable, the index of its entry
// in dayEntries. A column lists the timed entries after the untimed ones, in
// the order of their start. The timetable keeps that order.
func (c *taskCalendar) blockIndexes(day time.Time, tt calTimetable) []int {
	base := c.untimedCount(day)
	out := make([]int, 0, len(tt.blocks))
	n := 0
	for _, e := range c.dayStored(day) {
		if e.clock() == "" {
			continue
		}
		if _, _, ok := blockSlots(e, tt.zoom); ok {
			out = append(out, base+n)
		}
		n++
	}
	return out
}

// calLevelStep is the width in cells that a level of overlap moves a block
// to the right in a column of w cells. It is an eighth of the column, from
// one to four cells.
func calLevelStep(w int) int { return clampInt(w/8, 1, 4) }

// Owner values of a cell in a slot that no block holds, and of a cell that
// holds the count of the blocks beyond the deepest level.
const (
	calCellEmpty = -1
	calCellMore  = -2
)

// slotOwners gives, for each of the w cells of a slot, the index of the block
// that is on top there. A block starts at the indent of its level and runs to
// the right end. A block of a higher level covers the blocks of lower levels,
// so each lower block stays visible in the cells to the left of the next one.
func (tt calTimetable) slotOwners(slot, w int) []int {
	owners := make([]int, w)
	for i := range owners {
		owners[i] = calCellEmpty
	}
	if slot < 0 || slot >= tt.slots {
		return owners
	}
	var covering []int
	for i, b := range tt.blocks {
		if b.first <= slot && slot <= b.last {
			covering = append(covering, i)
		}
	}
	sort.SliceStable(covering, func(i, j int) bool {
		return tt.blocks[covering[i]].level < tt.blocks[covering[j]].level
	})
	step := calLevelStep(w)
	for _, i := range covering {
		for x := min((tt.blocks[i].level-1)*step, w-1); x < w; x++ {
			owners[x] = i
		}
	}
	return owners
}

// calOverLabel is the text at the right end of a slot that holds n blocks
// beyond the deepest level.
func calOverLabel(n int) string { return fmt.Sprintf("+%d", n) }

// hourLabelCell is the label of hour row i of the view and the rule after it.
func (c *taskCalendar) hourLabelCell(a *App, lay calLayout, i int) string {
	th := a.theme
	label := hourLabel(a, (lay.hourStart+i)*c.hourStep())
	return th.Muted.Render(padRight(label, lay.gutter-2)) + th.Dim.Render(" │")
}

// hourCell draws one slot of a column that is w cells wide. A block shows its
// mark and its text on its first row only, and its level gives its colour.
// A slot that holds more blocks than the levels have ends with their count.
// The selected block and the empty cells of the slot of the cursor are in the
// focused selection style.
func (c *taskCalendar) hourCell(a *App, col calColumn, slot, w int) string {
	th := a.theme
	owners := col.tt.slotOwners(slot, w)
	over := 0
	if slot >= 0 && slot < len(col.tt.over) {
		over = col.tt.over[slot]
	}
	if over > 0 {
		for x := max(0, w-cellWidth(calOverLabel(over))); x < w; x++ {
			owners[x] = calCellMore
		}
	}
	var sb strings.Builder
	for x := 0; x < w; {
		end := x
		for end < w && owners[end] == owners[x] {
			end++
		}
		width := end - x
		switch owner := owners[x]; owner {
		case calCellEmpty:
			blank := strings.Repeat(" ", width)
			if slot == col.cursor {
				blank = th.SelectedFocus.Render(blank)
			}
			sb.WriteString(blank)
		case calCellMore:
			sb.WriteString(th.Bold.Render(padRight(calOverLabel(over), width)))
		default:
			b := col.tt.blocks[owner]
			text := ""
			if slot == b.first {
				text = " " + b.entry.mark + b.entry.content()
			}
			style := th.CalLevel[b.level-1]
			if col.sel >= 0 && owner < len(col.idx) && col.idx[owner] == col.sel {
				style = th.SelectedFocus
			}
			if b.entry.done {
				style = style.Strikethrough(true)
			}
			sb.WriteString(style.Render(padRight(truncateCells(text, width), width)))
		}
		x = end
	}
	return sb.String()
}

// hourEntryAt gives the index in the entries of the day of the entry that is
// on top at cell x of a slot in the hours of box b, or false when no block is
// there.
func (c *taskCalendar) hourEntryAt(b calDayBox, slot, x int) (int, bool) {
	col := c.columnTable(b.day)
	owners := col.tt.slotOwners(slot, b.hours.w)
	if x < 0 || x >= len(owners) || owners[x] < 0 || owners[x] >= len(col.idx) {
		return 0, false
	}
	return col.idx[owners[x]], true
}

// dayRows draws the body of the day view: the hour rows on the left and the
// all-day pane on the right, with a rule between them.
func (c *taskCalendar) dayRows(a *App, day time.Time, lay calLayout, col calColumn, w int, focused bool) []string {
	th := a.theme
	b := lay.boxes[0]
	scroll := c.columnScroll(b)
	rule := th.Dim.Render("│")
	rows := make([]string, 0, lay.pane.h)
	for i := 0; i < lay.pane.h; i++ {
		var sb strings.Builder
		if i < b.hours.h {
			sb.WriteString(c.hourLabelCell(a, lay, i))
			sb.WriteString(c.hourCell(a, col, lay.hourStart+i, b.hours.w))
		} else {
			sb.WriteString(strings.Repeat(" ", lay.pane.x))
		}
		sb.WriteString(rule)
		if i == 0 {
			head := " " + th.Bold.Render("All day") + th.Muted.Render(fmt.Sprintf(" (%d)", len(c.allDay(day))))
			sb.WriteString(padRight(head, lay.pane.w-1))
		} else {
			sb.WriteString(c.columnLine(a, b, i-1, focused, scroll))
		}
		rows = append(rows, padRight(sb.String(), w))
	}
	return rows
}
