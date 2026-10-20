// Package keymaps is the bindable-action catalog the terminal UI consults:
// the same action ids and default bindings as the desktop's keymap table,
// so a `[keymaps]` override in config.toml means the same thing in both. A
// binding of "" unbinds the action outright.
package keymaps

import "strings"

// Entry is one bindable action.
type Entry struct {
	ID             string
	Group          string
	DefaultBinding string
	Title          string
}

// Unbound is the override value that removes a key entirely.
const Unbound = ""

// Catalog lists every action, in the desktop's order.
var Catalog = []Entry{
	{"global.searchNotes", "global", "Ctrl+P", "Search notes"},
	{"global.commandPalette", "global", "Ctrl+Shift+P", "Open command palette"},
	{"global.newQuickNote", "global", "Ctrl+Shift+N", "New quick note"},
	{"global.newNoteHere", "global", "Ctrl+N", "New note in current folder"},
	{"global.openSettings", "global", "Ctrl+,", "Open settings"},
	{"global.toggleSidebar", "global", "Ctrl+B", "Toggle sidebar"},
	{"global.toggleConnections", "global", "Ctrl+Shift+B", "Toggle connections panel"},
	{"global.toggleOutlinePanel", "global", "Ctrl+Shift+O", "Toggle outline panel"},
	{"global.focusPaneLeft", "global", "Alt+H", "Focus pane left"},
	{"global.focusPaneDown", "global", "Alt+J", "Focus pane down"},
	{"global.focusPaneUp", "global", "Alt+K", "Focus pane up"},
	{"global.focusPaneRight", "global", "Alt+L", "Focus pane right"},
	{"global.modeEdit", "global", "Alt+E", "Switch to editor mode"},
	{"global.modeSplit", "global", "Alt+S", "Switch to split mode"},
	{"global.modePreview", "global", "Alt+P", "Switch to preview mode"},
	{"global.toggleZenMode", "global", "Alt+.", "Toggle Zen mode"},
	{"global.closeActiveTab", "global", "Ctrl+W", "Close active tab"},
	{"global.reopenClosedTab", "global", "Ctrl+Shift+T", "Reopen closed tab"},
	{"global.toggleWordWrap", "global", "Alt+Z", "Toggle word wrap"},
	{"global.historyBack", "global", "Alt+Left", "Go back in note history"},
	{"global.historyForward", "global", "Alt+Right", "Go forward in note history"},
	{"global.toggleRecentNote", "global", "Ctrl+Tab", "Switch to previous note"},
	{"tabs.select1", "global", "Alt+1", "Go to tab 1"},
	{"tabs.select2", "global", "Alt+2", "Go to tab 2"},
	{"tabs.select3", "global", "Alt+3", "Go to tab 3"},
	{"tabs.select4", "global", "Alt+4", "Go to tab 4"},
	{"tabs.select5", "global", "Alt+5", "Go to tab 5"},
	{"tabs.select6", "global", "Alt+6", "Go to tab 6"},
	{"tabs.select7", "global", "Alt+7", "Go to tab 7"},
	{"tabs.select8", "global", "Alt+8", "Go to tab 8"},
	{"tabs.select9", "global", "Alt+9", "Go to tab 9"},
	{"helix.leaderPrefix", "helix", "Space", "Leader key"},
	{"helix.leaderOpenBuffers", "helix", "o", "Leader: open buffers"},
	{"helix.leaderWorkflows", "helix", "a", "Leader: open workflows"},
	{"helix.leaderAtlas", "helix", "g", "Leader: open atlas"},
	{"helix.leaderSearchNotes", "helix", "f", "Leader: search notes"},
	{"helix.leaderSearchGroup", "helix", "s", "Leader: search…"},
	{"helix.leaderSearchVaultText", "helix", "t", "Leader search: vault text"},
	{"helix.leaderToggleSidebar", "helix", "e", "Leader: toggle sidebar"},
	{"helix.leaderNoteOutline", "helix", "p", "Leader: note outline"},
	{"helix.leaderSwitchVault", "helix", "v", "Leader: switch vault"},
	{"helix.leaderViewGroup", "helix", "z", "Leader: view (preview, split, editor, wrap, line numbers, zen, theme)"},
	{"helix.leaderKanban", "helix", "k", "Leader: Kanban board"},
	{"helix.leaderNoteActions", "helix", "l", "Leader: note actions"},
	{"helix.leaderFormatNote", "helix", "f", "Leader note action: format note"},
	{"helix.leaderCopyMarkdown", "helix", "y", "Leader note action: copy note as Markdown"},
	{"helix.leaderToggleFavorite", "helix", "s", "Leader note action: toggle favorite"},
	{"helix.leaderQuickCapture", "helix", "q", "Leader: open quick capture"},
	{"helix.leaderTemplatePicker", "helix", "t", "Leader: new from template"},
	{"helix.leaderInsertTemplate", "helix", "i", "Leader: insert template into note"},
	{"helix.leaderDailyNote", "helix", "d", "Leader: today's daily note"},
	{"helix.leaderWeeklyNote", "helix", "w", "Leader: this week's note"},
	{"helix.leaderMonthlyNote", "helix", "m", "Leader: this month's note"},
	{"helix.leaderCalendar", "helix", "c", "Leader: toggle calendar"},
	{"helix.hintMode", "helix", "h", "Leader: hint mode"},
	{"helix.panePrefix", "helix", "Ctrl+W", "Pane command prefix"},
	{"helix.paneFocusLeft", "helix", "h", "Pane: focus left"},
	{"helix.paneFocusDown", "helix", "j", "Pane: focus down"},
	{"helix.paneFocusUp", "helix", "k", "Pane: focus up"},
	{"helix.paneFocusRight", "helix", "l", "Pane: focus right"},
	{"helix.paneSplitRight", "helix", "v", "Pane: split right"},
	{"helix.paneSplitDown", "helix", "s", "Pane: split down"},
	{"helix.historyBack", "helix", "Ctrl+O", "Go back in note history"},
	{"helix.historyForward", "helix", "Ctrl+I", "Go forward in note history"},
	{"helix.bufferPrevious", "helix", "[ b", "Previous buffer"},
	{"helix.bufferNext", "helix", "] b", "Next buffer"},
	{"helix.tabPrevious", "helix", "g T", "Previous tab"},
	{"helix.tabNext", "helix", "g t", "Next tab"},
	{"helix.gotoTabPrevious", "helix", "g p", "Previous tab (gp)"},
	{"helix.gotoTabNext", "helix", "g n", "Next tab (gn)"},
	{"helix.goToDefinition", "helix", "g d", "Follow link at cursor"},
	{"helix.foldCurrent", "helix", "z c", "Fold heading at cursor"},
	{"helix.unfoldCurrent", "helix", "z o", "Unfold heading at cursor"},
	{"helix.foldAll", "helix", "z M", "Fold all headings"},
	{"helix.unfoldAll", "helix", "z R", "Unfold all headings"},
	{"nav.moveDown", "navigation", "j", "Move selection down"},
	{"nav.moveUp", "navigation", "k", "Move selection up"},
	{"nav.moveLeft", "navigation", "h", "Move selection left"},
	{"nav.moveRight", "navigation", "l", "Move selection right"},
	{"nav.jumpTop", "navigation", "g g", "Jump to top"},
	{"nav.jumpBottom", "navigation", "G", "Jump to bottom"},
	{"nav.halfPageDown", "view-actions", "Ctrl+D", "Half-page down"},
	{"nav.halfPageUp", "view-actions", "Ctrl+U", "Half-page up"},
	{"nav.openSideItem", "navigation", "l", "Open sidebar or note-list item"},
	{"nav.openResult", "navigation", "o", "Open result"},
	{"nav.back", "navigation", "h", "Back out"},
	{"nav.toggleFolder", "navigation", "o", "Toggle folder"},
	{"nav.filter", "navigation", "/", "Focus filter or search"},
	{"nav.contextMenu", "view-actions", "m", "Open context menu"},
	{"nav.peekPreview", "view-actions", "p", "Peek preview"},
	{"nav.restore", "view-actions", "r", "Restore trashed note"},
	{"nav.delete", "view-actions", "x", "Delete selected result"},
	{"nav.toggleTask", "view-actions", "x", "Toggle task"},
	{"tasks.moveTaskUp", "view-actions", "K", "Move task up"},
	{"tasks.moveTaskDown", "view-actions", "J", "Move task down"},
	{"editor.toggleCheckbox", "view-actions", "Ctrl+L", "Toggle checkbox"},
	{"editor.reflowParagraph", "view-actions", "Ctrl+Q", "Reflow paragraph"},
	{"nav.localEx", "view-actions", ":", "Open local ex prompt"},
	{"nav.newQuickNote", "view-actions", "n", "New quick note from Quick Notes view"},
	{"nav.unarchive", "view-actions", "u", "Unarchive selected note"},
}

