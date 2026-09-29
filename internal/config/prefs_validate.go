package config

import (
	"fmt"
	"strings"
)

func validateKnownValues(doc map[string]any) error {
	for section, defaults := range ValueTables(DefaultPrefs()) {
		raw, exists := doc[section]
		if !exists {
			continue
		}
		table, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be a table", section)
		}
		for key, want := range defaults {
			got, exists := table[key]
			if !exists {
				continue
			}
			valid := false
			switch want.(type) {
			case bool:
				_, valid = got.(bool)
			case string:
				_, valid = got.(string)
			case int:
				_, valid = got.(int64)
				if !valid {
					_, valid = got.(int)
				}
			}
			if !valid {
				return fmt.Errorf("%s.%s has the wrong type (expected %T)", section, key, want)
			}
		}
	}
	enums := map[string]map[string][]string{
		"vim":        {"which_key_hint_mode": {"timed", "sticky", "instant"}, "wrapped_line_motions": {"display", "logical"}},
		"editor":     {"default_view_mode": {"edit", "split", "preview"}, "line_number_mode": {"off", "absolute", "relative", "hybrid"}, "completed_task_style": {"none", "gray", "strikethrough", "gray-strikethrough"}, "time_format": {"12h", "24h"}},
		"view":       {"tasks_view_mode": {"list", "kanban", "board", "calendar"}, "calendar_week_start": {"monday", "sunday"}, "note_sort_order": {"none", "name-asc", "name-desc", "updated-desc", "updated-asc", "created-desc", "created-asc"}},
		"appearance": {"theme_mode": {"dark", "light", "system", "auto"}},
		"terminal":   {"images": {"auto", "kitty", "off"}},
	}
	for section, keys := range enums {
		table, _ := doc[section].(map[string]any)
		for key, choices := range keys {
			raw, exists := table[key]
			if !exists {
				continue
			}
			value, _ := raw.(string)
			ok := false
			for _, v := range choices {
				ok = ok || strings.EqualFold(value, v)
			}
			if !ok {
				return fmt.Errorf("%s.%s must be one of %s", section, key, strings.Join(choices, ", "))
			}
		}
	}
	return nil
}
