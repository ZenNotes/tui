package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/zennotescli/internal/vim"
)

// Mouse support covers every surface: click to focus and select, double
// click to open or toggle, right click for the same menus the keyboard
// reaches with m, wheel scrolling, drag to select text in the editor, and
// drag on a divider to resize the sidebar or a split. It is on by default;
// [terminal] mouse = false or :mouse turns it off, which also gives the
// terminal's own text selection back (most terminals also allow it with
// Shift or Option held while the app owns the mouse).

const doubleClickWindow = 400 * time.Millisecond

// mouseState tracks clicks and drags between events.
type mouseState struct {
	lastClickAt time.Time
	lastX       int
	lastY       int
	drag        string // "", "sidebar", "divider", "select"
	dragNode    *paneNode
	dragPane    *pane
	dragBuf     *noteBuffer
	dragStart   vim.Pos
	dragVisual  bool
}

// scroller is a view that can move by rows for the wheel.
type scroller interface {
	scrollBy(a *App, delta int)
}

// clicker is a view that handles clicks; x and y are relative to the
// view's content area (below the tab bar), double marks a double click.
type clicker interface {
	click(a *App, p *pane, x, y int, m tea.MouseMsg, double bool)
}

// hitRegion names what sits under a screen cell.
type hitRegion struct {
	kind string // sidebar, sidebar-border, divider, tabbar, pane, sidepanel, statusbar, hintline, other
	pane *pane
	node *paneNode
}

func (a *App) hitTest(x, y int) hitRegion {
	main := a.mainRect()
	if y >= a.height-2 {
		if y == a.height-2 {
			return hitRegion{kind: "statusbar"}
		}
		return hitRegion{kind: "hintline"}
	}
	if a.sidebarOpen && !a.zen {
		if x < a.sidebarWidth {
			return hitRegion{kind: "sidebar"}
		}
		if x == a.sidebarWidth {
			return hitRegion{kind: "sidebar-border"}
		}
	}
	if a.sidePanelOpen() && x >= main.x+main.w {
		return hitRegion{kind: "sidepanel"}
	}
	if node := a.dividerAt(a.panes, x, y); node != nil {
		return hitRegion{kind: "divider", node: node}
	}
	if p := a.paneAt(x, y); p != nil {
		if a.tabBarHeight() > 0 && y == p.rect.y {
			return hitRegion{kind: "tabbar", pane: p}
		}
		return hitRegion{kind: "pane", pane: p}
	}
	return hitRegion{kind: "other"}
}

func (a *App) paneAt(x, y int) *pane {
	for _, p := range a.panes.leaves() {
		if x >= p.rect.x && x < p.rect.x+p.rect.w && y >= p.rect.y && y < p.rect.y+p.rect.h {
			return p
		}
	}
	return nil
}

// dividerAt finds the split whose divider line sits under a cell.
func (a *App) dividerAt(n *paneNode, x, y int) *paneNode {
	if n == nil || n.leaf != nil {
		return nil
	}
	second := n.second.leaves()[0].rect
	firstLeaves := n.first.leaves()
	if n.vertical {
		top, bottom := second.y, second.y+second.h
		for _, l := range firstLeaves {
			top = min(top, l.rect.y)
			bottom = max(bottom, l.rect.y+l.rect.h)
		}
		if x == second.x-1 && y >= top && y < bottom {
			return n
		}
	} else {
		left, right := second.x, second.x+second.w
		for _, l := range firstLeaves {
			left = min(left, l.rect.x)
			right = max(right, l.rect.x+l.rect.w)
		}
		if y == second.y-1 && x >= left && x < right {
			return n
		}
	}
	if hit := a.dividerAt(n.first, x, y); hit != nil {
		return hit
	}
	return a.dividerAt(n.second, x, y)
}

