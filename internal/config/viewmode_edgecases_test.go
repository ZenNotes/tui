package config

import (
	"reflect"
	"testing"
)

// A hand-edited rule whose mode is not a string is kept, as an invalid rule
// that can be seen and removed, instead of disappearing on load.
func TestNonStringViewModeRuleIsKeptInvalid(t *testing.T) {
	writeConfigFile(t, "[terminal.view_mode_rules]\n\"a=b\" = 1\n\"c=d\" = true\n\"e=f\" = \"edit\"\n")
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ViewModeRules) != 3 {
		t.Fatalf("rules %v, want all three entries", p.ViewModeRules)
	}
	for _, e := range ViewModeRuleEntries(p.ViewModeRules) {
		switch e.Key {
		case "a=b", "c=d":
			if e.Valid || e.Reason != "bad mode" {
				t.Fatalf("%q: want invalid with bad mode, got %+v", e.Key, e)
			}
		case "e=f":
			if !e.Valid {
				t.Fatalf("%q: want valid, got %+v", e.Key, e)
			}
		}
	}
	if err := RemoveViewModeRule("a=b"); err != nil {
		t.Fatal(err)
	}
	rules := ruleTable(readConfigDoc(t))
	if _, ok := rules["a=b"]; ok || len(rules) != 2 {
		t.Fatalf("after remove: %v", rules)
	}
}

// Setting a rule that differs from an existing entry only in case or quoting
// changes that entry instead of adding a second one, even when it is invalid.
func TestSetViewModeRuleChangesCaseVariantInPlace(t *testing.T) {
	writeConfigFile(t, "[terminal.view_mode_rules]\n\"page-status=done\" = \"preview\"\n\"Kind=Journal\" = \"sideways\"\n")
	if err := SetViewModeRule("Page-Status=DONE", "split"); err != nil {
		t.Fatal(err)
	}
	if err := SetViewModeRule(`kind="journal"`, "edit"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"page-status=done": "split", "Kind=Journal": "edit"}
	if got := ruleTable(readConfigDoc(t)); !reflect.DeepEqual(got, want) {
		t.Fatalf("rules %v, want %v", got, want)
	}
}
