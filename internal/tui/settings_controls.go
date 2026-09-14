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
	lines := []string{padRight(" Enter edit · / filter · :config external editor", w), padRight(" "+v.filter, w)}
	for i := v.scroll; i < len(v.lines) && len(lines) < h; i++ {
		line := padRight(" "+truncateCells(v.lines[i], max(1, w-2)), w)
		if focused && i == v.list.cursor {
			line = a.theme.SelectedFocus.Render(line)
		}
		lines = append(lines, line)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}
func (v *settingsView) handleKey(a *App, k vim.Key) bool {
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
	"which_key_hint_mode": {"timed", "sticky"}, "wrapped_line_motions": {"display", "logical"}, "default_view_mode": {"edit", "split", "preview"}, "line_number_mode": {"off", "absolute", "relative"}, "completed_task_style": {"none", "gray", "strikethrough", "gray-strikethrough"}, "time_format": {"12h", "24h"}, "theme_mode": {"dark", "light", "system"}, "note_sort_order": {"modified-desc", "created-desc", "title-asc", "title-desc"}, "calendar_week_start": {"monday", "sunday"}, "tasks_view_mode": {"list", "kanban", "calendar"}, "preview_style": {"auto", "dark", "light", "ascii", "notty"}, "images": {"auto", "kitty", "off"},
}

func (v *settingsView) edit(a *App, row settingRow) {
	save := func(value any) {
		var err error
		if row.vault {
			err = a.backend.UpdateVaultSettings(a.ctx, func(raw map[string]any) {
				if row.section == "tasks" {
					parts := []string{}
					for _, s := range strings.Split(fmt.Sprint(value), ",") {
						if s = strings.TrimSpace(s); s != "" {
							parts = append(parts, s)
						}
					}
					raw["tasksExcludedFolders"] = parts
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
