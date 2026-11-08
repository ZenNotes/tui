package tui

import "time"

// calDefaultMinute is the minute of the day at which a week or day layout
// opens when it has no current time and no timed entry to show.
const calDefaultMinute = 8 * 60

// nowMinute is the minute of the day of the calendar clock.
func nowMinute() int {
	now := calendarNow()
	return now.Hour()*60 + now.Minute()
}

// holdsNow reports whether the hour row with the given index holds the
// current time, in a layout that shows today.
func (c *taskCalendar) holdsNow(lay calLayout, row int) bool {
	today := calendarNow()
	for _, b := range lay.boxes {
		if sameDay(b.day, today) {
			return nowMinute()/c.hourStep() == row
		}
	}
	return false
}

// openingMinute is the minute of the day at which a week or day layout of the
// given day opens. It is the current time when today is in view, else the
// start of the first timed entry in view, else 08:00.
func (c *taskCalendar) openingMinute(a *App, day time.Time) int {
	today := calendarNow()
	first := -1
	for _, d := range c.columnDays(a, day) {
		if sameDay(d, today) {
			return nowMinute()
		}
		if blocks := c.timetable(d, 60).blocks; len(blocks) > 0 {
			if m := timeMinute(blocks[0].entry.clock()); first < 0 || m < first {
				first = m
			}
		}
	}
	if first >= 0 {
		return first
	}
	return calDefaultMinute
}

// openAtTime puts the cursor of a week or day layout on its opening time and
// shows that row first in the hour list. A list that would then end before
// the last row of the day ends on the last row, and the cursor row stays on
// screen. The month layout has no hour list, so it changes nothing.
func (v *tasksView) openAtTime(a *App) {
	c := v.calc()
	if c.sub == calMonth {
		return
	}
	minute := c.openingMinute(a, v.day)
	c.setCursorRow(minute / c.hourStep())
	c.scroll = c.curMin
}

// goToNow shows the day of today in a week or day layout, with the cursor on
// the row of the current time.
func (v *tasksView) goToNow(a *App) {
	now := calendarNow()
	v.month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	v.selectDay(a, now)
	v.openAtTime(a)
}
