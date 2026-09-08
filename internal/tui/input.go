package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/zennotescli/internal/vim"
)

type leaderHintMsg struct{ seq int }

// leaderState is a leader sequence in progress.
type leaderState struct {
	seq       int
	keys      []vim.Key
	node      *leaderNode
	showHints bool
}

func (l *leaderState) pendingLabel() string {
	parts := []string{}
	for _, k := range l.keys {
		parts = append(parts, keyLabel(k))
	}
	return strings.Join(parts, " ")
}

// handleKey routes one key: overlay first, then a leader or pane prefix in
// progress, then the focused surface.
func (a *App) handleKey(k vim.Key) tea.Cmd {
	if a.message != "" && !(k.Is("esc") && a.overlay == nil) {
		a.message, a.messageErr = "", false
	}
	if a.overlay != nil {
		a.overlay.handleKey(a, k)
		return a.afterKey()
	}
	if a.leader != nil {
		a.leaderKey(k)
		return a.afterKey()
	}
	if a.paneKeys {
		a.paneKeys = false
		a.paneCommand(k)
		return a.afterKey()
	}
	if k.IsCtrl('c') {
		if buf := a.activeBuffer(); buf != nil && a.focus == focusPane && buf.ed.Mode() != vim.ModeNormal {
			buf.ed.HandleKey(vim.KeyEsc)
			return a.afterKey()
		}
		a.quit()
		return a.afterKey()
	}
	buf := a.activeBuffer()
	editing := false
	if buf != nil && a.focus == focusPane {
		m := buf.ed.Mode()
		editing = m == vim.ModeInsert || m == vim.ModeReplace || m == vim.ModeCmdline || !a.prefs.VimMode
	}
	if editing {
		if a.chordKey(k, true) {
			return a.afterKey()
		}
		buf.ed.HandleKey(k)
		if k.IsRune('[') {
			a.maybeWikilinkCompletion(buf)
		}
		return a.afterKey()
	}
	if a.chordKey(k, false) {
		return a.afterKey()
	}
	if a.prefs.VimMode && a.isLeaderKey(k) {
		if buf == nil || a.focus != focusPane || buf.ed.Pending() == "" {
			a.startLeader()
			return a.afterKey()
		}
	}
	switch a.focus {
	case focusSidebar:
		a.sidebar.handleKey(a, k)
	case focusOutline:
		a.outline.handleKey(a, k)
	case focusConnections:
		a.connections.handleKey(a, k)
	case focusCalendar:
		a.calendar.handleKey(a, k)
	default:
		a.paneKey(k)
	}
	return a.afterKey()
}

// afterKey flushes queued commands and the quit signal.
func (a *App) afterKey() tea.Cmd {
	if a.quitting {
		return tea.Quit
	}
	if a.leader != nil && !a.leader.showHints && a.prefs.WhichKeyHints && a.leader.seq != a.leaderHintScheduled {
		a.leaderHintScheduled = a.leader.seq
		seq := a.leader.seq
		delay := time.Duration(a.prefs.WhichKeyHintTimeoutMs) * time.Millisecond
		if a.prefs.WhichKeyHintMode != "timed" {
			delay = 0
		}
		a.queue(tea.Tick(delay, func(time.Time) tea.Msg { return leaderHintMsg{seq: seq} }))
	}
	if len(a.pendingCmds) == 0 {
		return nil
	}
	cmds := a.pendingCmds
	a.pendingCmds = nil
	return tea.Batch(cmds...)
}

func (a *App) queue(cmd tea.Cmd) {
	if cmd != nil {
		a.pendingCmds = append(a.pendingCmds, cmd)
	}
}

// refreshIndex reloads notes, folders and tasks in the background.
func (a *App) refreshIndex() {
	a.queue(func() tea.Msg { return a.loadIndexCmd()() })
}

