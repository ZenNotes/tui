package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/search"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

// command is one ex command the host owns, also listed in the palette.
type command struct {
	names []string
	title string
	hint  string
	run   func(a *App, buf *noteBuffer, cmd vim.ExCommand) error
	// palette false keeps a command out of the palette (argument-only ones).
	palette bool
}

var errNotInTerminal = fmt.Errorf("that feature lives in the desktop app; the terminal cannot run it")

func (a *App) commandTable() []command {
	if a.commands != nil {
		return a.commands
	}
	openView := func(path string) func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
		return func(a *App, _ *noteBuffer, _ vim.ExCommand) error { a.openVirtualByPath(path); return nil }
	}
	a.commands = []command{
		{names: []string{"w", "write", "save"}, title: "Save note", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			if strings.TrimSpace(cmd.Args) != "" {
				return a.saveAs(buf, cmd.Args)
			}
			return a.saveBufferReport(buf)
		}},
		{names: []string{"q", "quit", "close"}, title: "Close tab", hint: ":q", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			return a.closeActiveTabCommand(cmd.Bang)
		}},
		{names: []string{"wq", "x", "xit"}, title: "Save and close tab", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			if err := a.saveBufferReport(buf); err != nil {
				return err
			}
			return a.closeActiveTabCommand(true)
		}},
		{names: []string{"qa", "qall", "quitall"}, title: "Quit ZenNotes", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			if cmd.Bang {
				a.quitting = true
				return nil
			}
			a.quit()
			return nil
		}},
		{names: []string{"wa", "wall"}, title: "Save all notes", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			if err := a.saveAllBuffers(); err != nil {
				return err
			}
			a.notify("Saved all notes")
			return nil
		}},
		{names: []string{"wqa", "xa", "wqall", "xall"}, title: "Save all and quit", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.quit()
			return nil
		}},
		{names: []string{"bd", "bdelete", "bc", "bclose"}, title: "Close buffer", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.closeActiveTabCommand(cmd.Bang)
		}},
		{names: []string{"bn", "bnext", "tabnext", "tabn"}, title: "Next tab", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			if n, err := strconv.Atoi(strings.TrimSpace(cmd.Args)); err == nil {
				a.selectTab(n)
				return nil
			}
			a.cycleTab(1)
			return nil
		}},
		{names: []string{"bp", "bprev", "bprevious", "tabprev", "tabp", "tabprevious"}, title: "Previous tab", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.cycleTab(-1)
			return nil
		}},
		{names: []string{"buffers", "ls", "files"}, title: "List open buffers", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openBufferPicker()
			return nil
		}},
		{names: []string{"tabnew", "new"}, title: "New note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			title := strings.TrimSpace(cmd.Args)
			if title == "" {
				a.newNoteHere()
				return nil
			}
			folder, sub := a.currentFolderContext()
			if folder == vault.FolderTrash {
				folder, sub = vault.FolderInbox, ""
			}
			a.createNote(folder, title, sub, nil, true)
			return nil
		}},
		{names: []string{"tabclose", "tabc"}, title: "Close tab", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.closeActiveTabCommand(cmd.Bang)
		}},
		{names: []string{"tabonly", "tabo"}, title: "Close other tabs", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.closeOtherTabs()
			return nil
		}},
		{names: []string{"pin", "tabpin", "unpin"}, title: "Pin or unpin the tab", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.togglePin(a.activePane, a.activePane.active)
			return nil
		}},
		{names: []string{"tabmove", "tabm"}, title: "Move the tab left or right (:tabmove -1 / +1)", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			arg := strings.TrimSpace(cmd.Args)
			delta := 1
			switch {
			case arg == "" || arg == "+1" || arg == "right":
				delta = 1
			case arg == "-1" || arg == "left":
				delta = -1
			case arg == "0" || arg == "first":
				for a.activePane.active > 0 {
					a.moveTab(a.activePane, -1)
				}
				return nil
			case arg == "$" || arg == "last":
				for a.activePane.active < len(a.activePane.tabs)-1 {
					a.moveTab(a.activePane, 1)
				}
				return nil
			default:
				if n, err := strconv.Atoi(arg); err == nil {
					delta = n
				} else {
					return fmt.Errorf("usage: :tabmove -1 | +1 | first | last")
				}
			}
			step := 1
			if delta < 0 {
				step = -1
				delta = -delta
			}
			for i := 0; i < delta; i++ {
				a.moveTab(a.activePane, step)
			}
			return nil
		}},
		{names: []string{"tabreopen", "reopen"}, title: "Reopen closed tab", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.reopenClosedTab()
			return nil
		}},
		{names: []string{"only", "on"}, title: "Close other panes", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.onlyPane()
			return nil
		}},
		{names: []string{"sp", "split"}, title: "Split pane down", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.splitPane(false)
			return a.openArgument(cmd.Args)
		}},
		{names: []string{"vs", "vsplit", "vsp"}, title: "Split pane right", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.splitPane(true)
			return a.openArgument(cmd.Args)
		}},
		{names: []string{"e", "edit", "open", "o"}, title: "Open note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			if strings.TrimSpace(cmd.Args) == "" {
				a.openNoteSearch("")
				return nil
			}
			return a.openArgument(cmd.Args)
		}},
		{names: []string{"move", "mv"}, title: "Move note to folder", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			target := strings.TrimSpace(cmd.Args)
			if target == "" {
				a.moveNotePicker(buf.path)
				return nil
			}
			folder, sub, err := a.parseFolderTarget(target)
			if err != nil {
				return err
			}
			a.moveNote(buf.path, folder, sub)
			return nil
		}},
		{names: []string{"rename", "saveas"}, title: "Rename note", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			if strings.TrimSpace(cmd.Args) == "" {
				a.renameNote(buf.path)
				return nil
			}
			return a.saveAs(buf, cmd.Args)
		}},
		{names: []string{"delete", "del", "trashnote"}, title: "Move note to trash", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			a.trashActiveNote()
			return nil
		}},
		{names: []string{"archive"}, title: "Archive or unarchive note", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.archiveActiveNote()
			return nil
		}},
		{names: []string{"restore"}, title: "Restore note from trash", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			a.restoreNote(buf.path)
			return nil
		}},
		{names: []string{"duplicate", "dup"}, title: "Duplicate note", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			a.duplicateNote(buf.path)
			return nil
		}},
		{names: []string{"favorite", "fav"}, title: "Toggle favorite", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.toggleActiveFavorite()
			return nil
		}},
		{names: []string{"copylink", "yanklink"}, title: "Copy note link", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.copyActiveLink()
			return nil
		}},
		{names: []string{"copy", "yanknote"}, title: "Copy note as Markdown", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.copyActiveNote()
			return nil
		}},
		{names: []string{"format", "fmt"}, title: "Format note", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.formatActiveNote()
			return nil
		}},
		{names: []string{"desktop", "app"}, title: "Open in ZenNotes app", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openActiveInDesktop()
			return nil
		}},
		{names: []string{"tasks"}, title: "Open Tasks", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.openTasksMode(strings.TrimSpace(cmd.Args))
		}},
		{names: []string{"kanban", "board"}, title: "Open Tasks as a Kanban board", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			return a.openTasksMode("kanban")
		}},
		{names: []string{"taskcalendar", "tcal", "agenda"}, title: "Open Tasks as a calendar", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			return a.openTasksMode("calendar")
		}},
		{names: []string{"tasklist"}, title: "Open Tasks as a list", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			return a.openTasksMode("list")
		}},
		{names: []string{"tags"}, title: "Open Tags", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openTags(strings.TrimPrefix(strings.TrimSpace(cmd.Args), "#"))
			return nil
		}},
		{names: []string{"tag"}, title: "Filter by tag", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openTags(strings.TrimPrefix(strings.TrimSpace(cmd.Args), "#"))
			return nil
		}},
		{names: []string{"trash"}, title: "Open Trash", palette: true, run: openView(tabTrash)},
		{names: []string{"quick", "quicknotes"}, title: "Open Quick Notes", palette: true, run: openView(tabQuickNotes)},
		{names: []string{"archived", "archiveview"}, title: "Open Archive", palette: true, run: openView(tabArchive)},
		{names: []string{"home"}, title: "Open Home", palette: true, run: openView(tabHome)},
		{names: []string{"help", "h", "manual"}, title: "Open the manual", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openHelp()
			if q := strings.TrimSpace(cmd.Args); q != "" {
				if t := a.activeTab(); t != nil {
					if hv, ok := t.view.(*helpView); ok {
						hv.filter = q
						hv.refresh(a)
					}
				}
			}
			return nil
		}},
		{names: []string{"settings", "preferences"}, title: "Open Settings", palette: true, run: openView(tabSettings)},
		{names: []string{"view"}, title: "Open a view", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			switch strings.ToLower(strings.TrimSpace(cmd.Args)) {
			case "tasks":
				a.openTasks()
			case "tags":
				a.openTags("")
			case "trash":
				a.openNoteList(tabTrash)
			case "quick", "quicknotes", "quick-notes":
				a.openNoteList(tabQuickNotes)
			case "archive":
				a.openNoteList(tabArchive)
			case "home":
				a.openHome()
			case "help":
				a.openHelp()
			case "settings":
				a.openSettings()
			case "board", "kanban", "calendar", "list":
				a.openTasks()
				if t := a.activeTab(); t != nil {
					if tv, ok := t.view.(*tasksView); ok {
						tv.setMode(strings.ToLower(strings.TrimSpace(cmd.Args)))
					}
				}
			default:
				return fmt.Errorf("unknown view: %s", cmd.Args)
			}
			return nil
		}},
		{names: []string{"template", "tmpl"}, title: "New note from template", palette: true, run: func(a *App, buf *noteBuffer, cmd vim.ExCommand) error {
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.openTemplatePicker(false)
				return nil
			}
			// `:template save [name]` keeps the current note as a template;
			// `:template builtins` hides or restores the built-in ones.
			if word, rest, _ := strings.Cut(name, " "); word == "save" {
				a.saveNoteAsTemplate(buf, strings.TrimSpace(rest))
				return nil
			} else if word == "builtins" {
				a.prefs.HideBuiltinTemplates = !a.prefs.HideBuiltinTemplates
				if a.prefs.HideBuiltinTemplates {
					a.notify("Built-in templates hidden (editor.hide_builtin_templates in config.toml keeps it)")
				} else {
					a.notify("Built-in templates restored")
				}
				return nil
			}
			for _, t := range a.allTemplates() {
				if strings.EqualFold(t.Name, name) || t.ID == name {
					a.createFromTemplate(t)
					return nil
				}
			}
			return fmt.Errorf("no template named %q", name)
		}},
		{names: []string{"insert", "inserttemplate"}, title: "Insert template", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.openTemplatePicker(true)
				return nil
			}
			for _, t := range a.allTemplates() {
				if strings.EqualFold(t.Name, name) || t.ID == name {
					a.insertTemplate(t)
					return nil
				}
			}
			return fmt.Errorf("no template named %q", name)
		}},
		{names: []string{"daily", "today"}, title: "Open today's daily note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.periodicCommand(periodic.Daily, cmd.Args)
		}},
		{names: []string{"weekly"}, title: "Open this week's note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.periodicCommand(periodic.Weekly, cmd.Args)
		}},
		{names: []string{"monthly"}, title: "Open this month's note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			return a.periodicCommand(periodic.Monthly, cmd.Args)
		}},
		{names: []string{"outline"}, title: "Toggle outline panel", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.toggleSidePanel(&a.outlineOpen, focusOutline)
			return nil
		}},
		{names: []string{"connections", "backlinks", "links"}, title: "Toggle connections panel", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.toggleSidePanel(&a.connectionsOpen, focusConnections)
			return nil
		}},
		{names: []string{"calendar", "cal"}, title: "Toggle calendar panel", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.toggleSidePanel(&a.calendarOpen, focusCalendar)
			return nil
		}},
		{names: []string{"closepanel"}, title: "Close side panels", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.closeSidePanels()
			return nil
		}},
		{names: []string{"sidebar"}, title: "Toggle sidebar", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			if strings.TrimSpace(cmd.Args) == "tags" {
				key := "section:tags"
				a.sidebar.collapsed[key] = !a.sidebar.collapsed[key]
				a.sidebar.rebuild(a)
				if a.sidebar.collapsed[key] {
					a.notify("Tags hidden in the sidebar")
				} else {
					a.notify("Tags shown in the sidebar")
				}
				return nil
			}
			a.toggleSidebar()
			return nil
		}},
		{names: []string{"zen"}, title: "Toggle Zen mode", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.toggleZen()
			return nil
		}},
		{names: []string{"editmode", "edit-mode"}, title: "Editor mode", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.setPaneMode(modeEdit)
			return nil
		}},
		{names: []string{"splitmode", "split-mode"}, title: "Split mode (editor + preview)", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.setPaneMode(modeSplit)
			return nil
		}},
		{names: []string{"previewmode", "preview-mode", "preview"}, title: "Preview mode", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.setPaneMode(modePreview)
			return nil
		}},
		{names: []string{"togglepreview", "tp"}, title: "Toggle editor/preview", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.togglePreview()
			return nil
		}},
		{names: []string{"fold"}, title: "Fold heading at cursor", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf != nil {
				buf.ed.Feed("zc")
			}
			return nil
		}},
		{names: []string{"unfold"}, title: "Unfold heading at cursor", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf != nil {
				buf.ed.Feed("zo")
			}
			return nil
		}},
		{names: []string{"foldall"}, title: "Fold all headings", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf != nil {
				buf.ed.Feed("zM")
			}
			return nil
		}},
		{names: []string{"unfoldall"}, title: "Unfold all headings", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf != nil {
				buf.ed.Feed("zR")
			}
			return nil
		}},
		{names: []string{"newtask", "task"}, title: "New task in today's daily note", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			text := strings.TrimSpace(cmd.Args)
			if text == "" {
				a.promptFor("New task", "", "Task text, with due:YYYY-MM-DD or !high as needed", func(a *App, text string) {
					if strings.TrimSpace(text) != "" {
						a.addTaskToDaily(text, time.Now())
					}
				})
				return nil
			}
			a.addTaskToDaily(text, time.Now())
			return nil
		}},
		{names: []string{"filter"}, title: "Filter tasks", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openTasks()
			if t := a.activeTab(); t != nil {
				if tv, ok := t.view.(*tasksView); ok {
					tv.filter = strings.TrimSpace(cmd.Args)
					tv.refresh(a)
				}
			}
			return nil
		}},
		{names: []string{"capture"}, title: "Quick capture", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openQuickCapture()
			return nil
		}},
		{names: []string{"search", "find"}, title: "Search vault text", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openTextSearch(strings.TrimSpace(cmd.Args))
			return nil
		}},
		{names: []string{"notes", "files"}, title: "Search notes", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			a.openNoteSearch(strings.TrimSpace(cmd.Args))
			return nil
		}},
		{names: []string{"db", "database", "base"}, title: "Open database", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.openDatabasePicker()
				return nil
			}
			a.openDatabase(name)
			return nil
		}},
		{names: []string{"dbnew", "newdb"}, title: "New database", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.promptFor("New database", "", "Name", func(a *App, name string) {
					if strings.TrimSpace(name) != "" {
						a.createDatabase(strings.TrimSpace(name))
					}
				})
				return nil
			}
			a.createDatabase(name)
			return nil
		}},
		{names: []string{"dbconvert"}, title: "Convert the open .csv database to a .base folder", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			t := a.activePane.activeTab()
			if t == nil {
				return fmt.Errorf("open a database first (:db)")
			}
			dv, ok := t.view.(*databaseView)
			if !ok || dv.doc == nil {
				return fmt.Errorf("open a database first (:db)")
			}
			if !database.IsLooseCSVPath(dv.csvPath) {
				a.notify(dv.doc.Title + " is already a folder database")
				return nil
			}
			dv.convertToFolder(a, nil)
			return nil
		}},
		{names: []string{"unbind"}, title: "Unbind a keymap action (this session)", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			id := strings.TrimSpace(cmd.Args)
			if !a.keymap.Set(id, "") {
				return fmt.Errorf("unknown action id: %s (see :keymaps)", id)
			}
			a.notify("Unbound " + id + " for this session; add `\"" + id + "\" = \"\"` under [keymaps] in config.toml to keep it")
			return nil
		}},
		{names: []string{"bind", "map"}, title: "Bind a keymap action (this session)", run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			parts := strings.Fields(cmd.Args)
			if len(parts) < 2 {
				return fmt.Errorf("usage: :bind <action id> <keys>")
			}
			id := parts[0]
			keys := strings.Join(parts[1:], " ")
			if !a.keymap.Set(id, keys) {
				return fmt.Errorf("unknown action id: %s (see :keymaps)", id)
			}
			a.notify("Bound " + id + " to " + keys + " for this session")
			return nil
		}},
		{names: []string{"keymaps", "keys"}, title: "Show keymap actions", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openKeymapList()
			return nil
		}},
		{names: []string{"commands", "cmd", "palette"}, title: "Command palette", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openCommandPalette()
			return nil
		}},
		{names: []string{"reload", "refresh"}, title: "Reload the vault", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.refreshIndex()
			a.notify("Reloading the vault")
			return nil
		}},
		{names: []string{"wrap"}, title: "Toggle word wrap", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.prefs.WordWrap = !a.prefs.WordWrap
			return nil
		}},
		{names: []string{"linenumbers", "number", "nu"}, title: "Cycle line numbers", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			modes := []string{"off", "absolute", "relative", "hybrid"}
			arg := strings.TrimSpace(cmd.Args)
			if arg != "" {
				a.prefs.LineNumberMode = arg
				return nil
			}
			for i, m := range modes {
				if m == a.prefs.LineNumberMode {
					a.prefs.LineNumberMode = modes[(i+1)%len(modes)]
					a.notify("Line numbers: " + a.prefs.LineNumberMode)
					return nil
				}
			}
			a.prefs.LineNumberMode = "absolute"
			return nil
		}},
		{names: []string{"theme"}, title: "Toggle dark/light theme", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			mode := strings.ToLower(strings.TrimSpace(cmd.Args))
			if mode == "" {
				if a.theme.Dark {
					mode = "light"
				} else {
					mode = "dark"
				}
			}
			if mode != "dark" && mode != "light" && mode != "system" {
				return fmt.Errorf("theme is dark, light or system")
			}
			a.prefs.ThemeMode = mode
			a.theme = NewTheme(mode, a.systemDark)
			a.styleCache = nil
			a.glamour = nil
			a.invalidatePreviews()
			a.notify("Theme: " + mode)
			return nil
		}},
		{names: []string{"previewstyle", "style"}, title: "Preview style (Glamour name, JSON path, or zen)", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.openPreviewStylePicker()
				return nil
			}
			if err := a.setPreviewStyle(name); err != nil {
				return err
			}
			a.notify("Preview style: " + name)
			return nil
		}},
		{names: []string{"link", "wikilink"}, title: "Insert a link to a note", palette: true, run: func(a *App, buf *noteBuffer, _ vim.ExCommand) error {
			if buf == nil {
				return fmt.Errorf("no note is active")
			}
			a.openWikilinkPicker(buf)
			return nil
		}},
		{names: []string{"editor", "ed"}, title: "Edit note in $EDITOR", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.editActiveInEditor()
			return nil
		}},
		{names: []string{"suspend", "stop"}, title: "Suspend to the shell (fg to return)", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.queue(tea.Suspend)
			return nil
		}},
		{names: []string{"mouse"}, title: "Toggle mouse support", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			switch strings.ToLower(strings.TrimSpace(cmd.Args)) {
			case "on":
				a.mouseEnabled = true
			case "off":
				a.mouseEnabled = false
			default:
				a.mouseEnabled = !a.mouseEnabled
			}
			if a.mouseEnabled {
				a.queue(tea.EnableMouseCellMotion)
				a.notify("Mouse on")
			} else {
				a.queue(tea.DisableMouse)
				a.notify("Mouse off: the terminal's own selection works again")
			}
			return nil
		}},
		{names: []string{"keyhelp"}, title: "Keys for the focused surface", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.openKeyHelp()
			return nil
		}},
		{names: []string{"vim"}, title: "Toggle Vim mode (this session)", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.prefs.VimMode = !a.prefs.VimMode
			for _, buf := range a.buffers {
				buf.ed.SetOptions(a.editorOptions())
			}
			a.notify(map[bool]string{true: "Vim mode on", false: "Vim mode off: arrows, Enter and Escape only in lists"}[a.prefs.VimMode])
			return nil
		}},
		{names: []string{"emptytrash"}, title: "Empty trash", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.emptyTrash()
			return nil
		}},
		{names: []string{"newfolder", "mkdir"}, title: "New folder", palette: true, run: func(a *App, _ *noteBuffer, cmd vim.ExCommand) error {
			folder, sub := a.currentFolderContext()
			if folder == vault.FolderTrash || folder == vault.FolderQuick {
				folder, sub = vault.FolderInbox, ""
			}
			name := strings.TrimSpace(cmd.Args)
			if name == "" {
				a.createFolderPrompt(folder, sub)
				return nil
			}
			return a.createFolderNamed(folder, sub, name)
		}},
		{names: []string{"version"}, title: "Show version", palette: true, run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error {
			a.notify("zn " + a.opts.Version + " · " + a.backend.Label())
			return nil
		}},
		{names: []string{"harper", "spell"}, title: "Grammar check (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
		{names: []string{"imgwidth"}, title: "Image width (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
		{names: []string{"workflow", "workflows"}, title: "Workflows (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
		{names: []string{"atlas"}, title: "Atlas (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
		{names: []string{"share", "publish"}, title: "Share (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
		{names: []string{"sync"}, title: "Cloud sync (desktop only)", run: func(a *App, _ *noteBuffer, _ vim.ExCommand) error { return errNotInTerminal }},
	}
	a.commands = append(a.commands, a.parityCommands()...)
	return a.commands
}

// runEx is the engine's host hook for commands it does not own.
func (a *App) runEx(buf *noteBuffer, cmd vim.ExCommand) (bool, error) {
	name := strings.ToLower(cmd.Name)
	for _, c := range a.commandTable() {
		for _, n := range c.names {
			if n == name {
				err := c.run(a, buf, cmd)
				if err != nil {
					a.notifyError(err.Error())
				}
				return true, nil
			}
		}
	}
	if strings.HasPrefix(name, "demo") {
		return true, errNotInTerminal
	}
	return false, nil
}

// exCommandNames feeds the ex line's tab completion.
func (a *App) exCommandNames() []string {
	names := []string{}
	for _, c := range a.commandTable() {
		names = append(names, c.names[0])
	}
	sort.Strings(names)
	return names
}

// completeEx proposes completions for an ex line, the way Vim's wildmenu
// does: command names first, then the arguments a command understands.
// It returns the text to keep and the candidates that replace the rest.
func (a *App) completeEx(text string) (string, []string) {
	name, arg, hasArg := strings.Cut(text, " ")
	if !hasArg {
		return "", a.exCommandNames()
	}
	keep := name + " "
	arg = strings.TrimLeft(arg, " ")
	keep = text[:len(text)-len(arg)]
	var cands []string
	switch strings.ToLower(name) {
	case "e", "edit", "open", "o", "sp", "split", "vs", "vsplit", "vsp":
		if a.idx != nil {
			for _, n := range a.idx.notes {
				if n.Folder != vault.FolderTrash {
					cands = append(cands, n.Title)
				}
			}
			sort.Strings(cands)
		}
	case "move", "mv":
		cands = []string{"inbox", "quick", "archive"}
		if a.idx != nil {
			for _, f := range a.idx.folders {
				if f.Folder != vault.FolderTrash {
					cands = append(cands, string(f.Folder)+"/"+f.Subpath)
				}
			}
		}
	case "style", "previewstyle":
		cands = append(append([]string{}, glamourStyleNames...), "zen")
	case "theme":
		cands = []string{"dark", "light", "system"}
	case "view":
		cands = []string{"tasks", "tags", "trash", "quick", "archive", "home", "help", "settings", "list", "board", "calendar"}
	case "template", "tmpl", "insert", "inserttemplate":
		for _, t := range a.allTemplates() {
			cands = append(cands, t.Name)
		}
		if name == "template" || name == "tmpl" {
			cands = append(cands, "save", "builtins")
		}
	case "daily", "today", "weekly", "monthly":
		cands = []string{"today", "yesterday", "tomorrow", "-1", "+1"}
	case "tabmove", "tabm":
		cands = []string{"-1", "+1", "first", "last"}
	case "bind", "map", "unbind":
		for _, e := range a.keymap.Entries() {
			cands = append(cands, e.ID)
		}
	case "mouse":
		cands = []string{"on", "off"}
	case "tasks":
		cands = []string{"list", "kanban", "calendar"}
	case "vault", "vaults", "local":
		for _, v := range config.KnownVaults() {
			cands = append(cands, v.Name)
		}
		if name != "local" {
			for _, p := range config.RemoteProfiles() {
				cands = append(cands, p.Name)
			}
		}
	case "server", "connect":
		for _, p := range config.RemoteProfiles() {
			cands = append(cands, p.Name)
		}
	case "notesort", "sortnotes":
		cands = append([]string{}, noteSortOrders...)
	case "donestyle":
		cands = append([]string{}, doneStyles...)
	case "reveal", "finder":
		cands = []string{"vault"}
	case "copypath", "yankpath":
		cands = []string{"abs"}
	case "sidebar":
		cands = []string{"tags"}
	case "nu", "number", "linenumbers":
		cands = []string{"off", "absolute", "relative", "hybrid"}
	case "tag", "tags":
		if a.idx != nil {
			for _, t := range a.idx.tags {
				cands = append(cands, t.tag)
			}
		}
	case "db", "database", "base":
		cands = a.databaseNames()
	case "help", "h", "manual":
		for _, line := range helpLines(a, "") {
			if strings.HasPrefix(line, "## ") {
				cands = append(cands, strings.ToLower(strings.TrimPrefix(line, "## ")))
			}
		}
	case "filter":
		cands = []string{"due:today", "due:overdue", "due:none", "is:open", "is:done", "is:waiting", "priority:high", "priority:med", "priority:low", "status:"}
		if a.idx != nil {
			for _, t := range a.idx.tags {
				cands = append(cands, "#"+t.tag)
			}
		}
	default:
		return "", nil
	}
	return keep, cands
}

// runCommandLine executes an ex line from a non-editor surface.
func (a *App) runCommandLine(line string) {
	line = strings.TrimSpace(strings.TrimPrefix(line, ":"))
	if line == "" {
		return
	}
	if buf := a.activeBuffer(); buf != nil {
		buf.ed.ExecuteEx(line)
		return
	}
	name, args, _ := strings.Cut(line, " ")
	bang := strings.HasSuffix(name, "!")
	name = strings.TrimSuffix(name, "!")
	handled, err := a.runEx(nil, vim.ExCommand{Name: name, Args: strings.TrimSpace(args), Bang: bang, Raw: line})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if !handled {
		a.notifyError("Not an editor command: " + name)
	}
}

// openLocalEx opens a `:` prompt for panels without an editor.
func (a *App) openLocalEx() {
	a.overlay = &exPrompt{input: newTextInput("")}
}

func (a *App) openCommandPalette() {
	items := []paletteItem{}
	seen := map[string]bool{}
	for _, c := range a.commandTable() {
		if !c.palette || seen[c.title] {
			continue
		}
		seen[c.title] = true
		items = append(items, paletteItem{label: c.title, detail: ":" + c.names[0], id: c.names[0]})
	}
	extra := []paletteItem{
		{label: "Toggle favorite for this note", detail: "Space l s", id: "fav"},
		{label: "New quick note", id: "quicknote"},
		{label: "Hint mode", detail: "Space h", id: "hints"},
	}
	items = append(items, extra...)
	a.overlay = &palette{title: "Commands", placeholder: "Command", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		switch it.id {
		case "quicknote":
			a.newQuickNote()
		case "hints":
			a.startHintMode()
		default:
			a.runCommandLine(it.id)
		}
	}}
}

func (a *App) openKeymapList() {
	items := []paletteItem{}
	for _, e := range a.keymap.Entries() {
		binding := e.DefaultBinding
		if binding == "" {
			binding = "(unbound)"
		}
		items = append(items, paletteItem{label: e.Title, detail: e.ID, hint: binding, id: e.ID})
	}
	a.overlay = &palette{title: "Keymap actions (override under [keymaps] in config.toml)", placeholder: "Action", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		a.notify(it.id + " = " + it.hint)
	}}
}

// --- command helpers ---

func (a *App) saveBufferReport(buf *noteBuffer) error {
	if buf == nil {
		return nil
	}
	if err := a.saveBuffer(buf); err != nil {
		return err
	}
	buf.ed.ClearMessage()
	a.notify(fmt.Sprintf("\"%s\" %dL written", buf.path, buf.ed.LineCount()))
	return nil
}

func (a *App) saveAs(buf *noteBuffer, title string) error {
	if buf == nil {
		return fmt.Errorf("no note is active")
	}
	title = strings.TrimSpace(title)
	if err := a.saveBuffer(buf); err != nil {
		return err
	}
	next, err := a.backend.RenameNote(a.ctx, buf.path, title)
	if err != nil {
		return err
	}
	a.repointNote(buf.path, next.Path)
	a.notify("Renamed to " + next.Title)
	a.refreshIndex()
	return nil
}

func (a *App) closeActiveTabCommand(force bool) error {
	p := a.activePane
	if len(p.tabs) == 0 {
		if len(a.panes.leaves()) > 1 {
			a.panes.remove(p)
			a.activePane = a.panes.leaves()[0]
			a.layout()
			return nil
		}
		a.quit()
		return nil
	}
	if buf := a.activeBuffer(); buf != nil && !force {
		if err := a.saveBuffer(buf); err != nil {
			return err
		}
	}
	lastTab := len(p.tabs) == 1 && len(a.panes.leaves()) == 1
	a.closeTab(p, p.active)
	if lastTab {
		a.quit()
	}
	a.layout()
	return nil
}

// closeOtherTabs closes every unpinned tab except the active one.
func (a *App) closeOtherTabs() {
	p := a.activePane
	keep := p.activeTab()
	for i := len(p.tabs) - 1; i >= 0; i-- {
		if p.tabs[i] != keep && !p.tabs[i].pinned {
			a.closeTab(p, i)
		}
	}
}

func (a *App) openArgument(arg string) error {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil
	}
	if _, ok := a.noteMeta(arg); ok {
		a.openNote(arg, true)
		return nil
	}
	if a.idx != nil {
		if meta, ok := vault.ResolveWikilink(a.idx.notes, arg); ok {
			a.openNote(meta.Path, true)
			return nil
		}
		if matches := search.Notes(a.idx.search, arg, 1, false); len(matches) > 0 {
			a.openNote(matches[0].Path, true)
			return nil
		}
	}
	return fmt.Errorf("no note matches %q", arg)
}

