package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vault"
)

// paneMode is how a note tab shows its note.
type paneMode string

const (
	modeEdit    paneMode = "edit"
	modeSplit   paneMode = "split"
	modePreview paneMode = "preview"
)

// Virtual tab paths, the same scheme the desktop uses.
const (
	tabTasks      = "zen://tasks"
	tabTags       = "zen://tags"
	tabQuickNotes = "zen://quick-notes"
	tabArchive    = "zen://archive"
	tabTrash      = "zen://trash"
	tabHelp       = "zen://help"
	tabHome       = "zen://home"
	tabSettings   = "zen://settings"
	tabDatabase   = "zen://database/"
)

func isVirtualPath(path string) bool {
	return strings.HasPrefix(path, "zen://")
}

// tab is one entry in a pane's tab strip: a note, or a built-in view.
type tab struct {
	path    string
	mode    paneMode
	preview *previewState
	view    view
	pinned  bool
}

// pane is a leaf of the split tree holding tabs.
type pane struct {
	tabs     []*tab
	active   int
	rect     rect
	closed   [][]*tab
	tabSpans [][2]int
	// plusSpan is where the strip's "+" sits, for clicks.
	plusSpan [2]int
	// tabScroll is the first tab drawn when the strip overflows.
	tabScroll int
}

type rect struct {
	x, y, w, h int
}

// paneNode is the split tree: a leaf, or two children side by side or
// stacked.
type paneNode struct {
	leaf          *pane
	vertical      bool // true: first is left, second right
	first, second *paneNode
	ratio         float64
}

func newPaneTree() *paneNode {
	return &paneNode{leaf: &pane{}}
}

func (n *paneNode) leaves() []*pane {
	if n == nil {
		return nil
	}
	if n.leaf != nil {
		return []*pane{n.leaf}
	}
	return append(n.first.leaves(), n.second.leaves()...)
}

// layout assigns rectangles to leaves.
func (n *paneNode) layout(r rect) {
	if n.leaf != nil {
		n.leaf.rect = r
		return
	}
	if n.vertical {
		firstW := int(float64(r.w) * n.ratio)
		firstW = max(10, min(r.w-10, firstW))
		n.first.layout(rect{r.x, r.y, firstW, r.h})
		n.second.layout(rect{r.x + firstW + 1, r.y, r.w - firstW - 1, r.h})
		return
	}
	firstH := int(float64(r.h) * n.ratio)
	firstH = max(3, min(r.h-3, firstH))
	n.first.layout(rect{r.x, r.y, r.w, firstH})
	n.second.layout(rect{r.x, r.y + firstH + 1, r.w, r.h - firstH - 1})
}

// split turns a leaf into two, the new one carrying a copy of the active tab.
func (n *paneNode) split(target *pane, vertical bool) *pane {
	if n.leaf == target {
		newPane := &pane{}
		if t := target.activeTab(); t != nil {
			copyTab := &tab{path: t.path, mode: t.mode, view: t.view}
			if t.preview != nil {
				pv := *t.preview
				copyTab.preview = &pv
			}
			newPane.tabs = []*tab{copyTab}
		}
		n.leaf = nil
		n.vertical = vertical
		n.ratio = 0.5
		n.first = &paneNode{leaf: target}
		n.second = &paneNode{leaf: newPane}
		return newPane
	}
	if n.first != nil {
		if p := n.first.split(target, vertical); p != nil {
			return p
		}
	}
	if n.second != nil {
		return n.second.split(target, vertical)
	}
	return nil
}

// remove drops a leaf, collapsing its parent.
func (n *paneNode) remove(target *pane) bool {
	if n.leaf != nil {
		return false
	}
	if n.first.leaf == target {
		*n = *n.second
		return true
	}
	if n.second.leaf == target {
		*n = *n.first
		return true
	}
	return n.first.remove(target) || n.second.remove(target)
}

func (p *pane) activeTab() *tab {
	if len(p.tabs) == 0 {
		return nil
	}
	if p.active >= len(p.tabs) {
		p.active = len(p.tabs) - 1
	}
	return p.tabs[p.active]
}