func (a *App) quit() {
	if err := a.saveAllBuffers(); err != nil {
		a.notifyError("Save failed: " + err.Error())
		return
	}
	a.quitting = true
}

func (a *App) isLeaderKey(k vim.Key) bool {
	leader := a.keymap.Binding("vim.leaderPrefix")
	if leader == "" {
		return false
	}
	keys := bindingKeys(leader)
	return len(keys) == 1 && sameKey(k, keys[0])
}

// bound reports whether k is the (single-chord) binding of an action.
func (a *App) bound(k vim.Key, id string) bool {
	keys := bindingKeys(a.keymap.Binding(id))
	return len(keys) == 1 && sameKey(k, keys[0])
}

// chordKey handles bindings that hold everywhere: modifier chords. With
// editing true only the chords safe inside insert mode apply.
func (a *App) chordKey(k vim.Key, editing bool) bool {
	if !k.Ctrl && !k.Alt {
		return false
	}
	switch {
	case a.bound(k, "global.focusPaneLeft"):
		a.focusDirection('h')
	case a.bound(k, "global.focusPaneDown"):
		a.focusDirection('j')
	case a.bound(k, "global.focusPaneUp"):
		a.focusDirection('k')
	case a.bound(k, "global.focusPaneRight"):
		a.focusDirection('l')
	case a.bound(k, "global.toggleWordWrap"):
		a.prefs.WordWrap = !a.prefs.WordWrap
		a.notify(map[bool]string{true: "Word wrap on", false: "Word wrap off"}[a.prefs.WordWrap])
	case a.bound(k, "global.toggleZenMode"):
		a.toggleZen()
	case a.bound(k, "global.toggleSidebar"):
		a.toggleSidebar()
	case a.bound(k, "global.toggleConnections"):
		a.toggleSidePanel(&a.connectionsOpen, focusConnections)
	case a.bound(k, "global.toggleOutlinePanel"):
		a.toggleSidePanel(&a.outlineOpen, focusOutline)
	case a.bound(k, "global.modeEdit"):
		a.setPaneMode(modeEdit)
	case a.bound(k, "global.modeSplit"):
		a.setPaneMode(modeSplit)
	case a.bound(k, "global.modePreview"):
		a.setPaneMode(modePreview)
	case a.bound(k, "global.historyBack"):
		a.jumpBack()
	case a.bound(k, "global.historyForward"):
		a.jumpForward()
	case a.bound(k, "editor.toggleCheckbox"):
		if buf := a.activeBuffer(); buf != nil && a.focus == focusPane {
			buf.ed.ToggleCheckboxAtCursor()
		}
	case a.bound(k, "editor.reflowParagraph"):
		if buf := a.activeBuffer(); buf != nil && a.focus == focusPane {
			buf.ed.ReflowParagraph()
		}
	case k.IsCtrl('s'):
		a.saveNow()
	case k.IsCtrl('g'):
		a.openCommandPalette()
	case k.IsCtrl('z'):
		a.queue(tea.Suspend)
	case a.tabSelectChord(k):
	case editing && a.prefs.VimMode:
		// Insert mode keeps Ctrl+P/N (completion) and the rest for Vim.
		return false
	case a.bound(k, "global.searchNotes"):
		a.openNoteSearch("")
	case a.bound(k, "global.commandPalette"):
		a.openCommandPalette()
	case a.bound(k, "global.newNoteHere"):
		a.newNoteHere()
	case a.bound(k, "global.newQuickNote"):
		a.newQuickNote()
	case a.bound(k, "global.openSettings"):
		a.openSettings()
	case a.bound(k, "global.toggleRecentNote"):
		a.toggleRecentNote()
	case a.bound(k, "global.reopenClosedTab"):
		a.reopenClosedTab()
	case !a.prefs.VimMode && a.bound(k, "global.closeActiveTab"):
		a.closeTab(a.activePane, a.activePane.active)
	case a.prefs.VimMode && a.bound(k, "vim.panePrefix"):
		a.paneKeys = true
	case a.prefs.VimMode && a.bound(k, "vim.historyBack"):
		a.jumpBack()
	case a.prefs.VimMode && a.bound(k, "vim.historyForward"):
		a.jumpForward()
	default:
		return false
	}
	return true
}