// handleMouse is the entry point for every mouse event.
func (a *App) handleMouse(m tea.MouseMsg) {
	switch m.Action {
	case tea.MouseActionRelease:
		a.mouse.drag = ""
		a.mouse.dragBuf = nil
		a.mouse.dragNode = nil
		return
	case tea.MouseActionMotion:
		a.mouseDrag(m)
		return
	}
	if a.overlay != nil {
		a.overlayMouse(m)
		return
	}
	if a.leader != nil {
		a.leader = nil
	}
	switch m.Button {
	case tea.MouseButtonWheelUp:
		a.scrollAt(m.X, m.Y, -3)
	case tea.MouseButtonWheelDown:
		a.scrollAt(m.X, m.Y, 3)
	case tea.MouseButtonLeft:
		now := time.Now()
		double := now.Sub(a.mouse.lastClickAt) < doubleClickWindow && a.mouse.lastX == m.X && a.mouse.lastY == m.Y
		a.mouse.lastClickAt, a.mouse.lastX, a.mouse.lastY = now, m.X, m.Y
		if double {
			a.mouse.lastClickAt = time.Time{}
		}
		a.mouseLeft(m, double)
	case tea.MouseButtonMiddle:
		a.mouseMiddle(m)
	case tea.MouseButtonRight:
		a.mouseRight(m)
	}
}

// --- wheel ---

func (a *App) scrollAt(x, y, delta int) {
	hit := a.hitTest(x, y)
	switch hit.kind {
	case "sidebar":
		s := a.sidebar
		s.cursor = max(0, min(max(0, len(s.rows)-1), s.cursor+delta))
	case "sidepanel":
		switch a.sidePanelFocus() {
		case focusOutline:
			a.outline.list.move(delta, len(a.outline.items))
		case focusConnections:
			a.connections.list.move(delta, len(a.connections.items))
		case focusCalendar:
			a.calendar.day = a.calendar.day.AddDate(0, 0, 7*sign(delta))
			a.calendar.month = time.Date(a.calendar.day.Year(), a.calendar.day.Month(), 1, 0, 0, 0, 0, time.Local)
		}
	case "pane", "tabbar":
		p := hit.pane
		t := p.activeTab()
		if t == nil {
			return
		}
		if t.view != nil {
			if s, ok := t.view.(scroller); ok {
				if tv, ok := t.view.(*tasksView); ok && tv.mode == "kanban" {
					tv.pointColumn(a, p, x)
				}
				s.scrollBy(a, delta)
			}
			return
		}
		buf := a.buffers[t.path]
		if buf == nil {
			return
		}
		if t.mode == modePreview && t.preview != nil {
			pv := t.preview
			n := 0
			if pv.cache != nil {
				n = len(pv.cache.lines)
			}
			pv.cursor = max(0, min(max(0, n-1), pv.cursor+delta))
			return
		}
		mode := buf.ed.Mode()
		if !a.prefs.VimMode || mode == vim.ModeInsert || mode == vim.ModeReplace || mode == vim.ModeCmdline {
			top := max(0, min(max(0, buf.ed.LineCount()-1), buf.ed.ScrollTop()+delta))
			buf.ed.SetScrollTop(top)
			return
		}
		if delta > 0 {
			buf.ed.Feed("3<C-e>")
		} else {
			buf.ed.Feed("3<C-y>")
		}
	}
}

// --- left button ---

