package tui

import (
	"github.com/ZenNotes/tui/internal/periodic"
)

// leaderNode is one entry of the Space leader tree.
type leaderNode struct {
	key      string
	title    string
	run      func(a *App)
	children []*leaderNode
	hidden   bool
}

// leaderTree builds the leader menu from the resolved keymap: the same
// action ids as the desktop, so `[keymaps]` overrides carry over.
func (a *App) leaderTree() *leaderNode {
	root := &leaderNode{}
	add := func(parent *leaderNode, id, title string, run func(a *App)) *leaderNode {
		key := a.keymap.Binding(id)
		if key == "" {
			return nil
		}
		n := &leaderNode{key: key, title: title, run: run}
		parent.children = append(parent.children, n)
		return n
	}
	add(root, "helix.leaderOpenBuffers", "Open buffers", func(a *App) { a.openBufferPicker() })
	add(root, "helix.leaderSearchNotes", "Search notes", func(a *App) { a.openNoteSearch("") })
	if s := add(root, "helix.leaderSearchGroup", "Search", nil); s != nil {
		add(s, "helix.leaderSearchVaultText", "Vault text", func(a *App) { a.openTextSearch("") })
		s.children = append(s.children, &leaderNode{key: "n", title: "Notes", run: func(a *App) { a.openNoteSearch("") }})
	}
	add(root, "helix.leaderToggleSidebar", "Toggle sidebar", func(a *App) { a.toggleSidebar() })
	add(root, "helix.leaderNoteOutline", "Note outline", func(a *App) { a.toggleSidePanel(&a.outlineOpen, focusOutline) })
	// Space v is the desktop's "switch vault"; the view group lives on its
	// own key so nothing about vaults hides under "view".
	add(root, "helix.leaderSwitchVault", "Switch vault", func(a *App) { a.openVaultSwitcher() })
	if v := add(root, "helix.leaderViewGroup", "View", nil); v != nil {
		v.children = append(v.children,
			&leaderNode{key: "p", title: "Preview mode", run: func(a *App) { a.setPaneMode(modePreview) }},
			&leaderNode{key: "s", title: "Split mode", run: func(a *App) { a.setPaneMode(modeSplit) }},
			&leaderNode{key: "e", title: "Editor mode", run: func(a *App) { a.setPaneMode(modeEdit) }},
			&leaderNode{key: "v", title: "Toggle editor/preview", run: func(a *App) { a.togglePreview() }},
			&leaderNode{key: "w", title: "Word wrap", run: func(a *App) { a.prefs.WordWrap = !a.prefs.WordWrap }},
			&leaderNode{key: "n", title: "Line numbers", run: func(a *App) { a.runCommandLine("nu") }},
			&leaderNode{key: "z", title: "Zen mode", run: func(a *App) { a.toggleZen() }},
			&leaderNode{key: "t", title: "Theme", run: func(a *App) { a.runCommandLine("theme") }},
		)
	}
	if l := add(root, "helix.leaderNoteActions", "Note actions", nil); l != nil {
		add(l, "helix.leaderFormatNote", "Format note", func(a *App) { a.formatActiveNote() })
		add(l, "helix.leaderCopyMarkdown", "Copy note as Markdown", func(a *App) { a.copyActiveNote() })
		add(l, "helix.leaderToggleFavorite", "Toggle favorite", func(a *App) { a.toggleActiveFavorite() })
		l.children = append(l.children,
			&leaderNode{key: "r", title: "Rename note", run: func(a *App) { a.renameActiveNote() }},
			&leaderNode{key: "m", title: "Move note", run: func(a *App) { a.moveActiveNotePrompt() }},
			&leaderNode{key: "d", title: "Move to trash", run: func(a *App) { a.trashActiveNote() }},
			&leaderNode{key: "a", title: "Archive note", run: func(a *App) { a.archiveActiveNote() }},
			&leaderNode{key: "c", title: "Copy link", run: func(a *App) { a.copyActiveLink() }},
			&leaderNode{key: "o", title: "Open in ZenNotes app", run: func(a *App) { a.openActiveInDesktop() }},
			&leaderNode{key: "e", title: "Edit in $EDITOR", run: func(a *App) { a.editActiveInEditor() }},
		)
	}
	add(root, "helix.leaderQuickCapture", "Quick capture", func(a *App) { a.openQuickCapture() })
	add(root, "helix.leaderTemplatePicker", "New from template", func(a *App) { a.openTemplatePicker(false) })
	add(root, "helix.leaderInsertTemplate", "Insert template", func(a *App) { a.openTemplatePicker(true) })
	add(root, "helix.leaderDailyNote", "Daily note", func(a *App) { a.openPeriodic(periodic.Daily, 0) })
	add(root, "helix.leaderWeeklyNote", "Weekly note", func(a *App) { a.openPeriodic(periodic.Weekly, 0) })
	add(root, "helix.leaderMonthlyNote", "Monthly note", func(a *App) { a.openPeriodic(periodic.Monthly, 0) })
	add(root, "helix.leaderCalendar", "Calendar", func(a *App) { a.toggleSidePanel(&a.calendarOpen, focusCalendar) })
	add(root, "helix.hintMode", "Hint mode", func(a *App) { a.startHintMode() })
	add(root, "helix.leaderKanban", "Kanban board", func(a *App) { _ = a.openTasksMode("kanban") })
	root.children = append(root.children,
		&leaderNode{key: ";", title: "Command palette", run: func(a *App) { a.openCommandPalette() }},
		&leaderNode{key: "x", title: "Tasks", run: func(a *App) { _ = a.openTasksMode("") }},
		&leaderNode{key: "n", title: "New note", run: func(a *App) { a.newNoteHere() }},
		&leaderNode{key: "?", title: "Help", run: func(a *App) { a.openHelp() }},
	)
	return root
}
