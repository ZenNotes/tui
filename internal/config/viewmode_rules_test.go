package config

import (
	"reflect"
	"testing"
)

// ViewModeRule is a normalised valid rule, ValidViewModeRules gives the valid
// ones in match order, and ViewModeRuleEntries gives every entry with its raw
// key and mode, a Valid flag and a Reason for the invalid ones.

func TestViewModeRulesParseNormaliseAndSort(t *testing.T) {
	raw := map[string]string{
		"page-status=done":    "preview",
		"Kind=Journal":        "split",
		` "Tags" = "Draft" `:  "Edit",
		"a=b":                 " PREVIEW ",
		"page-status=in-done": "edit",
	}
	want := []ViewModeRule{
		{Key: "a", Value: "b", Mode: "preview"},
		{Key: "kind", Value: "journal", Mode: "split"},
		{Key: "page-status", Value: "done", Mode: "preview"},
		{Key: "page-status", Value: "in-done", Mode: "edit"},
		{Key: "tags", Value: "draft", Mode: "edit"},
	}
	if got := ValidViewModeRules(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestInvalidViewModeRulesAreIgnoredButReported(t *testing.T) {
	raw := map[string]string{
		"page-status=done": "preview",
		"kind=journal":     "reading",
		"status":           "edit",
		"=x":               "edit",
	}
	valid := ValidViewModeRules(raw)
	if len(valid) != 1 || valid[0].Key != "page-status" {
		t.Fatalf("valid rules: %+v", valid)
	}
	entries := ViewModeRuleEntries(raw)
	if len(entries) != len(raw) {
		t.Fatalf("entries: %+v", entries)
	}
	reasons := map[string]string{}
	for _, e := range entries {
		if e.Valid != (e.Key == "page-status=done") {
			t.Fatalf("entry %+v: wrong validity", e)
		}
		if !e.Valid {
			if e.Reason == "" {
				t.Fatalf("entry %+v: no reason", e)
			}
			reasons[e.Key] = e.Reason
		}
	}
	if len(reasons) != 3 || reasons["kind=journal"] == reasons["status"] || reasons["status"] == reasons["=x"] {
		t.Fatalf("reasons should tell the three failures apart: %v", reasons)
	}
}

// An empty value part is a rule like any other when read from the file.
func TestViewModeRuleWithEmptyValueIsValid(t *testing.T) {
	valid := ValidViewModeRules(map[string]string{"status=": "edit"})
	if len(valid) != 1 || valid[0].Key != "status" || valid[0].Value != "" {
		t.Fatalf("got %+v", valid)
	}
}

// The first "=" splits key from value.
func TestViewModeRuleSplitsOnFirstEquals(t *testing.T) {
	valid := ValidViewModeRules(map[string]string{"a=b=c": "edit"})
	if len(valid) != 1 || valid[0].Key != "a" || valid[0].Value != "b=c" {
		t.Fatalf("got %+v", valid)
	}
}
