package config

import (
	"reflect"
	"strings"
	"testing"
)

// The board and preview settings of ZenNotes/zennotes#1122 live in
// [terminal]: the desktop app has no such options, so they are zn's alone.
func TestBoardAndFrontmatterPrefsDefaults(t *testing.T) {
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	for name, p := range map[string]Prefs{"defaults": DefaultPrefs(), "no file": mustLoadPrefs(t)} {
		if p.KanbanHideEmptyColumns || p.KanbanCardTitleLines != 1 || len(p.KanbanCardFields) != 0 || p.PreviewFrontmatter != "raw" {
			t.Errorf("%s: hide %v, lines %d, fields %v, frontmatter %q", name, p.KanbanHideEmptyColumns, p.KanbanCardTitleLines, p.KanbanCardFields, p.PreviewFrontmatter)
		}
	}
}

func TestBoardAndFrontmatterPrefsLoad(t *testing.T) {
	writeTerminalConfig(t, `kanban_hide_empty_columns = true
kanban_card_title_lines = 3
kanban_card_fields = ["Risico", " sessie ", "risico", ""]
preview_frontmatter = "Collapsed"`)
	p := mustLoadPrefs(t)
	if !p.KanbanHideEmptyColumns || p.KanbanCardTitleLines != 3 || p.PreviewFrontmatter != "collapsed" {
		t.Errorf("hide %v, lines %d, frontmatter %q", p.KanbanHideEmptyColumns, p.KanbanCardTitleLines, p.PreviewFrontmatter)
	}
	// Frontmatter keys are read lower-cased, so the names are kept that way.
	if want := []string{"risico", "sessie"}; !reflect.DeepEqual(p.KanbanCardFields, want) {
		t.Errorf("card fields %v, want %v", p.KanbanCardFields, want)
	}
}

// A value zn does not know never stops it from starting: it loads as the
// default, and zn doctor names the key.
func TestBoardAndFrontmatterPrefsBadValues(t *testing.T) {
	for _, body := range []string{`kanban_card_title_lines = 0`, `kanban_card_title_lines = 9`, `kanban_card_title_lines = "two"`} {
		writeTerminalConfig(t, body)
		if p := mustLoadPrefs(t); p.KanbanCardTitleLines != 1 {
			t.Errorf("%s loaded as %d lines, want 1", body, p.KanbanCardTitleLines)
		}
	}
	writeTerminalConfig(t, `preview_frontmatter = "folded"`)
	if p := mustLoadPrefs(t); p.PreviewFrontmatter != "raw" {
		t.Errorf("an unknown preview_frontmatter loaded as %q, want raw", p.PreviewFrontmatter)
	}
	err := ValidateConfig()
	if err == nil {
		t.Fatal("ValidateConfig accepted preview_frontmatter = \"folded\"")
	}
	for _, want := range []string{"terminal.preview_frontmatter", "raw, collapsed, hidden"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// The settings rows and the save path read the scalar keys from one table.
func TestBoardAndFrontmatterPrefsAreSettingsRows(t *testing.T) {
	terminal := ValueTables(DefaultPrefs())["terminal"]
	for key, want := range map[string]any{"kanban_hide_empty_columns": false, "kanban_card_title_lines": 1, "preview_frontmatter": "raw"} {
		if got, ok := terminal[key]; !ok || got != want {
			t.Errorf("terminal.%s = %v (present %v), want %v", key, got, ok, want)
		}
	}
}

func mustLoadPrefs(t *testing.T) Prefs {
	t.Helper()
	p, _, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	return p
}