func (a *App) tabSelectChord(k vim.Key) bool {
	ids := []string{"tabs.select1", "tabs.select2", "tabs.select3", "tabs.select4", "tabs.select5", "tabs.select6", "tabs.select7", "tabs.select8", "tabs.select9"}
	for i, id := range ids {
		if a.bound(k, id) {
			a.selectTab(i + 1)
			return true
		}
	}
	return false
}

// paneKey is a key for the active pane's content.
func (a *App) paneKey(k vim.Key) {
	t := a.activeTab()
	if t == nil {
		if a.prefs.VimMode && k.IsRune('?') {
			a.openHelp()
			return
		}
		if k.Is("esc") {
			return
		}
		return
	}
	if t.view != nil {
		if k.Is("esc") && len(a.navKeys) > 0 {
			a.navKeys = nil
			return
		}
		t.view.handleKey(a, k)
		return
	}
	buf := a.buffers[t.path]
	if buf == nil {
		return
	}
	if t.mode == modePreview {
		a.previewKey(t, buf, k)
		return
	}
	a.editorKey(buf, k)
}

// editorKey feeds a normal or visual mode key, first checking the app's own
// multi-key sequences (`gt`, `]b`) which the engine would otherwise own.
func (a *App) editorKey(buf *noteBuffer, k vim.Key) {
	if a.prefs.VimMode && (k.Is("tab") && !k.Shift) && buf.ed.Pending() == "" && a.bound(vim.Key{Rune: 'i', Ctrl: true}, "vim.historyForward") {
		a.jumpForward()
		return
	}
	seqIDs := []string{"vim.bufferPrevious", "vim.bufferNext", "vim.tabPrevious", "vim.tabNext"}
	if len(a.navKeys) > 0 {
		seq := append(append([]vim.Key{}, a.navKeys...), k)
		a.navKeys = nil
		for _, id := range seqIDs {
			complete, _ := matchSequence(seq, bindingKeys(a.keymap.Binding(id)))
			if complete {
				count, hasCount := buf.ed.PendingCount()
				buf.ed.ResetPending()
				a.runSequenceAction(id, count, hasCount)
				return
			}
		}
		for _, kk := range seq {
			buf.ed.HandleKey(kk)
		}
		return
	}
	if _, digits := buf.ed.PendingCount(); buf.ed.Pending() == "" || digits {
		for _, id := range seqIDs {
			keys := bindingKeys(a.keymap.Binding(id))
			if len(keys) > 1 && sameKey(k, keys[0]) {
				a.navKeys = []vim.Key{k}
				return
			}
			if len(keys) == 1 && sameKey(k, keys[0]) {
				count, hasCount := buf.ed.PendingCount()
				buf.ed.ResetPending()
				a.runSequenceAction(id, count, hasCount)
				return
			}
		}
	}
	buf.ed.HandleKey(k)
}

func (a *App) runSequenceAction(id string, count int, hasCount bool) {
	if !hasCount || count < 1 {
		count = 1
	}
	switch id {
	case "vim.bufferPrevious":
		for i := 0; i < count; i++ {
			a.cycleTab(-1)
		}
	case "vim.bufferNext":
		for i := 0; i < count; i++ {
			a.cycleTab(1)
		}
	case "vim.tabPrevious":
		for i := 0; i < count; i++ {
			a.cycleTab(-1)
		}
	case "vim.tabNext":
		if hasCount {
			a.selectTab(count)
			return
		}
		a.cycleTab(1)
	}
}

