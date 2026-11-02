package tui

import (
	"encoding/json"
	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/templates"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

func TestInitialRemoteFailureStillRetriesPolling(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.opts.Target.Kind = backend.KindRemote
	a.idx = nil
	a.pendingCmds = nil
	_, cmd := a.Update(remotePollMsg{})
	found := false
	for _, m := range collectAsyncTestResults(cmd) {
		if _, ok := m.(indexLoadedMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("initial index failure stops remote retries")
	}
}

func TestMarkCommandFromEditorAndPaletteSelectsNote(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.openNote("note.md", true)
	for _, r := range ":mark" {
		a.handleKey(vim.R(r))
	}
	a.handleKey(vim.KeyEnter)
	if !a.markedNotes["note.md"] {
		t.Fatal(":mark was swallowed by Vim's line-mark command")
	}
	if _, err := a.runEx(nil, vim.ExCommand{Name: "mark"}); err != nil {
		t.Fatal(err)
	}
	if a.markedNotes["note.md"] {
		t.Fatal("palette command did not toggle the active note")
	}
}

func TestShutdownPreservesConflictingEditsInRecovery(t *testing.T) {
	a, root := newFeatureTestApp(t)
	a.openNote("note.md", true)
	a.activeBuffer().ed.ReplaceText("Local unsaved body")
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("External body"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.shutdown(); err == nil {
		t.Fatal("shutdown did not report unsaved conflict")
	}
	files, err := filepath.Glob(filepath.Join(config.UserDataDir(), "tui-recovery", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing recovery: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Notes map[string]string `json:"notes"`
	}
	if err = json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Notes["note.md"] != "Local unsaved body" {
		t.Fatalf("wrong recovery: %s", data)
	}
	original, _ := os.ReadFile(filepath.Join(root, "note.md"))
	if string(original) != "External body" {
		t.Fatal("external file overwritten")
	}
}

func TestFileRenameFindsReferencesAddedAfterFilesRefresh(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	m := a.backend.(backend.AssetManager)
	as, err := m.ImportAsset(a.ctx, "note.md", "report.pdf", strings.NewReader("pdf"))
	if err != nil {
		t.Fatal(err)
	}
	notes, err := a.backend.ListAssets(a.ctx)
	if err != nil || len(notes) != 1 {
		t.Fatalf("assets: %v %v", notes, err)
	}
	v := &filesView{assets: notes, uses: map[string][]vault.NoteMeta{}}
	if _, err = a.backend.WriteNote(a.ctx, "note.md", "[Report](<"+as.Path+">)"); err != nil {
		t.Fatal(err)
	}
	v.rename(a, notes[0])
	a.overlay.(*prompt).onSubmit(a, "renamed.pdf")
	got, err := a.backend.ReadNote(a.ctx, "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "renamed.pdf") {
		t.Fatalf("new reference was missed: %s", got.Body)
	}
}

func TestTemplateMetadataDoesNotOverwriteAnExternalBodyEdit(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	original := templates.Template{Name: "Example", Body: "Original body"}
	file, err := a.backend.WriteTemplate(a.ctx, vault.WriteTemplateInput{Slug: "example", Raw: templateRaw(original)})
	if err != nil {
		t.Fatal(err)
	}
	v := &templatesView{}
	v.refresh(a)
	selected := templates.ParseCustom(file)
	original.Body = "External body"
	if _, err = a.backend.WriteTemplate(a.ctx, vault.WriteTemplateInput{Slug: "example", Raw: templateRaw(original), PreviousSourcePath: file.SourcePath}); err != nil {
		t.Fatal(err)
	}
	selected.Description = "New description"
	v.save(a, selected, file.SourcePath, false)
	files, err := a.backend.ListTemplates(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0].Raw, "External body") {
		t.Fatalf("external template body overwritten: %v", files)
	}
}

func TestCommentsWorkflowUnicodeAndLongThread(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	if _, err := a.backend.WriteNote(a.ctx, "note.md", "# Note\n\nHello 世界 🌱\n"); err != nil {
		t.Fatal(err)
	}
	a.openNote("note.md", true)
	p := &commentsPanel{path: "note.md"}
	p.add(a)
	a.overlay.(*prompt).onSubmit(a, "世界 🌱")
	a.overlay.(*prompt).onSubmit(a, "First comment")
	threads, err := backend.ListCommentThreads(a.ctx, a.backend, "note.md", true)
	if err != nil || len(threads) != 1 {
		t.Fatalf("comments: %v %v", threads, err)
	}
	if threads[0].Line != 3 {
		t.Fatalf("Unicode anchor at line %d", threads[0].Line)
	}
	p.menu(a, threads[0])
	a.overlay.handleKey(a, vim.R('r'))
	a.overlay.(*prompt).onSubmit(a, strings.Repeat("Reply text\n", 80)+"Last reply line")
	threads, err = backend.ListCommentThreads(a.ctx, a.backend, "note.md", true)
	if err != nil || len(threads[0].Replies) != 1 {
		t.Fatal("reply missing", err)
	}
	p.menu(a, threads[0])
	a.overlay.handleKey(a, vim.R('v'))
	reader := a.overlay.(*textReader)
	reader.render(a, 80, 24)
	reader.handleKey(a, vim.Key{Name: "end"})
	if !strings.Contains(reader.render(a, 80, 24), "Last reply line") {
		t.Fatal("long reply tail unreachable")
	}
	p.menu(a, threads[0])
	a.overlay.handleKey(a, vim.R('x'))
	active, err := backend.ListCommentThreads(a.ctx, a.backend, "note.md", false)
	if err != nil || len(active) != 0 {
		t.Fatal("thread not resolved", err)
	}
	a.activeBuffer().ed.ReplaceText("# Anchor removed\n")
	p.menu(a, threads[0])
	// j is reserved for menu navigation; activate the explicit jump item.
	for _, it := range a.overlay.(*menu).items {
		if it.label == "Jump to anchor" {
			it.run(a)
			break
		}
	}
	if a.activeBuffer().ed.Cursor().Line >= a.activeBuffer().ed.LineCount() {
		t.Fatal("missing anchor left cursor outside note")
	}
}

func TestSidebarRangeAddsWithoutTogglingEarlierMarks(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	s := &sidebarState{rows: []sidebarRow{{kind: "note", path: "a.md"}, {kind: "note", path: "b.md"}, {kind: "note", path: "c.md"}}}
	s.handleKey(a, vim.Key{Name: "down", Shift: true})
	s.handleKey(a, vim.Key{Name: "down", Shift: true})
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		if !a.markedNotes[p] {
			t.Fatalf("range dropped %s: %v", p, a.markedNotes)
		}
	}
}

func TestBulkFolderMovePreservesHierarchyAndFailedMarks(t *testing.T) {
	a, root := newFeatureTestApp(t)
	for _, p := range []string{"projects/one.md", "projects/nested/two.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte("# Note\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.idx = a.loadIndexCmd()().(indexLoadedMsg).idx
	a.markSidebarRow(sidebarRow{kind: "folder", folder: vault.FolderInbox, subpath: "projects"})
	a.markPath("missing.md")
	a.applyBulkNotes([]string{"projects/one.md", "projects/nested/two.md", "missing.md"}, "move", "destination")
	for _, p := range []string{"destination/projects/one.md", "destination/projects/nested/two.md"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Fatalf("missing retained hierarchy %s: %v", p, err)
		}
	}
	if len(a.markedNotes) != 1 || !a.markedNotes["missing.md"] {
		t.Fatalf("failed selection lost: %v", a.markedNotes)
	}
}
