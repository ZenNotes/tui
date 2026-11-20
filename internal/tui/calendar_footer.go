package tui

import (
	"fmt"
	"strings"
	"time"
)

// calFooterMark leads the selected entry when the footer lists several.
const calFooterMark = "▸ "

// footerHeight is the number of lines of the footer in a body of h lines: one
// sixth of the body, from two to five lines.
func footerHeight(h int) int { return clampInt(h/6, 2, 5) }

// footerEntries gives the entries that the footer describes and the index of
// the selected one. With the cursor on a slot of the grid these are the
// entries that cover the slot, in level order. With the focus on the all-day
// entries or on the strip it is the one selected entry. The list is empty on
// an empty slot.
func (c *taskCalendar) footerEntries(day time.Time) ([]calEntry, int) {
	switch c.focus {
	case calFocusItems:
		if entries := c.dayEntries(day); c.cursor < len(entries) {
			return []calEntry{entries[c.cursor]}, 0
		}
	case calFocusStrip:
		if c.strip < len(c.undated) {
			return []calEntry{{task: c.undated[c.strip], kind: calDue}}, 0
		}
	default:
		cover := c.slotEntries(day)
		sel := 0
		if i, ok := c.gridEntry(day); ok {
			key := entryKey(c.dayEntries(day)[i])
			for j, e := range cover {
				if entryKey(e) == key {
					sel = j
				}
			}
		}
		return cover, sel
	}
	return nil, 0
}

// detailItems are the parts of the line below the text of an entry: its dates,
// its repeat rule and its note.
func (e calEntry) detailItems(timeFormat string) []string {
	var items []string
	due := func() {
		if e.task.Due != "" {
			items = append(items, "due "+withTaskClock(e.task.Due, e.task.DueTime, e.task.DueEnd, timeFormat))
		}
	}
	when := formatWhenRange(e.occ.StartDate, e.occ.StartTime, e.occ.EndDate, e.occ.EndTime, timeFormat)
	repeat, until, note := e.repeat, e.until, e.note
	switch e.kind {
	case calDue:
		due()
		repeat, until, note = e.task.Repeat, e.task.Until, e.task.NoteTitle
	case calEvent:
		items = append(items, when)
	case calSession:
		items = append(items, "session "+when)
		if e.task.SourcePath == "" {
			items = append(items, "task not found: "+e.link)
		} else {
			due()
			if repeat == "" {
				repeat, until = e.task.Repeat, e.task.Until
			}
		}
		note = "session in " + e.note
		if e.task.SourcePath != "" && e.task.NoteTitle != "" {
			note = e.task.NoteTitle + " · " + note
		}
	}
	if repeat != "" {
		items = append(items, "repeat "+repeat)
		if until != "" {
			items[len(items)-1] += " until " + until
		}
	}
	if note != "" {
		items = append(items, note)
	}
	return items
}

// wrapItems joins items with " · " into lines of at most w cells. An item is
// never split unless it is wider than a line by itself.
func wrapItems(items []string, w int) []string {
	var out []string
	cur := ""
	for _, it := range items {
		switch {
		case cur == "":
			cur = it
		case cellWidth(cur+" · "+it) <= w:
			cur += " · " + it
		default:
			out = append(out, cur)
			cur = it
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	var cut []string
	for _, l := range out {
		cut = append(cut, wrapCommentText(l, w)...)
	}
	return cut
}

// footerBlock is the lines of one entry in the footer: its text wrapped, then
// its details. When max lines are not enough, the text gives up its last
// lines and the last line that stays ends with an ellipsis.
func footerBlock(a *App, e calEntry, w, max int) (text, detail []string) {
	text = wrapCommentText(e.content(), w)
	detail = wrapItems(e.detailItems(a.prefs.TimeFormat), w)
	if len(detail) >= max {
		detail = detail[:max-1]
	}
	if room := max - len(detail); len(text) > room {
		text = text[:room]
		text[room-1] = truncateCells(strings.TrimRight(text[room-1], " ")+" …", w)
	}
	return text, detail
}

// footerLines draws the footer: h lines of w cells under the grid. Each entry
// shows its whole text, wrapped, and a line of its dates, its repeat rule and
// its note. When several entries cover the slot, the selected one has a mark.
// When they do not all fit, the last line counts the rest.
func (c *taskCalendar) footerLines(a *App, day time.Time, w, h int) []string {
	th := a.theme
	out := make([]string, 0, h)
	entries, sel := c.footerEntries(day)
	multi := len(entries) > 1
	room := w - 2
	type block struct{ text, detail []string }
	blocks := make([]block, len(entries))
	size := 0
	for i, e := range entries {
		t, d := footerBlock(a, e, room, h)
		blocks[i] = block{t, d}
		size += len(t) + len(d)
	}
	first, last := 0, len(blocks)
	avail := h
	if size > h {
		// Keep one line for the count of the entries that do not show.
		avail = h - 1
		rest := 0
		for i := 0; i <= sel; i++ {
			rest += len(blocks[i].text) + len(blocks[i].detail)
		}
		if rest > avail {
			first = sel
		}
		used := 0
		last = first
		for last < len(blocks) && used+len(blocks[last].text)+len(blocks[last].detail) <= avail {
			used += len(blocks[last].text) + len(blocks[last].detail)
			last++
		}
		if last == first {
			t, d := footerBlock(a, entries[first], room, avail)
			blocks[first] = block{t, d}
			last = first + 1
		}
	}
	for i := first; i < last; i++ {
		lead := " "
		if multi {
			lead = "  "
			if i == sel {
				lead = calFooterMark
			}
		}
		for j, l := range blocks[i].text {
			if j > 0 {
				lead = strings.Repeat(" ", cellWidth(lead))
			}
			line := lead + l
			switch {
			case entries[i].done:
				line = th.Done.Render(line)
			case multi && i == sel:
				line = th.Bold.Render(line)
			}
			out = append(out, padRight(line, w))
		}
		for _, l := range blocks[i].detail {
			out = append(out, padRight(th.Muted.Render(strings.Repeat(" ", cellWidth(lead))+l), w))
		}
	}
	if hidden := len(blocks) - (last - first); hidden > 0 {
		out = append(out, padRight(th.Dim.Render(" "+fmt.Sprintf("+%d more · Enter lists them", hidden)), w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out[:h]
}
