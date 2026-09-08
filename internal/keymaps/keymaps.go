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
	{"vim.leaderPrefix", "vim", "Space", "Leader key"},
	{"vim.leaderOpenBuffers", "vim", "o", "Leader: open buffers"},
	{"vim.leaderWorkflows", "vim", "a", "Leader: open workflows"},
	{"vim.leaderAtlas", "vim", "g", "Leader: open atlas"},
	{"vim.leaderSearchNotes", "vim", "f", "Leader: search notes"},
	{"vim.leaderSearchGroup", "vim", "s", "Leader: search…"},
	{"vim.leaderSearchVaultText", "vim", "t", "Leader search: vault text"},
	{"vim.leaderToggleSidebar", "vim", "e", "Leader: toggle sidebar"},
	{"vim.leaderNoteOutline", "vim", "p", "Leader: note outline"},
	{"vim.leaderSwitchVault", "vim", "v", "Leader: switch vault"},
	{"vim.leaderViewGroup", "vim", "z", "Leader: view (preview, split, editor, wrap, line numbers, zen, theme)"},
	{"vim.leaderKanban", "vim", "k", "Leader: Kanban board"},
	{"vim.leaderNoteActions", "vim", "l", "Leader: note actions"},
	{"vim.leaderFormatNote", "vim", "f", "Leader note action: format note"},
	{"vim.leaderCopyMarkdown", "vim", "y", "Leader note action: copy note as Markdown"},
	{"vim.leaderToggleFavorite", "vim", "s", "Leader note action: toggle favorite"},
	{"vim.leaderQuickCapture", "vim", "q", "Leader: open quick capture"},
	{"vim.leaderTemplatePicker", "vim", "t", "Leader: new from template"},
	{"vim.leaderInsertTemplate", "vim", "i", "Leader: insert template into note"},
	{"vim.leaderDailyNote", "vim", "d", "Leader: today's daily note"},
	{"vim.leaderWeeklyNote", "vim", "w", "Leader: this week's note"},
	{"vim.leaderMonthlyNote", "vim", "m", "Leader: this month's note"},
	{"vim.leaderCalendar", "vim", "c", "Leader: toggle calendar"},
	{"vim.hintMode", "vim", "h", "Leader: hint mode"},
	{"vim.panePrefix", "vim", "Ctrl+W", "Pane command prefix"},
	{"vim.paneFocusLeft", "vim", "h", "Pane: focus left"},
	{"vim.paneFocusDown", "vim", "j", "Pane: focus down"},
	{"vim.paneFocusUp", "vim", "k", "Pane: focus up"},
	{"vim.paneFocusRight", "vim", "l", "Pane: focus right"},
	{"vim.paneSplitRight", "vim", "v", "Pane: split right"},
	{"vim.paneSplitDown", "vim", "s", "Pane: split down"},
	{"vim.historyBack", "vim", "Ctrl+O", "Go back in note history"},
	{"vim.historyForward", "vim", "Ctrl+I", "Go forward in note history"},
	{"vim.bufferPrevious", "vim", "[ b", "Previous buffer"},
	{"vim.bufferNext", "vim", "] b", "Next buffer"},
	{"vim.tabPrevious", "vim", "g T", "Previous tab"},
	{"vim.tabNext", "vim", "g t", "Next tab"},
	{"vim.goToDefinition", "vim", "g d", "Follow link at cursor"},
	{"vim.foldCurrent", "vim", "z c", "Fold heading at cursor"},
	{"vim.unfoldCurrent", "vim", "z o", "Unfold heading at cursor"},
	{"vim.foldAll", "vim", "z M", "Fold all headings"},
	{"vim.unfoldAll", "vim", "z R", "Unfold all headings"},
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
	b := r.Binding("vim.leaderPrefix")
	if b == "Space" {
		return " "
	}
	return b
}
