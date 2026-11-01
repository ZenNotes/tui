package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// calendarNow is the clock the calendar reads for "today", so tests can fix it.
var calendarNow = time.Now

// calSub is the calendar's layout: a month of day boxes, or a week or a day
// of columns.
type calSub int

const (
	calMonth calSub = iota
	calWeek
	calDay
)

// calFocus is the part of the calendar that takes the keys.
type calFocus int

const (
	calFocusGrid calFocus = iota
	calFocusItems
	calFocusStrip
)

// calMin holds the smallest body (width, height) each layout renders in.
// Below it the body only says the terminal is too small.
var calMin = map[calSub][2]int{
	calMonth: {72, 18},
	calWeek:  {56, 8},
	calDay:   {30, 7},
}

// calScheduledMark prefixes an entry placed by its scheduled date. It must
// stay one cell wide.
const calScheduledMark = "»"

// calRect is a region of the calendar body, in body cells.
type calRect struct{ x, y, w, h int }

func (r calRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// calDayBox is where one day sits in the grid.
type calDayBox struct {
	day  time.Time
	rect calRect
}

// calLayout is every region of the calendar body for one size. Rendering,
// clicks and menu placement all read the last one, so the geometry is
// written once.
type calLayout struct {
	w, h     int
	tooSmall bool
	// compact is the month view below its minimum: one line per week with a
	// count per day, the selected day's items under the grid.
	compact   bool
	items     calRect
	strip     calRect
	boxes     []calDayBox
	itemStart int
	stripRows int
	// stripStart is the index of the first strip task shown.
	stripStart int
}

// taskCalendar is the Tasks tab's calendar: day entries, the items beside
// the grid, and the strip of tasks without a date.
type taskCalendar struct {
	sub    calSub
	focus  calFocus
	cursor int
	strip  int
	// colScroll is how many entries the selected day's column has scrolled
	// off its top.
	colScroll int

	byDay   map[string][]calEntry
	undated []vault.Task
	layout  calLayout
}

func newTaskCalendar() *taskCalendar { return &taskCalendar{} }

// rebuild places the (already filtered) tasks and keeps the cursors inside
// the new lists.
func (c *taskCalendar) rebuild(tasks []vault.Task, day time.Time) {
	c.byDay, c.undated = bucketCalendarEntries(tasks)
	entries := c.dayEntries(day)
	if c.cursor >= len(entries) {
		c.cursor = max(0, len(entries)-1)
	}
	if c.focus == calFocusItems && len(entries) == 0 {
		c.focus = calFocusGrid
	}
	if c.strip >= len(c.undated) {
		c.strip = max(0, len(c.undated)-1)
	}
	if c.focus == calFocusStrip && len(c.undated) == 0 {
		c.focus = calFocusGrid
	}
}

func (c *taskCalendar) dayEntries(day time.Time) []calEntry {
	return c.byDay[day.Format("2006-01-02")]
}

// focusOrder is the parts Tab visits, in order: the grid, then the selected
// day's items when it has any, then the strip of undated tasks when it has any.
func (c *taskCalendar) focusOrder(day time.Time) []calFocus {
	order := []calFocus{calFocusGrid}
	if len(c.dayEntries(day)) > 0 {
		order = append(order, calFocusItems)
	}
	if len(c.undated) > 0 {
		order = append(order, calFocusStrip)
	}
	return order
}

// cycleFocus moves focus to the next part in focusOrder, wrapping around. It
// reports whether focus moved.
func (c *taskCalendar) cycleFocus(day time.Time) bool {
	order := c.focusOrder(day)
	if len(order) < 2 {
		return false
	}
	next := order[0]
	for i, f := range order {
		if f == c.focus {
			next = order[(i+1)%len(order)]
			break
		}
	}
	c.focus = next
	if next == calFocusItems {
		c.cursor = 0
	} else if next == calFocusStrip {
		c.strip = 0
	}
	return true
}

// selected is the task the item panel or the strip has selected, if any.
func (c *taskCalendar) selected(day time.Time) *vault.Task {
	switch c.focus {
	case calFocusItems:
		entries := c.dayEntries(day)
		if c.cursor < len(entries) {
			t := entries[c.cursor].task
			return &t
		}
	case calFocusStrip:
		if c.strip < len(c.undated) {
			t := c.undated[c.strip]
			return &t
		}
	}
	return nil
}

// monthLayout computes the regions of the month view for a body of w x h.
func (c *taskCalendar) monthLayout(a *App, month time.Time, w, h int) calLayout {
	lay := calLayout{w: w, h: h}
	if m := calMin[calMonth]; w < m[0] || h < m[1] {
		return c.compactMonthLayout(a, month, w, h)
	}
	panelW := clampInt(w*30/100, 26, 44)
	gw := w - panelW
	wnW := 0
	if a.prefs.CalendarShowWeekNumbers {
		wnW = 3
	}
	colW := (gw - wnW) / 7
	stripRows := clampInt(h/8, 2, 6)
	stripH := 1 + stripRows
	gridH := h - 2 - stripH
	first, weeks := monthGridStart(month, a.prefs.CalendarWeekStart != "sunday")
	rowH := gridH / weeks
	lay.items = calRect{x: gw, y: 0, w: panelW, h: h - stripH}
	lay.strip = calRect{x: 0, y: h - stripH, w: w, h: stripH}
	lay.stripRows = stripRows
	day := first
	for r := 0; r < weeks; r++ {
		for col := 0; col < 7; col++ {
			lay.boxes = append(lay.boxes, calDayBox{day: day, rect: calRect{x: wnW + col*colW, y: 2 + r*rowH, w: colW, h: rowH}})
			day = day.AddDate(0, 0, 1)
		}
	}
	return lay
}

// calCompactCell is the narrowest day cell of the compact month: the date
// and a count, as in the side calendar panel.
const calCompactCell = 5

// compactMonthLayout computes the compact month for a body too small for
// the day boxes: the grid at one line per week, the selected day's items
// under it, and the strip at the bottom. It is too small only when even
// that does not fit.
func (c *taskCalendar) compactMonthLayout(a *App, month time.Time, w, h int) calLayout {
	lay := calLayout{w: w, h: h, compact: true}
	wnW := 0
	if a.prefs.CalendarShowWeekNumbers {
		wnW = 3
	}
	colW := min((w-wnW)/7, 2*calCompactCell)
	first, weeks := monthGridStart(month, a.prefs.CalendarWeekStart != "sunday")
	gridH := 2 + weeks
	// Under the grid: the items' header and one item, then the strip's
	// header and one task.
	if colW < calCompactCell || h < gridH+4 {
		lay.tooSmall = true
		return lay
	}
	rest := h - gridH
	stripRows := clampInt(rest/4, 1, 4)
	stripH := 1 + stripRows
	lay.items = calRect{x: 0, y: gridH, w: w, h: rest - stripH}
	lay.strip = calRect{x: 0, y: h - stripH, w: w, h: stripH}
	lay.stripRows = stripRows
	day := first
	for r := 0; r < weeks; r++ {
		for col := 0; col < 7; col++ {
			lay.boxes = append(lay.boxes, calDayBox{day: day, rect: calRect{x: wnW + col*colW, y: 2 + r, w: colW, h: 1}})
			day = day.AddDate(0, 0, 1)
		}
	}
	return lay
}

// renderCompactMonth draws the compact month: the grid with a count per
// day, the selected day's items and the strip.
func (c *taskCalendar) renderCompactMonth(a *App, month, day time.Time, lay calLayout, w, h int, focused bool) string {
	th := a.theme
	rows := make([]string, 0, h)
	rows = append(rows, padRight(" "+th.Title.Render(month.Format("January 2006")), w))
	wnW := lay.boxes[0].rect.x
	colW := lay.boxes[0].rect.w
	names := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	if a.prefs.CalendarWeekStart == "sunday" {
		names = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	}
	head := strings.Repeat(" ", wnW)
	for _, n := range names {
		head += padRight(" "+th.Dim.Render(n), colW)
	}
	rows = append(rows, padRight(head, w))
	today := calendarNow()
	for r := 0; r < len(lay.boxes)/7; r++ {
		var sb strings.Builder
		if wnW > 0 {
			anchor := lay.boxes[r*7].day
			if a.prefs.CalendarWeekStart == "sunday" {
				anchor = anchor.AddDate(0, 0, 1)
			}
			_, wk := anchor.ISOWeek()
			sb.WriteString(th.Muted.Render(fmt.Sprintf("%2d ", wk)))
		}
		for col := 0; col < 7; col++ {
			box := lay.boxes[r*7+col]
			sb.WriteString(c.compactCell(a, box, sameDay(box.day, day), sameDay(box.day, today), box.day.Month() != month.Month(), focused))
		}
		rows = append(rows, padRight(sb.String(), w))
	}
	rows = append(rows, c.itemPanel(a, day, lay, focused)...)
	rows = append(rows, c.stripLines(a, lay, w, focused)...)
	return fitBlock(strings.Join(rows, "\n"), w, h)
}

// compactCell is one day of the compact grid: the date, then how many
// entries the day has (+ for ten or more).
func (c *taskCalendar) compactCell(a *App, box calDayBox, selected, today, outside, focused bool) string {
	th := a.theme
	w := box.rect.w
	num := fmt.Sprintf(" %2d", box.day.Day())
	count := ""
	if n := len(c.dayEntries(box.day)); n > 9 {
		count = " +"
	} else if n > 0 {
		count = fmt.Sprintf(" %d", n)
	}
	if selected {
		style := th.Selected
		if focused && c.focus == calFocusGrid {
			style = th.SelectedFocus
		}
		return style.Render(padRight(num+count, w))
	}
	style := th.Base
	switch {
	case today:
		style = th.Base.Foreground(th.Accent).Bold(true)
	case outside:
		style = th.Muted
	}
	return padRight(style.Render(num)+th.Marker.Render(count), w)
}

// columnDays is the days a week or day layout shows as columns, in order.
func (c *taskCalendar) columnDays(a *App, day time.Time) []time.Time {
	if c.sub == calDay {
		return []time.Time{day}
	}
	offset := int(day.Weekday())
	if a.prefs.CalendarWeekStart != "sunday" {
		offset = (offset + 6) % 7
	}
	first := time.Date(day.Year(), day.Month(), day.Day()-offset, 0, 0, 0, 0, time.Local)
	days := make([]time.Time, 7)
	for i := range days {
		days[i] = first.AddDate(0, 0, i)
	}
	return days
}

// columnLayout computes the regions of the week or day view for a body of
// w x h: one column per day under the header rows, and the strip pinned to
// the bottom of the body.
func (c *taskCalendar) columnLayout(a *App, day time.Time, w, h int) calLayout {
	lay := calLayout{w: w, h: h}
	if m := calMin[c.sub]; w < m[0] || h < m[1] {
		lay.tooSmall = true
		return lay
	}
	days := c.columnDays(a, day)
	headRows := 1
	if c.sub == calWeek {
		headRows = 2
	}
	stripRows := clampInt(h/8, 2, 6)
	stripH := 1 + stripRows
	lay.stripRows = stripRows
	lay.strip = calRect{x: 0, y: h - stripH, w: w, h: stripH}
	colH := h - stripH - headRows
	colW := w / len(days)
	for i, d := range days {
		lay.boxes = append(lay.boxes, calDayBox{day: d, rect: calRect{x: i * colW, y: headRows, w: colW, h: colH}})
	}
	return lay
}

// renderColumns draws the week or day view: a header, the day columns and
// the strip. Tasks carry a date but no time of day, so a column lists its
// entries from the top.
func (c *taskCalendar) renderColumns(a *App, day time.Time, lay calLayout, w, h int, focused bool) string {
	th := a.theme
	rows := make([]string, 0, h)
	first, last := lay.boxes[0].day, lay.boxes[len(lay.boxes)-1].day
	if c.sub == calDay {
		rows = append(rows, padRight(" "+th.Title.Render(day.Format("Monday, January 2, 2006")), w))
	} else {
		title := first.Format("January 2") + " – " + last.Format("January 2, 2006")
		rows = append(rows, padRight(" "+th.Title.Render(title), w))
		head := ""
		for _, b := range lay.boxes {
			cell := padRight(" "+b.day.Format("Mon")[:2]+b.day.Format(" 2"), b.rect.w)
			if sameDay(b.day, day) {
				cell = th.Selected.Render(cell)
			} else {
				cell = th.Dim.Render(cell)
			}
			head += cell
		}
		rows = append(rows, padRight(head, w))
	}
	for i := 0; i < lay.boxes[0].rect.h; i++ {
		var sb strings.Builder
		for _, b := range lay.boxes {
			scroll := 0
			if sameDay(b.day, day) {
				scroll = c.columnScroll(b)
			}
			sb.WriteString(c.columnLine(a, b, i, focused && sameDay(b.day, day), scroll))
		}
		rows = append(rows, padRight(sb.String(), w))
	}
	for len(rows) < lay.strip.y {
		rows = append(rows, strings.Repeat(" ", w))
	}
	rows = append(rows, c.stripLines(a, lay, w, focused)...)
	return fitBlock(strings.Join(rows, "\n"), w, h)
}

// maxColumnScroll is the furthest a day column can scroll: the last entry
// then sits on the last line.
func (c *taskCalendar) maxColumnScroll(b calDayBox) int {
	n := len(c.dayEntries(b.day))
	if n <= b.rect.h {
		return 0
	}
	return n - (b.rect.h - 1)
}

// columnScroll is the scroll applied to box b, kept inside its range.
func (c *taskCalendar) columnScroll(b calDayBox) int {
	items := c.focus == calFocusItems
	s := clampInt(c.colScroll, 0, c.maxColumnScroll(b))
	if items {
		// Keep the selected entry on screen as the selection moves.
		s = min(s, c.cursor)
		for s < c.maxColumnScroll(b) && c.cursor >= s+c.columnShown(b, s) {
			s++
		}
		c.colScroll = s
	}
	return s
}

// columnShown is how many entries box b lists when scrolled by s.
func (c *taskCalendar) columnShown(b calDayBox, s int) int {
	room := b.rect.h - min(s, 1)
	if rest := len(c.dayEntries(b.day)) - s; rest <= room {
		return rest
	}
	return room - 1
}

// scrollColumn scrolls the selected day's column by delta entries, never past
// either end. It reports whether there is a column to scroll.
func (c *taskCalendar) scrollColumn(day time.Time, delta int) bool {
	if c.sub == calMonth {
		return false
	}
	for _, b := range c.layout.boxes {
		if sameDay(b.day, day) {
			c.colScroll = clampInt(c.columnScroll(b)+delta, 0, c.maxColumnScroll(b))
			return true
		}
	}
	return false
}

// calMoreLabel is the overflow line for n hidden entries in a w-wide cell:
// " +N more" when it fits, else " +N", else " +N" cut to the width.
func calMoreLabel(n, w int) string {
	long := fmt.Sprintf(" +%d more", n)
	if cellWidth(long) <= w {
		return long
	}
	return truncateCells(fmt.Sprintf(" +%d", n), w)
}

// columnLine is line li of one day column: entries from the scroll offset,
// with "+N more" on the first line when some are scrolled off the top and on
// the last line when they do not all fit.
func (c *taskCalendar) columnLine(a *App, b calDayBox, li int, focusedDay bool, scroll int) string {
	th := a.theme
	w := b.rect.w
	entries := c.dayEntries(b.day)
	more := func(n int) string {
		return th.Dim.Render(padRight(calMoreLabel(n, w), w))
	}
	first := 0
	if scroll > 0 {
		if li == 0 {
			return more(scroll)
		}
		first = scroll
		li--
	}
	rest := entries[first:]
	room := b.rect.h - min(scroll, 1)
	shown := len(rest)
	if shown > room {
		shown = room - 1
	}
	switch {
	case li < shown:
		idx := first + li
		e := entries[idx]
		lead := " "
		if e.scheduled {
			lead = calScheduledMark
		}
		text := padRight(lead+truncateCells(e.task.Content, w-2), w)
		if focusedDay && c.focus == calFocusItems && idx == c.cursor {
			return th.SelectedFocus.Render(text)
		}
		return text
	case li == shown && shown < len(rest):
		return more(len(rest) - shown)
	}
	return strings.Repeat(" ", w)
}

// monthGridStart is the first day shown for a month and how many week rows
// cover it.
func monthGridStart(month time.Time, mondayFirst bool) (time.Time, int) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	offset := int(first.Weekday())
	if mondayFirst {
		offset = (offset + 6) % 7
	}
	days := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, -offset), (offset + days + 6) / 7
}

