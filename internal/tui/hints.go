package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ZenNotes/tui/internal/vim"
)

// hintPair is one entry of a help line: a catalog action id (or a literal
// key written as "key:x") and a short verb. Ids joined with "|" share a
// verb, like j/k.
type hintPair struct {
	id    string
	label string
}

// bindingLabel renders a catalog binding for help text: "g g" becomes
// "gg", "Ctrl+D" stays as is.
func bindingLabel(binding string) string {
	parts := strings.Fields(binding)
	if len(parts) == 0 {
		return ""
	}
	single := true
	for _, p := range parts {
		if len([]rune(p)) != 1 {
			single = false
		}
	}
	if single {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, " ")
}

// keyOf resolves a hint id to its display key, "" when unbound.
func (a *App) keyOf(id string) string {
	if strings.HasPrefix(id, "key:") {
		return strings.TrimPrefix(id, "key:")
	}
	labels := []string{}
	for _, part := range strings.Split(id, "|") {
		b := a.keymap.Binding(part)
		if b == "" {
			continue
		}
		labels = append(labels, bindingLabel(b))
	}
	return strings.Join(labels, "/")
}

// keysHint builds a one-line help string from pairs, honoring rebindings
// and dropping unbound actions.
func (a *App) keysHint(pairs []hintPair) string {
	parts := []string{}
	for _, p := range pairs {
		key := a.keyOf(p.id)
		if key == "" {
			continue
		}
		parts = append(parts, key+" "+p.label)
	}
	return strings.Join(parts, " · ")
}

// fitHint truncates a help line to the width, ending with an ellipsis.
func fitHint(s string, width int) string {
	if cellWidth(s) <= width {
		return s
	}
	cut := s
	for cellWidth(cut) > width-2 && strings.Contains(cut, " · ") {
		cut = cut[:strings.LastIndex(cut, " · ")]
	}
	if cellWidth(cut) > width-2 {
		return truncateCells(s, width)
	}
	return cut + " …"
}

// Per-surface help pairs. Views add their own view-only keys.
var (
	sidebarHints = []hintPair{
		{"nav.moveDown|nav.moveUp", "move"}, {"key:Enter", "open"}, {"nav.toggleFolder", "toggle"},
		{"nav.contextMenu", "menu"}, {"key:n", "new note"}, {"key:r", "rename"}, {"nav.delete", "trash"},
		{"nav.filter", "search"}, {"key:Esc", "editor"}, {"key:?", "keys"},
	}
	editorHints = []hintPair{
		{"vim.leaderPrefix", "leader"}, {"key:Space z p", "preview"}, {"key:Space z s", "split"}, {"key::help", "manual"}, {"global.searchNotes", "search"},
		{"vim.panePrefix", "pane"}, {"global.newNoteHere", "new note"}, {"vim.goToDefinition", "follow link"},
		{"editor.toggleCheckbox", "checkbox"},
	}
	plainEditorHints = []hintPair{
		{"global.searchNotes", "search"}, {"global.newNoteHere", "new note"}, {"key:Alt+H/J/K/L", "panes"},
		{"key:Ctrl+S", "save"}, {"key:Ctrl+G", "commands"},
	}
	panelHints = []hintPair{{"nav.moveDown|nav.moveUp", "move"}, {"key:Enter", "open"}, {"key:Esc", "back"}}
	arrowHints = []hintPair{{"key:↑/↓", "move"}, {"key:Enter", "open"}, {"key:Esc", "back"}}
)

// hintLine is the help line under the status bar for the focused surface.
func (a *App) hintLine() string {
	var line string
	switch {
	case !a.prefs.VimMode:
		switch a.focus {
		case focusSidebar:
			line = a.keysHint(append(arrowHints, hintPair{"key:←/→", "collapse/expand"}, hintPair{"global.newNoteHere", "new note"}))
		case focusOutline, focusConnections, focusCalendar:
			line = a.keysHint(arrowHints)
		default:
			if tab := a.activeTab(); tab != nil && tab.view != nil {
				line = a.keysHint(append(arrowHints, hintPair{"global.searchNotes", "search"})) + " · Vim mode off: single-key shortcuts disabled"
			} else {
				line = a.keysHint(plainEditorHints)
			}
		}
	case a.focus == focusSidebar:
		line = a.keysHint(sidebarHints)
	case a.focus == focusOutline || a.focus == focusConnections || a.focus == focusCalendar:
		line = a.keysHint(panelHints)
	default:
		if tab := a.activeTab(); tab != nil && tab.view != nil {
			line = tab.view.hint(a)
		} else if tab != nil && tab.mode == modePreview {
			line = a.previewHint()
		} else {
			line = a.keysHint(editorHints)
		}
	}
	return fitHint(line, max(10, a.width-1))
}

