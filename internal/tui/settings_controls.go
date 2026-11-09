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

type ruleRowKind int

const (
	rowSetting ruleRowKind = iota
	rowRule
	rowInvalidRule
	rowAddRule
)

const viewModeRulesSection = "terminal.view_mode_rules"

// addRuleValue is the value of the row that adds a rule. Its own type keeps
// that row apart from every rule, whatever key a rule is written under.
type addRuleValue struct{}

func (addRuleValue) String() string { return "Enter to add a rule" }

// kind tells plain settings from the view-mode rule rows. A rule row's key
// is the rule's key exactly as written in config.toml.
func (r settingRow) kind() ruleRowKind {
	switch {
	case r.section != viewModeRulesSection:
		return rowSetting
	case r.value == addRuleValue{}:
		return rowAddRule
	case r.reason() != "":
		return rowInvalidRule
	}
	return rowRule
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
		if s.kind() == rowInvalidRule {
			line += " (invalid: " + s.reason() + ")"
		}
		if s.kind() == rowAddRule || strings.Contains(strings.ToLower(line), strings.ToLower(v.filter)) {
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
		if section == "terminal" {
			v.addRuleRows(a, add)
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
		// The template folder takes its notes out of Tasks too (#1024).
		add(settingRow{"templates", "folder", a.idx.settings.TemplateFolder, true})
	}
	v.list.clamp(len(v.settings))
}

// reason is the short explanation for an invalid rule row.
func (r settingRow) reason() string {
	_, reason := config.ParseViewModeRule(r.key, fmt.Sprint(r.value))
	return reason
}

// addRuleRows lists the view-mode rules after the terminal rows: the valid
// ones in match order, then the invalid ones, then the row that adds a rule.
// The add row stays under every filter.
func (v *settingsView) addRuleRows(a *App, add func(settingRow)) {
	for _, e := range config.ViewModeRuleEntries(a.prefs.ViewModeRules) {
		add(settingRow{viewModeRulesSection, e.Key, e.Mode, false})
	}
	add(settingRow{viewModeRulesSection, "(add rule)", addRuleValue{}, false})
}

// addRule asks for the frontmatter field, then the value, then the mode, and
// writes the rule only after the mode is picked. Esc at any step writes
// nothing.
func (v *settingsView) addRule(a *App) {
	a.promptFor("View-mode rule: frontmatter field", "", "For example page-status · Esc cancels", func(a *App, field string) {
		field = strings.TrimSpace(field)
		if field == "" {
			a.notifyError("Enter a frontmatter field")
			return
		}
		if strings.Contains(field, "=") {
			a.notifyError("The field cannot contain \"=\"")
			return
		}
		a.promptFor("View-mode rule: value of "+field, "", "For example done · a list field matches any element · Esc cancels", func(a *App, value string) {
			value = strings.TrimSpace(value)
			if value == "" {
				a.notifyError("Enter a value to match")
				return
			}
			key := field + "=" + value
			rule, _ := config.ParseViewModeRule(key, "edit")
			for _, e := range config.ViewModeRuleEntries(a.prefs.ViewModeRules) {
				if e.Valid && e.Rule.Key == rule.Key && e.Rule.Value == rule.Value {
					a.notifyError("A rule for " + rule.Key + "=" + rule.Value + " already exists")
					return
				}
			}
			items := []paletteItem{}
			for _, m := range config.ViewModeModes {
				items = append(items, paletteItem{label: m, id: m})
			}
			a.overlay = &palette{title: "Pane mode for " + key, items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
				if err := config.SetViewModeRule(key, it.id); err != nil {
					a.notifyError(err.Error())
					return
				}
				if err := a.reloadPreferences(); err != nil {
					a.notifyError(err.Error())
				}
				v.refresh(a)
			}}
		})
	})
}

// selectedRuleRow is the rule row under the cursor, valid or invalid.
func (v *settingsView) selectedRuleRow() (settingRow, bool) {
	if v.list.cursor < 0 || v.list.cursor >= len(v.settings) {
		return settingRow{}, false
	}
	row := v.settings[v.list.cursor]
	return row, row.kind() == rowRule || row.kind() == rowInvalidRule
}

// applyRuleChange reloads preferences after a rule was written or removed and
// refreshes the rows.
func (v *settingsView) applyRuleChange(a *App, err error) {
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if err := a.reloadPreferences(); err != nil {
		a.notifyError(err.Error())
	}
	v.refresh(a)
}

// removeRule deletes a rule by its key exactly as written in config.toml.
func (v *settingsView) removeRule(a *App, row settingRow) {
	v.applyRuleChange(a, config.RemoveViewModeRule(row.key))
}

// editRule opens the mode palette for a rule row. A rule whose key is
// malformed cannot take a mode, so it only offers remove.
func (v *settingsView) editRule(a *App, row settingRow) {
	items := []paletteItem{}
	if _, reason := config.ParseViewModeRule(row.key, "edit"); reason == "" {
		for _, m := range config.ViewModeModes {
			items = append(items, paletteItem{label: m, id: m})
		}
	}
	items = append(items, paletteItem{label: "remove", id: "remove"})
	a.overlay = &palette{title: "Pane mode for " + row.key, items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
		if it.id == "remove" {
			v.removeRule(a, row)
			return
		}
		v.applyRuleChange(a, config.SetViewModeRule(row.key, it.id))
	}}
}