// resolveAction matches the typed keys against list bindings. A prefix
// match parks the keys and reports pending.
func (a *App) resolveAction(k vim.Key, ids ...string) (string, bool) {
	seq := append(append([]vim.Key{}, a.navKeys...), k)
	anyPrefix := false
	for _, id := range ids {
		b := a.keymap.Binding(id)
		if b == "" {
			continue
		}
		complete, prefix := matchSequence(seq, bindingKeys(b))
		if complete {
			a.navKeys = nil
			return id, false
		}
		if prefix {
			anyPrefix = true
		}
	}
	if anyPrefix {
		a.navKeys = seq
		return "", true
	}
	a.navKeys = nil
	return "", false
}

// listKeyAllowed gates single-key list shortcuts on Vim mode: with Vim off
// only arrows, Enter, Escape and modifier chords reach a list.
func (a *App) listKeyAllowed(k vim.Key) bool {
	if a.prefs.VimMode {
		return true
	}
	return !k.Printable()
}

// --- focus and panels ---

func (a *App) focusDirection(dir rune) {
	switch a.focus {
	case focusSidebar:
		if dir == 'l' {
			a.focus = focusPane
			a.activePane = a.leftmostPane()
		}
		return
	case focusOutline, focusConnections, focusCalendar:
		if dir == 'h' {
			a.focus = focusPane
			a.activePane = a.rightmostPane()
		}
		return
	}
	if next := a.panes.neighbor(a.activePane, dir); next != nil {
		a.activePane = next
		return
	}
	switch dir {
	case 'h':
		if a.sidebarOpen && !a.zen {
			a.focus = focusSidebar
		}
	case 'l':
		if a.sidePanelOpen() {
			a.focus = a.sidePanelFocus()
		}
	}
}

func (a *App) leftmostPane() *pane {
	leaves := a.panes.leaves()
	best := leaves[0]
	for _, p := range leaves {
		if p.rect.x < best.rect.x {
			best = p
		}
	}
	return best
}

func (a *App) rightmostPane() *pane {
	leaves := a.panes.leaves()
	best := leaves[0]
	for _, p := range leaves {
		if p.rect.x+p.rect.w > best.rect.x+best.rect.w {
			best = p
		}
	}
	return best
}

func (a *App) sidePanelFocus() focusTarget {
	switch {
	case a.outlineOpen:
		return focusOutline
	case a.connectionsOpen:
		return focusConnections
	case a.calendarOpen:
		return focusCalendar
	}
	return focusPane
}

func (a *App) toggleSidebar() {
	a.sidebarOpen = !a.sidebarOpen
	if !a.sidebarOpen && a.focus == focusSidebar {
		a.focus = focusPane
	}
	if a.sidebarOpen && a.zen {
		a.zen = false
	}
	a.layout()
	a.markSessionDirty()
}

// toggleSidePanel opens one side panel at a time, focusing it.
func (a *App) toggleSidePanel(flag *bool, focus focusTarget) {
	if *flag {
		*flag = false
		if a.focus == focus {
			a.focus = focusPane
		}
		a.layout()
		return
	}
	a.outlineOpen, a.connectionsOpen, a.calendarOpen = false, false, false
	*flag = true
	a.zen = false
	a.focus = focus
	a.refreshPanels()
	a.layout()
}

func (a *App) closeSidePanels() {
	a.outlineOpen, a.connectionsOpen, a.calendarOpen = false, false, false
	if a.focus != focusPane && a.focus != focusSidebar {
		a.focus = focusPane
	}
	a.layout()
}

func (a *App) toggleZen() {
	a.zen = !a.zen
	if a.zen {
		a.focus = focusPane
	}
	a.layout()
}

// togglePreview flips the active tab between the editor and the preview.
func (a *App) togglePreview() {
	t := a.activeTab()
	if t == nil || t.view != nil {
		a.notify("Open a note first")
		return
	}
	if t.mode == modePreview {
		a.setPaneMode(modeEdit)
		a.notify("Editor mode")
	} else {
		a.setPaneMode(modePreview)
		a.notify("Preview mode (Space z v or Alt+E to edit)")
	}
}

