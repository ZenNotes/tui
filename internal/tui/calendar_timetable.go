package tui

import (
	"sort"
	"time"
)

// calMaxLevel is the deepest indentation level of overlapping blocks. A block
// that finds no free level among the levels up to this one shares it.
const calMaxLevel = 4

// calBlock is a timed entry placed on the slots of one day.
type calBlock struct {
	entry calEntry
	// first and last are the first and the last slot that the block covers.
	first, last int
	// level is the indentation level of the block, from 1 to calMaxLevel.
	level int
}

// calTimetable is the layout of one day for a zoom: the timed entries as
// blocks on slots, and the entries that cover the day whole.
type calTimetable struct {
	// zoom is the minutes that one slot covers.
	zoom int
	// slots is the number of slots in the day.
	slots int
	// blocks are the timed entries, earliest first, ties in note order.
	blocks []calBlock
	// untimed are the entries with no time, in the order of the day.
	untimed []calEntry
	// over holds, for each slot, how many blocks found no free level among
	// the first calMaxLevel levels there.
	over []int
}

// blockEnd is the time of day at which the entry ends, or "" when it has no
// end. A due entry takes the end of its due value.
func (e calEntry) blockEnd() string {
	if e.kind == calDue {
		return e.task.DueEnd
	}
	return e.end
}

// timeMinute is the minute of the day of an `HH:MM` time. The time 24:00 is
// the end of the day. It gives -1 for a text that is not a time.
func timeMinute(clock string) int {
	if clock == "24:00" {
		return 24 * 60
	}
	return clockMinute(clock)
}

// blockSlots gives the first and the last slot that an entry covers. The
// block runs from the slot that holds its start to the slot before its end.
// An entry with no end, or with an end that is not after its start, covers
// the one slot of its start. It gives false for an entry with no start.
func blockSlots(e calEntry, zoom int) (first, last int, ok bool) {
	start := timeMinute(e.clock())
	if start < 0 || start >= 24*60 {
		return 0, 0, false
	}
	first, last = start/zoom, start/zoom
	if end := timeMinute(e.blockEnd()); end > start {
		last = (min(end, 24*60) - 1) / zoom
	}
	return first, last, true
}

// timetable lays out the entries of a day on slots of zoom minutes. A block
// takes the lowest level that is free in all of its slots, and the blocks are
// placed in the order of their start. A block that finds no free level up to
// calMaxLevel takes the last level and counts in over for each of its slots.
func (c *taskCalendar) timetable(day time.Time, zoom int) calTimetable {
	if zoom <= 0 {
		zoom = 60
	}
	slots := 24 * 60 / zoom
	tt := calTimetable{zoom: zoom, slots: slots, over: make([]int, slots)}
	for _, e := range c.dayStored(day) {
		first, last, ok := blockSlots(e, zoom)
		if !ok {
			tt.untimed = append(tt.untimed, e)
			continue
		}
		tt.blocks = append(tt.blocks, calBlock{entry: e, first: first, last: last})
	}
	sort.SliceStable(tt.blocks, func(i, j int) bool {
		return timeMinute(tt.blocks[i].entry.clock()) < timeMinute(tt.blocks[j].entry.clock())
	})
	used := make([][calMaxLevel + 1]bool, slots)
	for i := range tt.blocks {
		b := &tt.blocks[i]
		b.level = 1
		for b.level < calMaxLevel && levelTaken(used, b.first, b.last, b.level) {
			b.level++
		}
		if b.level == calMaxLevel && levelTaken(used, b.first, b.last, b.level) {
			for s := b.first; s <= b.last; s++ {
				if used[s][calMaxLevel] {
					tt.over[s]++
				}
			}
		}
		for s := b.first; s <= b.last; s++ {
			used[s][b.level] = true
		}
	}
	return tt
}

// levelTaken reports whether a block holds the level in any slot from first
// to last.
func levelTaken(used [][calMaxLevel + 1]bool, first, last, level int) bool {
	for s := first; s <= last; s++ {
		if used[s][level] {
			return true
		}
	}
	return false
}