// parseFolderTarget reads `inbox/Work`, `archive`, `quick` or a custom label.
func (a *App) parseFolderTarget(target string) (vault.NoteFolder, string, error) {
	target = strings.Trim(strings.TrimSpace(target), "/")
	head, sub, _ := strings.Cut(target, "/")
	switch strings.ToLower(head) {
	case "inbox", "notes":
		return vault.FolderInbox, sub, nil
	case "quick", "quick-notes", "quicknotes":
		return vault.FolderQuick, sub, nil
	case "archive":
		return vault.FolderArchive, sub, nil
	case "trash":
		return vault.FolderTrash, sub, nil
	}
	for _, f := range []vault.NoteFolder{vault.FolderInbox, vault.FolderQuick, vault.FolderArchive} {
		if strings.EqualFold(a.folderLabel(f), head) {
			return f, sub, nil
		}
	}
	// A bare subfolder name under the inbox.
	if a.idx != nil {
		for _, f := range a.idx.folders {
			if f.Folder == vault.FolderInbox && strings.EqualFold(f.Subpath, target) {
				return vault.FolderInbox, f.Subpath, nil
			}
		}
	}
	return "", "", fmt.Errorf("unknown folder: %s (use inbox/Sub, quick, archive)", target)
}

func (a *App) periodicCommand(kind periodic.Kind, args string) error {
	arg := strings.ToLower(strings.TrimSpace(args))
	date := time.Now()
	switch arg {
	case "", "today", "this":
	case "yesterday", "prev", "previous", "last":
		date = shiftPeriod(kind, date, -1)
	case "tomorrow", "next":
		date = shiftPeriod(kind, date, 1)
	default:
		if n, err := strconv.Atoi(arg); err == nil {
			date = shiftPeriod(kind, date, n)
		} else if d, err := time.ParseInLocation("2006-01-02", arg, time.Local); err == nil {
			date = d
		} else {
			return fmt.Errorf("unknown date: %s (use YYYY-MM-DD, yesterday, tomorrow or an offset)", args)
		}
	}
	a.openPeriodicOn(kind, date)
	return nil
}