func (a *App) mouseLeft(m tea.MouseMsg, double bool) {
	hit := a.hitTest(m.X, m.Y)
	main := a.mainRect()
	switch hit.kind {
	case "sidebar-border":
		a.mouse.drag = "sidebar"
	case "divider":
		a.mouse.drag = "divider"
		a.mouse.dragNode = hit.node
	case "sidebar":
		a.focus = focusSidebar
		row := a.sidebar.scroll + (m.Y - main.y - 2)
		if row < 0 || row >= len(a.sidebar.rows) || a.sidebar.rows[row].kind == "gap" {
			return
		}
		a.sidebar.cursor = row
		r := a.sidebar.rows[row]
		if r.kind == "folder" || r.kind == "favorite-folder" || r.kind == "section" || r.kind == "tags" || r.kind == "databases" || (r.kind == "tag" && r.expandable && !double) {
			a.sidebar.toggle(a)
			return
		}
		a.sidebar.activate(a, false)
	case "tabbar":
		a.activePane = hit.pane
		a.focus = focusPane
		if idx := hit.pane.tabAt(m.X - hit.pane.rect.x); idx >= 0 {
			hit.pane.active = idx
			a.markSessionDirty()
		} else if hit.pane.plusAt(m.X - hit.pane.rect.x) {
			a.newNoteHere()
		}
	case "pane":
		a.activePane = hit.pane
		a.focus = focusPane
		a.paneClick(hit.pane, m, double)
	case "sidepanel":
		a.sidePanelClick(m, double, main)
	}
}

func (a *App) paneClick(p *pane, m tea.MouseMsg, double bool) {
	t := p.activeTab()
	if t == nil {
		return
	}
	inner := a.contentRect(p)
	x := m.X - inner.x
	y := m.Y - inner.y
	if x < 0 || y < 0 || x >= inner.w || y >= inner.h {
		// The frame itself: focusing the pane is all a click there does.
		return
	}
	if t.view != nil {
		if c, ok := t.view.(clicker); ok {
			c.click(a, p, x, y, m, double)
		}
		return
	}
	buf := a.buffers[t.path]
	if buf == nil {
		return
	}
	if t.mode == modePreview {
		a.previewClick(t, buf, y, m, double)
		return
	}
	if t.mode == modeSplit && x > inner.w/2 {
		// Clicking the preview half of a split moves the editor cursor to
		// that block's source line.
		if t.preview != nil && t.preview.cache != nil {
			idx := t.preview.scroll + y
			if idx >= 0 && idx < len(t.preview.cache.lines) {
				buf.ed.GotoLine(t.preview.cache.lines[idx].srcLine)
			}
		}
		return
	}
	pos, ok := a.editorPosAt(p, buf, m.X, m.Y)
	if !ok {
		return
	}
	mode := buf.ed.Mode()
	if m.Ctrl || m.Alt {
		if link, ok := linkAt(buf.ed.Line(pos.Line), pos.Col); ok {
			a.followLink(link)
			return
		}
	}
	if mode == vim.ModeInsert || mode == vim.ModeReplace || mode == vim.ModeCmdline {
		buf.ed.SetCursor(pos)
		return
	}
	if a.prefs.VimMode && mode != vim.ModeNormal {
		buf.ed.HandleKey(vim.KeyEsc)
	}
	buf.ed.SetCursor(pos)
	if double && a.prefs.VimMode {
		buf.ed.Feed("viw")
		return
	}
	if a.prefs.VimMode {
		a.mouse.drag = "select"
		a.mouse.dragBuf = buf
		a.mouse.dragPane = p
		a.mouse.dragStart = pos
		a.mouse.dragVisual = false
	}
}

// editorPosAt maps a screen cell to a buffer position using the rows of
// the last render.
func (a *App) editorPosAt(p *pane, buf *noteBuffer, x, y int) (vim.Pos, bool) {
	inner := a.contentRect(p)
	textY := y - inner.y
	textX := x - inner.x
	for _, ri := range buf.rows {
		if ri.y != textY {
			continue
		}
		col := ri.segStart
		cells := 0
		runes := buf.ed.LineRunes(ri.line)
		for col < ri.segEnd && cells+runeWidth(runes[col]) <= textX-ri.x {
			cells += runeWidth(runes[col])
			col++
		}
		if col > ri.segStart && col >= ri.segEnd && ri.segEnd < len(runes) {
			col = ri.segEnd - 1
		}
		return vim.Pos{Line: ri.line, Col: col}, true
	}
	// Below the last row: end of the buffer.
	if len(buf.rows) > 0 && textY > buf.rows[len(buf.rows)-1].y {
		last := buf.ed.LineCount() - 1
		return vim.Pos{Line: last, Col: max(0, len(buf.ed.LineRunes(last))-1)}, true
	}
	return vim.Pos{}, false
}

