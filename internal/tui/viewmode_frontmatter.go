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

// frontmatterModeOn reports whether the configuration asks notes to pick
// their pane mode from frontmatter.
func (a *App) frontmatterModeOn() bool {
	return strings.TrimSpace(a.prefs.TerminalViewModeField) != "" ||
		len(config.ValidViewModeRules(a.prefs.ViewModeRules)) > 0
}

// watchFrontmatterMode marks a newly opened note tab so that its frontmatter
// sets the pane mode once the note text is available. It does nothing when
// the feature is off, so the default open path is unchanged.
func (a *App) watchFrontmatterMode(t *tab) {
	if !a.frontmatterModeOn() || isVirtualPath(t.path) {
		return
	}
	t.frontmatterPending = true
	a.withLoadedBuffer(t.path, a.applyFrontmatterMode)
}

// applyFrontmatterMode sets the pane mode of every still-pending tab on the
// loaded note. It writes the tab only: the remembered per-note modes and the
// keep-across default stay as the user left them.
func (a *App) applyFrontmatterMode(buf *noteBuffer) {
	var pending []*tab
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if t.path == buf.path && t.frontmatterPending {
				t.frontmatterPending = false
				pending = append(pending, t)
			}
		}
	}
	if len(pending) == 0 {
		return
	}
	mode, ok := frontmatterPaneMode(buf.ed.Text(), a.prefs.TerminalViewModeField, config.ValidViewModeRules(a.prefs.ViewModeRules))
	if !ok {
		return
	}
	for _, t := range pending {
		t.mode = mode
		if mode != modeEdit && t.preview == nil {
			t.preview = &previewState{}
		}
	}
	a.layout()
	a.markSessionDirty()
}

// forceTabMode sets a tab's mode for an explicit request, such as a
// reference or a peek. The note's frontmatter, still to be read, will not
// override it.
func forceTabMode(t *tab, mode paneMode) {
	t.mode = mode
	t.frontmatterPending = false
	if mode != modeEdit && t.preview == nil {
		t.preview = &previewState{}
	}
}

// markNewNote records that path was just created and is about to be opened,
// so the open starts in the editor instead of taking a mode from frontmatter.
func (a *App) markNewNote(path string) {
	a.newNotes[path] = true
}

// consumeNewNote reports whether path carried a new-note mark and clears it,
// so a later open of the same note is an ordinary one.
func (a *App) consumeNewNote(path string) bool {
	if !a.newNotes[path] {
		return false
	}
	delete(a.newNotes, path)
	return true
}
