package tui

import (
	"strings"

	"github.com/ZenNotes/zennotescli/internal/config"
)

func configPathInfo() (string, string, error) {
	return config.UserDataDir(), config.ConfigTomlPath(), nil
}

// helpLines is the in-app manual: `# ` and `## ` headings, `key<TAB>desc`
// rows, plain prose. A filter keeps sections with a matching line.
func helpLines(a *App, filter string) []string {
	leader := "Space"
	if a.keymap.Binding("vim.leaderPrefix") != "" {
		leader = a.keymap.Binding("vim.leaderPrefix")
	}
	all := []string{
		"# ZenNotes in the terminal",
		"zn tui is ZenNotes without the window: the same vault, notes, tasks, tags, templates and Vim motions,",
		"driven from the keyboard. Every key below assumes Vim mode ([vim] enabled = true in config.toml).",
		"With Vim mode off, lists answer only to arrows, Enter and Escape; modifier chords still work.",
		"",
		"## Layout",
		"Alt chords on macOS\tTerminals send Option as a character unless told otherwise: Ghostty needs macos-option-as-alt = true, iTerm2 \"Esc+\", Terminal.app \"Use Option as Meta\". Every Alt binding also has a leader or Ctrl+W form.",
		"Alt+H / Alt+L\tFocus the sidebar, panes and side panel left or right",
		"Alt+J / Alt+K\tFocus the pane below or above",
		"Ctrl+B\tShow or hide the sidebar",
		"Alt+.\tZen mode (editor only)",
		"Alt+1 … Alt+9\tJump to tab N",
		"Alt+E / Alt+S / Alt+P\tEditor, split (editor + preview) or preview mode for the tab",
		"Alt+Z\tToggle word wrap",
		"Alt+Left / Alt+Right\tBack and forward through the jump list",
		"Ctrl+P\tSearch notes (Enter opens; with no match Enter creates the note; Ctrl+D trashes the selected one)",
		"Ctrl+N\tNew note in the current folder",
		"Ctrl+S\tSave now (notes autosave a moment after you stop typing)",
		"Ctrl+C\tEscape in insert or visual mode; quits from normal mode after saving",
		"Ctrl+Z\tSuspend to the shell; fg returns",
		"?\tIn lists and views: the keys of that surface, following your [keymaps] overrides",
		"",
		"## Leader (" + leader + ")",
		leader + " f\tSearch notes",
		leader + " s t\tSearch vault text (full text, all notes)",
		leader + " o\tOpen buffers and recent notes",
		leader + " e\tToggle sidebar",
		leader + " p\tOutline panel",
		leader + " c\tCalendar panel",
		leader + " l f / y / s\tNote actions: format, copy as Markdown, toggle favorite",
		leader + " l r / m / d / a\tRename, move, trash, archive the note",
		leader + " l c / o\tCopy the note link, open the note in the desktop app",
		leader + " l e\tEdit the note in $EDITOR at the cursor line, reload on return (also :editor)",
		leader + " q\tQuick capture (first line becomes the title, Ctrl+S files it under Quick Notes)",
		leader + " t / i\tNew note from a template, insert a template",
		leader + " d / w / m\tToday's daily note, this week's, this month's",
		leader + " h\tHint mode: labels over links and rows, type a label to jump",
		leader + " z p / s / e / v\tView: preview mode, split mode, editor mode, toggle editor and preview (also Alt+P / Alt+S / Alt+E, :tp)",
		leader + " z w / n / z / t\tView: word wrap, line numbers, zen mode, theme",
		leader + " x\tTasks",
		leader + " n\tNew note",
		leader + " ;\tCommand palette (every ex command, searchable)",
		leader + " ?\tThis manual",
		"Hints appear after the which-key delay (vim.which_key_hint_mode = instant | timed).",
		"",
		"## Editor: motions",
		"h j k l w b e W B E 0 ^ $\tThe usual",
		"gg G { } ( ) H M L\tFile, paragraph, sentence and screen motions",
		"f t F T ; ,\tFind a character on the line",
		"% * # n N / ?\tMatching bracket, word search, pattern search (with incremental highlight)",
		"]] [[ ][ []\tNext and previous Markdown heading",
		"Ctrl+D Ctrl+U Ctrl+F Ctrl+B Ctrl+E Ctrl+Y zt zz zb\tScrolling",
		"Ctrl+O Ctrl+I\tJump list, across notes too",
		"gj gk\tMove by display row when a line wraps",
		"",
		"## Editor: operators and text objects",
		"d c y > < gu gU g~ = J gq\tOperators; double them for a line (dd, cc, yy, >>)",
		"iw aw iW aW is as ip ap\tWord, sentence and paragraph objects",
		"i( a( i[ a[ i{ a{ i< a< i\" a\" i' a' i` a`\tBracket and quote objects",
		"it at\tTag objects (HTML in notes)",
		"il al\tThe current list item",
		"x X s S r R ~\tCharacter edits",
		"i a I A o O\tEnter insert mode; o continues a list or checkbox",
		"u Ctrl+R .\tUndo, redo, repeat",
		"\"a … \"+ … \"_\tNamed, clipboard and black-hole registers",
		"qa … q @a @@\tRecord and replay macros",
		"ma 'a `a ''\tMarks",
		"Ctrl+A Ctrl+X\tIncrement or decrement the number under the cursor",
		"v V Ctrl+V gv o\tCharacter, line and block visual modes; block insert with I and A",
		"",
		"## Editor: Markdown",
		"Ctrl+L\tToggle the checkbox on the line (also in insert mode)",
		"gd\tFollow the link under the cursor (wikilink, Markdown link or URL); missing notes are created on confirm",
		"gx\tOpen the URL under the cursor in the browser",
		"gy\tCopy the link under the cursor",
		"zc zo za zM zR\tFold and unfold headings",
		"Ctrl+Q / gqip\tReflow the paragraph",
		"Enter in a list\tContinues the list or checkbox; Enter on an empty item ends it",
		"Tab / Shift+Tab in a list\tIndent or outdent the item",
		"jk\tLeaves insert mode (vim.insert_escape in config.toml)",
		"Auto-pairs, `->` to → and other text replacements follow the editor preferences.",
		"",
		"## Ex commands",
		":w :q :wq :x :qa :wa :bd\tSave, close the tab (the last tab quits), quit everything",
		":e <note> :new <title> :vs :sp :only\tOpen, create, split",
		":bn :bp ]b [b gt gT\tNext and previous tab",
		":pin :tabmove -1|+1|first|last\tPin or unpin the tab (pinned tabs sit first and survive :tabonly); reorder tabs",
		":link, or [[ in insert mode\tPick a note and insert a wikilink to it",
		":s/x/y/g :%s :g/pat/d :v :sort :norm\tSubstitute, global, sort and normal, with ranges",
		":move inbox/Work :mv archive\tMove the note to a folder",
		":rename <title> :delete :archive :restore :duplicate :favorite\tNote actions",
		":tasks :tags :trash :quick :archived :home :help :settings\tOpen a view",
		":daily [date] :weekly :monthly\tPeriodic notes (yesterday, tomorrow, +2, 2026-03-01)",
		":template <name> :insert <name>\tTemplates",
		":newtask <text> :filter <query>\tTasks",
		":outline :connections :calendar :closepanel :sidebar :zen\tPanels",
		":editmode :splitmode :previewmode\tTab view mode",
		":fold :unfold :foldall :unfoldall :format :wrap :nu :theme :vim\tEditor state",
		":db <name> :dbnew <name>\tDatabases",
		":bind <id> <keys> :unbind <id> :keymaps\tKeymaps for this session; make them permanent under [keymaps] in config.toml",
		":vault [name|path] :server [name|url] :local\tSwitch vaults and servers in place; the picker lists what zn and the desktop app know and can add a folder or connect to a server (the token prompt is masked and the token is stored); the vault you switch to becomes the default for the next launch",
		":rollover\tMove the newest earlier daily note's open tasks into today's note and open it",
		":forward :inprogress :cancel\tThe task under the cursor: forward it to today's note, mark it [/], mark it [-]",
		":taskfile [folder]\tA new task note (a note tagged task) in a folder",
		":copypath [abs] :reveal [vault]\tCopy the note's (or the selected folder's) path; show it in the file manager",
		":tabcloseright :tabmenu :ref\tClose the tabs to the right; the active tab's menu; pin the note as a reference in a preview split",
		":unarchive :purge :assets\tBring a note back from the archive; delete a trashed note for good; the attachment files",
		":notesort <order> :donestyle <style> :whichkey :tabs :quickdate :template save|builtins :sidebar tags\tSidebar sort, completed-task style, leader hints, the tab strip, dated quick-note titles, templates, the tags section",
		":website :discord :github :releases :issue\tZenNotes on the web",
		":commands\tThe command palette, with every command the desktop palette has that works in a terminal",
		"Tab / Shift+Tab on the : line\tCycle completions of the command, then of its argument (note titles, folders, styles, templates, views…); ↑↓ recall history",
		"",
		"## Home",
		"j k Enter\tMove through today's note, tasks, recent notes, the week strip, favorites and tags; Enter opens",
		"x\tToggle the selected task",
		"h l\tOn the week strip: pick a day; Enter opens or creates its daily note",
		"",
		"## Sidebar",
		"j k gg G Ctrl+D Ctrl+U\tMove",
		"Enter / l / o\tOpen a note or view; open or toggle a folder",
		"h\tCollapse the folder, or jump to its parent",
		"n / N\tNew note or new folder in the selected folder",
		"r / x / s\tRename, trash (or delete a folder), toggle favorite",
		"m\tContext menu with every action",
		"/\tSearch notes",
		"Esc\tBack to the editor",
		"",
		"## Mouse",
		"click\tFocus and select; double click opens a note, toggles a task, or edits a cell",
		"right click\tThe same menu as m: notes, tasks, tabs, tags",
		"wheel\tScroll the surface under the pointer",
		"drag\tSelect text in the editor (visual mode); drag the sidebar edge or a split divider to resize",
		"Ctrl+click\tFollow the link under the pointer",
		":mouse\tToggle mouse support; off hands selection back to the terminal (Shift or Option also bypass it)",
		"",
		"## Themes and styles",
		":theme dark | light | system\tInterface palette; system follows the terminal background",
		":style <name|path|zen>\tReading view style: auto (zennotes, matches the palette), dark, light, dracula, tokyo-night, pink, ascii, notty, a Glamour JSON file, or the built-in zen renderer",
		"[terminal] preview_style\tThe same setting in config.toml, next to mouse = true|false",
		"",
		"## Preview mode",
		"Esc / q / i / Ctrl+E\tBack to the editor (i lands on the line under the cursor); Space z e, :editmode and Alt+E do the same",
		"Embeds\tPictures paint inline in Kitty and Ghostty ([terminal] images in config.toml; tmux needs allow-passthrough on); PDF, audio, video, drawings, files and YouTube or Vimeo links are cards, Enter opens them; ![[Note]] renders the note inline; mermaid blocks draw as text, or as pictures with mmdc installed; $$ math renders through typst when installed",
		"j k gg G Ctrl+D Ctrl+U\tScroll",
		"Enter / gd\tFollow the link on the row",
		"x\tToggle the task on the row",
		"i / e\tEdit at that line",
		"",
		"## Tasks",
		"Open it\t:tasks, Space x, or the Tasks row in the sidebar; :tasks kanban, :kanban, Space k and the sidebar's kanban row open the board, :taskcalendar (or the calendar row) the calendar",
		"v\tCycle list, board and calendar",
		"x / Enter\tToggle done; open the task's note at its line",
		"p d w c /\tPriority, due date, waiting, cancelled, in progress",
		"e s < >\tEdit text, set a field, shift the due date by a day",
		"J K\tMove the task up or down inside its note",
		"n f F A\tNew task in the daily note, filter, clear the filter, include archived",
		"m\tTask menu (includes forward to today's daily note)",
		"Board: h l j k\tMove between columns and cards; H L move the card; g cycles the grouping",
		"Calendar: h j k l [ ] t\tMove days and months, t is today; Enter lists the day's tasks; o opens the daily note",
		"Filter syntax\twords, #tag, tag:x, note:x, due:today|overdue|none, priority:high, status:x, is:open|done|waiting, -term",
		"",
		"## Tags",
		"Tab\tSelect or deselect the tag (multi-select)",
		"a / c\tMatch any or all of the selected tags; clear the selection",
		"l / h / Enter\tMove between the tag list and the notes; open the note",
		"r / x\tRename or remove the tag in every note",
		"",
		"## Quick Notes, Archive, Trash",
		"n\tNew quick note (Quick Notes)",
		"u\tUnarchive (Archive)",
		"r / x / E\tRestore, delete permanently, empty the trash (Trash)",
		"",
		"## Databases",
		"Any .base folder, and any loose .csv file, is a database; a loose file keeps its schema in <name>.csv.base.json and gains record pages once converted to a folder (:dbconvert).",
		"h j k l 0 $ gg G\tMove between cells",
		"i / Enter\tEdit the cell, type-aware: pickers for select, multi-select and note links (Tab toggles, Enter commits), toggles a checkbox, date shorthand for dates",
		"[[ in a text cell\tLink a note from the cell",
		"x / Ctrl+A / Esc\tSelect the row, select every row, clear the selection",
		"a / dd / D\tAdd a row; delete the selected rows (or the current one); duplicate the row",
		"m\tRow menu: open page, duplicate, delete, copy as CSV",
		"o\tOpen (or create) the record's page; Ctrl+O comes back",
		"k (into the header) / Esc\tEnter and leave the header row",
		"header: i / t / H / L / x / dd / a\tRename, change the type, move left or right, hide, delete, add a field",
		"header: s\tSort by the column (asc, desc, clear), saved into the view",
		"f\tFilters of the view: add, remove, match all or any",
		"V\tViews: switch, new table, new board, rename, delete",
		"board: h l j k / H L / a / A / gb / < >\tMove between cards; move the card to the next column; add a card; add a column; change the group-by field; reorder columns",
		"C\tOn a select cell, a board column, or a select field's header: color the option",
		"yy\tCopy the selected rows (or the current one) as CSV",
		"R\tRaw CSV toggle",
		"",
		"## Panes",
		"Ctrl+W v / s\tSplit right or down",
		"Ctrl+W h j k l\tFocus a neighbor",
		"Ctrl+W q / o / w\tClose the tab, keep only this pane, cycle panes",
		"Ctrl+W < > =\tResize, equalize",
		"",
		"## Configuration",
		"~/.config/zennotes/config.toml\tShared with the desktop app: [vim], [editor], [appearance], [view], [keymaps], [text_replacements]",
		".zennotes/vault.json\tVault settings: system folders, periodic notes, favorites, task exclusions",
		"ZENNOTES_VAULT / ZENNOTES_SERVER / ZENNOTES_REMOTE_TOKEN\tPick a vault or server without flags",
		"zn tui --vault <name|path>\tOpen a specific vault; --server <name|url> opens a self-hosted server",
		"",
		"## Not in the terminal",
		"Workflows, Atlas, sharing, cloud sync, Harper grammar checks, image resizing and rendered math or diagrams stay in the desktop app.",
		"Space l o hands the current note to the desktop app when it is installed.",
	}
	if strings.TrimSpace(filter) == "" {
		return all
	}
	q := strings.ToLower(filter)
	out := []string{}
	section := []string{}
	keep := false
	flush := func() {
		if keep {
			out = append(out, section...)
			out = append(out, "")
		}
		section = nil
		keep = false
	}
	for _, line := range all {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			flush()
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			section = append(section, line)
			if strings.Contains(strings.ToLower(line), q) {
				keep = true
			}
			continue
		}
		if strings.Contains(strings.ToLower(line), q) {
			keep = true
			section = append(section, line)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{"No manual entry matches \"" + filter + "\""}
	}
	return out
}
