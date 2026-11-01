package tui

import (
	"testing"

	"github.com/ZenNotes/tui/internal/config"
)

func TestFrontmatterPaneMode(t *testing.T) {
	rules := func(raw map[string]string) []config.ViewModeRule { return config.ValidViewModeRules(raw) }
	fm := func(body string) string { return "---\n" + body + "\n---\n\nbody\n" }
	done := rules(map[string]string{"page-status=done": "preview"})

	tests := []struct {
		name  string
		text  string
		field string
		rules []config.ViewModeRule
		want  paneMode
		ok    bool
	}{
		{"no frontmatter", "just text\n", "view-mode", done, "", false},
		{"unrelated keys", fm("title: x\ntags: [a]"), "view-mode", done, "", false},
		{"field edit", fm("view-mode: edit"), "view-mode", nil, modeEdit, true},
		{"field split", fm("view-mode: split"), "view-mode", nil, modeSplit, true},
		{"field preview", fm("view-mode: preview"), "view-mode", nil, modePreview, true},
		{"key case", fm("View-Mode: preview"), "view-mode", nil, modePreview, true},
		{"value case", fm("view-mode: Preview"), "view-mode", nil, modePreview, true},
		{"value spaces and quotes", fm(`view-mode: " preview "`), "view-mode", nil, modePreview, true},
		{"unknown word", fm("view-mode: reading"), "view-mode", nil, "", false},
		{"unknown word falls to rule", fm("view-mode: reading\npage-status: done"), "view-mode", done, modePreview, true},
		{"empty value", fm("view-mode:"), "view-mode", nil, "", false},
		{"inline list", fm("view-mode: [preview]"), "view-mode", nil, "", false},
		{"block list", fm("view-mode:\n  - preview"), "view-mode", nil, "", false},
		{"list field falls to rule", fm("view-mode: [preview]\npage-status: done"), "view-mode", done, modePreview, true},
		{"empty field with rule", fm("view-mode: edit\npage-status: done"), "", done, modePreview, true},
		{"rule hit", fm("page-status: done"), "", done, modePreview, true},
		{"rule miss", fm("page-status: draft"), "", done, "", false},
		{"two rules one key", fm("page-status: draft"), "", rules(map[string]string{"page-status=done": "preview", "page-status=draft": "split"}), modeSplit, true},
		{"rule case and quotes", fm(`Page-Status: "DONE"`), "", rules(map[string]string{`"Page-Status"="Done"`: "preview"}), modePreview, true},
		{"rule normalised", fm(`Page-Status: "DONE"`), "", rules(map[string]string{` Page-Status = "Done" `: "Preview"}), modePreview, true},
		{"inline list element", fm("status: [draft, published]"), "", rules(map[string]string{"status=published": "preview"}), modePreview, true},
		{"block list element", fm("status:\n  - draft\n  - published"), "", rules(map[string]string{"status=published": "split"}), modeSplit, true},
		{"first sorted rule wins", fm("kind: journal\npage-status: done"), "", rules(map[string]string{"page-status=done": "preview", "kind=journal": "edit"}), modeEdit, true},
		{"invalid rule ignored", fm("page-status: done\nkind: journal"), "", rules(map[string]string{"kind=journal": "reading", "no-equals": "edit", "page-status=done": "split"}), modeSplit, true},
		{"field beats rule", fm("view-mode: edit\npage-status: done"), "view-mode", done, modeEdit, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := frontmatterPaneMode(tc.text, tc.field, tc.rules)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
