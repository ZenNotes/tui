package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With the feature on, a note whose frontmatter gives no mode opens exactly as
// it would with the feature off, before and after its body loads.
func TestNoMatchingFrontmatterOpensAsFeatureOff(t *testing.T) {
	notes := map[string]string{
		"none.md":      "# plain\n",
		"unrelated.md": "---\ntitle: x\ntags: [a]\n---\nbody\n",
		"miss.md":      "---\npage-status: in-progress\n---\nbody\n",
		"badfield.md":  "---\nview-mode: reading\n---\nbody\n",
		"listfield.md": "---\nview-mode: [preview]\n---\nbody\n",
	}
	for name, body := range notes {
		for _, tc := range []struct {
			label string
			setup func(a *App)
			want  paneMode
		}{
			{"default preview", func(a *App) { a.prefs.DefaultPaneMode = "preview" }, modePreview},
			{"remembered split", func(a *App) { a.noteModes[name] = modeSplit }, modeSplit},
			{"keep across split", func(a *App) { a.prefs.RetainViewMode = true; a.prefs.DefaultPaneMode = "split" }, modeSplit},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				a, root := newFeatureTestApp(t)
				writeVaultNote(t, root, name, body)
				enableViewModeField(a)
				a.prefs.ViewModeRules = map[string]string{"page-status=done": "edit"}
				tc.setup(a)
				a.deferReads = true
				a.openNote(name, true)
				if got := modeOf(t, a, name); got != tc.want {
					t.Fatalf("before load: %q, want %q", got, tc.want)
				}
				loadDeferred(t, a, name)
				if got := modeOf(t, a, name); got != tc.want {
					t.Fatalf("after load: %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// An invalid rule never applies, and the valid rules still do.
func TestInvalidRuleIgnoredOnOpenValidRuleApplies(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\nkind: journal\npage-status: done\n---\nbody\n")
	a.prefs.DefaultPaneMode = "edit"
	a.prefs.ViewModeRules = map[string]string{
		"kind=journal":     "reading",
		"no-equals":        "preview",
		"=x":               "preview",
		"page-status=done": "split",
	}
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeSplit {
		t.Fatalf("mode %q, want split", got)
	}
}

// An empty field turns off only the field: a rule still applies after a
// deferred read.
func TestEmptyFieldKeepsRulesOnDeferredOpen(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "---\nview-mode: edit\npage-status: done\n---\nbody\n")
	a.prefs.TerminalViewModeField = ""
	a.prefs.ViewModeRules = map[string]string{"page-status=done": "preview"}
	a.deferReads = true
	a.openNote("n.md", true)
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("mode %q, want preview", got)
	}
}

// The README documents the settings a reader looks up for per-note view modes.
func TestReadmeDocumentsFrontmatterViewMode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)
	for _, want := range []string{
		"terminal.view_mode_field",
		"view-mode",
		"[terminal.view_mode_rules]",
		`"page-status=done" = "preview"`,
		"keep_view_mode_across_notes",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README does not mention %q", want)
		}
	}
	if !strings.Contains(readme, "Only `zn tui` reads these settings") {
		t.Error("README does not say that only zn tui reads these settings")
	}
}
