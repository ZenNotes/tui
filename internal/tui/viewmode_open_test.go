package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
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

func tabsOf(a *App, path string) []*tab {
	var out []*tab
	for _, p := range a.panes.leaves() {
		for _, t := range p.tabs {
			if t.path == path {
				out = append(out, t)
			}
		}
	}
	return out
}

func modesOf(ts []*tab) []paneMode {
	var out []paneMode
	for _, t := range ts {
		out = append(out, t.mode)
	}
	return out
}

func TestRestoredSessionTabKeepsSavedMode(t *testing.T) {
	root := newSessionParityVault(t, "n.md")
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	a := newSessionParityApp(t, root)
	a.openNote("n.md", true)
	a.activeTab().mode = modeEdit
	a.saveSession()

	restored := newSessionParityApp(t, root)
	enableViewModeField(restored)
	restored.deferReads = true
	restored.restoreSession()
	if got := modeOf(t, restored, "n.md"); got != modeEdit {
		t.Fatalf("before load: %q, want edit", got)
	}
	loadDeferred(t, restored, "n.md")
	if got := modeOf(t, restored, "n.md"); got != modeEdit {
		t.Fatalf("after load: %q, want edit", got)
	}
}

func TestSplitCopyFollowsFrontmatterMode(t *testing.T) {
	for _, preLoad := range []bool{false, true} {
		name := "after load"
		if preLoad {
			name = "before load"
		}
		t.Run(name, func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", fmPreviewNote)
			enableViewModeField(a)
			a.deferReads = true
			a.openNote("n.md", true)
			if preLoad {
				a.splitPane(true)
				loadDeferred(t, a, "n.md")
			} else {
				loadDeferred(t, a, "n.md")
				a.splitPane(true)
			}
			ts := tabsOf(a, "n.md")
			if len(ts) != 2 || ts[0].mode != modePreview || ts[1].mode != modePreview {
				t.Fatalf("tabs %d, modes %v", len(ts), modesOf(ts))
			}
		})
	}
}

func TestSplitCopyKeepsManualMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	enableViewModeField(a)
	a.deferReads = true
	a.openNote("n.md", true)
	a.setPaneMode(modeEdit)
	a.splitPane(true)
	loadDeferred(t, a, "n.md")
	ts := tabsOf(a, "n.md")
	if len(ts) != 2 || ts[0].mode != modeEdit || ts[1].mode != modeEdit {
		t.Fatalf("tabs %d, modes %v", len(ts), modesOf(ts))
	}
}

func TestReferencePaneStaysPreview(t *testing.T) {
	for _, fm := range []string{"edit", "split"} {
		t.Run(fm, func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", "---\nview-mode: "+fm+"\n---\nbody\n")
			writeVaultNote(t, root, "other.md", "# other\n")
			a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
			enableViewModeField(a)
			a.deferReads = true
			a.openNote("other.md", true)
			loadDeferred(t, a, "other.md")
			a.openReference("n.md")
			loadDeferred(t, a, "n.md")
			ts := tabsOf(a, "n.md")
			if len(ts) != 1 || ts[0].mode != modePreview {
				t.Fatalf("modes %v, want preview", modesOf(ts))
			}
		})
	}
}

const (
	fmPreviewBody = "---\nview-mode: preview\n---\n# New\n"
	fmRuleBody    = "---\npage-status: done\n---\n# New\n"
)

// previewEverywhere makes every frontmatter and default route point at preview
// so only the new-note handling can keep a note in edit.
func previewEverywhere(a *App, defaultMode string) {
	enableViewModeField(a)
	a.prefs.ViewModeRules = map[string]string{"page-status=done": "preview"}
	a.prefs.DefaultPaneMode = defaultMode
	a.deferReads = true
}

func activeNotePath(t *testing.T, a *App) string {
	t.Helper()
	tb := a.activePane.activeTab()
	if tb == nil {
		t.Fatal("no active tab")
	}
	return tb.path
}

// wantEditBeforeAndAfterLoad checks the active note's mode across the load.
func wantEditBeforeAndAfterLoad(t *testing.T, a *App) string {
	t.Helper()
	p := activeNotePath(t, a)
	if got := modeOf(t, a, p); got != modeEdit {
		t.Fatalf("before load: %q, want edit", got)
	}
	loadDeferred(t, a, p)
	if got := modeOf(t, a, p); got != modeEdit {
		t.Fatalf("after load: %q, want edit", got)
	}
	if len(a.newNotes) != 0 {
		t.Fatalf("new-note mark left behind: %v", a.newNotes)
	}
	return p
}

