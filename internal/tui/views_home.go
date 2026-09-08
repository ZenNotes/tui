package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// The Home view is a dashboard: a header with the day and the vault's
// numbers, a Today card with the daily note and what is due, the recent
// notes, a week strip, favorites and the top tags. Every entry is reachable
// with j/k and Enter as well as the mouse.

type homeView struct {
	items      []homeItem
	cursor     int
	hits       []homeHit
	scroll     int
	rows       int
	tags       []tagCount
	week       []time.Time
	weekDay    int
	weekInit   bool
	tasksToday []vault.Task
	recent     []vault.NoteMeta
	favs       []homeItem
	stats      homeStats
	dailyPath  string
	dailyTitle string
}

type homeStats struct {
	notes, open, dueToday, overdue, done int
}

// homeItem is one selectable entry, in navigation order.
type homeItem struct {
	kind   string // daily, task, note, folder, week, tags, tag, action
	label  string
	note   *vault.NoteMeta
	task   *vault.Task
	folder vault.NoteFolder
	sub    string
	tag    string
	run    func(a *App)
}

// homeHit is where an item was drawn, relative to the content area.
type homeHit struct {
	x, y, w int
	idx     int
}

func (v *homeView) title() string { return "Home" }

func (v *homeView) hintPairs(a *App) []hintPair {
	return []hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"key:Enter", "open"}, {"nav.toggleTask", "toggle task"}, {"key:h/l", "week day"}, {"global.newNoteHere", "new note"}, {"global.searchNotes", "search"}, {"key:r", "refresh"}}
}

func (v *homeView) refresh(a *App) {
	v.items = v.items[:0]
	v.favs = v.favs[:0]
	if a.idx == nil {
		return
	}
	now := time.Now()
	// Daily note.
	_, _, title, rel := a.periodicLocation(periodic.Daily, now)
	v.dailyTitle = title
	v.dailyPath = ""
	if _, ok := a.noteMeta(rel); ok {
		v.dailyPath = rel
	}
	v.items = append(v.items, homeItem{kind: "daily", label: title, run: func(a *App) { a.openPeriodic(periodic.Daily, 0) }})
	// Tasks: overdue first, then today.
	tasks := a.displayTasks()
	groups := vault.GroupTasks(tasks, now)
	todayISO := vault.TodayISO(now)
	v.stats = homeStats{notes: 0}
	for _, n := range a.idx.notes {
		if n.Folder != vault.FolderTrash {
			v.stats.notes++
		}
	}
	for _, t := range tasks {
		switch {
		case t.Checked:
			v.stats.done++
		case vault.IsTaskOpen(t):
			v.stats.open++
		}
		if vault.IsOverdue(t, now) {
			v.stats.overdue++
		} else if t.Due == todayISO && vault.IsTaskOpen(t) {
			v.stats.dueToday++
		}
	}
	v.tasksToday = v.tasksToday[:0]
	for _, t := range groups.Today {
		if vault.IsOverdue(t, now) {
			v.tasksToday = append(v.tasksToday, t)
		}
	}
	for _, t := range groups.Today {
		if !vault.IsOverdue(t, now) {
			v.tasksToday = append(v.tasksToday, t)
		}
	}
	for i := range v.tasksToday {
		if i >= 12 {
			break
		}
		t := v.tasksToday[i]
		v.items = append(v.items, homeItem{kind: "task", task: &v.tasksToday[i], label: t.Content})
	}
	// Recent notes.
	v.recent = v.recent[:0]
	seen := map[string]bool{}
	for _, r := range a.recent {
		if meta, ok := a.noteMeta(r); ok && !seen[r] && meta.Folder != vault.FolderTrash {
			seen[r] = true
			v.recent = append(v.recent, meta)
		}
	}
	notes := append([]vault.NoteMeta{}, a.idx.notes...)
	sortNotes(notes, "updated")
	for _, n := range notes {
		if len(v.recent) >= 8 {
			break
		}
		if n.Folder == vault.FolderTrash || seen[n.Path] {
			continue
		}
		seen[n.Path] = true
		v.recent = append(v.recent, n)
	}
	for i := range v.recent {
		n := v.recent[i]
		v.items = append(v.items, homeItem{kind: "note", note: &v.recent[i], label: n.Title})
	}
	// Week strip.
	v.week = v.week[:0]
	start := now
	offset := int(now.Weekday())
	if a.prefs.CalendarWeekStart != "sunday" {
		offset = (offset + 6) % 7
	}
	start = time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, time.Local)
	for i := 0; i < 7; i++ {
		v.week = append(v.week, start.AddDate(0, 0, i))
	}
	if !v.weekInit || v.weekDay < 0 || v.weekDay > 6 {
		v.weekDay = offset
		v.weekInit = true
	}
	v.items = append(v.items, homeItem{kind: "week", label: "This week"})
	// Favorites.
	for _, f := range a.favorites() {
		if i := strings.Index(f, ":"); i >= 0 {
			folder := vault.NoteFolder(f[:i])
			sub := f[i+1:]
			label := a.folderLabel(folder)
			if sub != "" {
				label = sub
			}
			v.favs = append(v.favs, homeItem{kind: "folder", label: label, folder: folder, sub: sub})
			continue
		}
		if meta, ok := a.noteMeta(f); ok {
			m := meta
			v.favs = append(v.favs, homeItem{kind: "note", label: meta.Title, note: &m})
		}
	}
	v.items = append(v.items, v.favs...)
	// Tags.
	v.tags = a.idx.tags
	if len(v.tags) > 0 {
		v.items = append(v.items, homeItem{kind: "tags", label: "Tags"})
	}
	if v.cursor >= len(v.items) {
		v.cursor = max(0, len(v.items)-1)
	}
}

