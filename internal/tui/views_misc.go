package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/zennotescli/internal/vault"
	"github.com/ZenNotes/zennotescli/internal/vim"
)

// --- Help ---

type helpView struct {
	filter string
	lines  []string
	scroll int
	rows   int
}

func newHelpView() *helpView { return &helpView{} }

func (v *helpView) title() string      { return "Help" }
func (v *helpView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *helpView) refresh(a *App) {
	v.lines = helpLines(a, v.filter)
	if v.scroll > max(0, len(v.lines)-1) {
		v.scroll = max(0, len(v.lines)-1)
	}
}

func (v *helpView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	if v.lines == nil {
		v.refresh(a)
	}
	lines := []string{}
	if v.filter != "" {
		lines = append(lines, padRight(" "+th.Muted.Render("filter: ")+th.Tag.Render(v.filter), w))
	}
	v.rows = h - len(lines)
	for i := v.scroll; i < len(v.lines) && len(lines) < h; i++ {
		line := v.lines[i]
		switch {
		case strings.HasPrefix(line, "## "):
			lines = append(lines, padRight(" "+sectionHeader(th, strings.TrimPrefix(line, "## "), "", w-2), w))
		case strings.HasPrefix(line, "# "):
			lines = append(lines, padRight(th.Title.Render(" "+strings.TrimPrefix(line, "# ")), w))
		case strings.Contains(line, "\t"):
			key, desc, _ := strings.Cut(line, "\t")
			lines = append(lines, padRight("   "+th.KeyHint.Render(padRight(key, 22))+th.Base.Render(truncateCells(desc, w-26)), w))
		default:
			lines = append(lines, padRight(" "+th.Dim.Render(truncateCells(line, w-2)), w))
		}
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (v *helpView) handleKey(a *App, k vim.Key) bool {
	n := len(v.lines)
	cur := listCursor{cursor: v.scroll, scroll: v.scroll}
	if a.listNav(&cur, k, max(1, n-v.rows+1), v.rows) {
		v.scroll = cur.cursor
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.filter", "nav.localEx")
	if pending {
		return true
	}
	switch id {
	case "nav.filter":
		a.promptFor("Filter the manual", v.filter, "", func(a *App, text string) {
			v.filter = strings.TrimSpace(text)
			v.scroll = 0
			v.refresh(a)
		})
	case "nav.localEx":
		a.openLocalEx()
	default:
		return false
	}
	return true
}

// --- Settings (read-only) ---

type settingsView struct {
	scroll int
	rows   int
	lines  []string
}

func (v *settingsView) title() string      { return "Settings" }
func (v *settingsView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *settingsView) refresh(a *App) {
	p := a.prefs
	_, cfgPath, _ := configPathInfo()
	v.lines = []string{
		"# Preferences",
		"config.toml\t" + cfgPath,
		"",
		"## Vim",
		"enabled\t" + fmt.Sprint(p.VimMode),
		"insert_escape\t" + p.VimInsertEscape,
		"yank_to_clipboard\t" + fmt.Sprint(p.VimYankToClipboard),
		"which_key_hints\t" + fmt.Sprint(p.WhichKeyHints) + " (" + p.WhichKeyHintMode + ", " + fmt.Sprint(p.WhichKeyHintTimeoutMs) + "ms)",
		"",
		"## Editor",
		"tab_size\t" + fmt.Sprint(p.EditorTabSize),
		"scroll_off\t" + fmt.Sprint(p.EditorScrollOff),
		"word_wrap\t" + fmt.Sprint(p.WordWrap),
		"default_view_mode\t" + p.DefaultPaneMode,
		"line_number_mode\t" + p.LineNumberMode,
		"completed_task_style\t" + p.CompletedTaskStyle,
		"auto_pairs\t" + fmt.Sprint(p.AutoPairs),
		"text_replacements\t" + fmt.Sprint(p.TextReplacementsEnabled) + " " + fmt.Sprint(p.TextReplacements),
		"sync_title_heading\t" + fmt.Sprint(p.SyncTitleHeadingOnRename),
		"",
		"## Appearance",
		"theme_mode\t" + p.ThemeMode,
		"unified_sidebar\t" + fmt.Sprint(p.UnifiedSidebar) + " (the terminal always nests notes under folders)",
		"",
		"## Views",
		"note_sort_order\t" + p.NoteSortOrder,
		"nested_tags\t" + fmt.Sprint(p.NestedTags),
		"quick_note_title_prefix\t" + p.QuickNoteTitlePrefix,
		"calendar_week_start\t" + p.CalendarWeekStart,
		"tasks_view_mode\t" + p.TasksViewMode,
		"show_archived_tasks\t" + fmt.Sprint(p.ShowArchivedTasks),
		"kanban_group_by\t" + p.KanbanGroupBy,
		"",
		"## Keymap overrides",
	}
	over := a.keymap.Overrides()
	if len(over) == 0 {
		v.lines = append(v.lines, "(none)")
	}
	ids := make([]string, 0, len(over))
	for id := range over {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		val := over[id]
		if val == "" {
			val = "(unbound)"
		}
		v.lines = append(v.lines, id+"\t"+val)
	}
	if a.idx != nil {
		s := a.idx.settings
		v.lines = append(v.lines, "", "# Vault (.zennotes/vault.json)",
			"primary notes\t"+string(a.idx.primary),
			"daily notes\t"+fmt.Sprint(s.DailyNotes.Enabled)+" "+s.DailyNotes.Directory+" / "+s.DailyNotes.TitlePattern,
			"weekly notes\t"+fmt.Sprint(s.WeeklyNotes.Enabled)+" "+s.WeeklyNotes.Directory+" / "+s.WeeklyNotes.TitlePattern,
			"monthly notes\t"+fmt.Sprint(s.MonthlyNotes.Enabled)+" "+s.MonthlyNotes.Directory+" / "+s.MonthlyNotes.TitlePattern,
			"favorites\t"+fmt.Sprint(len(s.Favorites)),
			"tasks excluded\t"+strings.Join(s.TasksExcludedFolders, ", "),
		)
		for _, f := range []vault.NoteFolder{vault.FolderInbox, vault.FolderQuick, vault.FolderArchive, vault.FolderTrash} {
			v.lines = append(v.lines, string(f)+" folder\t"+vault.ResolveFolderPath(f, s.SystemFolderPaths))
		}
	}
	v.lines = append(v.lines, "", "# Connection", "backend\t"+a.backend.Label())
}

func (v *settingsView) render(a *App, w, h int, focused bool) string {
	if v.lines == nil {
		v.refresh(a)
	}
	hv := helpView{lines: v.lines, scroll: v.scroll, rows: h - 1}
	th := a.theme
	lines := []string{padRight(" "+th.Muted.Render("read-only  ·  :config opens config.toml"), w)}
	for i := v.scroll; i < len(v.lines) && len(lines) < h; i++ {
		line := v.lines[i]
		switch {
		case strings.HasPrefix(line, "## "):
			lines = append(lines, padRight(th.Heading.Render(" "+strings.TrimPrefix(line, "## ")), w))
		case strings.HasPrefix(line, "# "):
			lines = append(lines, padRight(th.Title.Render(" "+strings.TrimPrefix(line, "# ")), w))
		case strings.Contains(line, "\t"):
			key, val, _ := strings.Cut(line, "\t")
			lines = append(lines, padRight("   "+th.Dim.Render(padRight(key, 26))+th.Base.Render(truncateCells(val, w-30)), w))
		default:
			lines = append(lines, padRight(" "+line, w))
		}
	}
	_ = hv
	v.rows = h - 1
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (v *settingsView) handleKey(a *App, k vim.Key) bool {
	cur := listCursor{cursor: v.scroll, scroll: v.scroll}
	if a.listNav(&cur, k, max(1, len(v.lines)-v.rows+1), v.rows) {
		v.scroll = cur.cursor
		return true
	}
	if a.listKeyAllowed(k) && k.IsRune(':') {
		a.openLocalEx()
		return true
	}
	return false
}

var _ = context.Background