func TestNewNoteFromTemplateOpensInEdit(t *testing.T) {
	for _, body := range []string{fmPreviewBody, fmRuleBody} {
		for _, def := range []string{"preview", "split"} {
			a, _ := newFeatureTestApp(t)
			previewEverywhere(a, def)
			tpl := templates.Template{ID: "custom:x", Name: "X", Body: body}
			a.createFromTemplate(tpl)
			typeText(a, "From Template")
			a.overlay.handleKey(a, enterKey())
			wantEditBeforeAndAfterLoad(t, a)
		}
	}
}

func TestNewBlankNoteOpensInEditWithFeatureOn(t *testing.T) {
	for _, def := range []string{"preview", "split"} {
		for _, body := range []string{fmPreviewBody, fmRuleBody} {
			a, _ := newFeatureTestApp(t)
			previewEverywhere(a, def)
			b := body
			a.createNote(vault.FolderInbox, "Blank", "", &b, true)
			wantEditBeforeAndAfterLoad(t, a)
		}
	}
}

func useDailyTemplate(t *testing.T, a *App, body string) {
	t.Helper()
	raw := templates.ComposeFile(templates.ComposeInput{Name: "Daily FM", Body: body})
	if _, err := a.backend.WriteTemplate(context.Background(), vault.WriteTemplateInput{Slug: "dailyfm", Raw: raw}); err != nil {
		t.Fatal(err)
	}
	a.idx.settings.DailyNotes.Enabled = true
	a.idx.settings.DailyNotes.TemplateID = "custom:dailyfm"
}

func TestNewPeriodicNoteOpensInEdit(t *testing.T) {
	for _, body := range []string{fmPreviewBody, fmRuleBody} {
		a, _ := newFeatureTestApp(t)
		useDailyTemplate(t, a, body)
		previewEverywhere(a, "preview")
		a.openPeriodicOn(periodic.Daily, time.Now())
		wantEditBeforeAndAfterLoad(t, a)
	}
}

func TestExistingPeriodicNoteFollowsFrontmatter(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	useDailyTemplate(t, a, fmPreviewBody)
	previewEverywhere(a, "edit")
	a.openPeriodicOn(periodic.Daily, time.Now())
	p := activeNotePath(t, a)
	loadDeferred(t, a, p)
	a.closeTab(a.activePane, a.activePane.findTab(p))
	a.openPeriodicOn(periodic.Daily, time.Now())
	if got := modeOf(t, a, p); got != modeEdit {
		t.Fatalf("before load: %q", got)
	}
	loadDeferred(t, a, p)
	if got := modeOf(t, a, p); got != modePreview {
		t.Fatalf("existing daily note: %q, want preview", got)
	}
}

func TestNewTaskFileOpensInEdit(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	previewEverywhere(a, "preview")
	a.prefs.ViewModeRules = map[string]string{"status=open": "preview"}
	if err := a.newTaskFile(""); err != nil {
		t.Fatal(err)
	}
	typeText(a, "Write it")
	a.overlay.handleKey(a, enterKey())
	wantEditBeforeAndAfterLoad(t, a)
}

func TestRolloverCreatedDailyNoteOpensInEdit(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	useDailyTemplate(t, a, fmPreviewBody)
	previewEverywhere(a, "preview")
	if err := a.rolloverNow(); err != nil {
		t.Fatal(err)
	}
	wantEditBeforeAndAfterLoad(t, a)
}

func TestNotesCreatedWithoutOpeningLeaveNoMark(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	useDailyTemplate(t, a, fmRuleBody)
	previewEverywhere(a, "edit")
	before := map[string]bool{}
	for _, n := range a.idx.notes {
		before[n.Path] = true
	}

	a.addTaskToDaily("one", time.Now())
	a.saveCapture("Captured\nbody")
	if len(a.newNotes) != 0 {
		t.Fatalf("mark set: %v", a.newNotes)
	}
	var created []string
	for _, n := range a.idx.notes {
		if !before[n.Path] {
			created = append(created, n.Path)
		}
	}
	if len(created) == 0 {
		t.Fatal("no notes were created")
	}
	// A note created without opening follows its frontmatter on a later open.
	daily := ""
	for _, p := range created {
		if strings.Contains(p, time.Now().Format("2006-01-02")) {
			daily = p
		}
	}
	if daily == "" {
		t.Fatalf("daily note not found in %v", created)
	}
	a.openNote(daily, true)
	loadDeferred(t, a, daily)
	if got := modeOf(t, a, daily); got != modePreview {
		t.Fatalf("later open: %q, want preview", got)
	}

	// Without daily notes the task goes to an Inbox "Tasks" note.
	a.idx.settings.DailyNotes.Enabled = false
	a.addTaskToDaily("two", time.Now())
	if len(a.newNotes) != 0 {
		t.Fatalf("mark set: %v", a.newNotes)
	}
}

