package tui

import (
	"os"
	"path/filepath"
	"testing"
)

const fmPreviewNote = "---\nview-mode: preview\npage-status: done\n---\n# Body\n"

func writeVaultNote(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// loadDeferred finishes a deferred read of an open note.
func loadDeferred(t *testing.T, a *App, path string) {
	t.Helper()
	buf := a.buffers[path]
	if buf == nil {
		t.Fatalf("no buffer for %s", path)
	}
	a.applyBufferRead(a.readBufferCmd(buf)().(bufferReadMsg))
}

func modeOf(t *testing.T, a *App, path string) paneMode {
	t.Helper()
	for _, p := range a.panes.leaves() {
		if i := p.findTab(path); i >= 0 {
			return p.tabs[i].mode
		}
	}
	t.Fatalf("no tab for %s", path)
	return ""
}

func enableViewModeField(a *App) { a.prefs.TerminalViewModeField = "view-mode" }

func TestDefaultConfigIgnoresViewModeFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(a *App)
		want  paneMode
	}{
		{"default edit", func(a *App) { a.prefs.DefaultPaneMode = "edit" }, modeEdit},
		{"default split", func(a *App) { a.prefs.DefaultPaneMode = "split" }, modeSplit},
		{"remembered", func(a *App) { a.noteModes["n.md"] = modeSplit }, modeSplit},
		{"keep across", func(a *App) { a.prefs.RetainViewMode = true; a.prefs.DefaultPaneMode = "split" }, modeSplit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", fmPreviewNote)
			tc.setup(a)
			a.deferReads = true
			a.openNote("n.md", true)
			if got := modeOf(t, a, "n.md"); got != tc.want {
				t.Fatalf("before load: %q, want %q", got, tc.want)
			}
			loadDeferred(t, a, "n.md")
			if got := modeOf(t, a, "n.md"); got != tc.want {
				t.Fatalf("after load: %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFieldModeAppliesOnOpen(t *testing.T) {
	for _, m := range []paneMode{modeEdit, modeSplit, modePreview} {
		t.Run(string(m), func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", "---\nview-mode: "+string(m)+"\n---\nbody\n")
			a.prefs.DefaultPaneMode = "split"
			if m == modeSplit {
				a.prefs.DefaultPaneMode = "edit"
			}
			enableViewModeField(a)
			a.openNote("n.md", true)
			if got := modeOf(t, a, "n.md"); got != m {
				t.Fatalf("mode %q, want %q", got, m)
			}
			if m != modeEdit && a.activeTab().preview == nil {
				t.Fatal("preview state not allocated")
			}
		})
	}
}

func TestFrontmatterModeBeatsRememberedMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.noteModes["n.md"] = modeEdit
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("mode %q", got)
	}
}

func TestFrontmatterModeBeatsKeepAcrossMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.prefs.RetainViewMode = true
	a.prefs.DefaultPaneMode = "edit"
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("mode %q", got)
	}
}

func TestRuleModeAppliesOnOpen(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	a.prefs.ViewModeRules = map[string]string{"page-status=done": "split"}
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeSplit {
		t.Fatalf("mode %q", got)
	}
}

func TestDeferredReadSwitchesModeAfterLoad(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.deferReads = true
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("before load: %q", got)
	}
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("after load: %q", got)
	}
	if a.activeTab().preview == nil {
		t.Fatal("preview state not allocated")
	}

	t.Run("closed before load", func(t *testing.T) {
		a, root := newFeatureTestApp(t)
		writeVaultNote(t, root, "n.md", fmPreviewNote)
		enableViewModeField(a)
		a.deferReads = true
		a.openNote("n.md", true)
		msg := a.readBufferCmd(a.buffers["n.md"])().(bufferReadMsg)
		// A loading note cannot be closed through closeTab, so drop the tab
		// and its buffer the way a closed pane would.
		a.activePane.tabs = nil
		a.closeBufferIfUnused("n.md")
		a.applyBufferRead(msg)
		for _, p := range a.panes.leaves() {
			if len(p.tabs) != 0 {
				t.Fatalf("stray tab: %d", len(p.tabs))
			}
		}
	})
}

func TestManualSwitchBeforeLoadWins(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.deferReads = true
	a.openNote("n.md", true)
	a.setPaneMode(modeSplit)
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modeSplit {
		t.Fatalf("mode %q", got)
	}
}

func TestSavingNewFrontmatterKeepsOpenTabMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "# plain\n")
	writeVaultNote(t, root, "other.md", "other\n")
	a.prefs.ViewModeRules = map[string]string{"page-status=done": "preview"}
	a.openNote("n.md", true)
	buf := a.buffers["n.md"]
	buf.ed.SetText("---\npage-status: done\n---\n# plain\n")
	if err := a.saveBuffer(buf); err != nil {
		t.Fatal(err)
	}
	a.openNote("other.md", true)
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("mode %q", got)
	}
}

func TestReopenAfterCloseUsesNewFrontmatter(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "# plain\n")
	a.prefs.ViewModeRules = map[string]string{"page-status=done": "preview"}
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("first open %q", got)
	}
	a.closeTab(a.activePane, 0)
	writeVaultNote(t, root, "n.md", "---\npage-status: done\n---\n# plain\n")
	a.openNote("n.md", true)
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("reopen %q", got)
	}
}

func TestQuietOpenAppliesFrontmatterMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.deferReads = true
	a.openNoteQuiet("n.md")
	if got := modeOf(t, a, "n.md"); got != modeEdit {
		t.Fatalf("before load %q", got)
	}
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("after load %q", got)
	}
}

func TestManualSwitchAfterApplyIsRemembered(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.prefs.RetainViewMode = true
	a.openNote("n.md", true)
	a.setPaneMode(modeSplit)
	if a.noteModes["n.md"] != modeSplit || a.prefs.DefaultPaneMode != "split" {
		t.Fatalf("noteModes %q, default %q", a.noteModes["n.md"], a.prefs.DefaultPaneMode)
	}
}

func TestAppliedModeLeavesKeepAcrossAndRememberedAlone(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.prefs.RetainViewMode = true
	a.prefs.DefaultPaneMode = "edit"
	a.deferReads = true
	a.openNote("n.md", true)
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modePreview {
		t.Fatalf("mode %q", got)
	}
	if a.prefs.DefaultPaneMode != "edit" {
		t.Fatalf("keep-across default changed to %q", a.prefs.DefaultPaneMode)
	}
	if _, ok := a.noteModes["n.md"]; ok {
		t.Fatal("applied mode was remembered")
	}
}
