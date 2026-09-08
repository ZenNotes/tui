package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/zennotescli/internal/periodic"
	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// tasksView is the Tasks tab: list, board and calendar modes.
type tasksView struct {
	mode   string
	filter string
	tasks  []vault.Task

	list listCursor
	rows []taskRow

	groupBy string
	columns []kanbanColumn
	col     int
	card    int

	month     time.Time
	day       time.Time
	dayTasks  []vault.Task
	dayCursor int
	dayFocus  bool
	byDay     map[string][]vault.Task
	pageRows  int

	boardStarts []int
	calFirst    time.Time
	calWeeks    int
}

type taskRow struct {
	header string
	task   *vault.Task
}

type kanbanColumn struct {
	id    string
	title string
	cards []vault.Task
}

func newTasksView(a *App) *tasksView {
	mode := a.prefs.TasksViewMode
	if mode == "" {
		mode = "list"
	}
	if mode == "board" {
		mode = "kanban"
	}
	now := time.Now()
	v := &tasksView{mode: mode, groupBy: a.prefs.KanbanGroupBy, month: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local), day: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)}
	if v.groupBy == "" {
		v.groupBy = "status"
	}
	v.refresh(a)
	return v
}

func (v *tasksView) title() string { return "Tasks" }

func (v *tasksView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *tasksView) setMode(mode string) {
	switch mode {
	case "board", "kanban":
		v.mode = "kanban"
	case "calendar":
		v.mode = "calendar"
	default:
		v.mode = "list"
	}
}

// refresh recomputes rows, columns and calendar buckets.
func (v *tasksView) refresh(a *App) {
	all := a.displayTasks()
	v.tasks = filterTasks(all, v.filter)
	groups := vault.GroupTasks(v.tasks, time.Now())
	v.rows = nil
	add := func(name string, tasks []vault.Task, extra string) {
		if len(tasks) == 0 {
			return
		}
		header := fmt.Sprintf("%s (%d)", name, len(tasks))
		if extra != "" {
			header += " " + extra
		}
		v.rows = append(v.rows, taskRow{header: header})
		for i := range tasks {
			t := tasks[i]
			v.rows = append(v.rows, taskRow{task: &t})
		}
	}
	overdue := ""
	if groups.OverdueCount > 0 {
		overdue = fmt.Sprintf("· %d overdue", groups.OverdueCount)
	}
	add("Today", groups.Today, overdue)
	add("Upcoming", groups.Upcoming, "")
	add("Waiting", groups.Waiting, "")
	add("Done", groups.Done, "")
	add("Forwarded", groups.Forwarded, "")
	add("Cancelled", groups.Cancelled, "")
	v.list.clamp(len(v.rows))
	if v.list.cursor < len(v.rows) && v.rows[v.list.cursor].header != "" && len(v.rows) > 1 {
		v.list.cursor++
	}
	v.buildColumns(a)
	v.byDay = vault.BucketTasksByDueDate(v.tasks)
	v.dayTasks = v.byDay[v.day.Format("2006-01-02")]
	if v.dayCursor >= len(v.dayTasks) {
		v.dayCursor = max(0, len(v.dayTasks)-1)
	}
}

// filterTasks applies the cross-view filter: free words match content and
// note title, `key:value` terms match tag, note, due, priority, status,
// folder or any custom field.
func filterTasks(tasks []vault.Task, filter string) []vault.Task {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(filter)))
	if len(terms) == 0 {
		return tasks
	}
	out := []vault.Task{}
	for _, t := range tasks {
		ok := true
		for _, term := range terms {
			if !taskMatchesTerm(t, term) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, t)
		}
	}
	return out
}

func taskMatchesTerm(t vault.Task, term string) bool {
	negate := false
	if strings.HasPrefix(term, "-") && len(term) > 1 {
		negate = true
		term = term[1:]
	}
	match := false
	if key, val, ok := strings.Cut(term, ":"); ok && val != "" {
		switch key {
		case "tag", "#":
			for _, tag := range t.Tags {
				if strings.Contains(strings.ToLower(tag), val) {
					match = true
				}
			}
		case "note", "in":
			match = strings.Contains(strings.ToLower(t.NoteTitle), val)
		case "due":
			switch val {
			case "today":
				match = t.Due == vault.TodayISO(time.Now())
			case "overdue":
				match = vault.IsOverdue(t, time.Now())
			case "none":
				match = t.Due == ""
			case "any":
				match = t.Due != ""
			default:
				match = strings.HasPrefix(t.Due, val)
			}
		case "priority", "prio", "p":
			match = t.Priority == val || (val == "none" && t.Priority == "")
		case "status":
			match = strings.EqualFold(t.Status, val) || strings.EqualFold(t.Fields["status"], val)
		case "folder":
			match = strings.EqualFold(string(t.NoteFolder), val)
		case "is":
			switch val {
			case "done", "checked":
				match = t.Checked
			case "open":
				match = vault.IsTaskOpen(t)
			case "waiting":
				match = t.Waiting
			case "progress", "in-progress":
				match = t.InProgress
			case "cancelled":
				match = t.Cancelled
			}
		default:
			match = strings.EqualFold(t.Fields[key], val)
		}
	} else if strings.HasPrefix(term, "#") {
		for _, tag := range t.Tags {
			if strings.Contains(strings.ToLower(tag), term[1:]) {
				match = true
			}
		}
	} else {
		match = strings.Contains(strings.ToLower(t.Content), term) || strings.Contains(strings.ToLower(t.NoteTitle), term)
	}
	if negate {
		return !match
	}
	return match
}