// addTaskToDaily appends a task line to a day's daily note, creating the
// note when needed. Without daily notes the task goes to an Inbox "Tasks"
// note.
func (a *App) addTaskToDaily(text string, date time.Time) {
	line := "- [ ] " + strings.TrimSpace(text)
	if a.idx != nil && a.idx.settings.DailyNotes.Enabled {
		folder, sub, title, rel := a.periodicLocation(periodic.Daily, date)
		if _, ok := a.noteMeta(rel); !ok {
			body := a.periodicBody(periodic.Daily, title, date)
			meta, err := a.backend.CreateNote(a.ctx, folder, title, sub, &body)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			a.ignoreChange(meta.Path)
			a.addNoteToIndex(meta)
			rel = meta.Path
		}
		if err := a.mutateNote(rel, func(body string) (string, bool) {
			return vault.InsertTasksUnderTasksHeading(body, []string{line}), true
		}); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.notify("Added task to " + date.Format("2006-01-02"))
		a.refreshIndex()
		a.queue(func() tea.Msg { return a.loadTasksCmd()() })
		return
	}
	rel := ""
	if a.idx != nil {
		if meta, ok := vault.ResolveWikilink(a.idx.notes, "Tasks"); ok && meta.Folder == vault.FolderInbox {
			rel = meta.Path
		}
	}
	if rel == "" {
		body := "# Tasks\n\n"
		meta, err := a.backend.CreateNote(a.ctx, vault.FolderInbox, "Tasks", "", &body)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		a.ignoreChange(meta.Path)
		a.addNoteToIndex(meta)
		rel = meta.Path
	}
	if err := a.mutateNote(rel, func(body string) (string, bool) {
		return vault.AppendToBody(body, line), true
	}); err != nil {
		a.notifyError(err.Error())
		return
	}
	a.notify("Added task to Tasks")
	a.refreshIndex()
	a.queue(func() tea.Msg { return a.loadTasksCmd()() })
}

