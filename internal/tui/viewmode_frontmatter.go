package tui

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vault"
)

// frontmatterPaneMode picks the pane mode a note's frontmatter asks for.
// A valid value in the named field wins. Otherwise the first rule, in the
// order given, that matches a frontmatter key and value wins. A list field
// matches a rule when any element does. It reports false when nothing
// applies, including when the note has no frontmatter.
func frontmatterPaneMode(text, field string, rules []config.ViewModeRule) (paneMode, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" && len(rules) == 0 {
		return "", false
	}
	block, _, ok := vault.Frontmatter(text)
	if !ok {
		return "", false
	}
	if field != "" {
		scalars, _ := vault.ParseFrontmatterScalars(text)
		keys := make([]string, 0, len(scalars))
		for k := range scalars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.ToLower(k) != field {
				continue
			}
			switch v := strings.ToLower(strings.TrimSpace(scalars[k])); v {
			case string(modeEdit), string(modeSplit), string(modePreview):
				return paneMode(v), true
			}
		}
	}
	if len(rules) == 0 {
		return "", false
	}
	fields := vault.ParseFrontmatterFields(block)
	for _, r := range rules {
		for _, el := range fields[r.Key] {
			if strings.ToLower(strings.TrimSpace(vault.Unquote(el))) == r.Value {
				return paneMode(r.Mode), true
			}
		}
	}
	return "", false
}