// --- board ---

func (v *tasksView) groupOptions() []string {
	opts := []string{"status", "priority", "due"}
	seen := map[string]bool{"status": true, "priority": true, "due": true}
	for _, t := range v.tasks {
		for k := range t.Fields {
			if !seen[k] {
				seen[k] = true
				opts = append(opts, k)
			}
		}
	}
	sort.Strings(opts[3:])
	return opts
}

func (v *tasksView) columnTitle(a *App, id, fallback string) string {
	if custom := a.prefs.KanbanColumnTitles[id]; strings.TrimSpace(custom) != "" {
		return custom
	}
	return fallback
}

func (v *tasksView) buildColumns(a *App) {
	cols := []kanbanColumn{}
	place := func(id string, t vault.Task) {
		for i := range cols {
			if cols[i].id == id {
				cols[i].cards = append(cols[i].cards, t)
				return
			}
		}
	}
	switch v.groupBy {
	case "priority":
		cols = []kanbanColumn{{id: "high", title: v.columnTitle(a, "high", "High")}, {id: "med", title: v.columnTitle(a, "med", "Medium")}, {id: "low", title: v.columnTitle(a, "low", "Low")}, {id: "none", title: v.columnTitle(a, "none", "No priority")}}
		for _, t := range v.tasks {
			if t.Checked || t.Cancelled || t.Forwarded {
				continue
			}
			id := t.Priority
			if id == "" {
				id = "none"
			}
			place(id, t)
		}
	case "due":
		cols = []kanbanColumn{{id: "overdue", title: "Overdue"}, {id: "today", title: "Today"}, {id: "tomorrow", title: "Tomorrow"}, {id: "week", title: "This week"}, {id: "later", title: "Later"}, {id: "none", title: "No date"}}
		today := time.Now()
		todayISO := vault.TodayISO(today)
		tomorrowISO := vault.TodayISO(today.AddDate(0, 0, 1))
		weekISO := vault.TodayISO(today.AddDate(0, 0, 7))
		for _, t := range v.tasks {
			if t.Checked || t.Cancelled || t.Forwarded {
				continue
			}
			switch {
			case t.Due == "":
				place("none", t)
			case t.Due < todayISO:
				place("overdue", t)
			case t.Due == todayISO:
				place("today", t)
			case t.Due == tomorrowISO:
				place("tomorrow", t)
			case t.Due <= weekISO:
				place("week", t)
			default:
				place("later", t)
			}
		}
	case "status", "":
		cols = []kanbanColumn{{id: "todo", title: v.columnTitle(a, "todo", "To Do")}, {id: "in-progress", title: v.columnTitle(a, "in-progress", "In Progress")}, {id: "done", title: v.columnTitle(a, "done", "Done")}}
		custom := []string{}
		seen := map[string]bool{"todo": true, "in-progress": true, "done": true}
		for _, s := range a.prefs.KanbanStatuses {
			s = strings.ToLower(strings.TrimSpace(s))
			if s != "" && !seen[s] {
				seen[s] = true
				custom = append(custom, s)
			}
		}
		for _, t := range v.tasks {
			s := strings.ToLower(strings.TrimSpace(t.Fields["status"]))
			if s != "" && !seen[s] {
				seen[s] = true
				custom = append(custom, s)
			}
		}
		for _, s := range custom {
			cols = append(cols[:len(cols)-1], kanbanColumn{id: s, title: v.columnTitle(a, s, strings.Title(strings.ReplaceAll(s, "-", " ")))}, cols[len(cols)-1])
		}
		for _, t := range v.tasks {
			if t.Cancelled || t.Forwarded {
				continue
			}
			s := strings.ToLower(strings.TrimSpace(t.Fields["status"]))
			switch {
			case t.Checked:
				place("done", t)
			case s != "" && s != "todo" && s != "in-progress" && s != "done":
				place(s, t)
			case t.InProgress || s == "in-progress":
				place("in-progress", t)
			default:
				place("todo", t)
			}
		}
	default:
		key := v.groupBy
		values := []string{}
		seen := map[string]bool{}
		for _, t := range v.tasks {
			val := strings.ToLower(strings.TrimSpace(t.Fields[key]))
			if val != "" && !seen[val] {
				seen[val] = true
				values = append(values, val)
			}
		}
		sort.Strings(values)
		for _, val := range values {
			cols = append(cols, kanbanColumn{id: val, title: v.columnTitle(a, val, val)})
		}
		cols = append(cols, kanbanColumn{id: "none", title: "No " + key})
		for _, t := range v.tasks {
			if t.Checked || t.Cancelled || t.Forwarded {
				continue
			}
			val := strings.ToLower(strings.TrimSpace(t.Fields[key]))
			if val == "" {
				val = "none"
			}
			place(val, t)
		}
	}
	v.columns = cols
	if v.col >= len(cols) {
		v.col = max(0, len(cols)-1)
	}
	if len(cols) > 0 && v.card >= len(cols[v.col].cards) {
		v.card = max(0, len(cols[v.col].cards)-1)
	}
}

// --- rendering ---

