package tui

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

func settingsLineIndex(t *testing.T, v *settingsView, line string) int {
	t.Helper()
	i := slices.Index(v.lines, line)
	if i < 0 {
		t.Fatalf("no settings row %q in:\n%s", line, strings.Join(v.lines, "\n"))
	}
	return i
}

func writeRules(t *testing.T, a *App, rules map[string]string) {
	t.Helper()
	for k, m := range rules {
		if err := config.UpdateConfig(func(doc map[string]any) error {
			terminal, _ := doc["terminal"].(map[string]any)
			if terminal == nil {
				terminal = map[string]any{}
				doc["terminal"] = terminal
			}
			table, _ := terminal["view_mode_rules"].(map[string]any)
			if table == nil {
				table = map[string]any{}
				terminal["view_mode_rules"] = table
			}
			table[k] = m
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.reloadPreferences(); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsEditsViewModeField(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	v := &settingsView{}
	v.refresh(a)
	v.list.cursor = settingsLineIndex(t, v, "user · terminal.view_mode_field = ")
	if !v.handleKey(a, vim.Key{Name: "enter"}) {
		t.Fatal("enter was not handled")
	}
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatalf("overlay = %T, want a prompt", a.overlay)
	}
	if !strings.Contains(p.help, "view-mode") {
		t.Fatalf("prompt help %q does not suggest view-mode", p.help)
	}
	p.onSubmit(a, "view-mode")
	data, err := os.ReadFile(config.ConfigTomlPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `view_mode_field = "view-mode"`) {
		t.Fatalf("config.toml:\n%s", data)
	}
	if a.prefs.TerminalViewModeField != "view-mode" {
		t.Fatalf("reloaded field = %q", a.prefs.TerminalViewModeField)
	}
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("note opened in %q, want preview", got)
	}

	v.refresh(a)
	v.list.cursor = settingsLineIndex(t, v, "user · terminal.view_mode_field = view-mode")
	v.handleKey(a, vim.Key{Name: "enter"})
	a.overlay.(*prompt).onSubmit(a, "")
	if a.prefs.TerminalViewModeField != "" {
		t.Fatalf("field after clearing = %q", a.prefs.TerminalViewModeField)
	}
	data, _ = os.ReadFile(config.ConfigTomlPath())
	if !strings.Contains(string(data), `view_mode_field = ""`) {
		t.Fatalf("config.toml after clearing:\n%s", data)
	}
}

func TestSettingsListsViewModeRulesInOrder(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview", "Kind=Journal": "edit", "other=x": "split"})
	v := &settingsView{}
	v.refresh(a)
	var got []string
	for _, l := range v.lines {
		if strings.Contains(l, "terminal.view_mode_rules.") {
			got = append(got, l)
		}
	}
	want := []string{
		"user · terminal.view_mode_rules.Kind=Journal = edit",
		"user · terminal.view_mode_rules.other=x = split",
		"user · terminal.view_mode_rules.page-status=done = preview",
		"user · terminal.view_mode_rules.(add rule) = Enter to add a rule",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rule rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The rules follow the terminal rows and come before the vault rows.
	last := slices.Index(v.lines, want[3])
	for i, l := range v.lines {
		if strings.HasPrefix(l, "vault · ") && i < last {
			t.Fatalf("vault row %q sits before the rule rows", l)
		}
	}
	for _, i := range []int{0, 1, 2, 3} {
		if i < 3 && v.settings[slices.Index(v.lines, want[i])].kind() != rowRule {
			t.Fatalf("row %q is not a rule row", want[i])
		}
	}
}

func TestSettingsListsInvalidRulesMarked(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\nkind: journal\n---\nbody\n")
	a.prefs.DefaultPaneMode = "edit"
	writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "sideways", "status": "preview", "=x": "edit"})
	v := &settingsView{}
	v.refresh(a)
	var got []string
	for _, l := range v.lines {
		if strings.Contains(l, "terminal.view_mode_rules.") {
			got = append(got, l)
		}
	}
	want := []string{
		"user · terminal.view_mode_rules.page-status=done = preview",
		`user · terminal.view_mode_rules.=x = edit (invalid: empty key)`,
		"user · terminal.view_mode_rules.kind=journal = sideways (invalid: bad mode)",
		`user · terminal.view_mode_rules.status = preview (invalid: no "=")`,
		"user · terminal.view_mode_rules.(add rule) = Enter to add a rule",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rule rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range want[1:4] {
		if v.settings[slices.Index(v.lines, l)].kind() != rowInvalidRule {
			t.Fatalf("row %q is not marked invalid", l)
		}
	}
	// A note that only an invalid rule matches opens as with the feature off.
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("note opened in %q, want edit", got)
	}
}

func TestSettingsFilterMatchesRulesAndKeepsAddRow(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "split"})
	add := "user · terminal.view_mode_rules.(add rule) = Enter to add a rule"
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"page-status", []string{"user · terminal.view_mode_rules.page-status=done = preview"}},
		{"journal", []string{"user · terminal.view_mode_rules.kind=journal = split"}},
		{"= split", []string{"user · terminal.view_mode_rules.kind=journal = split"}},
		{"no-such-text-anywhere", nil},
	} {
		v := &settingsView{filter: tc.filter}
		v.refresh(a)
		want := append(slices.Clone(tc.want), add)
		if !slices.Equal(v.lines, want) {
			t.Fatalf("filter %q gave:\n%s\nwant:\n%s", tc.filter, strings.Join(v.lines, "\n"), strings.Join(want, "\n"))
		}
	}
}