func (a *App) createFolderNamed(folder vault.NoteFolder, parent, name string) error {
	sub := strings.Trim(name, "/")
	if parent != "" {
		sub = parent + "/" + sub
	}
	if err := a.backend.CreateFolder(a.ctx, folder, sub); err != nil {
		return err
	}
	a.notify("Created folder " + sub)
	a.refreshIndex()
	return nil
}

// openPreviewStylePicker lists the built-in Glamour styles plus zen.
func (a *App) openPreviewStylePicker() {
	items := []paletteItem{}
	for _, name := range glamourStyleNames {
		items = append(items, paletteItem{label: name, id: name})
	}
	items = append(items, paletteItem{label: "zen", detail: "built-in renderer", id: "zen"})
	a.overlay = &palette{title: "Preview style (or :style path/to/style.json)", placeholder: "Style", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		if err := a.setPreviewStyle(it.id); err != nil {
			a.notifyError(err.Error())
			return
		}
		a.notify("Preview style: " + it.id)
	}}
}

// openTasksMode opens the Tasks view, switched to a mode when one is
// named: list, kanban (board) or calendar. Empty keeps the current mode.
func (a *App) openTasksMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "list", "kanban", "board", "calendar":
	default:
		return fmt.Errorf("unknown tasks mode %q: list, kanban or calendar", mode)
	}
	a.openTasks()
	if mode == "" {
		return nil
	}
	if t := a.activeTab(); t != nil {
		if tv, ok := t.view.(*tasksView); ok {
			tv.setMode(mode)
		}
	}
	return nil
}