func (p *pane) findTab(path string) int {
	for i, t := range p.tabs {
		if t.path == path {
			return i
		}
	}
	return -1
}

// neighbor finds the pane next to p in a direction by geometry.
func (n *paneNode) neighbor(p *pane, dir rune) *pane {
	var best *pane
	bestDist := 1 << 30
	cx, cy := p.rect.x+p.rect.w/2, p.rect.y+p.rect.h/2
	for _, other := range n.leaves() {
		if other == p {
			continue
		}
		r := other.rect
		ok := false
		dist := 0
		switch dir {
		case 'h':
			ok = r.x+r.w <= p.rect.x && r.y < p.rect.y+p.rect.h && r.y+r.h > p.rect.y
			dist = p.rect.x - (r.x + r.w)
		case 'l':
			ok = r.x >= p.rect.x+p.rect.w && r.y < p.rect.y+p.rect.h && r.y+r.h > p.rect.y
			dist = r.x - (p.rect.x + p.rect.w)
		case 'k':
			ok = r.y+r.h <= p.rect.y && r.x < p.rect.x+p.rect.w && r.x+r.w > p.rect.x
			dist = p.rect.y - (r.y + r.h)
		case 'j':
			ok = r.y >= p.rect.y+p.rect.h && r.x < p.rect.x+p.rect.w && r.x+r.w > p.rect.x
			dist = r.y - (p.rect.y + p.rect.h)
		}
		if !ok {
			continue
		}
		dist = dist*1000 + abs((r.x+r.w/2)-cx) + abs((r.y+r.h/2)-cy)
		if dist < bestDist {
			best, bestDist = other, dist
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// --- app-level tab operations ---

// openNote shows a note in the active pane, reusing its tab when open.
func (a *App) openNote(path string, focus bool) {
	pane := a.activePane
	if pane == nil {
		return
	}
	if idx := pane.findTab(path); idx >= 0 {
		pane.active = idx
	} else {
		if _, err := a.openBuffer(path); err != nil {
			a.notifyError(err.Error())
			return
		}
		mode := paneMode(a.prefs.DefaultPaneMode)
		if mode == "" {
			mode = modeEdit
		}
		if remembered, ok := a.noteModes[path]; ok && !a.prefs.KeepViewModeAcrossNotes() {
			mode = remembered
		}
		t := &tab{path: path, mode: mode}
		pane.tabs = append(pane.tabs, t)
		pane.active = len(pane.tabs) - 1
	}
	if cur := a.activeBuffer(); cur != nil && cur.path != path {
		a.pushJump(cur.path, cur.ed.Cursor())
	}
	a.touchRecent(path)
	if focus {
		a.focus = focusPane
	}
	a.markSessionDirty()
}

// openVirtual shows a built-in view in the active pane.
func (a *App) openVirtual(path string, factory func() view) {
	pane := a.activePane
	if idx := pane.findTab(path); idx >= 0 {
		pane.active = idx
	} else {
		v := factory()
		pane.tabs = append(pane.tabs, &tab{path: path, view: v})
		pane.active = len(pane.tabs) - 1
	}
	if t := pane.activeTab(); t != nil && t.view != nil {
		t.view.refresh(a)
	}
	a.focus = focusPane
	a.markSessionDirty()
}

// closeTab closes a tab, saving its note first.
func (a *App) closeTab(p *pane, idx int) {
	if idx < 0 || idx >= len(p.tabs) {
		return
	}
	t := p.tabs[idx]
	if t.pinned {
		a.notify("Pinned tab: :pin again to unpin it first")
		return
	}
	if !isVirtualPath(t.path) {
		if buf, ok := a.buffers[t.path]; ok {
			if err := a.saveBuffer(buf); err != nil {
				a.notifyError(err.Error())
				return
			}
		}
	}
	a.closedTabs = append(a.closedTabs, closedTab{path: t.path, mode: t.mode, index: idx, pane: p})
	p.tabs = append(p.tabs[:idx], p.tabs[idx+1:]...)
	if p.active >= len(p.tabs) {
		p.active = max(0, len(p.tabs)-1)
	} else if p.active > idx {
		p.active--
	}
	a.closeBufferIfUnused(t.path)
	if len(p.tabs) == 0 && len(a.panes.leaves()) > 1 {
		a.panes.remove(p)
		leaves := a.panes.leaves()
		a.activePane = leaves[0]
	}
	a.markSessionDirty()
}

type closedTab struct {
	path  string
	mode  paneMode
	index int
	pane  *pane
}

func (a *App) reopenClosedTab() {
	if len(a.closedTabs) == 0 {
		a.notify("No closed tab to reopen")
		return
	}
	last := a.closedTabs[len(a.closedTabs)-1]
	a.closedTabs = a.closedTabs[:len(a.closedTabs)-1]
	if isVirtualPath(last.path) {
		a.openVirtualByPath(last.path)
		return
	}
	a.openNote(last.path, true)
	if t := a.activePane.activeTab(); t != nil {
		t.mode = last.mode
	}
}

// activeTab is the active pane's active tab.
func (a *App) activeTab() *tab {
	if a.activePane == nil {
		return nil
	}
	return a.activePane.activeTab()
}

// activeBuffer is the note buffer under the cursor, if the active tab is a note.
func (a *App) activeBuffer() *noteBuffer {
	t := a.activeTab()
	if t == nil || isVirtualPath(t.path) {
		return nil
	}
	return a.buffers[t.path]
}

// touchRecent moves a note to the front of the recently used list.
func (a *App) touchRecent(path string) {
	out := []string{path}
	for _, p := range a.recent {
		if p != path {
			out = append(out, p)
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	a.recent = out
}

// cycleTab moves through the active pane's tabs, falling back to recent
// notes when it holds only one.
func (a *App) cycleTab(delta int) {
	p := a.activePane
	if len(p.tabs) > 1 {
		p.active = ((p.active+delta)%len(p.tabs) + len(p.tabs)) % len(p.tabs)
		if t := p.activeTab(); t != nil && !isVirtualPath(t.path) {
			a.touchRecent(t.path)
		}
		a.markSessionDirty()
		return
	}
	if len(a.recent) > 1 {
		cur := ""
		if t := p.activeTab(); t != nil {
			cur = t.path
		}
		idx := 0
		for i, r := range a.recent {
			if r == cur {
				idx = i
			}
		}
		next := a.recent[((idx+delta)%len(a.recent)+len(a.recent))%len(a.recent)]
		if _, ok := a.noteMeta(next); ok {
			a.openNote(next, true)
		}
	}
}

// selectTab jumps to tab n (1-based) counting across panes.
func (a *App) selectTab(n int) {
	count := 0
	for _, p := range a.panes.leaves() {
		for i := range p.tabs {
			count++
			if count == n {
				a.activePane = p
				p.active = i
				a.focus = focusPane
				return
			}
		}
	}
	// Past the end lands on the last tab.
	leaves := a.panes.leaves()
	if len(leaves) > 0 {
		last := leaves[len(leaves)-1]
		if len(last.tabs) > 0 {
			a.activePane = last
			last.active = len(last.tabs) - 1
		}
	}
}

func (a *App) toggleRecentNote() {
	if len(a.recent) < 2 {
		return
	}
	a.openNote(a.recent[1], true)
}

// renderTabBar draws a pane's tab strip: pinned tabs first, padded labels
// with the active tab as a filled block and the others dimmed, a dot on
// unsaved notes, a "+" that opens a new note, and overflow arrows that
// keep the active tab in view.
func (a *App) renderTabBar(p *pane, width int, focused bool) string {
	if !a.prefs.TabsEnabled() {
		return ""
	}
	th := a.theme
	labels := make([]string, len(p.tabs))
	widths := make([]int, len(p.tabs))
	for i, t := range p.tabs {
		label := truncateCells(a.tabTitle(t), 24)
		switch t.mode {
		case modePreview:
			label = "◫ " + label
		case modeSplit:
			label = "◧ " + label
		}
		if t.pinned {
			label = "◆ " + label
		}
		if buf, ok := a.buffers[t.path]; ok && buf.dirty() {
			label += " ●"
		}
		labels[i] = "  " + label + "  "
		widths[i] = cellWidth(labels[i])
	}
	const plusW = 3
	total := plusW
	for _, w := range widths {
		total += w
	}
	avail := width
	if total > width {
		avail = width - 4 // room for the arrows
	}
	if p.tabScroll > p.active {
		p.tabScroll = p.active
	}
	for {
		used := 0
		for i := p.tabScroll; i <= p.active && i < len(widths); i++ {
			used += widths[i]
		}
		if used <= avail || p.tabScroll >= p.active {
			break
		}
		p.tabScroll++
	}
	if total <= width {
		p.tabScroll = 0
	}
	p.tabSpans = p.tabSpans[:0]
	p.plusSpan = [2]int{0, 0}
	var b strings.Builder
	x := 0
	if p.tabScroll > 0 {
		b.WriteString(th.Muted.Render(" ‹"))
		x += 2
	}
	shownEnd := len(p.tabs)
	for i := 0; i < len(p.tabs); i++ {
		if i < p.tabScroll {
			p.tabSpans = append(p.tabSpans, [2]int{-1, -1})
			continue
		}
		if x+widths[i] > width-boolInt(total > width)*2 {
			shownEnd = i
			break
		}
		style := th.TabInactive
		switch {
		case i == p.active && focused:
			style = th.TabActive
		case i == p.active:
			style = lipgloss.NewStyle().Background(th.BgSelected).Foreground(th.Fg).Bold(true)
		}
		b.WriteString(style.Render(labels[i]))
		p.tabSpans = append(p.tabSpans, [2]int{x, x + widths[i]})
		x += widths[i]
	}
	for i := shownEnd; i < len(p.tabs); i++ {
		p.tabSpans = append(p.tabSpans, [2]int{-1, -1})
	}
	if shownEnd == len(p.tabs) && x+plusW <= width {
		b.WriteString(th.Muted.Render(" + "))
		p.plusSpan = [2]int{x, x + plusW}
		x += plusW
	}
	line := b.String()
	if shownEnd < len(p.tabs) {
		line = padRight(line, width-2) + th.Muted.Render("› ")
	}
	return padRight(line, width)
}

// togglePin pins or unpins a tab; pinned tabs sit at the left of the
// strip and survive "close others".
func (a *App) togglePin(p *pane, idx int) {
	if idx < 0 || idx >= len(p.tabs) {
		return
	}
	t := p.tabs[idx]
	t.pinned = !t.pinned
	a.reorderPinned(p, t)
	if t.pinned {
		a.notify("Pinned " + a.tabTitle(t))
	} else {
		a.notify("Unpinned " + a.tabTitle(t))
	}
	a.markSessionDirty()
}

// reorderPinned moves pinned tabs to the front, keeping relative order.
func (a *App) reorderPinned(p *pane, keepActive *tab) {
	pinned := []*tab{}
	rest := []*tab{}
	for _, t := range p.tabs {
		if t.pinned {
			pinned = append(pinned, t)
		} else {
			rest = append(rest, t)
		}
	}
	p.tabs = append(pinned, rest...)
	for i, t := range p.tabs {
		if t == keepActive {
			p.active = i
		}
	}
}

// moveTab shifts the active tab left or right inside its group.
func (a *App) moveTab(p *pane, delta int) {
	i := p.active
	j := i + delta
	if j < 0 || j >= len(p.tabs) || p.tabs[i].pinned != p.tabs[j].pinned {
		return
	}
	p.tabs[i], p.tabs[j] = p.tabs[j], p.tabs[i]
	p.active = j
	a.markSessionDirty()
}

// tabTitle names a tab.
func (a *App) tabTitle(t *tab) string {
	if t.view != nil {
		return t.view.title()
	}
	if meta, ok := a.noteMeta(t.path); ok {
		return meta.Title
	}
	base := t.path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".md")
}

var _ = lipgloss.Left
var _ = vault.FolderInbox