// render draws the calendar into a w x h body.
func (c *taskCalendar) render(a *App, month, day time.Time, w, h int, focused bool) string {
	var lay calLayout
	if c.sub == calMonth {
		lay = c.monthLayout(a, month, w, h)
	} else {
		lay = c.columnLayout(a, day, w, h)
	}
	c.layout = lay
	th := a.theme
	if lay.tooSmall {
		m := calMin[c.sub]
		lines := make([]string, max(h, 0))
		if len(lines) > 0 {
			msg := fmt.Sprintf("Terminal too small: needs %d×%d", m[0], m[1])
			lines[len(lines)/2] = centerLine(th.Muted.Render(msg), w)
		}
		return fitBlock(strings.Join(lines, "\n"), w, h)
	}

	if c.sub != calMonth {
		return c.renderColumns(a, day, lay, w, h, focused)
	}
	if lay.compact {
		return c.renderCompactMonth(a, month, day, lay, w, h, focused)
	}
	weeks := len(lay.boxes) / 7
	gw := lay.items.x
	wnW := lay.boxes[0].rect.x
	colW := lay.boxes[0].rect.w
	rowH := lay.boxes[0].rect.h
	left := make([]string, lay.items.h)
	left[0] = padRight(" "+th.Title.Render(month.Format("January 2006")), gw)
	names := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	if a.prefs.CalendarWeekStart == "sunday" {
		names = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	}
	head := strings.Repeat(" ", wnW)
	for _, n := range names {
		head += padRight(" "+th.Dim.Render(n), colW)
	}
	left[1] = padRight(head, gw)
	today := calendarNow()
	for r := 0; r < weeks; r++ {
		for li := 0; li < rowH; li++ {
			var sb strings.Builder
			if wnW > 0 {
				if li == 0 {
					anchor := lay.boxes[r*7].day
					if a.prefs.CalendarWeekStart == "sunday" {
						anchor = anchor.AddDate(0, 0, 1)
					}
					_, wk := anchor.ISOWeek()
					sb.WriteString(th.Muted.Render(fmt.Sprintf("%2d ", wk)))
				} else {
					sb.WriteString(strings.Repeat(" ", wnW))
				}
			}
			for col := 0; col < 7; col++ {
				box := lay.boxes[r*7+col]
				sb.WriteString(c.boxLine(a, box, li, sameDay(box.day, day), sameDay(box.day, today), box.day.Month() != month.Month(), focused))
			}
			left[2+r*rowH+li] = padRight(sb.String(), gw)
		}
	}
	for i := range left {
		if left[i] == "" {
			left[i] = strings.Repeat(" ", gw)
		}
	}

	panel := c.itemPanel(a, day, lay, focused)
	rows := make([]string, 0, h)
	for i := 0; i < lay.items.h; i++ {
		rows = append(rows, left[i]+panel[i])
	}
	rows = append(rows, c.stripLines(a, lay, w, focused)...)
	return fitBlock(strings.Join(rows, "\n"), w, h)
}

