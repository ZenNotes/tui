package tui

import (
	"fmt"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
	"sort"
	"strconv"
	"strings"
)

type settingRow struct {
	section, key string
	value        any
	vault        bool
}
type settingsView struct {
	scroll, rows int
	dscroll      int
	lines        []string
	filter       string
	list         listCursor
	settings     []settingRow
}

func (v *settingsView) title() string      { return "Settings" }
func (v *settingsView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }
func (v *settingsView) refresh(a *App) {
	v.settings = nil
	v.lines = nil
	add := func(s settingRow) {
		scope := "user"
		if s.vault {
			scope = "vault"
		}
		line := scope + " · " + s.section + "." + s.key + " = " + fmt.Sprint(s.value)
		if strings.Contains(strings.ToLower(line), strings.ToLower(v.filter)) {
			v.settings = append(v.settings, s)
			v.lines = append(v.lines, line)
		}
	}
	tables := config.ValueTables(a.prefs.Prefs)
	for _, section := range []string{"vim", "editor", "appearance", "view", "terminal"} {
		keys := []string{}
		for k := range tables[section] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "unified_sidebar" || key == "wrap_tabs" {
				continue
			}
			add(settingRow{section, key, tables[section][key], false})
		}
	}
	if a.idx != nil {
		for _, p := range []struct {
			name  string
			value vault.PeriodicNotes
		}{{"dailyNotes", a.idx.settings.DailyNotes}, {"weeklyNotes", a.idx.settings.WeeklyNotes}, {"monthlyNotes", a.idx.settings.MonthlyNotes}} {
			for _, kv := range []struct {
				k string
				v any
			}{{"enabled", p.value.Enabled}, {"directory", p.value.Directory}, {"titlePattern", p.value.TitlePattern}, {"locale", p.value.Locale}} {
				add(settingRow{p.name, kv.k, kv.v, true})
			}
		}
		add(settingRow{"tasks", "includedFolders", strings.Join(a.idx.settings.TasksIncludedFolders, ", "), true})
		add(settingRow{"tasks", "excludedFolders", strings.Join(a.idx.settings.TasksExcludedFolders, ", "), true})
	}
	v.list.clamp(len(v.settings))
}
func (v *settingsView) render(a *App, w, h int, focused bool) string {
	if v.lines == nil {
		v.refresh(a)
	}
	v.rows = max(1, h-2)
	v.list.ensureVisible(v.rows)
	v.scroll = v.list.scroll
	lines := []string{padRight(truncateCells(" Enter edit · / filter · v version · :config external editor", w), w), padRight(" "+v.filter, w)}
	disp, cur := v.display(w)
	body := max(1, h-2)
	if cur >= 0 {
		start := cur
		for n := 0; n < 2 && start > 0 && disp[start-1].idx < 0; n++ {
			start--
		}
		if start < v.dscroll {
			v.dscroll = start
		} else if cur >= v.dscroll+body {
			v.dscroll = cur - body + 1
		}
	}
	v.dscroll = max(0, min(v.dscroll, len(disp)-1))
	for i := v.dscroll; i < len(disp) && len(lines) < h; i++ {
		line := padRight(disp[i].text, w)
		switch {
		case disp[i].idx < 0:
			line = a.theme.Heading.Render(line)
		case focused && disp[i].idx == v.list.cursor:
			line = a.theme.SelectedFocus.Render(line)
		}
		lines = append(lines, line)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

type settingDisplay struct {
	text string
	idx  int // index into settings; -1 for a heading
}

// display lays the settings out under two top-level sections, User and Vault,
// each split into subsections named by the config prefix (vim, editor,
// dailyNotes, ...). Rows keep their settings order, so the cursor index maps
// straight onto v.settings. It returns the cell index of the cursor row.
func (v *settingsView) display(w int) ([]settingDisplay, int) {
	out := []settingDisplay{}
	cur := -1
	scope, sub := "", ""
	for i, s := range v.settings {
		name := "User"
		if s.vault {
			name = "Vault"
		}
		if name != scope {
			if scope != "" {
				out = append(out, settingDisplay{"", -1})
			}
			scope, sub = name, ""
			out = append(out, settingDisplay{truncateCells(" "+strings.ToUpper(name), w), -1})
		}
		if s.section != sub {
			sub = s.section
			out = append(out, settingDisplay{truncateCells("  "+sub, w), -1})
		}
		if i == v.list.cursor {
			cur = len(out)
		}
		out = append(out, settingDisplay{truncateCells("    "+s.key+" = "+fmt.Sprint(s.value), w), i})
	}
	return out, cur
}
func (v *settingsView) handleKey(a *App, k vim.Key) bool {
	if k.IsRune('v') {
		_ = a.showVersion(vim.ExCommand{Name: "version"})
		return true
	}
	if a.listNav(&v.list, k, len(v.settings), v.rows) {
		return true
	}
	if k.Is("enter") {
		if v.list.cursor < len(v.settings) {
			v.edit(a, v.settings[v.list.cursor])
		}
		return true
	}
	if k.IsRune('/') {
		a.promptFor("Filter settings", v.filter, "Names, values or scope", func(a *App, s string) { v.filter = s; v.list = listCursor{}; v.refresh(a) })
		return true
	}
	if k.IsRune(':') {
		a.openLocalEx()
		return true
	}
	return false
}

var settingChoices = map[string][]string{
	"which_key_hint_mode": {"timed", "sticky"}, "wrapped_line_motions": {"display", "logical"}, "default_view_mode": {"edit", "split", "preview"}, "line_number_mode": {"off", "absolute", "relative"}, "completed_task_style": {"none", "gray", "strikethrough", "gray-strikethrough"}, "time_format": {"12h", "24h"}, "theme_mode": {"dark", "light", "system"}, "note_sort_order": {"modified-desc", "created-desc", "title-asc", "title-desc"}, "calendar_week_start": {"monday", "sunday"}, "tasks_view_mode": {"list", "kanban", "calendar"}, "preview_style": {"auto", "dark", "light", "ascii", "notty"}, "images": {"auto", "kitty", "off"}, "math": {"typst", "latex"},
}

func (v *settingsView) edit(a *App, row settingRow) {
	save := func(value any) {
		var err error
		if row.vault {
			err = a.backend.UpdateVaultSettings(a.ctx, func(raw map[string]any) {
				if row.section == "tasks" {
					setTasksFolderList(raw, row.key, fmt.Sprint(value))
					return
				}
				m, _ := raw[row.section].(map[string]any)
				if m == nil {
					m = map[string]any{}
					raw[row.section] = m
				}
				m[row.key] = value
			})
		} else {
			err = config.SetValue(row.section, row.key, value)
		}
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		if row.vault {
			a.refreshIndex()
		} else if err = a.reloadPreferences(); err != nil {
			a.notifyError(err.Error())
		}
		v.refresh(a)
	}
	if b, ok := row.value.(bool); ok {
		save(!b)
		return
	}
	choices := settingChoices[row.key]
	if row.key == "note_sort_order" {
		choices = noteSortOrders
	}
	if row.key == "preview_style" {
		choices = append(append([]string{}, glamourStyleNames...), "zen")
	}
	if len(choices) > 0 {
		items := []paletteItem{}
		for _, c := range choices {
			items = append(items, paletteItem{label: c, id: c})
		}
		a.overlay = &palette{title: row.section + "." + row.key, items: items, filtered: items, onSelect: func(a *App, it paletteItem) { save(it.id) }}
		return
	}
	a.promptFor(row.section+"."+row.key, fmt.Sprint(row.value), "Saved immediately · Esc cancels", func(a *App, text string) {
		if _, ok := row.value.(int); ok {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || n < 0 || n > 10000 {
				a.notifyError("Enter an integer from 0 to 10000")
				return
			}
			if row.key == "tab_size" && (n < 1 || n > 16) {
				a.notifyError("Tab size must be 1–16")
				return
			}
			if row.key == "which_key_hint_timeout_ms" && (n < 400 || n > 3000) {
				a.notifyError("Leader timeout must be 400–3000 ms")
				return
			}
			save(n)
		} else {
			save(text)
		}
	})
}

// setTasksFolderList writes one of the vault's Tasks folder lists
// (`tasks.excludedFolders`, or the Task folders in `tasks.includedFolders`,
// #962) from a comma-separated edit, keeping the other keys under `tasks`. An
// empty edit removes the list, and an empty `tasks` object goes with it. The
// excluded list used to land in a top-level `tasksExcludedFolders` key that no
// runtime reads, so an edit here changed nothing.
func setTasksFolderList(raw map[string]any, key, value string) {
	parts := []string{}
	for _, s := range strings.Split(value, ",") {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	tasks, _ := raw["tasks"].(map[string]any)
	if tasks == nil {
		tasks = map[string]any{}
	}
	if len(parts) == 0 {
		delete(tasks, key)
	} else {
		tasks[key] = parts
	}
	if len(tasks) == 0 {
		delete(raw, "tasks")
	} else {
		raw["tasks"] = tasks
	}
}