func (v *settingsView) render(a *App, w, h int, focused bool) string {
	if v.lines == nil {
		v.refresh(a)
	}
	v.rows = max(1, h-2)
	th := a.theme
	lines := []string{padRight(truncateCells(" Enter edit · / filter · v version · :config external editor", w), w), padRight(" "+v.filter, w)}
	disp, cur := v.display(w)
	// v.scroll counts display lines, headings included. Moving up onto the
	// first row of a group scrolls its headings (at most the scope and the
	// prefix) back into view with it, and the page never scrolls past its
	// last line, so a taller window or a shorter list shows the top again.
	if cur >= 0 {
		start := cur
		for n := 0; n < 2 && start > 0 && disp[start-1].idx < 0; n++ {
			start--
		}
		if start < v.scroll {
			v.scroll = start
		} else if cur >= v.scroll+v.rows {
			v.scroll = cur - v.rows + 1
		}
	}
	v.scroll = max(0, min(v.scroll, len(disp)-v.rows))
	for i := v.scroll; i < len(disp) && len(lines) < h; i++ {
		d := disp[i]
		switch {
		case d.scope != "":
			lines = append(lines, padRight(" "+sectionHeader(th, d.scope, settingScopeFiles[d.scope], w-2), w))
		case d.idx < 0:
			lines = append(lines, th.Bold.Render(padRight(d.text, w)))
		case focused && d.idx == v.list.cursor:
			lines = append(lines, th.SelectedFocus.Render(padRight(d.text, w)))
		default:
			lines = append(lines, padRight(d.text, w))
		}
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

// settingScopeFiles names where each scope's settings are saved.
var settingScopeFiles = map[string]string{"User": "config.toml", "Vault": ".zennotes/vault.json"}

type settingDisplay struct {
	text  string
	idx   int    // index into settings; -1 for a heading
	scope string // set on the heading that opens the User or Vault group
}

// display lays the settings out under two groups, User (config.toml) and
// Vault (.zennotes/vault.json), each split into subsections named by the
// config prefix (vim, editor, dailyNotes, ...). Rows keep their settings
// order, so the cursor index maps straight onto v.settings. A row shows its
// filter line less the scope and prefix its headings already name. It
// returns the display line of the cursor row, or -1.
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
			scope, sub = name, ""
			out = append(out, settingDisplay{idx: -1, scope: name})
		}
		if s.section != sub {
			sub = s.section
			out = append(out, settingDisplay{text: truncateCells("  "+sub, w), idx: -1})
		}
		if i == v.list.cursor {
			cur = len(out)
		}
		text := strings.TrimPrefix(v.lines[i], strings.ToLower(name)+" · "+s.section+".")
		out = append(out, settingDisplay{text: truncateCells("    "+text, w), idx: i})
	}
	return out, cur
}
func (v *settingsView) handleKey(a *App, k vim.Key) bool {
	if k.IsRune('v') {
		_ = a.showVersion(vim.ExCommand{Name: "version"})
		return true
	}
	if k.IsRune('d') {
		if row, ok := v.selectedRuleRow(); ok {
			v.removeRule(a, row)
		}
		return true
	}
	if a.listNav(&v.list, k, len(v.settings), v.rows) {
		return true
	}
	if k.Is("enter") {
		if v.list.cursor < len(v.settings) {
			switch row := v.settings[v.list.cursor]; row.kind() {
			case rowSetting:
				v.edit(a, row)
			case rowAddRule:
				v.addRule(a)
			case rowRule, rowInvalidRule:
				v.editRule(a, row)
			}
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
	"which_key_hint_mode": {"timed", "sticky"}, "wrapped_line_motions": {"display", "logical"}, "default_view_mode": {"edit", "split", "preview"}, "line_number_mode": {"off", "absolute", "relative"}, "completed_task_style": {"none", "gray", "strikethrough", "gray-strikethrough"}, "time_format": {"12h", "24h"}, "theme_mode": {"dark", "light", "system"}, "note_sort_order": {"modified-desc", "created-desc", "title-asc", "title-desc"}, "calendar_week_start": {"monday", "sunday"}, "tasks_view_mode": {"list", "kanban", "calendar"}, "preview_style": {"auto", "dark", "light", "ascii", "notty"}, "images": {"auto", "kitty", "off"}, "math": {"typst", "latex"}, "task_note_marker": {"tag", "page-type"},
}

func (v *settingsView) edit(a *App, row settingRow) {
	save := func(value any) {
		if text := strings.TrimSpace(fmt.Sprint(value)); row.section == "templates" && text != "" && vault.NormalizeTemplateFolder(text) == "" {
			a.notifyError("Not a usable template folder: " + text)
			return
		}
		var err error
		if row.vault {
			err = a.backend.UpdateVaultSettings(a.ctx, func(raw map[string]any) {
				if row.section == "tasks" {
					setTasksFolderList(raw, row.key, fmt.Sprint(value))
					return
				}
				if row.section == "templates" {
					setTemplateFolder(raw, fmt.Sprint(value))
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
	help := "Saved immediately · Esc cancels"
	if row.section == "terminal" && row.key == "view_mode_field" && strings.TrimSpace(fmt.Sprint(row.value)) == "" {
		help = "Frontmatter field that sets the pane mode, for example view-mode · " + help
	}
	a.promptFor(row.section+"."+row.key, fmt.Sprint(row.value), help, func(a *App, text string) {
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

// setTemplateFolder writes the vault's template folder (`templates.folder`,
// ZenNotes/zennotes#1024) cleaned the way every runtime reads it, keeping any
// other key under `templates`. An empty edit removes it, and an empty
// `templates` object goes with it, so vault.json never keeps an empty stub.
func setTemplateFolder(raw map[string]any, value string) {
	templates, _ := raw["templates"].(map[string]any)
	if templates == nil {
		templates = map[string]any{}
	}
	if folder := vault.NormalizeTemplateFolder(value); folder != "" {
		templates["folder"] = folder
	} else {
		delete(templates, "folder")
	}
	if len(templates) == 0 {
		delete(raw, "templates")
	} else {
		raw["templates"] = templates
	}
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
