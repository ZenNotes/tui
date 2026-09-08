package config

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Prefs is the slice of the portable preferences the CLI and the terminal UI
// honor. Defaults match the desktop's PORTABLE_DEFAULTS; a missing file or
// key keeps the default.
type Prefs struct {
	VimMode               bool
	VimInsertEscape       string
	VimYankToClipboard    bool
	WhichKeyHints         bool
	WhichKeyHintMode      string
	WhichKeyHintTimeoutMs int

	SyncTitleHeadingOnRename bool
	EditorTabSize            int
	EditorScrollOff          int
	WordWrap                 bool
	DefaultPaneMode          string
	LineNumberMode           string
	CompletedTaskStyle       string
	TimeFormat               string
	AutoPairs                bool
	MarkdownSnippets         bool
	HideBuiltinTemplates     bool
	TextReplacementsEnabled  bool
	TextReplacements         map[string]string

	ThemeMode      string
	UnifiedSidebar bool

	NoteSortOrder           string
	GroupByKind             bool
	NestedTags              bool
	QuickNoteDateTitle      bool
	QuickNoteTitlePrefix    string
	CalendarWeekStart       string
	CalendarShowWeekNumbers bool
	TasksViewMode           string
	ShowArchivedTasks       bool
	KanbanGroupBy           string
	KanbanStatuses          []string
	KanbanColumnTitles      map[string]string
	SystemFolderLabels      map[string]string
	WorkflowsEnabled        bool
	AtlasEnabled            bool

	KeymapOverrides map[string]string

	// Terminal-only preferences, from [terminal] in config.toml. The desktop
	// app ignores the table.
	TerminalMouse bool
	PreviewStyle  string
	// TerminalImages is auto, kitty or off: whether the preview paints
	// pictures with the Kitty graphics protocol.
	TerminalImages string
}

// DefaultPrefs are the shipped defaults.
func DefaultPrefs() Prefs {
	return Prefs{
		VimMode:                  true,
		WhichKeyHints:            true,
		WhichKeyHintMode:         "timed",
		WhichKeyHintTimeoutMs:    900,
		SyncTitleHeadingOnRename: true,
		EditorTabSize:            4,
		EditorScrollOff:          0,
		WordWrap:                 true,
		DefaultPaneMode:          "edit",
		LineNumberMode:           "off",
		CompletedTaskStyle:       "none",
		TimeFormat:               "24h",
		AutoPairs:                true,
		MarkdownSnippets:         true,
		TextReplacementsEnabled:  true,
		TextReplacements:         map[string]string{"->": "→"},
		ThemeMode:                "dark",
		PreviewStyle:             "auto",
		TerminalMouse:            true,
		UnifiedSidebar:           true,
		NoteSortOrder:            "none",
		GroupByKind:              true,
		NestedTags:               true,
		QuickNoteTitlePrefix:     "Quick Note",
		CalendarWeekStart:        "monday",
		CalendarShowWeekNumbers:  true,
		TasksViewMode:            "list",
		KanbanGroupBy:            "status",
		KanbanStatuses:           []string{},
		KanbanColumnTitles:       map[string]string{},
		SystemFolderLabels:       map[string]string{},
		AtlasEnabled:             true,
		KeymapOverrides:          map[string]string{},
	}
}

// LoadPrefs reads config.toml. A missing file is not an error: the defaults
// come back, with the path the file would live at.
func LoadPrefs() (Prefs, string, error) {
	prefs := DefaultPrefs()
	path := ConfigTomlPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return prefs, path, nil
		}
		return prefs, path, err
	}
	var doc map[string]any
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return prefs, path, err
	}
	applyPrefs(&prefs, doc)
	return prefs, path, nil
}