func (a *App) previewClick(t *tab, buf *noteBuffer, y int, m tea.MouseMsg, double bool) {
	pv := t.preview
	if pv == nil || pv.cache == nil {
		return
	}
	idx := pv.scroll + y
	if idx < 0 || idx >= len(pv.cache.lines) {
		return
	}
	pv.cursor = idx
	row := pv.cache.lines[idx]
	if (m.Ctrl || m.Alt) && len(row.links) > 0 {
		a.followLink(row.links[0])
		return
	}
	if double {
		if row.task {
			a.previewToggleTask(t, buf)
			return
		}
		if len(row.links) > 0 {
			a.previewFollow(t, buf)
		}
	}
}

func (a *App) sidePanelClick(m tea.MouseMsg, double bool, main rect) {
	// The panel is framed: a gap column, the border, one cell of padding.
	x0 := main.x + main.w + 3
	y := m.Y - main.y
	switch a.sidePanelFocus() {
	case focusOutline:
		a.focus = focusOutline
		idx := a.outline.list.scroll + y - 1
		if idx >= 0 && idx < len(a.outline.items) {
			a.outline.list.cursor = idx
			if buf := a.activeBuffer(); buf != nil {
				buf.ed.GotoLine(a.outline.items[idx].Line - 1)
			}
		}
	case focusConnections:
		a.focus = focusConnections
		idx := a.connections.list.scroll + y - 1
		if idx >= 0 && idx < len(a.connections.items) {
			it := a.connections.items[idx]
			if it.header != "" {
				return
			}
			a.connections.list.cursor = idx
			if it.path != "" {
				a.openNote(it.path, true)
			} else if it.target != "" {
				a.followLink(linkTarget{kind: "wikilink", target: it.target})
			}
		}
	case focusCalendar:
		a.focus = focusCalendar
		p := a.calendar
		if day, ok := dayAtCell(p.calFirst, p.calWeeks, m.X-x0-1, y-1, a.prefs.CalendarShowWeekNumbers); ok {
			p.day = day
			if day.Month() != p.month.Month() || day.Year() != p.month.Year() {
				p.month = time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
			}
			if double {
				a.calendar.handleKey(a, vim.KeyEnter)
			}
		}
	}
}

// dayAtCell maps a cell inside a month grid (x from the grid's left edge,
// y from its title row) to a day.
func dayAtCell(first time.Time, weeks int, x, y int, weekNumbers bool) (time.Time, bool) {
	if first.IsZero() {
		return time.Time{}, false
	}
	row := y - 2
	if row < 0 || row >= weeks {
		return time.Time{}, false
	}
	if weekNumbers {
		x -= 4
	}
	if x < 0 {
		return time.Time{}, false
	}
	col := x / 5
	if col > 6 {
		return time.Time{}, false
	}
	return first.AddDate(0, 0, row*7+col), true
}

// --- middle and right buttons ---

func (a *App) mouseMiddle(m tea.MouseMsg) {
	hit := a.hitTest(m.X, m.Y)
	if hit.kind == "tabbar" {
		if idx := hit.pane.tabAt(m.X - hit.pane.rect.x); idx >= 0 {
			a.activePane = hit.pane
			a.closeTab(hit.pane, idx)
			a.layout()
		}
	}
}

