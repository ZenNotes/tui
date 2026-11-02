package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func writeConfigFile(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ZENNOTES_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConfigDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if _, err := toml.DecodeFile(ConfigTomlPath(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func ruleTable(doc map[string]any) map[string]any {
	terminal, _ := doc["terminal"].(map[string]any)
	rules, _ := terminal["view_mode_rules"].(map[string]any)
	return rules
}

// A fresh config turns both the field and the rules off.
func TestViewModeDefaultsAreOff(t *testing.T) {
	if p := DefaultPrefs(); p.TerminalViewModeField != "" || len(p.ViewModeRules) != 0 {
		t.Fatalf("defaults: field %q rules %v", p.TerminalViewModeField, p.ViewModeRules)
	}
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	p, _, err := LoadPrefs()
	if err != nil || p.TerminalViewModeField != "" || len(p.ViewModeRules) != 0 {
		t.Fatalf("no file: field %q rules %v err %v", p.TerminalViewModeField, p.ViewModeRules, err)
	}
	writeTerminalConfig(t, `math = "latex"`)
	p, _, err = LoadPrefs()
	if err != nil || p.TerminalViewModeField != "" || len(p.ViewModeRules) != 0 {
		t.Fatalf("no keys: field %q rules %v err %v", p.TerminalViewModeField, p.ViewModeRules, err)
	}
	if strings.Contains(DefaultConfigTOML, "view_mode_field = \"view-mode\"") {
		t.Fatal("the starter config must leave the field off")
	}
}

// [terminal] holds scalars plus the rules sub-table; both load.
func TestViewModeRulesLoadFromTerminalSubtable(t *testing.T) {
	writeConfigFile(t, `[terminal]
math = "latex"
view_mode_field = "view-mode"

[terminal.view_mode_rules]
"page-status=done" = "preview"
"kind=journal" = "split"
`)
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if p.TerminalViewModeField != "view-mode" || p.TerminalMath != "latex" {
		t.Fatalf("scalars: field %q math %q", p.TerminalViewModeField, p.TerminalMath)
	}
	want := map[string]string{"page-status=done": "preview", "kind=journal": "split"}
	if !reflect.DeepEqual(p.ViewModeRules, want) {
		t.Fatalf("rules: got %v want %v", p.ViewModeRules, want)
	}
}

// A hand-edited bad rule never stops the config from loading.
func TestInvalidViewModeRulesDoNotBlockLoad(t *testing.T) {
	writeConfigFile(t, `[terminal]
math = "latex"

[terminal.view_mode_rules]
"page-status=done" = "preview"
"kind=journal" = "reading"
"status" = "edit"
"=x" = "edit"
`)
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	if p.TerminalMath != "latex" || p.ViewModeRules["page-status=done"] != "preview" {
		t.Fatalf("rest of the config lost: math %q rules %v", p.TerminalMath, p.ViewModeRules)
	}
	if err := ValidateConfig(); err != nil && strings.Contains(err.Error(), "view_mode_rules") {
		t.Fatalf("a bad rule must not fail validation: %v", err)
	}
}

// A wrong-typed field leaves the field off and loading intact.
func TestBadViewModeFieldTypeDoesNotBlockLoad(t *testing.T) {
	writeTerminalConfig(t, "view_mode_field = 3\nmath = \"latex\"")
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	if p.TerminalViewModeField != "" || p.TerminalMath != "latex" {
		t.Fatalf("field %q math %q", p.TerminalViewModeField, p.TerminalMath)
	}
}

// Writing a rule keeps the rest of [terminal], the other tables and unknown data.
func TestSetViewModeRuleKeepsOtherTablesAndKeys(t *testing.T) {
	writeConfigFile(t, `[terminal]
math = "latex"

[editor]
word_wrap = false

[unrecognized.nested]
list = [1, 2, 3]
`)
	if err := SetViewModeRule("page-status=done", "preview"); err != nil {
		t.Fatal(err)
	}
	if err := SetViewModeRule("kind=journal", "split"); err != nil {
		t.Fatal(err)
	}
	doc := readConfigDoc(t)
	rules := ruleTable(doc)
	if rules["page-status=done"] != "preview" || rules["kind=journal"] != "split" || len(rules) != 2 {
		t.Fatalf("rules: %v", rules)
	}
	if doc["terminal"].(map[string]any)["math"] != "latex" {
		t.Fatalf("[terminal] math lost: %v", doc["terminal"])
	}
	if doc["editor"].(map[string]any)["word_wrap"] != false || doc["unrecognized"] == nil {
		t.Fatalf("other tables lost: %v", doc)
	}
	if err := SetViewModeRule("page-status=done", "edit"); err != nil {
		t.Fatal(err)
	}
	if got := ruleTable(readConfigDoc(t))["page-status=done"]; got != "edit" {
		t.Fatalf("changed rule: %v", got)
	}
	p, _, err := LoadPrefs()
	if err != nil || p.ViewModeRules["page-status=done"] != "edit" {
		t.Fatalf("reload: %v err %v", p.ViewModeRules, err)
	}
}

// Removing the last rule drops the empty table but keeps [terminal].
func TestRemoveLastViewModeRuleDropsTable(t *testing.T) {
	writeConfigFile(t, `[terminal]
math = "latex"

[terminal.view_mode_rules]
"page-status=done" = "preview"
"kind=journal" = "split"
`)
	if err := RemoveViewModeRule("page-status=done"); err != nil {
		t.Fatal(err)
	}
	if rules := ruleTable(readConfigDoc(t)); len(rules) != 1 || rules["kind=journal"] != "split" {
		t.Fatalf("after first removal: %v", rules)
	}
	if err := RemoveViewModeRule("kind=journal"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigTomlPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "view_mode_rules") {
		t.Fatalf("empty rules table kept:\n%s", raw)
	}
	if readConfigDoc(t)["terminal"].(map[string]any)["math"] != "latex" {
		t.Fatalf("[terminal] lost:\n%s", raw)
	}
}

// A malformed key is removed by its raw key exactly as written.
func TestRemoveMalformedViewModeRule(t *testing.T) {
	writeConfigFile(t, `[terminal.view_mode_rules]
"status" = "edit"
"=x" = "edit"
"kind=journal" = "reading"
"page-status=done" = "preview"
`)
	for _, key := range []string{"status", "=x", "kind=journal"} {
		if err := RemoveViewModeRule(key); err != nil {
			t.Fatalf("remove %q: %v", key, err)
		}
	}
	rules := ruleTable(readConfigDoc(t))
	if len(rules) != 1 || rules["page-status=done"] != "preview" {
		t.Fatalf("rules after removal: %v", rules)
	}
}

// A bad rule already in the file never blocks a later write.
func TestWritingRuleWithExistingInvalidRuleSucceeds(t *testing.T) {
	writeConfigFile(t, `[terminal.view_mode_rules]
"status" = "edit"
"kind=journal" = "reading"
`)
	if err := SetViewModeRule("page-status=done", "preview"); err != nil {
		t.Fatalf("set with a bad rule present: %v", err)
	}
	rules := ruleTable(readConfigDoc(t))
	if rules["page-status=done"] != "preview" || rules["status"] != "edit" || rules["kind=journal"] != "reading" {
		t.Fatalf("rules: %v", rules)
	}
	if err := SetValue("terminal", "math", "latex"); err != nil {
		t.Fatalf("unrelated write with a bad rule present: %v", err)
	}
	if len(ruleTable(readConfigDoc(t))) != 3 {
		t.Fatal("rules lost by an unrelated write")
	}
}

// The field is settable through the writer :settings and zn config use, and
// listed among the editable terminal settings.
func TestViewModeFieldRoundTrips(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	if err := SetValue("terminal", "view_mode_field", "view-mode"); err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadPrefs()
	if err != nil || p.TerminalViewModeField != "view-mode" {
		t.Fatalf("field %q err %v", p.TerminalViewModeField, err)
	}
	if got := ValueTables(p)["terminal"]["view_mode_field"]; got != "view-mode" {
		t.Fatalf("ValueTables: %v", got)
	}
}