func applyPrefs(p *Prefs, doc map[string]any) {
	section := func(name string) map[string]any {
		m, _ := doc[name].(map[string]any)
		return m
	}
	vim := section("vim")
	setBool(vim, "enabled", &p.VimMode)
	setString(vim, "insert_escape", &p.VimInsertEscape)
	setBool(vim, "yank_to_clipboard", &p.VimYankToClipboard)
	setBool(vim, "which_key_hints", &p.WhichKeyHints)
	setString(vim, "which_key_hint_mode", &p.WhichKeyHintMode)
	setInt(vim, "which_key_hint_timeout_ms", &p.WhichKeyHintTimeoutMs)

	editor := section("editor")
	setBool(editor, "sync_title_heading_on_rename", &p.SyncTitleHeadingOnRename)
	setInt(editor, "tab_size", &p.EditorTabSize)
	setInt(editor, "scroll_off", &p.EditorScrollOff)
	setBool(editor, "word_wrap", &p.WordWrap)
	setString(editor, "default_view_mode", &p.DefaultPaneMode)
	setString(editor, "line_number_mode", &p.LineNumberMode)
	setString(editor, "completed_task_style", &p.CompletedTaskStyle)
	setString(editor, "time_format", &p.TimeFormat)
	setBool(editor, "auto_pairs", &p.AutoPairs)
	setBool(editor, "markdown_snippets", &p.MarkdownSnippets)
	setBool(editor, "hide_builtin_templates", &p.HideBuiltinTemplates)
	setBool(editor, "text_replacements_enabled", &p.TextReplacementsEnabled)

	appearance := section("appearance")
	setString(appearance, "theme_mode", &p.ThemeMode)
	setBool(appearance, "unified_sidebar", &p.UnifiedSidebar)

	view := section("view")
	setString(view, "note_sort_order", &p.NoteSortOrder)
	setBool(view, "group_by_kind", &p.GroupByKind)
	setBool(view, "nested_tags", &p.NestedTags)
	setBool(view, "quick_note_date_title", &p.QuickNoteDateTitle)
	setString(view, "quick_note_title_prefix", &p.QuickNoteTitlePrefix)
	setString(view, "calendar_week_start", &p.CalendarWeekStart)
	setBool(view, "calendar_show_week_numbers", &p.CalendarShowWeekNumbers)
	setString(view, "tasks_view_mode", &p.TasksViewMode)
	setBool(view, "show_archived_tasks", &p.ShowArchivedTasks)
	setString(view, "kanban_group_by", &p.KanbanGroupBy)
	setBool(view, "workflows_enabled", &p.WorkflowsEnabled)
	setBool(view, "atlas_enabled", &p.AtlasEnabled)
	if list, ok := view["kanban_statuses"].([]any); ok {
		p.KanbanStatuses = []string{}
		for _, e := range list {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				p.KanbanStatuses = append(p.KanbanStatuses, strings.TrimSpace(s))
			}
		}
	}
	terminal := section("terminal")
	setBool(terminal, "mouse", &p.TerminalMouse)
	setString(terminal, "preview_style", &p.PreviewStyle)
	setString(terminal, "images", &p.TerminalImages)
	if strings.TrimSpace(p.PreviewStyle) == "" {
		p.PreviewStyle = "auto"
	}

	p.KeymapOverrides = mergeStringTable(p.KeymapOverrides, section("keymaps"))
	p.SystemFolderLabels = mergeStringTable(p.SystemFolderLabels, section("folder_labels"))
	p.KanbanColumnTitles = mergeStringTable(p.KanbanColumnTitles, section("kanban_column_titles"))
	if table := section("text_replacements"); table != nil {
		p.TextReplacements = mergeStringTable(map[string]string{}, table)
	}
	if p.EditorTabSize < 1 || p.EditorTabSize > 8 {
		p.EditorTabSize = 4
	}
	if p.EditorScrollOff < 0 {
		p.EditorScrollOff = 0
	}
}

func mergeStringTable(base map[string]string, table map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range table {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func setBool(m map[string]any, key string, dst *bool) {
	if v, ok := m[key].(bool); ok {
		*dst = v
	}
}

func setString(m map[string]any, key string, dst *string) {
	if v, ok := m[key].(string); ok {
		*dst = v
	}
}

func setInt(m map[string]any, key string, dst *int) {
	switch v := m[key].(type) {
	case int64:
		*dst = int(v)
	case float64:
		*dst = int(v)
	case int:
		*dst = v
	}
}

// DefaultConfigTOML is the commented starter file `zn config` writes when
// no config.toml exists yet. Every key matches the desktop app's names.
const DefaultConfigTOML = `# ZenNotes portable preferences. Shared by the desktop app and zn.

[vim]
enabled = true
insert_escape = "jk"
yank_to_clipboard = true
which_key_hints = true
which_key_hint_mode = "timed"
which_key_hint_timeout_ms = 900

[editor]
tab_size = 4
scroll_off = 0
word_wrap = true
default_view_mode = "edit"
line_number_mode = "off"
completed_task_style = "none"
auto_pairs = true
text_replacements_enabled = true
sync_title_heading_on_rename = true

[appearance]
# dark, light, or system (follows the terminal background in zn tui)
theme_mode = "system"

[view]
note_sort_order = "none"
nested_tags = true
quick_note_title_prefix = "Quick Note"
calendar_week_start = "monday"
calendar_show_week_numbers = true
tasks_view_mode = "list"
show_archived_tasks = false
kanban_group_by = "status"

[terminal]
# zn tui only. preview_style is a Glamour style name (auto, dark, light,
# dracula, tokyo-night, pink, ascii, notty), a path to a Glamour JSON style,
# or "zen" for the built-in renderer.
preview_style = "auto"
# images = auto | kitty | off: paint pictures in the preview with the Kitty
# graphics protocol (Kitty and Ghostty; inside tmux, set allow-passthrough on).
images = "auto"
mouse = true

[keymaps]
# "action.id" = "keys"   (an empty string unbinds an action)

[text_replacements]
"->" = "→"
`