// --- rendering ---

func (v *homeView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	if len(v.items) == 0 {
		v.refresh(a)
	}
	v.hits = v.hits[:0]
	v.rows = h
	pad := 1
	inner := w - 2*pad
	lines := []string{}
	addLine := func(s string) { lines = append(lines, strings.Repeat(" ", pad)+padRight(s, inner)) }

	// Header: vault, date, numbers.
	now := time.Now()
	left := th.Title.Render("✦ "+a.vaultLabel()) + th.Dim.Render("  "+now.Format("Monday, January 2"))
	stats := []string{th.Base.Render(fmt.Sprint(v.stats.notes)) + th.Dim.Render(" notes"), th.Base.Render(fmt.Sprint(v.stats.open)) + th.Dim.Render(" open")}
	if v.stats.dueToday > 0 {
		stats = append(stats, th.Marker.Render(fmt.Sprint(v.stats.dueToday))+th.Dim.Render(" due today"))
	}
	if v.stats.overdue > 0 {
		stats = append(stats, th.Overdue.Render(fmt.Sprint(v.stats.overdue)+" overdue"))
	}
	right := strings.Join(stats, th.Muted.Render("  ·  "))
	gap := max(1, inner-lipgloss.Width(left)-lipgloss.Width(right))
	addLine(left + strings.Repeat(" ", gap) + right)
	actions := []string{a.keyOf("global.newNoteHere") + " new note", a.keyOf("global.searchNotes") + " search", a.keyOf("vim.leaderPrefix") + " d daily", a.keyOf("vim.leaderPrefix") + " q capture", a.keyOf("vim.leaderPrefix") + " x tasks"}
	styled := []string{}
	for _, act := range actions {
		key, verb, _ := strings.Cut(act, " ")
		if strings.HasPrefix(act, a.keyOf("vim.leaderPrefix")+" ") && a.keyOf("vim.leaderPrefix") != "" {
			parts := strings.SplitN(act, " ", 3)
			key, verb = parts[0]+" "+parts[1], parts[2]
		}
		styled = append(styled, th.KeyHint.Render(key)+" "+th.Dim.Render(verb))
	}
	addLine(strings.Join(styled, th.Muted.Render("   ")))
	addLine("")

	twoCols := inner >= 96
	leftW, rightW := inner, 0
	if twoCols {
		rightW = min(46, inner*2/5)
		leftW = inner - rightW - 2
	}
	itemIdx := map[string]int{}
	for i, it := range v.items {
		itemIdx[v.itemKey(it)] = i
	}
	sel := func(i int) bool { return focused && i == v.cursor }

	leftCards := [][]string{}
	rightCards := [][]string{}
	var leftHits, rightHits []homeHit

	// Today card.
	{
		body := []string{}
		hits := []homeHit{}
		idx := itemIdx["daily"]
		label := "▸ " + v.dailyTitle
		if v.dailyPath == "" {
			label = "＋ Create today's note (" + v.dailyTitle + ")"
		}
		line := th.Base.Foreground(th.Accent).Render(label)
		if sel(idx) {
			line = th.SelectedFocus.Render(padRight(label, leftW-4))
		}
		hits = append(hits, homeHit{x: 0, y: len(body), w: leftW - 4, idx: idx})
		body = append(body, line)
		body = append(body, "")
		if len(v.tasksToday) == 0 {
			body = append(body, th.Muted.Render("Nothing due today"))
		}
		for i := range v.tasksToday {
			if i >= 12 {
				body = append(body, th.Muted.Render(fmt.Sprintf("… %d more in Tasks", len(v.tasksToday)-12)))
				break
			}
			t := v.tasksToday[i]
			idx := itemIdx["task:"+t.ID]
			body = append(body, v.taskRow(a, t, leftW-4, sel(idx)))
			hits = append(hits, homeHit{x: 0, y: len(body) - 1, w: leftW - 4, idx: idx})
		}
		leftCards = append(leftCards, cardLines(th, "Today", body, leftW))
		leftHits = append(leftHits, shiftHits(hits, 2, cardTop(leftCards[:len(leftCards)-1]))...)
	}
	// Recent card.
	{
		body := []string{}
		hits := []homeHit{}
		if len(v.recent) == 0 {
			body = append(body, th.Muted.Render("No notes yet. "+a.keyOf("global.newNoteHere")+" creates one."))
		}
		for i := range v.recent {
			n := v.recent[i]
			idx := itemIdx["note:"+n.Path]
			body = append(body, v.noteRow(a, n, leftW-4, sel(idx)))
			hits = append(hits, homeHit{x: 0, y: len(body) - 1, w: leftW - 4, idx: idx})
		}
		leftCards = append(leftCards, cardLines(th, "Recent", body, leftW))
		leftHits = append(leftHits, shiftHits(hits, 2, cardTop(leftCards[:len(leftCards)-1]))...)
	}
	// Week card.
	if twoCols || true {
		body := v.weekLines(a, rightOr(rightW, leftW)-4, sel(itemIdx["week"]))
		w := rightOr(rightW, leftW)
		card := cardLines(th, "This week", body, w)
		hits := []homeHit{{x: 0, y: 1, w: w - 4, idx: itemIdx["week"]}}
		if twoCols {
			rightHits = append(rightHits, shiftHits(hits, 2, cardTop(rightCards))...)
			rightCards = append(rightCards, card)
		} else {
			leftHits = append(leftHits, shiftHits(hits, 2, cardTop(leftCards))...)
			leftCards = append(leftCards, card)
		}
	}
	// Favorites card.
	if len(v.favs) > 0 {
		w := rightOr(rightW, leftW)
		body := []string{}
		hits := []homeHit{}
		for _, f := range v.favs {
			idx := itemIdx[v.itemKey(f)]
			glyph := "★ "
			if f.kind == "folder" {
				glyph = "▸ "
			}
			line := th.Base.Render(glyph + truncateCells(f.label, w-6))
			if sel(idx) {
				line = th.SelectedFocus.Render(padRight(glyph+truncateCells(f.label, w-6), w-4))
			}
			body = append(body, line)
			hits = append(hits, homeHit{x: 0, y: len(body) - 1, w: w - 4, idx: idx})
		}
		card := cardLines(th, "Favorites", body, w)
		if twoCols {
			rightHits = append(rightHits, shiftHits(hits, 2, cardTop(rightCards))...)
			rightCards = append(rightCards, card)
		} else {
			leftHits = append(leftHits, shiftHits(hits, 2, cardTop(leftCards))...)
			leftCards = append(leftCards, card)
		}
	}
	// Tags card.
	if len(v.tags) > 0 {
		w := rightOr(rightW, leftW)
		chips := []string{}
		for i, t := range v.tags {
			if i >= 12 {
				break
			}
			chips = append(chips, th.Tag.Render("#"+t.tag)+th.Muted.Render(" "+fmt.Sprint(t.count)))
		}
		body := wrapChips(chips, w-4)
		if sel(itemIdx["tags"]) {
			for i := range body {
				body[i] = th.Selected.Render(padRight(body[i], w-4))
			}
		}
		card := cardLines(th, "Tags", body, w)
		hits := []homeHit{{x: 0, y: 1, w: w - 4, idx: itemIdx["tags"]}}
		if twoCols {
			rightHits = append(rightHits, shiftHits(hits, 2, cardTop(rightCards))...)
			rightCards = append(rightCards, card)
		} else {
			leftHits = append(leftHits, shiftHits(hits, 2, cardTop(leftCards))...)
			leftCards = append(leftCards, card)
		}
	}

	leftLines := flatten(leftCards)
	rightLines := flatten(rightCards)
	headerRows := len(lines)
	if twoCols {
		n := max(len(leftLines), len(rightLines))
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(leftLines) {
				l = leftLines[i]
			}
			if i < len(rightLines) {
				r = rightLines[i]
			}
			addLine(padRight(l, leftW) + "  " + padRight(r, rightW))
		}
		for _, hh := range leftHits {
			v.hits = append(v.hits, homeHit{x: pad + hh.x, y: headerRows + hh.y, w: hh.w, idx: hh.idx})
		}
		for _, hh := range rightHits {
			v.hits = append(v.hits, homeHit{x: pad + leftW + 2 + hh.x, y: headerRows + hh.y, w: hh.w, idx: hh.idx})
		}
	} else {
		for _, l := range leftLines {
			addLine(l)
		}
		for _, hh := range leftHits {
			v.hits = append(v.hits, homeHit{x: pad + hh.x, y: headerRows + hh.y, w: hh.w, idx: hh.idx})
		}
	}
	// Scroll so the selection stays visible.
	selY := -1
	for _, hh := range v.hits {
		if hh.idx == v.cursor {
			selY = hh.y
			break
		}
	}
	if selY >= 0 {
		if selY < v.scroll {
			v.scroll = selY
		}
		if selY >= v.scroll+h {
			v.scroll = selY - h + 1
		}
	}
	if v.scroll > max(0, len(lines)-h) {
		v.scroll = max(0, len(lines)-h)
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	visible := lines[v.scroll:]
	if len(visible) > h {
		visible = visible[:h]
	}
	return fitBlock(strings.Join(visible, "\n"), w, h)
}