// --- key help overlay (`?`) ---

type keyHelpOverlay struct {
	title string
	pairs []hintPair
	extra []hintPair
}

func (h *keyHelpOverlay) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc") || k.IsRune('?') || k.IsRune('q') || k.IsCtrl('c'):
		a.overlay = nil
	case k.Is("enter"):
		a.overlay = nil
		a.openHelp()
	}
}

func (h *keyHelpOverlay) render(a *App, w, hgt int) string {
	th := a.theme
	rows := [][2]string{}
	for _, p := range append(append([]hintPair{}, h.pairs...), h.extra...) {
		key := a.keyOf(p.id)
		if key == "" {
			continue
		}
		rows = append(rows, [2]string{key, p.label})
	}
	keyW := 0
	for _, r := range rows {
		keyW = max(keyW, cellWidth(r[0]))
	}
	cols := 1
	if len(rows) > 8 {
		cols = 2
	}
	if len(rows) > 18 {
		cols = 3
	}
	per := (len(rows) + cols - 1) / cols
	colW := 0
	rendered := make([]string, len(rows))
	for i, r := range rows {
		rendered[i] = th.KeyHint.Render(padRight(r[0], keyW)) + "  " + r[1]
		colW = max(colW, lipgloss.Width(rendered[i])+3)
	}
	lines := []string{th.OverlayTitle.Render(h.title) + th.Muted.Render("  Enter opens the manual · Esc closes")}
	for i := 0; i < per; i++ {
		row := ""
		for c := 0; c < cols; c++ {
			idx := c*per + i
			if idx < len(rows) {
				row += padRight(rendered[idx], colW)
			}
		}
		lines = append(lines, strings.TrimRight(row, " "))
	}
	width := min(w-4, max(40, colW*cols+4))
	return th.Overlay.Width(width).Render(strings.Join(lines, "\n"))
}

// openKeyHelp shows the keys of whatever has focus.
func (a *App) openKeyHelp() {
	global := []hintPair{
		{"global.searchNotes", "search notes"}, {"global.newNoteHere", "new note"}, {"global.toggleSidebar", "toggle sidebar"},
		{"key:Ctrl+W h/j/k/l", "focus panes (also Alt+H/J/K/L)"},
		{"key:Space z e/s/p", "editor, split, preview mode (also Alt+E/S/P)"}, {"key:Esc", "in preview: back to the editor"},
		{"global.toggleZenMode", "zen mode"}, {"global.toggleWordWrap", "word wrap"}, {"vim.historyBack|vim.historyForward", "jump list"},
		{"key:Ctrl+S", "save"}, {"key:Ctrl+Z", "suspend"}, {"key:Ctrl+G", "command palette"},
	}
	title := "Keys"
	var pairs []hintPair
	switch a.focus {
	case focusSidebar:
		title = "Sidebar keys"
		pairs = append(sidebarHints, hintPair{"key:N", "new folder"}, hintPair{"key:s", "favorite"}, hintPair{"key:h", "collapse or parent"})
	case focusOutline, focusConnections, focusCalendar:
		title = "Panel keys"
		pairs = append(panelHints, hintPair{"key:q", "close panel"})
	default:
		if tab := a.activeTab(); tab != nil && tab.view != nil {
			title = tab.view.title() + " keys"
			if hv, ok := tab.view.(hintPairer); ok {
				pairs = hv.hintPairs(a)
			} else {
				pairs = []hintPair{{"key:" + tab.view.hint(a), ""}}
			}
		} else {
			title = "Editor keys"
			pairs = append(editorHints, hintPair{"key:gx", "open URL"}, hintPair{"key:gy", "copy link"}, hintPair{"key:zc/zo/zM/zR", "folds"},
				hintPair{"vim.bufferNext|vim.bufferPrevious", "next/previous tab"}, hintPair{"key::w :q :qa", "save, close, quit"})
		}
	}
	a.overlay = &keyHelpOverlay{title: title, pairs: pairs, extra: global}
}

// hintPairer views describe their keys as pairs so help follows rebindings.
type hintPairer interface {
	hintPairs(a *App) []hintPair
}

func (a *App) formatBindingList(ids ...string) string {
	out := []string{}
	for _, id := range ids {
		if k := a.keyOf(id); k != "" {
			out = append(out, k)
		}
	}
	return fmt.Sprint(strings.Join(out, "/"))
}