func (a *App) setPaneMode(mode paneMode) {
	t := a.activeTab()
	if t == nil || t.view != nil {
		return
	}
	t.mode = mode
	a.noteModes[t.path] = mode
	if mode != modeEdit && t.preview == nil {
		t.preview = &previewState{}
	}
	if mode == modeEdit {
		a.focus = focusPane
	}
	a.layout()
	a.markSessionDirty()
}

func (a *App) saveNow() {
	if buf := a.activeBuffer(); buf != nil {
		if err := a.saveBuffer(buf); err != nil {
			a.notifyError("Save failed: " + err.Error())
			return
		}
		a.notify("Saved " + a.tabTitle(a.activeTab()))
	}
}

// --- Ctrl+W pane commands ---

func (a *App) paneCommand(k vim.Key) {
	switch {
	case k.IsRune('h') || k.Is("left") || k.IsCtrl('h'):
		a.focusDirection('h')
	case k.IsRune('j') || k.Is("down") || k.IsCtrl('j'):
		a.focusDirection('j')
	case k.IsRune('k') || k.Is("up") || k.IsCtrl('k'):
		a.focusDirection('k')
	case k.IsRune('l') || k.Is("right") || k.IsCtrl('l'):
		a.focusDirection('l')
	case k.IsRune('v') || k.IsCtrl('v'):
		a.splitPane(true)
	case k.IsRune('s') || k.IsCtrl('s'):
		a.splitPane(false)
	case k.IsRune('q') || k.IsRune('c'):
		a.closeTab(a.activePane, a.activePane.active)
	case k.IsRune('o'):
		a.onlyPane()
	case k.IsRune('w') || k.IsCtrl('w'):
		a.cyclePane(1)
	case k.IsRune('p') || k.IsRune('W'):
		a.cyclePane(-1)
	case k.IsRune('='):
		a.equalizePanes(a.panes)
	case k.IsRune('>') || k.IsRune('+'):
		a.resizePane(0.05)
	case k.IsRune('<') || k.IsRune('-'):
		a.resizePane(-0.05)
	case k.Is("esc"):
	default:
		a.notify("Ctrl+W " + keyLabel(k) + " is not a pane command")
	}
}

func (a *App) splitPane(vertical bool) {
	if a.activeTab() == nil {
		a.notify("Open a note before splitting")
		return
	}
	if p := a.panes.split(a.activePane, vertical); p != nil {
		a.activePane = p
		a.focus = focusPane
	}
	a.layout()
	a.markSessionDirty()
}

func (a *App) onlyPane() {
	keep := a.activePane
	for _, p := range a.panes.leaves() {
		if p != keep {
			for _, t := range p.tabs {
				a.closeBufferIfUnusedExcept(t.path, keep)
			}
			a.panes.remove(p)
		}
	}
	a.activePane = keep
	a.layout()
	a.markSessionDirty()
}

func (a *App) closeBufferIfUnusedExcept(path string, keep *pane) {
	for _, t := range keep.tabs {
		if t.path == path {
			return
		}
	}
	if buf, ok := a.buffers[path]; ok {
		_ = a.saveBuffer(buf)
		delete(a.buffers, path)
	}
}

func (a *App) cyclePane(delta int) {
	leaves := a.panes.leaves()
	if len(leaves) < 2 {
		return
	}
	for i, p := range leaves {
		if p == a.activePane {
			a.activePane = leaves[((i+delta)%len(leaves)+len(leaves))%len(leaves)]
			a.focus = focusPane
			return
		}
	}
}

func (a *App) equalizePanes(n *paneNode) {
	if n == nil || n.leaf != nil {
		return
	}
	n.ratio = 0.5
	a.equalizePanes(n.first)
	a.equalizePanes(n.second)
	a.layout()
}