// boxLine is line li of one day box: the date number on top, then one line
// per entry, with "+N more" on the last line when they do not all fit.
func (c *taskCalendar) boxLine(a *App, box calDayBox, li int, selected, today, outside, focused bool) string {
	th := a.theme
	w := box.rect.w
	text := ""
	style := th.Base
	if outside {
		style = th.Muted
	}
	if li == 0 {
		text = fmt.Sprintf(" %d", box.day.Day())
		if today && !selected {
			style = th.Base.Foreground(th.Accent).Bold(true)
		}
	} else {
		entries := c.dayEntries(box.day)
		avail := box.rect.h - 1
		shown := len(entries)
		if shown > avail {
			shown = avail - 1
		}
		switch {
		case li-1 < shown:
			e := entries[li-1]
			lead := " "
			if e.scheduled {
				lead = calScheduledMark
			}
			text = lead + truncateCells(e.task.Content, w-1)
		case li-1 == shown && shown < len(entries):
			text = calMoreLabel(len(entries)-shown, w)
			if !selected {
				style = th.Dim
			}
		}
	}
	if selected {
		if focused && c.focus == calFocusGrid {
			style = th.SelectedFocus
		} else {
			style = th.Selected
		}
	}
	return style.Render(padRight(text, w))
}

// itemPanel lists every entry of the selected day beside the grid.
func (c *taskCalendar) itemPanel(a *App, day time.Time, lay calLayout, focused bool) []string {
	th := a.theme
	pw := lay.items.w
	sep := th.Dim.Render("│")
	if lay.items.x == 0 {
		// The compact month lists the items under the grid, not beside it.
		sep = ""
	}
	sepW := cellWidth(sep)
	entries := c.dayEntries(day)
	out := make([]string, lay.items.h)
	out[0] = padRight(sep+" "+th.Bold.Render(day.Format("Monday, January 2"))+th.Muted.Render(fmt.Sprintf(" (%d)", len(entries))), pw)
	rows := lay.items.h - 1
	start := 0
	if c.cursor >= rows {
		start = c.cursor - rows + 1
	}
	c.layout.itemStart = start
	for i := 1; i < lay.items.h; i++ {
		line := sep
		idx := start + i - 1
		switch {
		case idx < len(entries):
			e := entries[idx]
			lead := " "
			if e.scheduled {
				lead = calScheduledMark
			}
			body := " " + lead + " " + truncateCells(e.task.Content, pw-3-sepW)
			if focused && c.focus == calFocusItems && idx == c.cursor {
				body = th.SelectedFocus.Render(padRight(body, pw-sepW))
			}
			line += body
		case len(entries) == 0 && i == 1:
			line += " " + th.Muted.Render(truncateCells("Nothing here. n adds a task.", pw-1-sepW))
		}
		out[i] = padRight(line, pw)
	}
	return out
}