func (v *tasksView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	// The frame names the view; this line is its toolbar: the mode, then
	// whatever narrows the list.
	detail := ""
	if v.filter != "" {
		detail += "  ·  filter: " + v.filter
	}
	if a.prefs.ShowArchivedTasks {
		detail += "  ·  +archived"
	}
	header := " " + th.Bold.Render(v.mode) + th.Muted.Render(detail)
	lines := []string{padRight(header, w)}
	body := ""
	switch v.mode {
	case "kanban":
		body = v.renderBoard(a, w, h-1, focused)
	case "calendar":
		body = v.renderCalendar(a, w, h-1, focused)
	default:
		body = v.renderList(a, w, h-1, focused)
	}
	lines = append(lines, strings.Split(fitBlock(body, w, h-1), "\n")...)
	return strings.Join(lines, "\n")
}

func (v *tasksView) taskLine(a *App, t vault.Task, w int, selected bool) string {
	th := a.theme
	glyph := "☐"
	textStyle := th.Base
	switch {
	case t.Cancelled:
		glyph = "✕"
		textStyle = th.Done
	case t.Forwarded:
		glyph = "→"
		textStyle = th.Dim
	case t.Checked:
		glyph = "☑"
		textStyle = th.Done
	case t.InProgress:
		glyph = "◩"
	}
	if t.Kind == "file" {
		glyph = "▣"
		if t.Checked {
			glyph = "▣"
		}
	}
	meta := []string{}
	if t.Due != "" {
		due := "due " + t.Due
		if vault.IsOverdue(t, time.Now()) {
			meta = append(meta, th.Overdue.Render(due))
		} else {
			meta = append(meta, th.Dim.Render(due))
		}
	}
	if t.Priority != "" {
		meta = append(meta, lipgloss.NewStyle().Foreground(th.Purple).Render("!"+t.Priority))
	}
	if t.Waiting {
		meta = append(meta, th.Dim.Render("@waiting"))
	}
	metaText := strings.Join(meta, " ")
	note := th.Muted.Render("· " + t.NoteTitle)
	content := t.Content
	if t.Kind == "file" {
		content = t.NoteTitle
		note = th.Muted.Render("· task note")
	}
	avail := w - 4 - lipgloss.Width(metaText) - lipgloss.Width(note) - 2
	text := truncateCells(content, max(8, avail))
	row := " " + th.Checkbox.Render(glyph) + " " + textStyle.Render(text)
	if metaText != "" {
		row += " " + metaText
	}
	row += " " + note
	if selected {
		plain := " " + glyph + " " + text + " " + stripAnsi(metaText) + " " + stripAnsi(note)
		return th.SelectedFocus.Render(padRight(plain, w))
	}
	return padRight(row, w)
}

func (v *tasksView) renderList(a *App, w, h int, focused bool) string {
	th := a.theme
	if len(v.rows) == 0 {
		msg := "No tasks. Add `- [ ] something` to a note, or press n."
		if v.filter != "" {
			msg = "No tasks match the filter. Press f to change it, F to clear."
		}
		return fitBlock("\n "+th.Muted.Render(msg), w, h)
	}
	v.pageRows = h
	v.list.ensureVisible(h)
	lines := []string{}
	for i := v.list.scroll; i < len(v.rows) && len(lines) < h; i++ {
		r := v.rows[i]
		if r.header != "" {
			title, detail, _ := strings.Cut(r.header, " · ")
			lines = append(lines, padRight(" "+sectionHeader(th, title, detail, w-2), w))
			continue
		}
		lines = append(lines, v.taskLine(a, *r.task, w, focused && i == v.list.cursor))
	}
	return strings.Join(lines, "\n")
}