// Resolver answers "what is this action bound to" with overrides applied.
type Resolver struct {
	overrides map[string]string
	byID      map[string]Entry
}

// NewResolver applies config.toml overrides on top of the catalog.
func NewResolver(overrides map[string]string) *Resolver {
	r := &Resolver{overrides: map[string]string{}, byID: map[string]Entry{}}
	for _, e := range Catalog {
		r.byID[e.ID] = e
	}
	for id, binding := range overrides {
		if _, known := r.byID[id]; known {
			r.overrides[id] = strings.TrimSpace(binding)
		}
	}
	return r
}

// Binding is the effective binding, "" when unbound.
func (r *Resolver) Binding(id string) string {
	if b, ok := r.overrides[id]; ok {
		return b
	}
	return r.byID[id].DefaultBinding
}

// IsUnbound is true when the action has no key at all.
func (r *Resolver) IsUnbound(id string) bool {
	b, ok := r.overrides[id]
	return ok && b == Unbound
}

// Title is the action's display name.
func (r *Resolver) Title(id string) string {
	return r.byID[id].Title
}

// Entries lists the catalog with overrides applied.
func (r *Resolver) Entries() []Entry {
	out := make([]Entry, 0, len(Catalog))
	for _, e := range Catalog {
		e.DefaultBinding = r.Binding(e.ID)
		out = append(out, e)
	}
	return out
}

// Set changes an override in memory (`:unbind` and friends).
func (r *Resolver) Set(id, binding string) bool {
	if _, known := r.byID[id]; !known {
		return false
	}
	r.overrides[id] = binding
	return true
}

// Overrides is the current override map.
func (r *Resolver) Overrides() map[string]string {
	out := map[string]string{}
	for k, v := range r.overrides {
		out[k] = v
	}
	return out
}

// Known is true for a catalog id.
func (r *Resolver) Known(id string) bool {
	_, ok := r.byID[id]
	return ok
}

// LeaderKey is the single key that starts a leader sequence, "" when the
// leader is unbound.
func (r *Resolver) LeaderKey() string {
	b := r.Binding("helix.leaderPrefix")
	if b == "Space" {
		return " "
	}
	return b
}