func rightOr(rightW, leftW int) int {
	if rightW > 0 {
		return rightW
	}
	return leftW
}

func flatten(cards [][]string) []string {
	out := []string{}
	for i, c := range cards {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, c...)
	}
	return out
}

// cardTop is the row a new card starts at, after the cards already laid.
func cardTop(cards [][]string) int {
	return len(flatten(cards)) + boolInt(len(cards) > 0)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func shiftHits(hits []homeHit, dx, dy int) []homeHit {
	out := make([]homeHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, homeHit{x: h.x + dx, y: h.y + dy, w: h.w, idx: h.idx})
	}
	return out
}

// cardLines frames a body in a rounded box with the title in the top edge.
func cardLines(th Theme, title string, body []string, width int) []string {
	inner := width - 2
	top := th.Border.Render("╭─ ") + th.Bold.Render(title) + th.Border.Render(" "+strings.Repeat("─", max(0, inner-cellWidth(title)-3))+"╮")
	out := []string{top}
	for _, b := range body {
		out = append(out, th.Border.Render("│")+" "+padRight(b, inner-2)+" "+th.Border.Render("│"))
	}
	out = append(out, th.Border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

func wrapChips(chips []string, width int) []string {
	lines := []string{}
	cur := ""
	for _, c := range chips {
		cw := lipgloss.Width(c)
		if cur != "" && lipgloss.Width(cur)+2+cw > width {
			lines = append(lines, cur)
			cur = ""
		}
		if cur != "" {
			cur += "  "
		}
		cur += c
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

func (v *homeView) taskRow(a *App, t vault.Task, w int, selected bool) string {
	th := a.theme
	glyph := "☐"
	switch {
	case t.Checked:
		glyph = "☑"
	case t.InProgress:
		glyph = "◩"
	}
	meta := ""
	if vault.IsOverdue(t, time.Now()) {
		meta = th.Overdue.Render("overdue " + t.Due)
	} else if t.Due != "" {
		meta = th.Dim.Render(t.Due)
	}
	if t.Priority != "" {
		meta += " " + lipgloss.NewStyle().Foreground(th.Purple).Render("!"+t.Priority)
	}
	note := th.Muted.Render(truncateCells(t.NoteTitle, 22))
	avail := w - 3 - lipgloss.Width(meta) - lipgloss.Width(note) - 3
	text := truncateCells(t.Content, max(6, avail))
	if selected {
		return th.SelectedFocus.Render(padRight(glyph+" "+text+"  "+stripAnsi(meta)+" "+stripAnsi(note), w))
	}
	row := th.Checkbox.Render(glyph) + " " + th.Base.Render(text)
	fill := max(1, w-lipgloss.Width(row)-lipgloss.Width(meta)-lipgloss.Width(note)-1)
	return row + strings.Repeat(" ", fill) + meta + " " + note
}

func (v *homeView) noteRow(a *App, n vault.NoteMeta, w int, selected bool) string {
	th := a.theme
	age := formatAge(n.UpdatedAt)
	where := a.folderLabel(n.Folder)
	if _, sub := a.folderOfPath(n.Path); sub != "" {
		where = sub
	}
	title := truncateCells(n.Title, max(8, w-cellWidth(where)-len(age)-6))
	fill := max(1, w-cellWidth(title)-cellWidth(where)-len(age)-3)
	if selected {
		return th.SelectedFocus.Render(padRight(title+strings.Repeat(" ", fill)+where+"  "+age, w))
	}
	return th.Base.Render(title) + strings.Repeat(" ", fill) + th.Muted.Render(where) + "  " + th.Dim.Render(age)
}

// weekLines draws the seven-day strip: names, numbers, task counts, note dots.
func (v *homeView) weekLines(a *App, w int, selected bool) []string {
	th := a.theme
	cell := max(4, min(6, w/7))
	byDay := vault.BucketTasksByDueDate(a.displayTasks())
	daily := a.dailyNotesByDate()
	names, nums, counts := "", "", ""
	today := time.Now()
	for i, d := range v.week {
		key := d.Format("2006-01-02")
		name := d.Format("Mon")[:2]
		num := fmt.Sprintf("%2d", d.Day())
		open := 0
		for _, t := range byDay[key] {
			if vault.IsTaskOpen(t) {
				open++
			}
		}
		count := "·"
		if open > 0 {
			count = fmt.Sprint(open)
		}
		if _, ok := daily[key]; ok {
			count += "●"
		}
		nameS := th.Dim.Render(padRight(name, cell))
		numS := th.Base.Render(padRight(num, cell))
		if sameDay(d, today) {
			numS = th.Base.Foreground(th.Accent).Bold(true).Render(padRight(num, cell))
		}
		if selected && i == v.weekDay {
			numS = th.SelectedFocus.Render(padRight(num, cell-1)) + " "
		}
		countS := th.Marker.Render(padRight(count, cell))
		if open == 0 {
			countS = th.Muted.Render(padRight(count, cell))
		}
		names += nameS
		nums += numS
		counts += countS
	}
	sel := v.week[min(v.weekDay, len(v.week)-1)]
	foot := th.Dim.Render(sel.Format("Mon Jan 2") + ": ")
	if _, ok := daily[sel.Format("2006-01-02")]; ok {
		foot += th.Base.Render("daily note exists") + th.Dim.Render(" · Enter opens it")
	} else {
		foot += th.Dim.Render("Enter creates the daily note")
	}
	return []string{names, nums, counts, "", foot}
}

// dailyNotesByDate maps dates to daily note paths.
func (a *App) dailyNotesByDate() map[string]string {
	out := map[string]string{}
	if a.idx == nil {
		return out
	}
	for _, n := range a.idx.notes {
		if n.Folder != vault.FolderInbox {
			continue
		}
		_, sub := a.folderOfPath(n.Path)
		if d, ok := periodic.DateOf(periodic.Daily, sub, n.Title, a.idx.settings, a.primaryAtRoot()); ok {
			out[d.Format("2006-01-02")] = n.Path
		}
	}
	return out
}

func (v *homeView) itemKey(it homeItem) string {
	switch it.kind {
	case "task":
		return "task:" + it.task.ID
	case "note":
		return "note:" + it.note.Path
	case "folder":
		return "folder:" + string(it.folder) + ":" + it.sub
	}
	return it.kind
}

// --- keys ---

func (v *homeView) handleKey(a *App, k vim.Key) bool {
	n := len(v.items)
	cur := listCursor{cursor: v.cursor}
	if a.listNav(&cur, k, n, max(1, v.rows/2)) {
		v.cursor = cur.cursor
		return true
	}
	if n == 0 {
		return false
	}
	it := v.items[v.cursor]
	if k.Is("enter") {
		v.activate(a, it)
		return true
	}
	if it.kind == "week" && (k.Is("left") || k.Is("right") || (a.listKeyAllowed(k) && (k.IsRune('h') || k.IsRune('l')))) {
		if k.Is("left") || k.IsRune('h') {
			v.weekDay = max(0, v.weekDay-1)
		} else {
			v.weekDay = min(6, v.weekDay+1)
		}
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.openResult", "nav.toggleTask", "nav.localEx", "nav.filter", "nav.delete", "nav.contextMenu")
	if pending {
		return true
	}
	switch id {
	case "nav.openResult":
		v.activate(a, it)
	case "nav.toggleTask":
		if it.task != nil {
			a.toggleTask(*it.task)
		}
	case "nav.localEx":
		a.openLocalEx()
	case "nav.filter":
		a.openNoteSearch("")
	case "nav.delete":
		if it.note != nil {
			a.trashNote(it.note.Path)
		}
	case "nav.contextMenu":
		v.menu(a, it)
	default:
		switch {
		case k.IsRune('r'):
			a.refreshIndex()
		case k.IsRune('?'):
			a.openKeyHelp()
		default:
			return false
		}
	}
	return true
}

func (v *homeView) activate(a *App, it homeItem) {
	switch it.kind {
	case "daily":
		a.openPeriodic(periodic.Daily, 0)
	case "task":
		a.openTaskSource(*it.task)
	case "note":
		a.openNote(it.note.Path, true)
	case "folder":
		a.sidebar.collapsed[fmt.Sprintf("folder:%s:", it.folder)] = false
		a.sidebar.collapsed[fmt.Sprintf("folder:%s:%s", it.folder, it.sub)] = false
		a.sidebar.rebuild(a)
		a.sidebar.selectKey(fmt.Sprintf("folder:%s:%s", it.folder, it.sub))
		a.focus = focusSidebar
	case "week":
		if v.weekDay < len(v.week) {
			a.openPeriodicOn(periodic.Daily, v.week[v.weekDay])
		}
	case "tags":
		a.openTags("")
	case "tag":
		a.openTags(it.tag)
	}
}

func (v *homeView) menu(a *App, it homeItem) {
	switch {
	case it.note != nil:
		a.noteContextMenu(it.note.Path)
	case it.task != nil:
		a.taskMenu(*it.task)
	}
}

func (v *homeView) scrollBy(a *App, delta int) {
	v.cursor = max(0, min(max(0, len(v.items)-1), v.cursor+delta))
}

func (v *homeView) click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool) {
	y += v.scroll
	for _, hh := range v.hits {
		if y == hh.y && x >= hh.x && x < hh.x+hh.w {
			v.cursor = hh.idx
			it := v.items[hh.idx]
			if it.kind == "week" {
				cell := max(4, min(6, (hh.w)/7))
				if d := (x - hh.x) / cell; d >= 0 && d < 7 {
					v.weekDay = d
				}
			}
			switch {
			case m.Button == tea.MouseButtonRight:
				v.menu(a, it)
			case double:
				v.activate(a, it)
			case it.task != nil && x-hh.x <= 1:
				a.toggleTask(*it.task)
			case it.kind == "daily" || it.kind == "tags":
				v.activate(a, it)
			}
			return
		}
	}
}

func (v *homeView) selectionPos(a *App, p *pane) (int, int, bool) {
	for _, hh := range v.hits {
		if hh.idx == v.cursor {
			return a.contentRect(p).x + hh.x + 2, a.contentRect(p).y + hh.y - v.scroll + 1, true
		}
	}
	return 0, 0, false
}

func (v *homeView) hintTargets(a *App, p *pane) []hintTarget {
	out := []hintTarget{}
	for _, hh := range v.hits {
		idx := hh.idx
		out = append(out, hintTarget{x: a.contentRect(p).x + hh.x, y: a.contentRect(p).y + hh.y - v.scroll, run: func(a *App) {
			v.cursor = idx
			v.activate(a, v.items[idx])
		}})
	}
	return out
}

func (v *homeView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }
