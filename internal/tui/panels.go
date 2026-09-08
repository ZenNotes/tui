package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// --- Outline ---

type outlinePanel struct {
	items []vault.OutlineItem
	list  listCursor
	path  string
	rows  int
}

func (p *outlinePanel) refresh(a *App) {
	buf := a.activeBuffer()
	if buf == nil {
		p.items, p.path = nil, ""
		return
	}
	p.path = buf.path
	p.items = vault.Outline(buf.ed.Text())
	p.list.clamp(len(p.items))
}

func (p *outlinePanel) render(a *App, w, h int) string {
	th := a.theme
	p.refresh(a)
	lines := []string{padRight(th.Title.Render(" Outline"), w)}
	p.rows = h - 1
	if len(p.items) == 0 {
		lines = append(lines, padRight(" "+th.Muted.Render("No headings"), w))
	}
	cur := -1
	if buf := a.activeBuffer(); buf != nil {
		line := buf.ed.Cursor().Line + 1
		for i, it := range p.items {
			if it.Line <= line {
				cur = i
			}
		}
	}
	p.list.ensureVisible(p.rows)
	for i := p.list.scroll; i < len(p.items) && len(lines) < h; i++ {
		it := p.items[i]
		indent := strings.Repeat("  ", max(0, it.Level-1))
		text := " " + indent + truncateCells(it.Text, w-2-cellWidth(indent))
		row := padRight(th.Base.Render(text), w)
		if i == cur {
			row = padRight(th.Base.Foreground(th.Accent).Render(text), w)
		}
		if a.focus == focusOutline && i == p.list.cursor {
			row = th.SelectedFocus.Render(padRight(text, w))
		}
		lines = append(lines, row)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (p *outlinePanel) handleKey(a *App, k vim.Key) {
	if k.Is("esc") {
		a.focus = focusPane
		return
	}
	if a.listNav(&p.list, k, len(p.items), p.rows) {
		return
	}
	if k.Is("enter") || (a.listKeyAllowed(k) && (k.IsRune('l') || k.IsRune('o'))) {
		if p.list.cursor < len(p.items) {
			if buf := a.activeBuffer(); buf != nil {
				buf.ed.GotoLine(p.items[p.list.cursor].Line - 1)
				a.focus = focusPane
			}
		}
		return
	}
	if a.listKeyAllowed(k) {
		switch {
		case k.IsRune('h'):
			a.focus = focusPane
		case k.IsRune('q'):
			a.closeSidePanels()
		case k.IsRune(':'):
			a.openLocalEx()
		}
	}
}

// --- Connections ---

type connectionsPanel struct {
	path      string
	backlinks []vault.NoteMeta
	outgoing  []vault.NoteMeta
	missing   []string
	items     []connectionItem
	list      listCursor
	rows      int
}

type connectionItem struct {
	header string
	path   string
	label  string
	target string
}

func (p *connectionsPanel) refresh(a *App) {
	buf := a.activeBuffer()
	if buf == nil {
		p.items = nil
		return
	}
	p.path = buf.path
	links, err := a.backend.Backlinks(context.Background(), buf.path)
	if err != nil {
		links = nil
	}
	p.backlinks = links
	p.outgoing, p.missing = nil, nil
	seen := map[string]bool{}
	for _, l := range vault.ExtractWikilinks(buf.ed.Text()) {
		target := strings.TrimPrefix(l, "!")
		if i := strings.IndexAny(target, "|#"); i >= 0 {
			target = target[:i]
		}
		target = strings.TrimSpace(target)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		if a.idx == nil {
			continue
		}
		if meta, ok := vault.ResolveWikilink(a.idx.notes, target); ok {
			p.outgoing = append(p.outgoing, meta)
		} else {
			p.missing = append(p.missing, target)
		}
	}
	p.items = nil
	if len(p.backlinks) > 0 {
		p.items = append(p.items, connectionItem{header: fmt.Sprintf("Linked from (%d)", len(p.backlinks))})
		for _, n := range p.backlinks {
			p.items = append(p.items, connectionItem{path: n.Path, label: n.Title})
		}
	}
	if len(p.outgoing) > 0 {
		p.items = append(p.items, connectionItem{header: fmt.Sprintf("Links to (%d)", len(p.outgoing))})
		for _, n := range p.outgoing {
			p.items = append(p.items, connectionItem{path: n.Path, label: n.Title})
		}
	}
	if len(p.missing) > 0 {
		p.items = append(p.items, connectionItem{header: fmt.Sprintf("Unresolved (%d)", len(p.missing))})
		for _, m := range p.missing {
			p.items = append(p.items, connectionItem{target: m, label: m})
		}
	}
	p.list.clamp(len(p.items))
	if p.list.cursor < len(p.items) && p.items[p.list.cursor].header != "" && len(p.items) > 1 {
		p.list.cursor = 1
	}
}

func (p *connectionsPanel) render(a *App, w, h int) string {
	th := a.theme
	lines := []string{padRight(th.Title.Render(" Connections"), w)}
	p.rows = h - 1
	if len(p.items) == 0 {
		lines = append(lines, padRight(" "+th.Muted.Render("No links yet"), w))
	}
	p.list.ensureVisible(p.rows)
	for i := p.list.scroll; i < len(p.items) && len(lines) < h; i++ {
		it := p.items[i]
		if it.header != "" {
			lines = append(lines, padRight(th.Bold.Render(" "+it.header), w))
			continue
		}
		text := "   " + truncateCells(it.label, w-4)
		row := padRight(th.Base.Render(text), w)
		if it.target != "" {
			row = padRight(th.Dim.Render(text), w)
		}
		if a.focus == focusConnections && i == p.list.cursor {
			row = th.SelectedFocus.Render(padRight(text, w))
		}
		lines = append(lines, row)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (p *connectionsPanel) handleKey(a *App, k vim.Key) {
	if k.Is("esc") {
		a.focus = focusPane
		return
	}
	n := len(p.items)
	before := p.list.cursor
	if a.listNav(&p.list, k, n, p.rows) {
		dir := 1
		if p.list.cursor < before {
			dir = -1
		}
		for p.list.cursor >= 0 && p.list.cursor < n && p.items[p.list.cursor].header != "" {
			next := p.list.cursor + dir
			if next < 0 || next >= n {
				break
			}
			p.list.cursor = next
		}
		return
	}
	if k.Is("enter") || (a.listKeyAllowed(k) && (k.IsRune('l') || k.IsRune('o'))) {
		if p.list.cursor < n {
			it := p.items[p.list.cursor]
			if it.path != "" {
				a.openNote(it.path, true)
			} else if it.target != "" {
				a.followLink(linkTarget{kind: "wikilink", target: it.target})
			}
		}
		return
	}
	if a.listKeyAllowed(k) {
		switch {
		case k.IsRune('h'):
			a.focus = focusPane
		case k.IsRune('q'):
			a.closeSidePanels()
		case k.IsRune(':'):
			a.openLocalEx()
		}
	}
}

// --- Calendar panel ---

type calendarPanel struct {
	month    time.Time
	day      time.Time
	hasDaily map[string]string
	tasks    map[string][]vault.Task
	calFirst time.Time
	calWeeks int
}

func newCalendarPanel(now time.Time) *calendarPanel {
	return &calendarPanel{month: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local), day: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)}
}

func (p *calendarPanel) refresh(a *App) {
	p.hasDaily = map[string]string{}
	if a.idx == nil {
		return
	}
	for _, n := range a.idx.notes {
		if n.Folder != vault.FolderInbox {
			continue
		}
		_, sub := a.folderOfPath(n.Path)
		if d, ok := periodic.DateOf(periodic.Daily, sub, n.Title, a.idx.settings, a.primaryAtRoot()); ok {
			p.hasDaily[d.Format("2006-01-02")] = n.Path
		}
	}
	p.tasks = vault.BucketTasksByDueDate(a.displayTasks())
}

func (p *calendarPanel) render(a *App, w, h int) string {
	th := a.theme
	if p.hasDaily == nil {
		p.refresh(a)
	}
	grid, first, weeks := renderMonthGrid(a, p.month, p.day, a.prefs.CalendarWeekStart != "sunday", a.prefs.CalendarShowWeekNumbers, func(day time.Time) (string, lipgloss.Style) {
		key := day.Format("2006-01-02")
		open := 0
		for _, t := range p.tasks[key] {
			if vault.IsTaskOpen(t) {
				open++
			}
		}
		_, has := p.hasDaily[key]
		switch {
		case open > 0 && has:
			return "•" + fmt.Sprint(min(9, open)), th.Marker
		case open > 0:
			return fmt.Sprint(min(9, open)), th.Marker
		case has:
			return "•", th.Base.Foreground(th.Accent)
		}
		return "", th.Base
	}, a.focus == focusCalendar)
	p.calFirst, p.calWeeks = first, weeks
	lines := []string{padRight(th.Title.Render(" Calendar"), w)}
	for _, gl := range strings.Split(grid, "\n") {
		lines = append(lines, padRight(" "+gl, w))
	}
	lines = append(lines, "")
	key := p.day.Format("2006-01-02")
	if path, ok := p.hasDaily[key]; ok {
		if meta, ok := a.noteMeta(path); ok {
			lines = append(lines, padRight(" "+th.Base.Foreground(th.Accent).Render(truncateCells(meta.Title, w-2)), w))
		}
	} else {
		lines = append(lines, padRight(" "+th.Muted.Render("No daily note. Enter creates it."), w))
	}
	for i, t := range p.tasks[key] {
		if len(lines) >= h || i > 12 {
			break
		}
		glyph := "☐"
		style := th.Base
		if t.Checked {
			glyph = "☑"
			style = th.Done
		}
		lines = append(lines, padRight(" "+th.Checkbox.Render(glyph)+" "+style.Render(truncateCells(t.Content, w-4)), w))
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (p *calendarPanel) handleKey(a *App, k vim.Key) {
	if k.Is("esc") {
		a.focus = focusPane
		return
	}
	move := func(d time.Time) {
		p.day = d
		if d.Month() != p.month.Month() || d.Year() != p.month.Year() {
			p.month = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.Local)
		}
	}
	switch {
	case k.Is("left") || (a.listKeyAllowed(k) && k.IsRune('h')):
		move(p.day.AddDate(0, 0, -1))
	case k.Is("right") || (a.listKeyAllowed(k) && k.IsRune('l')):
		move(p.day.AddDate(0, 0, 1))
	case k.Is("down") || (a.listKeyAllowed(k) && k.IsRune('j')):
		move(p.day.AddDate(0, 0, 7))
	case k.Is("up") || (a.listKeyAllowed(k) && k.IsRune('k')):
		move(p.day.AddDate(0, 0, -7))
	case k.Is("pgup") || (a.listKeyAllowed(k) && (k.IsRune('[') || k.IsRune('H'))):
		p.month = p.month.AddDate(0, -1, 0)
		move(p.day.AddDate(0, -1, 0))
	case k.Is("pgdn") || (a.listKeyAllowed(k) && (k.IsRune(']') || k.IsRune('L'))):
		p.month = p.month.AddDate(0, 1, 0)
		move(p.day.AddDate(0, 1, 0))
	case a.listKeyAllowed(k) && (k.IsRune('t') || k.IsRune('T')):
		now := time.Now()
		p.month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		p.day = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	case k.Is("enter") || (a.listKeyAllowed(k) && k.IsRune('o')):
		a.openPeriodicOn(periodic.Daily, p.day)
	case a.listKeyAllowed(k) && k.IsRune('n'):
		day := p.day
		a.promptFor("New task for "+day.Format("2006-01-02"), "", "", func(a *App, text string) {
			if strings.TrimSpace(text) != "" {
				a.addTaskToDaily(text, day)
			}
		})
	case a.listKeyAllowed(k) && k.IsRune('w'):
		a.openPeriodicOn(periodic.Weekly, p.day)
	case a.listKeyAllowed(k) && k.IsRune('m'):
		a.openPeriodicOn(periodic.Monthly, p.day)
	case a.listKeyAllowed(k) && k.IsRune('q'):
		a.closeSidePanels()
	case a.listKeyAllowed(k) && k.IsRune(':'):
		a.openLocalEx()
	}
}

// renderSidePanel draws whichever side panel is open.
// renderSidePanel frames the open side panel; the panel's own heading
// moves into the frame's top edge so the rows keep their positions.
func (a *App) renderSidePanel(w, h int) string {
	if h < 3 || w < 8 {
		return fitBlock("", w, h)
	}
	title := ""
	var body string
	innerW, innerH := w-4, h-2
	switch {
	case a.outlineOpen:
		title, body = "outline", a.outline.render(a, innerW, innerH+1)
	case a.connectionsOpen:
		title, body = "links", a.connections.render(a, innerW, innerH+1)
	case a.calendarOpen:
		title, body = "calendar", a.calendar.render(a, innerW, innerH+1)
	default:
		return fitBlock("", w, h)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	focused := a.focus == focusOutline || a.focus == focusConnections || a.focus == focusCalendar
	return frameBox(a.theme, title, fitBlock(strings.Join(lines, "\n"), innerW, innerH), w, h, focused)
}
