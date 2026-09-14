package config

import (
	"fmt"
	"reflect"
)

// ValueTables contains the portable settings the terminal can edit. Keeping
// their file names in one place also drives the in-app settings controls.
func ValueTables(p Prefs) map[string]map[string]any {
	return map[string]map[string]any{
		"vim":        {"enabled": p.VimMode, "insert_escape": p.VimInsertEscape, "yank_to_clipboard": p.VimYankToClipboard, "which_key_hints": p.WhichKeyHints, "which_key_hint_mode": p.WhichKeyHintMode, "which_key_hint_timeout_ms": p.WhichKeyHintTimeoutMs, "wrapped_line_motions": p.VimWrappedLineMotions},
		"editor":     {"tab_size": p.EditorTabSize, "scroll_off": p.EditorScrollOff, "word_wrap": p.WordWrap, "default_view_mode": p.DefaultPaneMode, "line_number_mode": p.LineNumberMode, "completed_task_style": p.CompletedTaskStyle, "time_format": p.TimeFormat, "auto_pairs": p.AutoPairs, "auto_pair_quotes_in_prose": p.AutoPairQuotesInProse, "tabs_enabled": p.TabsEnabled, "wrap_tabs": p.WrapTabs, "keep_view_mode_across_notes": p.RetainViewMode, "markdown_snippets": p.MarkdownSnippets, "hide_builtin_templates": p.HideBuiltinTemplates, "text_replacements_enabled": p.TextReplacementsEnabled, "sync_title_heading_on_rename": p.SyncTitleHeadingOnRename},
		"appearance": {"theme_mode": p.ThemeMode, "unified_sidebar": p.UnifiedSidebar},
		"view":       {"note_sort_order": p.NoteSortOrder, "group_by_kind": p.GroupByKind, "nested_tags": p.NestedTags, "quick_note_date_title": p.QuickNoteDateTitle, "quick_note_title_prefix": p.QuickNoteTitlePrefix, "calendar_week_start": p.CalendarWeekStart, "calendar_show_week_numbers": p.CalendarShowWeekNumbers, "tasks_view_mode": p.TasksViewMode, "show_archived_tasks": p.ShowArchivedTasks, "kanban_group_by": p.KanbanGroupBy, "kanban_folder_root": p.KanbanFolderRoot},
		"terminal":   {"mouse": p.TerminalMouse, "preview_style": p.PreviewStyle, "images": p.TerminalImages},
	}
}

// SaveChanges patches only settings changed by this UI interaction, retaining
// concurrent changes made by the desktop to unrelated preferences.
func SaveChanges(before, after Prefs) error {
	previous, next := ValueTables(before), ValueTables(after)
	changes := map[string]map[string]any{}
	for section, values := range next {
		for key, value := range values {
			if !reflect.DeepEqual(previous[section][key], value) {
				if changes[section] == nil {
					changes[section] = map[string]any{}
				}
				changes[section][key] = value
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return UpdateConfig(func(doc map[string]any) error {
		for section, values := range changes {
			table, _ := doc[section].(map[string]any)
			if table == nil {
				if _, exists := doc[section]; exists {
					return fmt.Errorf("%s must be a table", section)
				}
				table = map[string]any{}
				doc[section] = table
			}
			for key, value := range values {
				table[key] = value
			}
		}
		return nil
	})
}