// stripLines draws the strip of tasks without a date under the grid.
func (c *taskCalendar) stripLines(a *App, lay calLayout, w int, focused bool) []string {
	th := a.theme
	out := make([]string, lay.strip.h)
	out[0] = padRight(" "+th.Bold.Render("No date")+th.Muted.Render(fmt.Sprintf(" (%d)", len(c.undated))), w)
	rows := lay.stripRows
	start := 0
	if c.focus == calFocusStrip && c.strip >= rows {
		start = c.strip - rows + 1
	}
	c.layout.stripStart = start
	for i := 0; i < rows; i++ {
		idx := start + i
		line := ""
		if idx < len(c.undated) {
			line = " " + truncateCells(c.undated[idx].Content, w-2)
			if focused && c.focus == calFocusStrip && idx == c.strip {
				line = th.SelectedFocus.Render(padRight(line, w))
			}
		}
		out[1+i] = padRight(line, w)
	}
	return out
}

// columnEntryAt is the index of the entry shown on line li of box b when its
// column is scrolled by s, or false for the "+N more" lines and empty ones.
func (c *taskCalendar) columnEntryAt(b calDayBox, s, li int) (int, bool) {
	li -= min(s, 1)
	if li < 0 || li >= c.columnShown(b, s) {
		return 0, false
	}
	return s + li, true
}