// With the feature off, a new note's mode is whatever note creation already
// gives it; its frontmatter plays no part, and nothing is left waiting.
func TestNewNoteWithFeatureOffIgnoresFrontmatter(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.prefs.DefaultPaneMode = "split"
	b := fmPreviewBody
	a.createNote(vault.FolderInbox, "Plain", "", &b, true)
	p := activeNotePath(t, a)
	if got := modeOf(t, a, p); got == modePreview {
		t.Fatalf("mode %q: frontmatter applied with the feature off", got)
	}
	for _, tb := range tabsOf(a, p) {
		if tb.frontmatterPending {
			t.Fatal("tab left waiting for frontmatter with the feature off")
		}
	}
	if len(a.newNotes) != 0 {
		t.Fatalf("mark left: %v", a.newNotes)
	}
}

func TestReopenedNewNoteFollowsFrontmatter(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	previewEverywhere(a, "edit")
	b := fmPreviewBody
	a.createNote(vault.FolderInbox, "Again", "", &b, true)
	p := wantEditBeforeAndAfterLoad(t, a)
	a.closeTab(a.activePane, a.activePane.findTab(p))
	a.openNote(p, true)
	loadDeferred(t, a, p)
	if got := modeOf(t, a, p); got != modePreview {
		t.Fatalf("reopened: %q, want preview", got)
	}
}

// A new note is for typing, so it opens in the editor with the frontmatter
// feature off too, whatever the default view mode.
func TestNewNoteOpensInEditWithFeatureOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(t *testing.T, a *App)
	}{
		{"blank", func(t *testing.T, a *App) {
			a.createNote(vault.FolderInbox, "Blank", "", nil, true)
		}},
		{"template", func(t *testing.T, a *App) {
			a.createFromTemplate(templates.Template{ID: "custom:x", Name: "X", Body: "# New\n"})
			typeText(a, "From Template")
			a.overlay.handleKey(a, enterKey())
		}},
		{"periodic", func(t *testing.T, a *App) {
			useDailyTemplate(t, a, "# Daily\n")
			a.openPeriodicOn(periodic.Daily, time.Now())
		}},
		{"task file", func(t *testing.T, a *App) {
			if err := a.newTaskFile(""); err != nil {
				t.Fatal(err)
			}
			typeText(a, "Write it")
			a.overlay.handleKey(a, enterKey())
		}},
	} {
		for _, def := range []string{"preview", "split"} {
			t.Run(tc.name+" "+def, func(t *testing.T) {
				a, _ := newFeatureTestApp(t)
				a.prefs.DefaultPaneMode = def
				a.deferReads = true
				tc.create(t, a)
				wantEditBeforeAndAfterLoad(t, a)
			})
		}
	}
}

// vaultSnapshot maps every file under root, hidden directories included, to its bytes.
func vaultSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestViewModeFeatureWritesNoVaultFiles(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", fmPreviewNote)
	writeVaultNote(t, root, "m.md", "---\nkind: journal\n---\nbody\n")
	writeVaultNote(t, root, "plain.md", "# plain\n")
	enableViewModeField(a)
	a.prefs.ViewModeRules = map[string]string{"kind=journal": "split"}
	before := vaultSnapshot(t, root)

	a.deferReads = true
	for _, name := range []string{"n.md", "m.md", "plain.md"} {
		a.openNote(name, true)
		loadDeferred(t, a, name)
	}
	a.setPaneMode(modeSplit)
	a.splitPane(true)
	a.setPaneMode(modeEdit)
	a.openNote("n.md", true)
	a.closeTab(a.activePane, a.activePane.findTab("n.md"))

	v := openAddRule(t, a)
	submitPrompt(t, a, "page-status")
	submitPrompt(t, a, "done")
	pickPalette(t, a, "preview")
	v.list.cursor = settingsLineIndex(t, v, ruleDoneRow)
	v.handleKey(a, vim.Key{Rune: 'd'})
	if _, ok := a.prefs.ViewModeRules["page-status=done"]; ok {
		t.Fatalf("rules after add and remove = %v", a.prefs.ViewModeRules)
	}

	after := vaultSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("vault files %d, want %d", len(after), len(before))
	}
	for name, body := range before {
		if after[name] != body {
			t.Fatalf("%s changed:\n%s\nwas:\n%s", name, after[name], body)
		}
	}
	if _, ok := after[filepath.Join(".zennotes", "vault.json")]; ok {
		t.Fatal(".zennotes/vault.json appeared")
	}
}