func (v *tasksView) renderBoard(a *App, w, h int, focused bool) string {
	th := a.theme
	if len(v.columns) == 0 {
		return fitBlock(" "+th.Muted.Render("No columns"), w, h)
	}
	colW := max(12, (w-len(v.columns)+1)/len(v.columns))
	blocks := []string{}
	v.boardStarts = make([]int, len(v.columns))
	for ci, col := range v.columns {
		lines := []string{}
		title := fmt.Sprintf(" %s", col.title)
		style := th.Bold
		if ci == v.col && focused {
			style = th.Bold.Foreground(th.Accent)
		}
		countText := fmt.Sprint(len(col.cards))
		head := style.Render(truncateCells(title, colW-len(countText)-2))
		head += strings.Repeat(" ", max(1, colW-lipgloss.Width(head)-len(countText)-1)) + th.Muted.Render(countText) + " "
		lines = append(lines, padRight(head, colW))
		if ci == v.col && focused {
			lines = append(lines, th.BorderFocus.Render(strings.Repeat("─", colW)))
		} else {
			lines = append(lines, th.Border.Render(strings.Repeat("─", colW)))
		}
		cardRows := h - 2
		start := 0
		if ci == v.col && v.card*3 >= cardRows {
			start = v.card - cardRows/3 + 1
		}
		v.boardStarts[ci] = start
		for i := start; i < len(col.cards) && len(lines) < h; i++ {
			c := col.cards[i]
			selected := focused && ci == v.col && i == v.card
			text := c.Content
			if c.Kind == "file" {
				text = c.NoteTitle
			}
			first := truncateCells(" "+text, colW)
			meta := []string{}
			if c.Due != "" {
				if vault.IsOverdue(c, time.Now()) {
					meta = append(meta, th.Overdue.Render(c.Due))
				} else {
					meta = append(meta, th.Dim.Render(c.Due))
				}
			}
			if c.Priority != "" {
				meta = append(meta, lipgloss.NewStyle().Foreground(th.Purple).Render("!"+c.Priority))
			}
			if c.Waiting {
				meta = append(meta, th.Dim.Render("waiting"))
			}
			meta = append(meta, th.Muted.Render(truncateCells(c.NoteTitle, max(4, colW-2-lipgloss.Width(strings.Join(meta, " "))))))
			second := " " + strings.Join(meta, " ")
			if selected {
				bar := th.BorderFocus.Render("▍")
				lines = append(lines, bar+th.SelectedFocus.Render(padRight(strings.TrimPrefix(first, " "), colW-1)), bar+th.Selected.Render(padRight(stripAnsi(strings.Join(meta, " ")), colW-1)))
			} else {
				lines = append(lines, padRight(th.Base.Render(first), colW), padRight(second, colW))
			}
			if len(lines) < h {
				lines = append(lines, strings.Repeat(" ", colW))
			}
		}
		blocks = append(blocks, fitBlock(strings.Join(lines, "\n"), colW, h))
		if ci < len(v.columns)-1 {
			blocks = append(blocks, a.renderVerticalBorder(h, false))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

func (v *tasksView) renderCalendar(a *App, w, h int, focused bool) string {
	th := a.theme
	weekStartMonday := a.prefs.CalendarWeekStart != "sunday"
	grid, first, weeks := renderMonthGrid(a, v.month, v.day, weekStartMonday, a.prefs.CalendarShowWeekNumbers, func(day time.Time) (string, lipgloss.Style) {
		tasks := v.byDay[day.Format("2006-01-02")]
		open := 0
		for _, t := range tasks {
			if vault.IsTaskOpen(t) {
				open++
			}
		}
		if open == 0 {
			return "", th.Base
		}
		return fmt.Sprint(open), th.Marker
	}, focused && !v.dayFocus)
	v.calFirst, v.calWeeks = first, weeks
	gridLines := strings.Split(grid, "\n")
	lines := []string{}
	for _, gl := range gridLines {
		lines = append(lines, padRight(" "+gl, w))
	}
	lines = append(lines, padRight(th.Bold.Render(" "+v.day.Format("Monday, January 2")+fmt.Sprintf("  (%d tasks)", len(v.dayTasks))), w))
	listH := h - len(lines)
	if len(v.dayTasks) == 0 {
		lines = append(lines, padRight(" "+th.Muted.Render("No tasks due this day. n adds one to the daily note."), w))
	}
	for i := 0; i < len(v.dayTasks) && i < listH; i++ {
		lines = append(lines, v.taskLine(a, v.dayTasks[i], w, focused && v.dayFocus && i == v.dayCursor))
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

// renderMonthGrid draws a month with a marker per day.
func renderMonthGrid(a *App, month, selected time.Time, mondayFirst, weekNumbers bool, mark func(day time.Time) (string, lipgloss.Style), focused bool) (string, time.Time, int) {
	th := a.theme
	cellW := 5
	lines := []string{th.Title.Render(month.Format("January 2006"))}
	names := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	if !mondayFirst {
		names = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	}
	head := ""
	if weekNumbers {
		head += "    "
	}
	for _, n := range names {
		head += padRight(th.Dim.Render(n), cellW)
	}
	lines = append(lines, head)
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	offset := int(first.Weekday())
	if mondayFirst {
		offset = (offset + 6) % 7
	}
	day := first.AddDate(0, 0, -offset)
	gridFirst := day
	today := time.Now()
	weeks := 0
	for row := 0; row < 6; row++ {
		line := ""
		if weekNumbers {
			_, wk := day.ISOWeek()
			line += th.Muted.Render(fmt.Sprintf("%2d  ", wk))
		}
		for c := 0; c < 7; c++ {
			marker, mstyle := mark(day)
			num := fmt.Sprintf("%2d", day.Day())
			style := th.Base
			if day.Month() != month.Month() {
				style = th.Muted
			}
			if sameDay(day, today) {
				style = style.Foreground(th.Accent).Bold(true)
			}
			cell := style.Render(num) + " "
			if marker != "" {
				cell += mstyle.Render(truncateCells(marker, 2))
			}
			if sameDay(day, selected) {
				if focused {
					cell = th.SelectedFocus.Render(padRight(num+" "+marker, cellW-1))
				} else {
					cell = th.Selected.Render(padRight(num+" "+marker, cellW-1))
				}
			}
			line += padRight(cell, cellW)
			day = day.AddDate(0, 0, 1)
		}
		lines = append(lines, line)
		weeks++
		if day.Month() != month.Month() && row >= 3 && day.Day() > 7 {
			break
		}
	}
	return strings.Join(lines, "\n"), gridFirst, weeks
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// --- keys ---

func (v *tasksView) handleKey(a *App, k vim.Key) bool {
	if k.IsRune('v') && a.listKeyAllowed(k) {
		switch v.mode {
		case "list":
			v.mode = "kanban"
		case "kanban":
			v.mode = "calendar"
		default:
			v.mode = "list"
		}
		v.refresh(a)
		return true
	}
	if k.IsRune('f') && a.listKeyAllowed(k) {
		a.promptFor("Filter tasks", v.filter, "words, #tag, tag:x, note:x, due:today|overdue|none, priority:high, status:x, is:open|done|waiting, -term", func(a *App, text string) {
			v.filter = strings.TrimSpace(text)
			v.refresh(a)
		})
		return true
	}
	if k.IsRune('F') && a.listKeyAllowed(k) {
		v.filter = ""
		v.refresh(a)
		return true
	}
	if k.IsRune('A') && a.listKeyAllowed(k) {
		a.prefs.ShowArchivedTasks = !a.prefs.ShowArchivedTasks
		v.refresh(a)
		return true
	}
	if k.IsRune('n') && a.listKeyAllowed(k) {
		day := time.Now()
		if v.mode == "calendar" {
			day = v.day
		}
		a.promptFor("New task for "+day.Format("2006-01-02"), "", "Task text; add due:YYYY-MM-DD, !high, #tag as needed", func(a *App, text string) {
			if strings.TrimSpace(text) != "" {
				a.addTaskToDaily(text, day)
			}
		})
		return true
	}
	if k.IsRune('?') && a.listKeyAllowed(k) {
		a.openKeyHelp()
		return true
	}
	switch v.mode {
	case "kanban":
		return v.boardKey(a, k)
	case "calendar":
		return v.calendarKey(a, k)
	}
	return v.listKey(a, k)
}

func (v *tasksView) selectedTask() *vault.Task {
	switch v.mode {
	case "kanban":
		if v.col < len(v.columns) && v.card < len(v.columns[v.col].cards) {
			t := v.columns[v.col].cards[v.card]
			return &t
		}
		return nil
	case "calendar":
		if v.dayFocus && v.dayCursor < len(v.dayTasks) {
			t := v.dayTasks[v.dayCursor]
			return &t
		}
		return nil
	}
	if v.list.cursor < len(v.rows) {
		return v.rows[v.list.cursor].task
	}
	return nil
}

func (v *tasksView) listKey(a *App, k vim.Key) bool {
	n := len(v.rows)
	before := v.list.cursor
	if a.listNav(&v.list, k, n, v.pageRows) {
		// Skip headers in the movement direction.
		dir := 1
		if v.list.cursor < before {
			dir = -1
		}
		for v.list.cursor < n && v.list.cursor >= 0 && v.rows[v.list.cursor].header != "" {
			next := v.list.cursor + dir
			if next < 0 || next >= n {
				dir = -dir
				next = v.list.cursor + dir
				if next < 0 || next >= n {
					break
				}
			}
			v.list.cursor = next
		}
		return true
	}
	t := v.selectedTask()
	if k.Is("enter") {
		if t != nil {
			a.openTaskSource(*t)
		}
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.toggleTask", "nav.openResult", "tasks.moveTaskUp", "tasks.moveTaskDown", "nav.localEx", "nav.contextMenu", "nav.filter")
	if pending {
		return true
	}
	switch id {
	case "nav.toggleTask":
		if t != nil {
			a.toggleTask(*t)
		}
	case "nav.openResult":
		if t != nil {
			a.openTaskSource(*t)
		}
	case "tasks.moveTaskUp":
		if t != nil {
			a.moveTaskWithinNote(*t, -1)
		}
	case "tasks.moveTaskDown":
		if t != nil {
			a.moveTaskWithinNote(*t, 1)
		}
	case "nav.localEx":
		a.openLocalEx()
	case "nav.contextMenu":
		if t != nil {
			a.taskMenu(*t)
		}
	case "nav.filter":
		a.promptFor("Filter tasks", v.filter, "", func(a *App, text string) {
			v.filter = strings.TrimSpace(text)
			v.refresh(a)
		})
	default:
		return v.taskEditKey(a, k, t)
	}
	return true
}

// taskEditKey handles the per-task edit keys shared by every mode.
func (v *tasksView) taskEditKey(a *App, k vim.Key, t *vault.Task) bool {
	if t == nil {
		return false
	}
	switch {
	case k.IsRune('p'):
		a.taskPriorityMenu(*t)
	case k.IsRune('d'):
		a.taskDuePrompt(*t)
	case k.IsRune('w'):
		a.setTaskWaiting(*t, !t.Waiting)
	case k.IsRune('c'):
		a.setTaskCancelled(*t, !t.Cancelled)
	case k.IsRune('/'):
		a.setTaskInProgress(*t, !t.InProgress)
	case k.IsRune('e'):
		a.taskTextPrompt(*t)
	case k.IsRune('s'):
		a.taskFieldPrompt(*t)
	case k.IsRune('>'):
		a.shiftTaskDue(*t, 1)
	case k.IsRune('<'):
		a.shiftTaskDue(*t, -1)
	case k.IsRune('y'):
		a.copyText(t.RawText)
	default:
		return false
	}
	return true
}

func (v *tasksView) boardKey(a *App, k vim.Key) bool {
	if !a.listKeyAllowed(k) && !k.Is("enter") && !k.Is("left") && !k.Is("right") && !k.Is("up") && !k.Is("down") {
		return false
	}
	t := v.selectedTask()
	switch {
	case k.IsRune('h') || k.Is("left"):
		if v.col > 0 {
			v.col--
			v.card = 0
		}
	case k.IsRune('l') || k.Is("right"):
		if v.col < len(v.columns)-1 {
			v.col++
			v.card = 0
		}
	case k.IsRune('j') || k.Is("down"):
		if v.col < len(v.columns) && v.card < len(v.columns[v.col].cards)-1 {
			v.card++
		}
	case k.IsRune('k') || k.Is("up"):
		if v.card > 0 {
			v.card--
		}
	case k.IsRune('H') || (k.Is("left") && k.Shift):
		if t != nil && v.col > 0 {
			v.moveCard(a, *t, v.col-1)
		}
	case k.IsRune('L') || (k.Is("right") && k.Shift):
		if t != nil && v.col < len(v.columns)-1 {
			v.moveCard(a, *t, v.col+1)
		}
	case k.IsRune('g'):
		opts := v.groupOptions()
		for i, o := range opts {
			if o == v.groupBy {
				v.groupBy = opts[(i+1)%len(opts)]
				v.refresh(a)
				a.notify("Board grouped by " + v.groupBy)
				return true
			}
		}
		v.groupBy = "status"
		v.refresh(a)
	case k.IsRune('G'):
		v.card = max(0, len(v.columns[v.col].cards)-1)
	case k.Is("enter") || k.IsRune('o'):
		if t != nil {
			a.openTaskSource(*t)
		}
	case k.IsRune('x'):
		if t != nil {
			a.toggleTask(*t)
		}
	case k.IsRune('m'):
		if t != nil {
			a.taskMenu(*t)
		}
	case k.IsRune(':'):
		a.openLocalEx()
	default:
		return v.taskEditKey(a, k, t)
	}
	return true
}

// moveCard rewrites a task so it lands in another column.
func (v *tasksView) moveCard(a *App, t vault.Task, target int) {
	col := v.columns[target]
	switch v.groupBy {
	case "priority":
		p := col.id
		if p == "none" {
			p = ""
		}
		a.setTaskPriority(t, p)
	case "due":
		today := time.Now()
		switch col.id {
		case "today":
			a.setTaskDue(t, vault.TodayISO(today))
		case "tomorrow":
			a.setTaskDue(t, vault.TodayISO(today.AddDate(0, 0, 1)))
		case "week":
			a.setTaskDue(t, vault.TodayISO(today.AddDate(0, 0, 7)))
		case "none":
			a.setTaskDue(t, "")
		default:
			a.notify("Set an explicit date with d to move a task there")
			return
		}
	case "status", "":
		switch col.id {
		case "todo":
			a.setTaskStatusColumn(t, "todo")
		case "in-progress":
			a.setTaskStatusColumn(t, "in-progress")
		case "done":
			a.setTaskStatusColumn(t, "done")
		default:
			a.setTaskStatusColumn(t, col.id)
		}
	default:
		val := col.id
		if val == "none" {
			val = ""
		}
		a.setTaskField(t, v.groupBy, val)
	}
	v.col = target
}

func (v *tasksView) calendarKey(a *App, k vim.Key) bool {
	t := v.selectedTask()
	if v.dayFocus {
		switch {
		case k.Is("esc") || k.IsRune('h') || k.Is("left"):
			v.dayFocus = false
		case k.IsRune('j') || k.Is("down"):
			if v.dayCursor < len(v.dayTasks)-1 {
				v.dayCursor++
			}
		case k.IsRune('k') || k.Is("up"):
			if v.dayCursor > 0 {
				v.dayCursor--
			}
		case k.Is("enter") || k.IsRune('o'):
			if t != nil {
				a.openTaskSource(*t)
			}
		case k.IsRune('x'):
			if t != nil {
				a.toggleTask(*t)
			}
		case k.IsRune('m'):
			if t != nil {
				a.taskMenu(*t)
			}
		default:
			return v.taskEditKey(a, k, t)
		}
		return true
	}
	if !a.listKeyAllowed(k) && !k.Is("enter") && !k.Is("left") && !k.Is("right") && !k.Is("up") && !k.Is("down") {
		return false
	}
	switch {
	case k.IsRune('h') || k.Is("left"):
		v.selectDay(a, v.day.AddDate(0, 0, -1))
	case k.IsRune('l') || k.Is("right"):
		v.selectDay(a, v.day.AddDate(0, 0, 1))
	case k.IsRune('j') || k.Is("down"):
		v.selectDay(a, v.day.AddDate(0, 0, 7))
	case k.IsRune('k') || k.Is("up"):
		v.selectDay(a, v.day.AddDate(0, 0, -7))
	case k.IsRune('[') || k.IsRune('H'):
		v.month = v.month.AddDate(0, -1, 0)
		v.selectDay(a, v.day.AddDate(0, -1, 0))
	case k.IsRune(']') || k.IsRune('L'):
		v.month = v.month.AddDate(0, 1, 0)
		v.selectDay(a, v.day.AddDate(0, 1, 0))
	case k.IsRune('t') || k.IsRune('T'):
		now := time.Now()
		v.month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		v.selectDay(a, now)
	case k.Is("enter"):
		if len(v.dayTasks) > 0 {
			v.dayFocus = true
			v.dayCursor = 0
		} else {
			a.openPeriodicOn(periodic.Daily, v.day)
		}
	case k.IsRune('o'):
		a.openPeriodicOn(periodic.Daily, v.day)
	case k.IsRune(':'):
		a.openLocalEx()
	default:
		return false
	}
	return true
}

func (v *tasksView) selectDay(a *App, day time.Time) {
	v.day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	if v.day.Month() != v.month.Month() || v.day.Year() != v.month.Year() {
		v.month = time.Date(v.day.Year(), v.day.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	v.dayTasks = v.byDay[v.day.Format("2006-01-02")]
	v.dayCursor = 0
}

func (v *tasksView) hintTargets(a *App, p *pane) []hintTarget {
	out := []hintTarget{}
	if v.mode != "list" {
		return nil
	}
	y0 := a.contentRect(p).y + 1
	for i := v.list.scroll; i < len(v.rows) && i-v.list.scroll < v.pageRows; i++ {
		if v.rows[i].task == nil {
			continue
		}
		idx := i
		out = append(out, hintTarget{x: a.contentRect(p).x + 1, y: y0 + (i - v.list.scroll), run: func(a *App) {
			v.list.cursor = idx
			a.openTaskSource(*v.rows[idx].task)
		}})
	}
	return out
}

// --- task mutations ---

func (a *App) afterTaskChange() {
	a.queue(func() tea.Msg { return a.loadTasksCmd()() })
	a.refreshIndex()
}

func (a *App) openTaskSource(t vault.Task) {
	a.openNote(t.SourcePath, true)
	if buf := a.buffers[t.SourcePath]; buf != nil && t.Kind != "file" {
		buf.ed.GotoLine(t.LineNumber)
	}
}

func (a *App) toggleTask(t vault.Task) {
	err := a.mutateNote(t.SourcePath, func(body string) (string, bool) {
		if t.Kind == "file" {
			return vault.ToggleFileTaskInBody(body, t.Checked, time.Now()), true
		}
		return vault.ToggleTaskInBody(body, t.TaskIndex, vault.DialectApp)
	})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.afterTaskChange()
}

func (a *App) editTaskLine(t vault.Task, edit func(body string) string) {
	err := a.mutateNote(t.SourcePath, func(body string) (string, bool) {
		next := edit(body)
		return next, next != body
	})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	a.afterTaskChange()
}

func (a *App) setTaskPriority(t vault.Task, p string) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			return vault.SetTaskFileField(body, "priority", vault.TaskFilePriorityValue(p))
		}
		return vault.SetTaskPriority(body, t.TaskIndex, p)
	})
}

func (a *App) setTaskDue(t vault.Task, due string) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			return vault.SetTaskFileField(body, "due", due)
		}
		return vault.SetTaskDue(body, t.TaskIndex, due)
	})
}

func (a *App) shiftTaskDue(t vault.Task, days int) {
	base := time.Now()
	if t.Due != "" {
		if d, err := time.ParseInLocation("2006-01-02", t.Due, time.Local); err == nil {
			base = d
		}
	}
	a.setTaskDue(t, vault.TodayISO(base.AddDate(0, 0, days)))
}

func (a *App) setTaskWaiting(t vault.Task, waiting bool) {
	if t.Kind == "file" {
		a.notify("Task notes carry their status in frontmatter; edit the note")
		return
	}
	a.editTaskLine(t, func(body string) string { return vault.SetTaskWaiting(body, t.TaskIndex, waiting) })
}

func (a *App) setTaskCancelled(t vault.Task, cancelled bool) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			return vault.SetTaskFileCancelled(body, cancelled)
		}
		return vault.SetTaskCancelled(body, t.TaskIndex, cancelled)
	})
}