// pressTask acts on the task a press landed on: a right-click opens its menu
// and a double-click its source note.
func (c *taskCalendar) pressTask(a *App, t vault.Task, right, double bool) {
	switch {
	case right:
		a.taskMenu(t)
	case double:
		a.openTaskSource(t)
	}
}

// click handles a mouse press at body cell (x, y) by hit-testing the last
// layout.
func (c *taskCalendar) click(a *App, v *tasksView, x, y int, right, double bool) {
	lay := c.layout
	if lay.tooSmall {
		return
	}
	if lay.strip.contains(x, y) {
		idx := lay.stripStart + y - lay.strip.y - 1
		if y > lay.strip.y && idx < len(c.undated) {
			c.focus = calFocusStrip
			c.strip = idx
			c.pressTask(a, c.undated[idx], right, double)
		}
		return
	}
	for _, b := range lay.boxes {
		if !b.rect.contains(x, y) {
			continue
		}
		if c.sub != calMonth {
			s := 0
			if sameDay(b.day, v.day) {
				s = c.columnScroll(b)
			}
			if idx, ok := c.columnEntryAt(b, s, y-b.rect.y); ok {
				if !sameDay(b.day, v.day) {
					v.selectDay(a, b.day)
				}
				c.focus = calFocusItems
				c.cursor = idx
				c.pressTask(a, c.dayEntries(b.day)[idx].task, right, double)
				return
			}
		}
		v.selectDay(a, b.day)
		c.focus = calFocusGrid
		if double {
			v.calendarKey(a, vim.KeyEnter)
		}
		return
	}
	if lay.items.contains(x, y) && y > lay.items.y {
		idx := lay.itemStart + y - lay.items.y - 1
		entries := c.dayEntries(v.day)
		if idx < len(entries) {
			c.focus = calFocusItems
			c.cursor = idx
			c.pressTask(a, entries[idx].task, right, double)
		}
	}
}

// selectionCell is the body cell menus open next to.
func (c *taskCalendar) selectionCell(day time.Time) (int, int) {
	lay := c.layout
	switch c.focus {
	case calFocusStrip:
		return lay.strip.x + 2, lay.strip.y + 1 + c.strip - lay.stripStart
	case calFocusItems:
		if c.sub == calMonth {
			return lay.items.x + 3, lay.items.y + 1 + c.cursor - lay.itemStart
		}
		for _, b := range lay.boxes {
			if sameDay(b.day, day) {
				s := c.columnScroll(b)
				return b.rect.x + 2, b.rect.y + c.cursor - s + min(s, 1)
			}
		}
	}
	for _, b := range lay.boxes {
		if sameDay(b.day, day) {
			return b.rect.x + 1, b.rect.y
		}
	}
	return 0, 0
}