// Peeking a note from the sidebar asks for preview; a slow read of the note's
// frontmatter must not take that back.
func TestSidebarPeekStaysPreview(t *testing.T) {
	for _, fm := range []string{"edit", "split"} {
		t.Run(fm, func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", "---\nview-mode: "+fm+"\n---\nbody\n")
			a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
			a.sidebar.rebuild(a)
			enableViewModeField(a)
			found := false
			for i, row := range a.sidebar.rows {
				if row.path == "n.md" {
					a.sidebar.cursor = i
					found = true
					break
				}
			}
			if !found {
				t.Fatal("fixture note is absent from the sidebar")
			}
			a.focus = focusSidebar
			a.deferReads = true
			a.sidebar.handleKey(a, vim.Key{Rune: 'p'})
			if got := modeOf(t, a, "n.md"); got != modePreview {
				t.Fatalf("mode before load %q, want preview", got)
			}
			loadDeferred(t, a, "n.md")
			if got := modeOf(t, a, "n.md"); got != modePreview {
				t.Fatalf("mode after load %q, want preview", got)
			}
		})
	}
}

// Reopening a closed tab is an ordinary open in a new tab: the frontmatter
// mode wins over the mode the tab was closed in, however the read lands.
func TestReopenedClosedTabFollowsFrontmatter(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "deferred"}[deferred], func(t *testing.T) {
			a, root := newFeatureTestApp(t)
			writeVaultNote(t, root, "n.md", "---\nview-mode: preview\n---\nbody\n")
			a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
			enableViewModeField(a)
			a.openNote("n.md", true)
			a.setPaneMode(modeSplit)
			a.closeTab(a.activePane, a.activePane.findTab("n.md"))
			if len(tabsOf(a, "n.md")) != 0 {
				t.Fatal("tab did not close")
			}
			a.deferReads = deferred
			a.reopenClosedTab()
			if deferred {
				loadDeferred(t, a, "n.md")
			}
			if got := modeOf(t, a, "n.md"); got != modePreview {
				t.Fatalf("mode %q, want preview from frontmatter", got)
			}
		})
	}
}

// A reopened note whose frontmatter asks for nothing keeps the mode its tab
// was closed in.
func TestReopenedClosedTabWithoutFrontmatterKeepsMode(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "n.md", "# plain\n")
	a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
	enableViewModeField(a)
	a.openNote("n.md", true)
	a.setPaneMode(modeSplit)
	a.closeTab(a.activePane, a.activePane.findTab("n.md"))
	a.deferReads = true
	a.reopenClosedTab()
	loadDeferred(t, a, "n.md")
	if got := modeOf(t, a, "n.md"); got != modeSplit {
		t.Fatalf("mode %q, want split", got)
	}
}

// Reopening a closed tab whose note has since gone leaves the active tab's
// mode alone.
func TestReopenedDeletedNoteLeavesActiveTabAlone(t *testing.T) {
	a, root := newFeatureTestApp(t)
	writeVaultNote(t, root, "gone.md", "---\nview-mode: preview\n---\nbody\n")
	writeVaultNote(t, root, "stay.md", "---\nview-mode: preview\n---\nbody\n")
	a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
	enableViewModeField(a)
	a.openNote("gone.md", true)
	a.closeTab(a.activePane, a.activePane.findTab("gone.md"))
	a.openNote("stay.md", true)
	a.setPaneMode(modeSplit)
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}
	a.reopenClosedTab()
	if got := modeOf(t, a, "stay.md"); got != modeSplit {
		t.Fatalf("active tab mode %q, want split", got)
	}
}