func (a *App) setTaskInProgress(t vault.Task, inProgress bool) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			return vault.SetTaskFileInProgress(body, inProgress)
		}
		return vault.SetTaskInProgress(body, t.TaskIndex, inProgress)
	})
}

func (a *App) setTaskField(t vault.Task, key, value string) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			return vault.SetTaskFileField(body, key, value)
		}
		return vault.SetTaskField(body, t.TaskIndex, key, value)
	})
}

// setTaskStatusColumn applies a board column: the three built-in states map
// to checkbox marks, anything else becomes an `@status:` field.
func (a *App) setTaskStatusColumn(t vault.Task, status string) {
	a.editTaskLine(t, func(body string) string {
		if t.Kind == "file" {
			switch status {
			case "done":
				return vault.SetTaskFileStatus(body, true, time.Now())
			case "in-progress":
				return vault.SetTaskFileInProgress(vault.SetTaskFileStatus(body, false, time.Now()), true)
			case "todo":
				return vault.SetTaskFileInProgress(vault.SetTaskFileStatus(body, false, time.Now()), false)
			}
			return vault.SetTaskFileField(vault.SetTaskFileStatus(body, false, time.Now()), "status", status)
		}
		next := body
		switch status {
		case "done":
			next = vault.SetTaskChecked(next, t.TaskIndex, true)
			next = vault.SetTaskField(next, t.TaskIndex, "status", "")
		case "in-progress":
			next = vault.SetTaskChecked(next, t.TaskIndex, false)
			next = vault.SetTaskInProgress(next, t.TaskIndex, true)
			next = vault.SetTaskField(next, t.TaskIndex, "status", "")
		case "todo":
			next = vault.SetTaskChecked(next, t.TaskIndex, false)
			next = vault.SetTaskInProgress(next, t.TaskIndex, false)
			next = vault.SetTaskField(next, t.TaskIndex, "status", "")
		default:
			next = vault.SetTaskChecked(next, t.TaskIndex, false)
			next = vault.SetTaskInProgress(next, t.TaskIndex, false)
			next = vault.SetTaskField(next, t.TaskIndex, "status", status)
		}
		return next
	})
}

