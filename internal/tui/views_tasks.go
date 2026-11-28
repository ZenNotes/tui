package tui

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/termsafe"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// tasksView is the Tasks tab: list, board and calendar modes.
type tasksView struct {
	mode   string
	filter string
	tasks  []vault.Task

	list listCursor
	rows []taskRow

	groupBy string
	// columns are the board's columns on screen; allColumns adds the empty
	// ones [terminal] kanban_hide_empty_columns keeps off it, which a card
	// can still be moved into.
	columns    []kanbanColumn
	allColumns []kanbanColumn
	col        int
	card       int
	// wantCol is the column a moved card is headed for. The selection goes
	// there once the reloaded tasks put it on the board, until a key moves on.
	wantCol string

	month    time.Time
	day      time.Time
	cal      *taskCalendar
	pageRows int

	boardStarts []int
	// boardCards holds where each drawn card sits, by column, counted from
	// the first card on screen.
	boardCards [][]cardSpan
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

// calc is the calendar state, made on first use.
func (v *tasksView) calc() *taskCalendar {
	if v.cal == nil {
		v.cal = newTaskCalendar()
	}
	return v.cal
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
	v.tasks = filterTaskQuery(all, v.filter)
	groups := vault.OrderTaskGroups(vault.GroupTasks(v.tasks, time.Now()), a.prefs.TasksListOrder)
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
	add("No due date", groups.Undated, "")
	add("Waiting", groups.Waiting, "")
	add("Done", groups.Done, "")
	add("Forwarded", groups.Forwarded, "")
	add("Cancelled", groups.Cancelled, "")
	v.list.clamp(len(v.rows))
	if v.list.cursor < len(v.rows) && v.rows[v.list.cursor].header != "" && len(v.rows) > 1 {
		v.list.cursor++
	}
	v.buildColumns(a)
	if v.mode == "kanban" {
		a.ensureCardFields(v.allColumns)
	}
	v.calc().rebuild(v.tasks, v.day)
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
			// The deadline, which a planned day in a daily note replaces.
			due := vault.TaskDeadline(t)
			switch val {
			case "today":
				match = due == vault.TodayISO(time.Now())
			case "overdue":
				match = vault.IsOverdue(t, time.Now())
			case "none":
				match = due == ""
			case "any":
				match = due != ""
			default:
				match = strings.HasPrefix(due, val)
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
	opts := []string{"status", "priority", "due", "folder", "field:status"}
	seen := map[string]bool{"field:status": true}
	for _, t := range v.tasks {
		for k := range t.Fields {
			if !seen["field:"+k] {
				seen["field:"+k] = true
				opts = append(opts, "field:"+k)
			}
		}
	}
	sort.Strings(opts[5:])
	return opts
}

func (v *tasksView) columnTitle(a *App, id, fallback string) string {
	if custom := a.prefs.KanbanColumnTitles[id]; strings.TrimSpace(custom) != "" {
		return custom
	}
	return fallback
}

func (v *tasksView) buildColumns(a *App) {
	// The selection follows its column by id: with empty columns hidden, a
	// column's place in the row changes as cards come and go.
	selected := ""
	if v.col < len(v.columns) {
		selected = v.columns[v.col].id
	}
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
			// By the day the task sits on: only a deadline is ever before
			// today, so Overdue still holds late work alone.
			day := vault.TaskDay(t, todayISO)
			switch {
			case day == "":
				place("none", t)
			case day < todayISO:
				place("overdue", t)
			case day == todayISO:
				place("today", t)
			case day == tomorrowISO:
				place("tomorrow", t)
			case day <= weekISO:
				place("week", t)
			default:
				place("later", t)
			}
		}
	case "status", "":
		for _, c := range []struct{ id, title string }{{"today", "Today"}, {"upcoming", "Upcoming"}, {"undated", "No due date"}, {"in-progress", "In progress"}, {"waiting", "Waiting"}, {"done", "Done"}} {
			cols = append(cols, kanbanColumn{id: c.id, title: v.columnTitle(a, c.id, c.title)})
		}
		today := vault.TodayISO(time.Now())
		for _, t := range v.tasks {
			if t.Cancelled || t.Forwarded {
				continue
			}
			id := "today"
			switch {
			case t.Checked:
				id = "done"
			case t.Waiting:
				id = "waiting"
			case t.InProgress:
				id = "in-progress"
			default:
				// The list's Today, Upcoming and No due date, by the day the
				// task sits on.
				switch day := vault.TaskDay(t, today); {
				case day == "":
					id = "undated"
				case day > today:
					id = "upcoming"
				}
			}
			place(id, t)
		}
	case "folder":
		groups := map[string][]vault.Task{}
		titles := map[string]string{}
		root := strings.Trim(strings.ReplaceAll(strings.TrimSpace(a.prefs.KanbanFolderRoot), "\\", "/"), "/")
		for _, t := range v.tasks {
			if t.Checked || t.Cancelled || t.Forwarded {
				continue
			}
			dir := path.Dir(strings.ReplaceAll(t.SourcePath, "\\", "/"))
			if dir == "." {
				dir = ""
			}
			prefix := vault.ResolveFolderPath(t.NoteFolder, a.folderPaths())
			if t.NoteFolder == vault.FolderInbox && a.primaryAtRoot() {
				prefix = ""
			}
			rel := dir
			if prefix != "" {
				if strings.EqualFold(dir, prefix) {
					rel = ""
				} else if strings.HasPrefix(strings.ToLower(dir), strings.ToLower(prefix)+"/") {
					rel = dir[len(prefix)+1:]
				}
			}
			id, title := dir, rel
			if title == "" {
				title = a.folderLabel(t.NoteFolder)
			}
			if id == "" {
				id = prefix
				if id == "" {
					id = "inbox"
				}
			}
			if root != "" {
				if t.NoteFolder != vault.FolderInbox || !(strings.EqualFold(rel, root) || strings.HasPrefix(strings.ToLower(rel), strings.ToLower(root)+"/")) {
					id = "__none__"
					title = "Other folders"
				} else {
					selected := rel
					if len(rel) > len(root) {
						selected = rel[:len(root)] + "/" + strings.Split(rel[len(root)+1:], "/")[0]
					}
					id = selected
					if prefix != "" {
						id = prefix + "/" + selected
					}
					title = path.Base(selected)
				}
			}
			groups[id] = append(groups[id], t)
			titles[id] = title
		}
		ids := []string{}
		for id := range groups {
			if id != "__none__" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(groups["__none__"]) > 0 {
			ids = append(ids, "__none__")
		}
		for _, id := range ids {
			cols = append(cols, kanbanColumn{id: id, title: titles[id], cards: groups[id]})
		}

	default:
		key := strings.TrimPrefix(v.groupBy, "field:")
		values := []string{}
		seen := map[string]bool{}
		add := func(val string) {
			val = strings.ToLower(strings.TrimSpace(val))
			if val != "" && !seen[val] {
				seen[val] = true
				values = append(values, val)
			}
		}
		if key == "status" {
			for _, s := range a.prefs.KanbanStatuses {
				add(s)
			}
		}
		configured := len(values)
		configuredSet := map[string]bool{}
		for _, val := range values {
			configuredSet[val] = true
		}
		// Closed tasks stay off a board, with one exception (desktop #949): a
		// task note whose value names a configured column stays in it, done or
		// cancelled. A task note's `status:` is its one state, so `status: done`
		// both closes it and names its column. Only configured columns count, or
		// every finished note would become a column; checkbox tasks keep the rule,
		// since a status inherited from the note would pull in checked subtasks.
		onBoard := func(t vault.Task) bool {
			if !t.Checked && !t.Cancelled && !t.Forwarded {
				return true
			}
			return t.Kind == "file" && configuredSet[strings.ToLower(strings.TrimSpace(t.Fields[key]))]
		}
		for _, t := range v.tasks {
			if onBoard(t) {
				add(t.Fields[key])
			}
		}
		sort.Strings(values[configured:])
		for _, val := range values {
			cols = append(cols, kanbanColumn{id: val, title: v.columnTitle(a, val, val)})
		}
		cols = append(cols, kanbanColumn{id: "__none__", title: "No " + key})
		for _, t := range v.tasks {
			if !onBoard(t) {
				continue
			}
			val := strings.ToLower(strings.TrimSpace(t.Fields[key]))
			if val == "" {
				val = "__none__"
			}
			place(val, t)
		}
	}
	for i := range cols {
		sort.SliceStable(cols[i].cards, func(x, y int) bool {
			l, r := cols[i].cards[x], cols[i].cards[y]
			ld, rd := l.Due, r.Due
			if ld == "" {
				ld = "9999-12-31"
			}
			if rd == "" {
				rd = "9999-12-31"
			}
			if ld != rd {
				return ld < rd
			}
			if l.SourcePath != r.SourcePath {
				return l.SourcePath < r.SourcePath
			}
			return l.TaskIndex < r.TaskIndex
		})
	}

	for i := range cols {
		order := a.boardOrder[v.groupBy+"|"+cols[i].id]
		rank := map[string]int{}
		for n, id := range order {
			rank[id] = n
		}
		sort.SliceStable(cols[i].cards, func(x, y int) bool {
			rx, okx := rank[cols[i].cards[x].ID]
			ry, oky := rank[cols[i].cards[y].ID]
			if okx != oky {
				return okx
			}
			return okx && rx < ry
		})
	}
	v.allColumns = cols
	if a.prefs.KanbanHideEmptyColumns {
		shown := []kanbanColumn{}
		for _, c := range cols {
			if len(c.cards) > 0 {
				shown = append(shown, c)
			}
		}
		cols = shown
	}
	v.columns = cols
	for i, c := range cols {
		if c.id == selected {
			v.col = i
		}
	}
	for i, c := range cols {
		if v.wantCol != "" && c.id == v.wantCol {
			v.col = i
			v.wantCol = ""
		}
	}
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
		body = v.calc().render(a, v.month, v.day, w, h-1, focused)
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
	// A planned day reads first, the deadline after it: the two dates on the
	// row are the desktop's plan mark and due chip. The date a planned day
	// took over from a daily note is not the task's own, so it is not shown.
	if plan := vault.PlannedDay(t); plan != "" && !t.Checked && !t.Cancelled {
		meta = append(meta, th.Dim.Render("plan "+plan))
	}
	if deadline := vault.TaskDeadline(t); deadline != "" {
		due := "due " + deadline
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
	note := th.Muted.Render("· " + termsafe.Line(t.NoteTitle))
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
		if len(v.allColumns) > 0 {
			return fitBlock(" "+th.Muted.Render("No cards. Empty columns are hidden (B e shows them)."), w, h)
		}
		return fitBlock(" "+th.Muted.Render("No columns"), w, h)
	}
	colW := max(12, (w-len(v.columns)+1)/len(v.columns))
	blocks := []string{}
	v.boardStarts = make([]int, len(v.columns))
	v.boardCards = make([][]cardSpan, len(v.columns))
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
		// The selected column scrolls just far enough to show its card whole:
		// walk up from it while the cards above still fit.
		cardRows := h - 2
		start := 0
		if ci == v.col && v.card < len(col.cards) {
			used := 0
			for i := v.card; i >= 0; i-- {
				rows := v.cardFace(a, col.cards[i], colW).height()
				if i < v.card {
					rows++
				}
				if used+rows > cardRows && i < v.card {
					start = i + 1
					break
				}
				used += rows
			}
		}
		v.boardStarts[ci] = start
		for i := start; i < len(col.cards) && len(lines) < h; i++ {
			c := col.cards[i]
			selected := focused && ci == v.col && i == v.card
			face := v.cardFace(a, c, colW)
			v.boardCards[ci] = append(v.boardCards[ci], cardSpan{top: len(lines) - 2, height: face.height()})
			if selected {
				bar := th.BorderFocus.Render("▍")
				for _, row := range face.title {
					lines = append(lines, bar+th.SelectedFocus.Render(padRight(row, colW-1)))
				}
				if len(face.meta) > 0 {
					lines = append(lines, bar+th.Selected.Render(padRight(face.metaPlain, colW-1)))
				}
				if face.fields != "" {
					lines = append(lines, bar+th.Selected.Render(padRight(face.fields, colW-1)))
				}
			} else {
				for _, row := range face.title {
					lines = append(lines, padRight(th.Base.Render(" "+row), colW))
				}
				if len(face.meta) > 0 {
					lines = append(lines, padRight(" "+strings.Join(face.meta, " "), colW))
				}
				if face.fields != "" {
					lines = append(lines, padRight(" "+th.Muted.Render(face.fields), colW))
				}
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

func (v *tasksView) ownsKey(a *App, k vim.Key) bool {
	switch v.mode {
	case "kanban":
		return k.IsRune('g')
	case "calendar":
		return v.calc().focus == calFocusGrid && (k.IsRune('[') || k.IsRune(']'))
	}
	return false
}

func (v *tasksView) handleKey(a *App, k vim.Key) bool {
	if k.IsRune('S') {
		a.savedTaskFilters()
		return true
	}
	if k.IsRune('B') {
		v.boardMenu(a)
		return true
	}
	if k.IsRune('O') && v.mode == "list" && a.listKeyAllowed(k) {
		next := "priority"
		if a.prefs.TasksListOrder == "priority" {
			next = "note"
		}
		a.setTasksListOrder(next)
		return true
	}
	if k.IsRune('v') && a.listKeyAllowed(k) {
		switch v.mode {
		case "list":
			v.mode = "kanban"
		case "kanban":
			v.mode = "calendar"
		default:
			v.mode = "list"
		}
		a.prefs.TasksViewMode = v.mode
		v.refresh(a)
		return true
	}
	if k.IsRune('f') && a.listKeyAllowed(k) {
		a.promptFor("Filter tasks", v.filter, "Text, #tag, !high or @field:value · advanced: where: due:today is:open", func(a *App, text string) {
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
		return v.calc().selected(v.day)
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
	case "tasks.moveTaskUp", "tasks.moveTaskDown":
		// J and K move lines in the note, which the list only shows in note order.
		if a.prefs.TasksListOrder == "priority" {
			a.notify("Reordering works in note order. Press O to switch.")
			return true
		}
		if t != nil {
			dir := 1
			if id == "tasks.moveTaskUp" {
				dir = -1
			}
			a.moveTaskWithinNote(*t, dir)
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
	case k.IsRune('>'), k.IsRune('<'):
		// Without a deadline the calendar counts from the selected day, as
		// the desktop's does; the list and board from today.
		base := time.Now()
		if v.mode == "calendar" {
			base = v.day
		}
		days := 1
		if k.IsRune('<') {
			days = -1
		}
		a.shiftTaskDue(*t, base, days)
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
	v.wantCol = ""
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
		if t != nil {
			v.moveCardBy(a, *t, -1)
		}
	case k.IsRune('L') || (k.Is("right") && k.Shift):
		if t != nil {
			v.moveCardBy(a, *t, 1)
		}
	case k.IsRune('g'):
		opts := v.groupOptions()
		for i, o := range opts {
			if o == v.groupBy {
				v.groupBy = opts[(i+1)%len(opts)]
				a.prefs.KanbanGroupBy = v.groupBy
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
	case a.bound(k, "nav.toggleTask"):
		if t != nil {
			a.toggleTask(*t)
		}
	case a.bound(k, "nav.contextMenu"):
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

// moveCardBy moves a card one column left or right. It walks every column
// of the board, the hidden empty ones too: hiding them is about the screen,
// and the next state in a workflow is often a column nothing is in yet.
func (v *tasksView) moveCardBy(a *App, t vault.Task, delta int) {
	if v.col >= len(v.columns) {
		return
	}
	all := v.allColumns
	if len(all) == 0 {
		all = v.columns
	}
	for i, c := range all {
		if c.id != v.columns[v.col].id {
			continue
		}
		if next := i + delta; next >= 0 && next < len(all) {
			v.moveCardTo(a, t, all[next])
		}
		return
	}
}

// moveCard moves a card to the column at an index of the columns on screen.
func (v *tasksView) moveCard(a *App, t vault.Task, target int) {
	v.moveCardTo(a, t, v.columns[target])
}

// moveCardTo rewrites a task so it lands in another column.
func (v *tasksView) moveCardTo(a *App, t vault.Task, col kanbanColumn) {
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
			a.moveTaskToDay(t, vault.TodayISO(today), today)
		case "tomorrow":
			a.moveTaskToDay(t, vault.TodayISO(today.AddDate(0, 0, 1)), today)
		case "week":
			a.moveTaskToDay(t, vault.TodayISO(today.AddDate(0, 0, 7)), today)
		case "none":
			a.moveTaskToDay(t, "", today)
		default:
			a.notify("Set an explicit date with d to move a task there")
			return
		}
	case "status", "":
		if col.id == "undated" && t.DueInferred {
			a.notify("This task's date comes from its daily note, so there is no due date to clear")
			return
		}
		a.setTaskStatusColumn(t, col.id)
	case "folder":
		a.notify("Folder boards are read-only. Move the source note to change its folder.")
		return
	default:
		val := col.id
		if val == "__none__" {
			val = ""
		}
		a.setTaskField(t, strings.TrimPrefix(v.groupBy, "field:"), val)
	}

	v.wantCol = col.id
	for i, c := range v.columns {
		if c.id == col.id {
			v.col = i
		}
	}
}

func (v *tasksView) calendarKey(a *App, k vim.Key) bool {
	t := v.selectedTask()
	if v.calc().focus != calFocusGrid {
		// With Vim off a focused task, like the list, takes no letters.
		if !a.listKeyAllowed(k) {
			return false
		}
		switch {
		case k.IsRune(':'):
			a.openLocalEx()
		case k.Is("esc") || k.IsRune('h') || k.Is("left"):
			v.calc().focus = calFocusGrid
		case k.Is("tab"):
			v.calc().cycleFocus(v.day)
		case k.IsRune('j') || k.Is("down"):
			if c := v.calc(); c.focus == calFocusStrip {
				if c.strip < len(c.undated)-1 {
					c.strip++
				}
			} else if c.cursor < len(c.dayEntries(v.day))-1 {
				c.cursor++
			}
		case k.IsRune('k') || k.Is("up"):
			if c := v.calc(); c.focus == calFocusStrip {
				if c.strip > 0 {
					c.strip--
				}
			} else if c.cursor > 0 {
				c.cursor--
			}
		case k.Is("enter") || k.IsRune('o'):
			if t != nil {
				a.openTaskSource(*t)
			}
		case a.bound(k, "nav.toggleTask"):
			if t != nil {
				a.toggleTask(*t)
			}
		case a.bound(k, "nav.contextMenu"):
			if t != nil {
				a.taskMenu(*t)
			}
		case k.IsRune('T'):
			// As in the desktop calendar: T moves the task to today, @ picks a date.
			if t != nil {
				a.setTaskDue(*t, vault.TodayISO(calendarNow()))
			}
		case k.IsRune('@'):
			if t != nil {
				v.calendarDueMenu(a, *t)
			}
		default:
			return v.taskEditKey(a, k, t)
		}
		return true
	}
	if !a.listKeyAllowed(k) && !k.Is("enter") && !k.Is("left") && !k.Is("right") && !k.Is("up") && !k.Is("down") {
		return false
	}
	columns := v.calc().sub != calMonth
	switch {
	case k.Is("esc"):
		v.calc().sub = (v.calc().sub + 1) % 3
	case columns && (k.IsRune('j') || k.Is("down")):
		v.calc().scrollColumn(v.day, 1)
	case columns && (k.IsRune('k') || k.Is("up")):
		v.calc().scrollColumn(v.day, -1)
	case k.IsRune('h') || k.Is("left"):
		v.selectDay(a, v.day.AddDate(0, 0, -1))
	case k.IsRune('l') || k.Is("right"):
		v.selectDay(a, v.day.AddDate(0, 0, 1))
	case k.IsRune('j') || k.Is("down"):
		v.selectDay(a, v.day.AddDate(0, 0, 7))
	case k.IsRune('k') || k.Is("up"):
		v.selectDay(a, v.day.AddDate(0, 0, -7))
	case a.bound(k, "nav.halfPageUp"):
		v.pageDay(a, -1)
	case a.bound(k, "nav.halfPageDown"):
		v.pageDay(a, 1)
	case k.IsRune('[') || k.IsRune('H'):
		v.month = v.month.AddDate(0, -1, 0)
		v.selectDay(a, v.day.AddDate(0, -1, 0))
	case k.IsRune(']') || k.IsRune('L'):
		v.month = v.month.AddDate(0, 1, 0)
		v.selectDay(a, v.day.AddDate(0, 1, 0))
	case k.IsRune('t') || k.IsRune('T'):
		now := calendarNow()
		v.month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		v.selectDay(a, now)
	case k.Is("tab"):
		return v.calc().cycleFocus(v.day)
	case k.Is("enter"):
		if !v.calc().cycleFocus(v.day) {
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

// calendarDueMenu offers the due dates the calendar can set at once. The
// choices have no shortcut keys, so letters never act on the task by accident.
func (v *tasksView) calendarDueMenu(a *App, t vault.Task) {
	now := calendarNow()
	set := func(d time.Time) func(a *App) {
		return func(a *App) { a.setTaskDue(t, vault.TodayISO(d)) }
	}
	a.showMenu("Due date", []menuItem{
		{label: "Today (" + vault.TodayISO(now) + ")", run: set(now)},
		{label: "Tomorrow (" + vault.TodayISO(now.AddDate(0, 0, 1)) + ")", run: set(now.AddDate(0, 0, 1))},
		{label: "Next week (" + vault.TodayISO(now.AddDate(0, 0, 7)) + ")", run: set(now.AddDate(0, 0, 7))},
		{label: "Selected day (" + vault.TodayISO(v.day) + ")", run: set(v.day)},
		{label: "Clear due date", run: func(a *App) { a.setTaskDue(t, "") }},
		{label: "Other…", run: func(a *App) { a.taskDuePrompt(t) }},
	})
}

// pageDay moves the selected day by one page of the current layout: a month,
// a week or a day. dir is -1 or 1.
func (v *tasksView) pageDay(a *App, dir int) {
	switch v.calc().sub {
	case calWeek:
		v.selectDay(a, v.day.AddDate(0, 0, 7*dir))
	case calDay:
		v.selectDay(a, v.day.AddDate(0, 0, dir))
	default:
		v.month = v.month.AddDate(0, dir, 0)
		v.selectDay(a, v.day.AddDate(0, dir, 0))
	}
}

func (v *tasksView) selectDay(a *App, day time.Time) {
	v.day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	if v.day.Month() != v.month.Month() || v.day.Year() != v.month.Year() {
		v.month = time.Date(v.day.Year(), v.day.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	v.calc().cursor = 0
	v.calc().colScroll = 0
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
	a.queue(a.loadTasksCmd())
	a.refreshIndex()
}

// openTaskSource opens the note and moves to the task line.
// LineNumber counts from zero. GotoLine counts from one.
func (a *App) openTaskSource(t vault.Task) {
	a.openNote(t.SourcePath, true)
	if buf := a.buffers[t.SourcePath]; buf != nil && t.Kind != "file" {
		buf.ed.GotoLine(t.LineNumber + 1)
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

// shiftTaskDue is `<` and `>`: days from the task's deadline, or from base
// when it has none. A planned task sits on its plan, so the plan is never what
// the count starts from.
func (a *App) shiftTaskDue(t vault.Task, base time.Time, days int) {
	a.setTaskDue(t, vault.NudgedDueDate(t, vault.TodayISO(base), days))
}

// moveTaskToDay puts t on day, or with "" on no day, the way the board's date
// columns move a card. A task without a planned day moves by its due date, as
// it always did. A planned task sits where its plan puts it, so the plan moves
// and the deadline stays, unless the deadline has arrived: then it holds the
// task on itself and moves too.
func (a *App) moveTaskToDay(t vault.Task, day string, now time.Time) {
	planned := vault.PlannedDay(t)
	if planned == "" {
		a.setTaskDue(t, day)
		return
	}
	todayISO := vault.TodayISO(now)
	a.editTaskLine(t, func(body string) string {
		set := func(key, value string) {
			if t.Kind == "file" {
				body = vault.SetTaskFileField(body, key, value)
			} else if key == "due" {
				body = vault.SetTaskDue(body, t.TaskIndex, value)
			} else {
				body = vault.SetTaskField(body, t.TaskIndex, key, value)
			}
		}
		if day == "" {
			set("due", "")
			set(vault.PlannedDayField, "")
			return body
		}
		// The due date first, so the line reads its two dates in the order
		// the rest of zn writes them.
		if deadline := vault.TaskDeadline(t); deadline != "" && deadline <= todayISO && deadline != day {
			set("due", day)
		}
		set(vault.PlannedDayField, day)
		return body
	})
}

// setTaskPlannedDay plans t for day, or with "" takes its plan off. The plan
// is `@scheduled:` on a checkbox task and frontmatter `scheduled:` on a task
// note; the deadline is not touched.
func (a *App) setTaskPlannedDay(t vault.Task, day string) {
	if !vault.CanPlanTask(t) {
		a.notify("A done, cancelled or forwarded task has no day left to plan")
		return
	}
	if vault.PlannedDay(t) == day {
		return
	}
	a.setTaskField(t, vault.PlannedDayField, day)
}

func (a *App) taskPlanPrompt(t vault.Task) {
	a.promptFor("Plan for", vault.PlannedDay(t), "the day to work on it: YYYY-MM-DD, today, tomorrow, +3, mon..sun, or empty to clear", func(a *App, text string) {
		day, err := parseDueInput(strings.TrimSpace(text), time.Now())
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.setTaskPlannedDay(t, day)
	})
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

// setTaskStatusColumn changes scheduling/checkbox state independently of custom fields.
func (a *App) setTaskStatusColumn(t vault.Task, status string) {
	a.editTaskLine(t, func(body string) string {
		checked := func(v bool) {
			if t.Kind == "file" {
				body = vault.SetTaskFileStatus(body, v, time.Now())
			} else {
				body = vault.SetTaskChecked(body, t.TaskIndex, v)
			}
		}
		waiting := func(v bool) {
			if t.Kind == "file" {
				value := ""
				if v {
					value = "true"
				}
				body = vault.SetTaskFileField(body, "waiting", value)
			} else {
				body = vault.SetTaskWaiting(body, t.TaskIndex, v)
			}
		}
		progress := func(v bool) {
			if t.Kind == "file" {
				body = vault.SetTaskFileInProgress(body, v)
			} else {
				body = vault.SetTaskInProgress(body, t.TaskIndex, v)
			}
		}
		due := func(v string) {
			if t.Kind == "file" {
				body = vault.SetTaskFileField(body, "due", v)
			} else {
				body = vault.SetTaskDue(body, t.TaskIndex, v)
			}
		}
		plan := func(v string) {
			if t.Kind == "file" {
				body = vault.SetTaskFileField(body, vault.PlannedDayField, v)
			} else {
				body = vault.SetTaskField(body, t.TaskIndex, vault.PlannedDayField, v)
			}
		}
		// A task with a planned day sits where its plan puts it, so the date
		// columns move the plan and leave the deadline the date it has. A task
		// without one is moved by its due date, as it always was.
		planned := vault.PlannedDay(t)
		now := time.Now()
		switch status {
		case "done":
			checked(true)
		case "waiting":
			checked(false)
			waiting(true)
		case "in-progress":
			checked(false)
			waiting(false)
			progress(true)
		case "undated":
			checked(false)
			waiting(false)
			progress(false)
			due("")
			// No day at all means no planned day either.
			if planned != "" {
				plan("")
			}
		case "today", "upcoming":
			checked(false)
			waiting(false)
			progress(false)
			today := vault.TodayISO(now)
			tomorrow := vault.TodayISO(now.AddDate(0, 0, 1))
			switch {
			case status == "today" && planned != "":
				plan(today)
			case status == "today":
				due(today)
			case planned == "":
				if t.Due > today {
					due(t.Due)
				} else {
					due(tomorrow)
				}
			default:
				// Either date can hold a planned task in Today: a deadline that
				// arrived, or a plan for today (or one it missed). Each moves
				// only if it is the one in the way.
				if deadline := vault.TaskDeadline(t); deadline != "" && deadline <= today {
					due(tomorrow)
				}
				if planned <= today {
					plan(tomorrow)
				}
			}
		}
		return body
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
		{label: "Plan for…", run: func(a *App) { a.taskPlanPrompt(t) }},
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