const addRuleRow = "user · terminal.view_mode_rules.(add rule) = Enter to add a rule"

func openAddRule(t *testing.T, a *App) *settingsView {
	t.Helper()
	v := &settingsView{}
	v.refresh(a)
	v.list.cursor = settingsLineIndex(t, v, addRuleRow)
	v.handleKey(a, vim.Key{Name: "enter"})
	if _, ok := a.overlay.(*prompt); !ok {
		t.Fatalf("overlay = %T, want the field prompt", a.overlay)
	}
	return v
}

func submitPrompt(t *testing.T, a *App, text string) {
	t.Helper()
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatalf("overlay = %T, want a prompt", a.overlay)
	}
	a.overlay = nil
	p.onSubmit(a, text)
}

func TestSettingsAddRuleWritesAfterModePick(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\npage-status: done\n---\nbody\n")
	if err := config.SetValue("terminal", "math", "latex"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetValue("editor", "tab_size", 3); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadPreferences(); err != nil {
		t.Fatal(err)
	}
	v := openAddRule(t, a)
	submitPrompt(t, a, " page-status ")
	submitPrompt(t, a, " done ")
	pal, ok := a.overlay.(*palette)
	if !ok {
		t.Fatalf("overlay = %T, want the mode palette", a.overlay)
	}
	before, _ := os.ReadFile(config.ConfigTomlPath())
	if strings.Contains(string(before), "view_mode_rules") {
		t.Fatalf("rule written before the mode was picked:\n%s", before)
	}
	for _, it := range pal.items {
		if it.id == "preview" {
			pal.onSelect(a, it)
		}
	}
	data, _ := os.ReadFile(config.ConfigTomlPath())
	for _, want := range []string{"math", "tab_size", "view_mode_rules", `"page-status=done" = "preview"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config.toml lacks %q:\n%s", want, data)
		}
	}
	if a.prefs.ViewModeRules["page-status=done"] != "preview" {
		t.Fatalf("prefs rules = %v", a.prefs.ViewModeRules)
	}
	settingsLineIndex(t, v, "user · terminal.view_mode_rules.page-status=done = preview")
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("note opened in %q, want preview", got)
	}
}

func TestSettingsAddRuleOffersOnlyThreeModes(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	openAddRule(t, a)
	submitPrompt(t, a, "page-status")
	submitPrompt(t, a, "done")
	pal, ok := a.overlay.(*palette)
	if !ok {
		t.Fatalf("overlay = %T, want the mode palette", a.overlay)
	}
	var got []string
	for _, it := range pal.items {
		got = append(got, it.id)
	}
	if !slices.Equal(got, []string{"edit", "split", "preview"}) {
		t.Fatalf("palette items = %v", got)
	}
}

func TestSettingsAddRuleCancelWritesNothing(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	if err := config.SetValue("terminal", "math", "latex"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config.ConfigTomlPath())
	esc := func() {
		t.Helper()
		a.overlay.handleKey(a, vim.KeyEsc)
		if a.overlay != nil {
			t.Fatalf("overlay %T survived esc", a.overlay)
		}
	}
	// Esc at the field prompt, at the value prompt and at the mode palette.
	openAddRule(t, a)
	esc()
	openAddRule(t, a)
	submitPrompt(t, a, "page-status")
	esc()
	openAddRule(t, a)
	submitPrompt(t, a, "page-status")
	submitPrompt(t, a, "done")
	esc()
	after, _ := os.ReadFile(config.ConfigTomlPath())
	if string(before) != string(after) {
		t.Fatalf("config.toml changed:\n%s\nwas:\n%s", after, before)
	}
}

func TestSettingsAddRuleRejectsBadInput(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview"})
	before, _ := os.ReadFile(config.ConfigTomlPath())
	for _, tc := range []struct{ field, value, want string }{
		{"", "", "field"},
		{"  ", "", "field"},
		{"a=b", "", "="},
		{"page-status", "", "value"},
		{"page-status", "done", "already exists"},
		{"Page-Status", " DONE ", "already exists"},
	} {
		a.message, a.messageErr = "", false
		openAddRule(t, a)
		submitPrompt(t, a, tc.field)
		if tc.value != "" || tc.field == "page-status" || tc.field == "Page-Status" {
			submitPrompt(t, a, tc.value)
		}
		if !a.messageErr || !strings.Contains(a.message, tc.want) {
			t.Fatalf("%q/%q: message = %q (error %v), want error containing %q", tc.field, tc.value, a.message, a.messageErr, tc.want)
		}
		if a.overlay != nil {
			t.Fatalf("%q/%q: overlay %T left open", tc.field, tc.value, a.overlay)
		}
		after, _ := os.ReadFile(config.ConfigTomlPath())
		if string(before) != string(after) {
			t.Fatalf("%q/%q changed config.toml:\n%s", tc.field, tc.value, after)
		}
	}
}

const ruleDoneRow = "user · terminal.view_mode_rules.page-status=done = preview"

func selectRule(t *testing.T, a *App, line string) *settingsView {
	t.Helper()
	v := &settingsView{}
	v.refresh(a)
	v.list.cursor = settingsLineIndex(t, v, line)
	return v
}

func pickPalette(t *testing.T, a *App, id string) {
	t.Helper()
	pal, ok := a.overlay.(*palette)
	if !ok {
		t.Fatalf("overlay = %T, want a palette", a.overlay)
	}
	for _, it := range pal.items {
		if it.id == id {
			a.overlay = nil
			pal.onSelect(a, it)
			return
		}
	}
	t.Fatalf("palette has no %q entry", id)
}

func paletteIDs(t *testing.T, a *App) []string {
	t.Helper()
	pal, ok := a.overlay.(*palette)
	if !ok {
		t.Fatalf("overlay = %T, want a palette", a.overlay)
	}
	var ids []string
	for _, it := range pal.items {
		ids = append(ids, it.id)
	}
	return ids
}

func configText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(config.ConfigTomlPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSettingsChangeRuleMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\npage-status: done\n---\nbody\n")
	writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "split"})
	v := selectRule(t, a, ruleDoneRow)
	v.handleKey(a, vim.Key{Name: "enter"})
	if got := paletteIDs(t, a); !slices.Equal(got, []string{"edit", "split", "preview", "remove"}) {
		t.Fatalf("palette items = %v", got)
	}
	pickPalette(t, a, "edit")
	if a.prefs.ViewModeRules["page-status=done"] != "edit" || a.prefs.ViewModeRules["kind=journal"] != "split" {
		t.Fatalf("prefs rules = %v", a.prefs.ViewModeRules)
	}
	settingsLineIndex(t, v, "user · terminal.view_mode_rules.page-status=done = edit")
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("note opened in %q, want edit", got)
	}
}

func TestSettingsRemoveRuleFromPicker(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "split"})
	v := selectRule(t, a, ruleDoneRow)
	v.handleKey(a, vim.Key{Name: "enter"})
	pickPalette(t, a, "remove")
	if _, ok := a.prefs.ViewModeRules["page-status=done"]; ok || len(a.prefs.ViewModeRules) != 1 {
		t.Fatalf("prefs rules = %v", a.prefs.ViewModeRules)
	}
	if slices.Contains(v.lines, ruleDoneRow) {
		t.Fatal("removed rule still listed")
	}
	if !strings.Contains(configText(t), `"kind=journal" = "split"`) {
		t.Fatalf("other rule lost:\n%s", configText(t))
	}
}

func TestSettingsRemoveRuleWithKey(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview"})
	v := selectRule(t, a, ruleDoneRow)
	if !v.handleKey(a, vim.Key{Rune: 'd'}) {
		t.Fatal("d was not handled")
	}
	if len(a.prefs.ViewModeRules) != 0 {
		t.Fatalf("prefs rules = %v", a.prefs.ViewModeRules)
	}
	if slices.Contains(v.lines, ruleDoneRow) {
		t.Fatal("removed rule still listed")
	}
	if strings.Contains(configText(t), "view_mode_rules") {
		t.Fatalf("empty rules table left in config.toml:\n%s", configText(t))
	}
}

func TestSettingsRemoveKeyIgnoredOffRuleRows(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview"})
	before := configText(t)
	v := &settingsView{}
	v.refresh(a)
	rows := slices.Clone(v.lines)
	for i, l := range rows {
		if strings.Contains(l, "terminal.view_mode_rules.page-status") {
			continue
		}
		v.list.cursor = i
		if !v.handleKey(a, vim.Key{Rune: 'd'}) {
			t.Fatalf("d on %q was not consumed", l)
		}
		if a.overlay != nil {
			t.Fatalf("d on %q opened %T", l, a.overlay)
		}
	}
	if configText(t) != before {
		t.Fatalf("config.toml changed:\n%s", configText(t))
	}
	if !slices.Equal(v.lines, rows) {
		t.Fatal("rows changed")
	}
}

func TestSettingsHintNamesRemoveOnRuleRow(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "sideways"})
	v := &settingsView{}
	v.refresh(a)
	for i, l := range v.lines {
		v.list.cursor = i
		got := false
		for _, h := range v.hintPairs(a) {
			if h.id == "key:d" && h.label == "remove rule" {
				got = true
			}
		}
		want := strings.Contains(l, "terminal.view_mode_rules.") && !strings.Contains(l, "(add rule)")
		if got != want {
			t.Fatalf("hint on %q: remove rule present = %v, want %v", l, got, want)
		}
	}
}

func TestSettingsRemoveInvalidRule(t *testing.T) {
	rows := map[string]string{
		"kind=journal": "user · terminal.view_mode_rules.kind=journal = sideways (invalid: bad mode)",
		"status":       `user · terminal.view_mode_rules.status = preview (invalid: no "=")`,
	}
	for key, row := range rows {
		for _, how := range []string{"d", "picker"} {
			a, _ := newFeatureTestApp(t)
			writeRules(t, a, map[string]string{"page-status=done": "preview", "kind=journal": "sideways", "status": "preview"})
			if err := config.SetValue("terminal", "math", "latex"); err != nil {
				t.Fatal(err)
			}
			v := selectRule(t, a, row)
			if how == "d" {
				v.handleKey(a, vim.Key{Rune: 'd'})
			} else {
				v.handleKey(a, vim.Key{Name: "enter"})
				pickPalette(t, a, "remove")
			}
			if _, ok := a.prefs.ViewModeRules[key]; ok {
				t.Fatalf("%s/%s: entry still in prefs", key, how)
			}
			if slices.Contains(v.lines, row) {
				t.Fatalf("%s/%s: row still listed", key, how)
			}
			text := configText(t)
			if !strings.Contains(text, `"page-status=done" = "preview"`) || !strings.Contains(text, "math") {
				t.Fatalf("%s/%s: other settings lost:\n%s", key, how, text)
			}
			if len(a.prefs.ViewModeRules) != 2 {
				t.Fatalf("%s/%s: prefs rules = %v", key, how, a.prefs.ViewModeRules)
			}
		}
	}
}

func TestSettingsFixInvalidRuleMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\nkind: journal\n---\nbody\n")
	a.prefs.DefaultPaneMode = "edit"
	writeRules(t, a, map[string]string{"kind=journal": "sideways"})
	v := selectRule(t, a, "user · terminal.view_mode_rules.kind=journal = sideways (invalid: bad mode)")
	v.handleKey(a, vim.Key{Name: "enter"})
	if got := paletteIDs(t, a); !slices.Equal(got, []string{"edit", "split", "preview", "remove"}) {
		t.Fatalf("palette items = %v", got)
	}
	pickPalette(t, a, "preview")
	settingsLineIndex(t, v, "user · terminal.view_mode_rules.kind=journal = preview")
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("note opened in %q, want preview", got)
	}
}

func TestSettingsMalformedRuleOffersOnlyRemove(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	writeRules(t, a, map[string]string{"status": "preview", "=x": "edit"})
	for _, line := range []string{
		`user · terminal.view_mode_rules.status = preview (invalid: no "=")`,
		"user · terminal.view_mode_rules.=x = edit (invalid: empty key)",
	} {
		v := selectRule(t, a, line)
		v.handleKey(a, vim.Key{Name: "enter"})
		if got := paletteIDs(t, a); !slices.Equal(got, []string{"remove"}) {
			t.Fatalf("%q palette items = %v", line, got)
		}
		a.overlay = nil
	}
}