func (a *App) mouseRight(m tea.MouseMsg) {
	a.menuAnchor = &point{x: m.X + 1, y: m.Y}
	defer func() { a.menuAnchor = nil }()
	hit := a.hitTest(m.X, m.Y)
	main := a.mainRect()
	switch hit.kind {
	case "sidebar":
		row := a.sidebar.scroll + (m.Y - main.y - 2)
		if row >= 0 && row < len(a.sidebar.rows) && a.sidebar.rows[row].kind != "gap" {
			a.sidebar.cursor = row
			a.focus = focusSidebar
			a.sidebar.contextMenu(a)
		}
	case "tabbar":
		idx := hit.pane.tabAt(m.X - hit.pane.rect.x)
		if idx < 0 {
			return
		}
		a.activePane = hit.pane
		hit.pane.active = idx
		a.focus = focusPane
		a.tabMenu(hit.pane, idx)
	case "pane":
		a.activePane = hit.pane
		a.focus = focusPane
		t := hit.pane.activeTab()
		if t == nil {
			return
		}
		if t.view != nil {
			if c, ok := t.view.(clicker); ok {
				c.click(a, hit.pane, m.X-hit.pane.rect.x, m.Y-hit.pane.rect.y-a.tabBarHeight(), m, false)
			}
			return
		}
		a.noteContextMenu(t.path)
	}
}

// tabMenu is the right-click menu of a tab.
func (a *App) tabMenu(p *pane, idx int) {
	t := p.tabs[idx]
	pinLabel := "Pin tab"
	if t.pinned {
		pinLabel = "Unpin tab"
	}
	items := []menuItem{
		{key: "p", label: pinLabel, run: func(a *App) { a.togglePin(p, idx) }},
		{key: "h", label: "Move left", run: func(a *App) { p.active = idx; a.moveTab(p, -1) }},
		{key: "l", label: "Move right", run: func(a *App) { p.active = idx; a.moveTab(p, 1) }},
		{sep: true},
		{key: "x", label: "Close tab", run: func(a *App) { a.closeTab(p, idx); a.layout() }},
		{key: "o", label: "Close other tabs", run: func(a *App) { p.active = idx; a.closeOtherTabs() }},
		{key: "w", label: "Open in a split", run: func(a *App) {
			p.active = idx
			a.splitPane(true)
		}},
	}
	if t.view == nil {
		items = append(items, menuItem{sep: true}, menuItem{key: "e", label: "Editor mode", run: func(a *App) { p.active = idx; a.setPaneMode(modeEdit) }},
			menuItem{key: "s", label: "Split mode", run: func(a *App) { p.active = idx; a.setPaneMode(modeSplit) }},
			menuItem{key: "p", label: "Preview mode", run: func(a *App) { p.active = idx; a.setPaneMode(modePreview) }})
	}
	a.showMenu(a.tabTitle(t), items)
}

// --- drags ---

func (a *App) mouseDrag(m tea.MouseMsg) {
	switch a.mouse.drag {
	case "sidebar":
		a.sidebarWidth = max(20, min(max(20, a.width/2), m.X))
		a.layout()
		a.markSessionDirty()
	case "divider":
		n := a.mouse.dragNode
		if n == nil || n.leaf != nil {
			return
		}
		first := n.first.leaves()[0].rect
		second := n.second.leaves()[0].rect
		if n.vertical {
			left := first.x
			right := second.x + second.w
			if right-left > 0 {
				n.ratio = float64(m.X-left) / float64(right-left)
			}
		} else {
			top := first.y
			bottom := second.y + second.h
			if bottom-top > 0 {
				n.ratio = float64(m.Y-top) / float64(bottom-top)
			}
		}
		n.ratio = max(0.15, min(0.85, n.ratio))
		a.layout()
	case "select":
		buf, p := a.mouse.dragBuf, a.mouse.dragPane
		if buf == nil || p == nil || a.buffers[buf.path] != buf {
			return
		}
		pos, ok := a.editorPosAt(p, buf, m.X, m.Y)
		if !ok || pos == a.mouse.dragStart && !a.mouse.dragVisual {
			return
		}
		if !a.mouse.dragVisual {
			buf.ed.SetCursor(a.mouse.dragStart)
			buf.ed.HandleKey(vim.R('v'))
			a.mouse.dragVisual = true
		}
		buf.ed.SetCursor(pos)
	}
}