func (a *App) resizePane(delta float64) {
	parent := a.parentOf(a.panes, a.activePane)
	if parent == nil {
		return
	}
	if parent.second.leaves()[0] == a.activePane || containsPane(parent.second, a.activePane) {
		delta = -delta
	}
	parent.ratio = max(0.15, min(0.85, parent.ratio+delta))
	a.layout()
}

func (a *App) parentOf(n *paneNode, p *pane) *paneNode {
	if n == nil || n.leaf != nil {
		return nil
	}
	if containsPane(n.first, p) {
		if n.first.leaf == p {
			return n
		}
		return a.parentOf(n.first, p)
	}
	if containsPane(n.second, p) {
		if n.second.leaf == p {
			return n
		}
		return a.parentOf(n.second, p)
	}
	return nil
}

func containsPane(n *paneNode, p *pane) bool {
	for _, leaf := range n.leaves() {
		if leaf == p {
			return true
		}
	}
	return false
}

// --- jump list across notes ---

func (a *App) pushJump(path string, pos vim.Pos) {
	if a.jumpIdx >= 0 && a.jumpIdx < len(a.jumps) {
		a.jumps = a.jumps[:a.jumpIdx]
	}
	if n := len(a.jumps); n > 0 && a.jumps[n-1].path == path && a.jumps[n-1].pos.Line == pos.Line {
		a.jumps = a.jumps[:n-1]
	}
	a.jumps = append(a.jumps, jumpEntry{path: path, pos: pos})
	if len(a.jumps) > 100 {
		a.jumps = a.jumps[1:]
	}
	a.jumpIdx = len(a.jumps)
}

func (a *App) jumpBack() {
	if a.jumpIdx >= len(a.jumps) {
		if buf := a.activeBuffer(); buf != nil {
			a.jumps = append(a.jumps, jumpEntry{path: buf.path, pos: buf.ed.Cursor()})
			a.jumpIdx = len(a.jumps) - 1
		}
	}
	if a.jumpIdx <= 0 {
		a.notify("At the start of the jump list")
		return
	}
	a.jumpIdx--
	a.gotoJump(a.jumps[a.jumpIdx])
}

func (a *App) jumpForward() {
	if a.jumpIdx+1 >= len(a.jumps) {
		a.notify("At the end of the jump list")
		return
	}
	a.jumpIdx++
	a.gotoJump(a.jumps[a.jumpIdx])
}

func (a *App) gotoJump(j jumpEntry) {
	if _, ok := a.noteMeta(j.path); !ok {
		return
	}
	a.openNoteQuiet(j.path)
	if buf := a.buffers[j.path]; buf != nil {
		buf.ed.SetCursor(j.pos)
		buf.ed.EnsureCursorVisible()
	}
	a.focus = focusPane
}

// --- leader ---

func (a *App) startLeader() {
	a.leaderSeq++
	a.leader = &leaderState{seq: a.leaderSeq, node: a.leaderTree()}
	if a.prefs.WhichKeyHints && a.prefs.WhichKeyHintMode != "timed" {
		a.leader.showHints = true
	}
}

func (a *App) leaderKey(k vim.Key) {
	l := a.leader
	if k.Is("esc") || k.IsCtrl('c') {
		a.leader = nil
		return
	}
	if !k.Printable() {
		a.leader = nil
		return
	}
	for _, child := range l.node.children {
		if child.key == string(k.Rune) {
			if len(child.children) > 0 {
				l.keys = append(l.keys, k)
				l.node = child
				a.leaderSeq++
				l.seq = a.leaderSeq
				if a.prefs.WhichKeyHints && a.prefs.WhichKeyHintMode != "timed" {
					l.showHints = true
				}
				return
			}
			a.leader = nil
			child.run(a)
			return
		}
	}
	a.leader = nil
	a.notify("Space " + l.pendingLabel() + " " + keyLabel(k) + " is not bound")
}
