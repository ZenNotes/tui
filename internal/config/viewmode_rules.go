package config

import (
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// ViewModeRule is one valid [terminal.view_mode_rules] entry in normalised
// form: the frontmatter key and value are lower-cased and unquoted, and the
// mode is edit, split or preview.
type ViewModeRule struct {
	Key   string
	Value string
	Mode  string
}

// ViewModeRuleEntry is one entry as written in the file. Key and Mode are
// the raw strings, so a malformed entry can still be shown and removed. A
// valid entry also carries its normalised Rule.
type ViewModeRuleEntry struct {
	Key    string
	Mode   string
	Valid  bool
	Reason string
	Rule   ViewModeRule
}

// ViewModeModes are the pane modes a rule can name.
var ViewModeModes = []string{"edit", "split", "preview"}

func normalizeViewModeMode(mode string) (string, bool) {
	m := strings.ToLower(strings.TrimSpace(mode))
	for _, v := range ViewModeModes {
		if m == v {
			return m, true
		}
	}
	return "", false
}

func normalizeViewModePart(s string) string {
	return strings.ToLower(strings.TrimSpace(vault.Unquote(s)))
}

// ParseViewModeRule validates one "<key>=<value>" entry and its mode. The
// first "=" splits key from value. On failure it returns a short reason.
func ParseViewModeRule(key, mode string) (ViewModeRule, string) {
	k, v, found := strings.Cut(key, "=")
	if !found {
		return ViewModeRule{}, `no "="`
	}
	k = normalizeViewModePart(k)
	if k == "" {
		return ViewModeRule{}, "empty key"
	}
	m, ok := normalizeViewModeMode(mode)
	if !ok {
		return ViewModeRule{}, "bad mode"
	}
	return ViewModeRule{Key: k, Value: normalizeViewModePart(v), Mode: m}, ""
}

// ValidateViewModeRule is the check writers run before storing a rule: the
// key must hold "=", a non-empty field and a non-empty value, and the mode
// must be edit, split or preview.
func ValidateViewModeRule(key, mode string) error {
	rule, reason := ParseViewModeRule(key, mode)
	if reason == "" && rule.Value == "" {
		reason = "empty value"
	}
	if reason != "" {
		return &viewModeRuleError{reason}
	}
	return nil
}

type viewModeRuleError struct{ reason string }

func (e *viewModeRuleError) Error() string { return "invalid view-mode rule: " + e.reason }

// ViewModeRuleEntries returns every entry, valid or not. Valid ones come
// first in match order, then the invalid ones sorted by raw key.
func ViewModeRuleEntries(raw map[string]string) []ViewModeRuleEntry {
	var valid, invalid []ViewModeRuleEntry
	for k, m := range raw {
		rule, reason := ParseViewModeRule(k, m)
		e := ViewModeRuleEntry{Key: k, Mode: m, Valid: reason == "", Reason: reason, Rule: rule}
		if e.Valid {
			valid = append(valid, e)
		} else {
			invalid = append(invalid, e)
		}
	}
	sort.Slice(valid, func(i, j int) bool {
		a, b := valid[i], valid[j]
		ak, bk := a.Rule.Key+"="+a.Rule.Value, b.Rule.Key+"="+b.Rule.Value
		if ak != bk {
			return ak < bk
		}
		return a.Key < b.Key
	})
	sort.Slice(invalid, func(i, j int) bool { return invalid[i].Key < invalid[j].Key })
	return append(valid, invalid...)
}

// ValidViewModeRules returns the usable rules in match order: sorted by the
// lower-cased "key=value" in byte order. Invalid entries are left out.
func ValidViewModeRules(raw map[string]string) []ViewModeRule {
	var out []ViewModeRule
	for _, e := range ViewModeRuleEntries(raw) {
		if e.Valid {
			out = append(out, e.Rule)
		}
	}
	return out
}