// --- overlays ---

func (a *App) overlayMouse(m tea.MouseMsg) {
	r := a.overlayRect
	inside := m.X >= r.x && m.X < r.x+r.w && m.Y >= r.y && m.Y < r.y+r.h
	bx, by := m.X-r.x-2, m.Y-r.y-1 // border and padding
	switch ov := a.overlay.(type) {
	case *palette:
		switch m.Button {
		case tea.MouseButtonWheelUp:
			ov.cursor = max(0, ov.cursor-3)
		case tea.MouseButtonWheelDown:
			ov.cursor = min(max(0, len(ov.filtered)-1), ov.cursor+3)
		case tea.MouseButtonLeft:
			if !inside {
				a.overlay = nil
				return
			}
			idx := ov.scroll + by - 3
			if idx >= 0 && idx < len(ov.filtered) {
				ov.cursor = idx
				ov.handleKey(a, vim.KeyEnter)
			}
		}
	case *menu:
		if m.Button != tea.MouseButtonLeft {
			return
		}
		if !inside {
			a.overlay = nil
			return
		}
		idx := by - 1
		if idx >= 0 && idx < len(ov.items) && !ov.items[idx].sep {
			ov.cursor = idx
			ov.handleKey(a, vim.KeyEnter)
		}
	case *confirmDialog:
		if m.Button != tea.MouseButtonLeft || !inside {
			return
		}
		if by == 2 {
			if bx < (r.w-4)/2 {
				ov.handleKey(a, vim.R('y'))
			} else {
				ov.handleKey(a, vim.R('n'))
			}
		}
	case *keyHelpOverlay, *hintOverlay:
		if m.Button == tea.MouseButtonLeft {
			a.overlay = nil
		}
	case *prompt, *captureOverlay:
		if m.Button == tea.MouseButtonLeft && !inside {
			a.overlay = nil
		}
	}
}

// tabAt maps an x offset inside a pane's tab bar to a tab index.
func (p *pane) tabAt(x int) int {
	for i, span := range p.tabSpans {
		if x >= span[0] && x < span[1] {
			return i
		}
	}
	return -1
}

// plusAt is true over the strip's "+" that opens a new note.
func (p *pane) plusAt(x int) bool {
	return p.plusSpan[1] > p.plusSpan[0] && x >= p.plusSpan[0] && x < p.plusSpan[1]
}

// selectionAnchor is where a keyboard-opened menu should appear: just
// right of the selected row of the focused surface.
func (a *App) selectionAnchor() *point {
	main := a.mainRect()
	switch a.focus {
	case focusSidebar:
		if a.sidebarOpen && !a.zen && len(a.sidebar.rows) > 0 {
			y := main.y + 2 + a.sidebar.cursor - a.sidebar.scroll
			return &point{x: a.sidebarWidth + 2, y: y}
		}
	case focusPane:
		p := a.activePane
		t := p.activeTab()
		if t == nil {
			return nil
		}
		if t.view != nil {
			if sp, ok := t.view.(selectionPositioner); ok {
				if x, y, ok := sp.selectionPos(a, p); ok {
					return &point{x: x, y: y}
				}
			}
			return nil
		}
		if buf := a.buffers[t.path]; buf != nil && t.mode != modePreview {
			cur := buf.ed.Cursor()
			for _, ri := range buf.rows {
				if ri.line == cur.Line && cur.Col >= ri.segStart && (cur.Col < ri.segEnd || ri.segEnd == len(buf.ed.LineRunes(ri.line))) {
					inner := a.contentRect(p)
					return &point{x: inner.x + ri.x + 2, y: inner.y + ri.y + 1}
				}
			}
		}
	}
	return nil
}

// selectionPositioner views report the screen cell of their selection.
type selectionPositioner interface {
	selectionPos(a *App, p *pane) (x, y int, ok bool)
}