func (a *App) moveTaskWithinNote(t vault.Task, dir int) {
	if t.Kind == "file" {
		return
	}
	target := t.TaskIndex + dir
	if target < 0 {
		return
	}
	a.editTaskLine(t, func(body string) string {
		return vault.MoveTaskLine(body, t.TaskIndex, target, dir < 0)
	})
}

func (a *App) taskPriorityMenu(t vault.Task) {
	a.showMenu("Priority", []menuItem{
		{key: "h", label: "High", run: func(a *App) { a.setTaskPriority(t, "high") }},
		{key: "m", label: "Medium", run: func(a *App) { a.setTaskPriority(t, "med") }},
		{key: "l", label: "Low", run: func(a *App) { a.setTaskPriority(t, "low") }},
		{key: "n", label: "None", run: func(a *App) { a.setTaskPriority(t, "") }},
	})
}

func (a *App) taskDuePrompt(t vault.Task) {
	a.promptFor("Due date", t.Due, "YYYY-MM-DD, today, tomorrow, +3, mon..sun, or empty to clear", func(a *App, text string) {
		due, err := parseDueInput(strings.TrimSpace(text), time.Now())
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.setTaskDue(t, due)
	})
}

// parseDueInput accepts the shorthand the prompt advertises.
func parseDueInput(text string, now time.Time) (string, error) {
	text = strings.ToLower(text)
	switch text {
	case "", "none", "clear":
		return "", nil
	case "today":
		return vault.TodayISO(now), nil
	case "tomorrow", "tmr":
		return vault.TodayISO(now.AddDate(0, 0, 1)), nil
	case "yesterday":
		return vault.TodayISO(now.AddDate(0, 0, -1)), nil
	}
	if strings.HasPrefix(text, "+") {
		var n int
		if _, err := fmt.Sscanf(text, "+%d", &n); err == nil {
			return vault.TodayISO(now.AddDate(0, 0, n)), nil
		}
	}
	days := map[string]time.Weekday{"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday}
	for name, wd := range days {
		if strings.HasPrefix(text, name) {
			delta := (int(wd) - int(now.Weekday()) + 7) % 7
			if delta == 0 {
				delta = 7
			}
			return vault.TodayISO(now.AddDate(0, 0, delta)), nil
		}
	}
	if vault.IsValidISODate(text) {
		return text, nil
	}
	return "", fmt.Errorf("not a date: %s", text)
}

func (a *App) taskTextPrompt(t vault.Task) {
	if t.Kind == "file" {
		a.openTaskSource(t)
		return
	}
	a.promptFor("Edit task", t.Content, "", func(a *App, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		a.editTaskLine(t, func(body string) string { return vault.SetTaskText(body, t.TaskIndex, strings.TrimSpace(text)) })
	})
}

func (a *App) taskFieldPrompt(t vault.Task) {
	a.promptFor("Set field", "status:", "key:value (empty value clears it)", func(a *App, text string) {
		key, val, ok := strings.Cut(strings.TrimSpace(text), ":")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || key == "" {
			return
		}
		a.setTaskField(t, key, strings.TrimSpace(val))
	})
}

func (a *App) taskMenu(t vault.Task) {
	a.showMenu(truncateCells(t.Content, 40), []menuItem{
		{key: "o", label: "Open note", run: func(a *App) { a.openTaskSource(t) }},
		{key: "x", label: "Toggle done", run: func(a *App) { a.toggleTask(t) }},
		{key: "/", label: "Toggle in progress", run: func(a *App) { a.setTaskInProgress(t, !t.InProgress) }},
		{key: "w", label: "Toggle waiting", run: func(a *App) { a.setTaskWaiting(t, !t.Waiting) }},
		{key: "c", label: "Toggle cancelled", run: func(a *App) { a.setTaskCancelled(t, !t.Cancelled) }},
		{sep: true},
		{key: "p", label: "Priority", run: func(a *App) { a.taskPriorityMenu(t) }},
		{key: "d", label: "Due date", run: func(a *App) { a.taskDuePrompt(t) }},
		{key: "e", label: "Edit text", run: func(a *App) { a.taskTextPrompt(t) }},
		{key: "s", label: "Set field", run: func(a *App) { a.taskFieldPrompt(t) }},
		{key: "f", label: "Forward to today's daily note", run: func(a *App) { a.forwardTask(t) }},
		{key: "y", label: "Copy line", run: func(a *App) { a.copyText(t.RawText) }},
	})
}

// forwardTask moves an open task (with its subtasks) to today's daily note,
// leaving a `[>]` record behind.
func (a *App) forwardTask(t vault.Task) {
	if t.Kind == "file" || a.idx == nil || !a.idx.settings.DailyNotes.Enabled {
		a.notify("Forwarding needs daily notes enabled and an inline task")
		return
	}
	_, _, title, rel := a.periodicLocation(periodic.Daily, time.Now())
	if rel == t.SourcePath {
		a.notify("That task is already in today's note")
		return
	}
	var moved []string
	err := a.mutateNote(t.SourcePath, func(body string) (string, bool) {
		next, lines := vault.ForwardTaskLines(body, t.TaskIndex, "[["+title+"]]")
		moved = lines
		return next, next != body
	})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if len(moved) == 0 {
		return
	}
	if _, ok := a.noteMeta(rel); !ok {
		body := a.periodicBody(periodic.Daily, title, time.Now())
		meta, err := a.backend.CreateNote(a.ctx, vault.FolderInbox, title, "", &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		rel = meta.Path
	}
	if err := a.mutateNote(rel, func(body string) (string, bool) {
		return vault.InsertTasksUnderTasksHeading(body, moved), true
	}); err != nil {
		a.notifyError(err.Error())
		return
	}
	a.notify("Forwarded to " + title)
	a.afterTaskChange()
}
