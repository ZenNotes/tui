package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/vim"
)

func newSessionParityVault(t *testing.T, paths ...string) string {
	t.Helper()
	t.Setenv("ZENNOTES_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".zennotes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(root, path), []byte("# "+path+"\n\nSaved content.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newSessionParityApp(t *testing.T, root string) *App {
	t.Helper()
	target := backend.Target{Kind: backend.KindLocal, Root: root}
	b, err := backend.New(target, backend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(context.Background(), Options{Backend: b, Target: target}, config.DefaultPrefs(), true)
	loaded := a.loadIndexCmd()().(indexLoadedMsg)
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	a.idx = loaded.idx
	a.sidebar.rebuild(a)
	return a
}

func TestSessionRestoresNestedWorkspace(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md", "Beta.md", "Gamma.md")
	a := newSessionParityApp(t, root)
	a.openNote("Alpha.md", true)
	a.activeTab().mode = modePreview
	a.togglePin(a.activePane, 0)
	a.openNote("Beta.md", true)
	a.activeTab().mode = modeSplit
	left := a.activePane
	right := a.panes.split(left, true)
	a.activePane = right
	a.openNote("Gamma.md", true)
	a.activeTab().mode = modePreview
	a.togglePin(right, right.active)
	bottom := a.panes.split(right, false)
	a.activePane = bottom
	a.openHelp()
	a.togglePin(bottom, bottom.active)
	bottom.active = 1
	a.activePane = right
	right.active = 1
	a.panes.ratio = 0.37
	a.panes.second.ratio = 0.62
	a.sidebarOpen = false
	a.sidebarWidth = 47
	a.sidebar.collapsed["tasks"] = true
	a.sidebar.favOpen["fav:inbox:Projects"] = true
	a.saveSession()

	restored := newSessionParityApp(t, root)
	restored.restoreSession()

	assertSessionParityTree(t, restored.panes, a.panes, restored.activePane, a.activePane, "root")
	if restored.sidebarOpen || restored.sidebarWidth != 47 {
		t.Errorf("sidebar state = open %v, width %d; want closed, width 47", restored.sidebarOpen, restored.sidebarWidth)
	}
	if !reflect.DeepEqual(restored.sidebar.collapsed, a.sidebar.collapsed) || !reflect.DeepEqual(restored.sidebar.favOpen, a.sidebar.favOpen) {
		t.Error("sidebar expansion state changed during restore")
	}
	for _, p := range restored.panes.leaves() {
		for _, tab := range p.tabs {
			if isVirtualPath(tab.path) {
				if tab.view == nil {
					t.Errorf("restored virtual tab %q has no view", tab.path)
				}
			} else if restored.buffers[tab.path] == nil {
				t.Errorf("restored note %q has no buffer", tab.path)
			}
		}
	}
}

func assertSessionParityTree(t *testing.T, got, want *paneNode, gotActive, wantActive *pane, location string) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Errorf("%s: restored split tree has a missing node", location)
		}
		return
	}
	if want.leaf != nil {
		if got.leaf == nil {
			t.Errorf("%s: want a tab pane, got a split", location)
			return
		}
		if (got.leaf == gotActive) != (want.leaf == wantActive) {
			t.Errorf("%s: active pane changed during restore", location)
		}
		if got.leaf.active != want.leaf.active {
			t.Errorf("%s: active tab = %d, want %d", location, got.leaf.active, want.leaf.active)
		}
		if len(got.leaf.tabs) != len(want.leaf.tabs) {
			t.Errorf("%s: restored %d tabs, want %d", location, len(got.leaf.tabs), len(want.leaf.tabs))
			return
		}
		for i, expected := range want.leaf.tabs {
			actual := got.leaf.tabs[i]
			if actual.path != expected.path || actual.mode != expected.mode || actual.pinned != expected.pinned {
				t.Errorf("%s tab %d = (%q, %q, pinned %v), want (%q, %q, pinned %v)", location, i, actual.path, actual.mode, actual.pinned, expected.path, expected.mode, expected.pinned)
			}
		}
		return
	}
	if got.leaf != nil {
		t.Errorf("%s: workspace collapsed to a single pane; want nested split tree", location)
		return
	}
	if got.vertical != want.vertical || got.ratio != want.ratio {
		t.Errorf("%s: split = (vertical %v, ratio %v), want (vertical %v, ratio %v)", location, got.vertical, got.ratio, want.vertical, want.ratio)
	}
	assertSessionParityTree(t, got.first, want.first, gotActive, wantActive, location+".first")
	assertSessionParityTree(t, got.second, want.second, gotActive, wantActive, location+".second")
}

func writeSessionParityFixture(t *testing.T, root, state string) {
	t.Helper()
	// Literal disk fixtures keep migration and corruption coverage independent
	// of the current session serializer's Go declarations.
	key, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"vaults":{` + string(key) + `:` + state + `}}`)
	if err := os.WriteFile(sessionPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRestoresLegacyFlatTabs(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md", "Beta.md")
	writeSessionParityFixture(t, root, `{
		"tabs":[
			{"path":"zen://help","pinned":true},
			{"path":"Alpha.md","mode":"preview","pinned":true},
			{"path":"Beta.md","mode":"split"}
		],
		"active":2,"sidebarOpen":true,"sidebarWidth":40
	}`)
	a := newSessionParityApp(t, root)
	a.restoreSession()

	want := &paneNode{leaf: &pane{tabs: []*tab{
		{path: tabHelp, pinned: true},
		{path: "Alpha.md", mode: modePreview, pinned: true},
		{path: "Beta.md", mode: modeSplit},
	}, active: 2}}
	assertSessionParityTree(t, a.panes, want, a.activePane, want.leaf, "legacy")
	if a.sidebarWidth != 40 {
		t.Errorf("legacy sidebar width = %d, want 40", a.sidebarWidth)
	}
}

func TestSessionKeepsActiveNoteWhenEarlierSavedFileIsMissing(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md", "Beta.md", "Gamma.md")
	writeSessionParityFixture(t, root, `{
		"tabs":[
			{"path":"Deleted.md","mode":"edit"},
			{"path":"Alpha.md","mode":"preview"},
			{"path":"Beta.md","mode":"split"},
			{"path":"Gamma.md","mode":"edit"}
		],
		"active":2,"sidebarOpen":true
	}`)
	a := newSessionParityApp(t, root)
	a.restoreSession()

	if got := len(a.activePane.tabs); got != 3 {
		t.Fatalf("restored %d tabs, want the three existing files", got)
	}
	if got := a.activeTab().path; got != "Beta.md" {
		t.Errorf("active note = %q, want Beta.md after skipping an earlier deleted file", got)
	}
	if _, ok := a.buffers["Deleted.md"]; ok {
		t.Error("missing file left a buffer behind")
	}
}

func TestSessionWithOnlyMissingFilesOpensHomeOnStartup(t *testing.T) {
	root := newSessionParityVault(t)
	writeSessionParityFixture(t, root, `{
		"tabs":[{"path":"Deleted.md","mode":"edit"}],
		"active":0,"sidebarOpen":true
	}`)
	a := newSessionParityApp(t, root)
	loaded := a.idx
	a.idx = nil
	a.Update(indexLoadedMsg{idx: loaded})

	if len(a.panes.leaves()) != 1 || a.activePane == nil {
		t.Fatal("startup with missing files did not leave a usable pane")
	}
	if tab := a.activeTab(); tab == nil || tab.path != tabHome || tab.view == nil {
		t.Fatalf("startup with missing files should open Home, got %#v", tab)
	}
}

func TestSessionRestoresEachOpenNotesCursorAndScroll(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md", "Beta.md")
	for _, path := range []string{"Alpha.md", "Beta.md"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(strings.Repeat("A long line of note content.\n", 80)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newSessionParityApp(t, root)
	a.openNote("Alpha.md", true)
	a.activeBuffer().ed.SetCursor(vim.Pos{Line: 25, Col: 9})
	a.activeBuffer().ed.SetScrollTop(20)
	a.activePane = a.panes.split(a.activePane, true)
	a.openNote("Beta.md", true)
	a.activeBuffer().ed.SetCursor(vim.Pos{Line: 54, Col: 15})
	a.activeBuffer().ed.SetScrollTop(48)
	a.saveSession()

	restored := newSessionParityApp(t, root)
	restored.restoreSession()

	for path, original := range a.buffers {
		buf := restored.buffers[path]
		if buf == nil {
			t.Errorf("%s: saved note was not reopened", path)
			continue
		}
		if got, want := buf.ed.Cursor(), original.ed.Cursor(); got != want {
			t.Errorf("%s: cursor = %+v, want %+v", path, got, want)
		}
		if got, want := buf.ed.ScrollTop(), original.ed.ScrollTop(); got != want {
			t.Errorf("%s: scroll top = %d, want %d", path, got, want)
		}
		if buf.dirty() {
			t.Errorf("%s: restoring a position marked the note modified", path)
		}
	}
}

func TestSessionClampsSavedPositionAfterNoteIsShortened(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte(strings.Repeat("A long line of note content.\n", 80)), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newSessionParityApp(t, root)
	a.openNote("Alpha.md", true)
	a.activeBuffer().ed.SetCursor(vim.Pos{Line: 50, Col: 20})
	a.activeBuffer().ed.SetScrollTop(45)
	a.saveSession()
	if err := os.WriteFile(filepath.Join(root, "Alpha.md"), []byte("Short"), 0o644); err != nil {
		t.Fatal(err)
	}

	restored := newSessionParityApp(t, root)
	restored.restoreSession()
	buf := restored.activeBuffer()
	if buf == nil {
		t.Fatal("shortened note was not reopened")
	}
	if got := buf.ed.Cursor(); got.Line != 0 || got.Col < 0 || got.Col > len("Short") {
		t.Errorf("cursor was not clamped to shortened note: %+v", got)
	}
	if got := buf.ed.ScrollTop(); got != 0 {
		t.Errorf("scroll top = %d, want 0 for a one-line note", got)
	}
}

func TestSessionStartupRecoversFromCorruptState(t *testing.T) {
	for _, state := range []struct {
		name string
		json string
	}{
		{name: "truncated JSON", json: `{"tabs":[`},
		{name: "missing split child", json: `{"layout":{"first":{"tabs":[{"path":"Alpha.md"}]}},"activePane":-100}`},
		{name: "invalid selections and mode", json: `{"layout":{"tabs":[{"path":"Alpha.md","mode":"unknown"}],"active":-100},"activePane":10000}`},
		{name: "unknown virtual tab", json: `{"layout":{"tabs":[{"path":"zen://unknown"}]},"activePane":10000}`},
	} {
		t.Run(state.name, func(t *testing.T) {
			root := newSessionParityVault(t, "Alpha.md")
			writeSessionParityFixture(t, root, state.json)
			a := newSessionParityApp(t, root)
			loaded := a.idx
			a.idx = nil
			a.Update(indexLoadedMsg{idx: loaded})
			assertSessionParityUsable(t, a)
		})
	}
}

func TestSessionBoundsExcessivelyDeepLayout(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	leaf := `{"tabs":[{"path":"Alpha.md","mode":"edit"}]}`
	layout := leaf
	for range 128 {
		layout = `{"vertical":true,"ratio":2,"first":` + leaf + `,"second":` + layout + `}`
	}
	writeSessionParityFixture(t, root, `{"layout":`+layout+`,"activePane":999999}`)
	a := newSessionParityApp(t, root)
	loaded := a.idx
	a.idx = nil
	a.Update(indexLoadedMsg{idx: loaded})

	assertSessionParityUsable(t, a)
	if got := len(a.panes.leaves()); got >= 129 {
		t.Errorf("all %d panes from an excessively deep layout were restored without a bound", got)
	}
	var checkSplits func(*paneNode)
	checkSplits = func(node *paneNode) {
		if node.leaf != nil {
			return
		}
		if node.ratio <= 0 || node.ratio >= 1 {
			t.Errorf("invalid split ratio survived restoration: %v", node.ratio)
		}
		checkSplits(node.first)
		checkSplits(node.second)
	}
	checkSplits(a.panes)
}

func assertSessionParityUsable(t *testing.T, a *App) {
	t.Helper()
	if a.activePane == nil {
		t.Fatal("restore left no active pane")
	}
	activeFound := false
	for _, p := range a.panes.leaves() {
		if p == a.activePane {
			activeFound = true
		}
	}
	if !activeFound {
		t.Fatal("active pane does not belong to the restored layout")
	}
	if tab := a.activeTab(); tab == nil {
		t.Fatal("startup did not provide a usable tab")
	} else if !isVirtualPath(tab.path) && tab.mode != modeEdit && tab.mode != modeSplit && tab.mode != modePreview {
		t.Errorf("startup retained an invalid note mode: %q", tab.mode)
	}
}

func TestSessionRestoresPreviewAndTaskViewStatePerPane(t *testing.T) {
	root := newSessionParityVault(t, "Alpha.md")
	a := newSessionParityApp(t, root)
	a.openNote("Alpha.md", true)
	a.activeTab().mode = modePreview
	a.activeTab().preview = &previewState{scroll: 17, cursor: 23}
	a.openTasks()
	board := a.activeTab().view.(*tasksView)
	board.setMode("kanban")
	board.filter = "tag:project"
	board.groupBy = "priority"
	board.day = time.Date(2026, time.October, 3, 0, 0, 0, 0, time.Local)
	right := a.panes.split(a.activePane, true)
	// A fresh Tasks tab has independent view state from the first pane.
	right.tabs = nil
	a.activePane = right
	a.openTasks()
	calendar := a.activeTab().view.(*tasksView)
	calendar.setMode("calendar")
	calendar.filter = "status:todo"
	calendar.groupBy = "status"
	calendar.day = time.Date(2027, time.February, 15, 0, 0, 0, 0, time.Local)
	a.saveSession()

	restored := newSessionParityApp(t, root)
	restored.restoreSession()
	leaves := restored.panes.leaves()
	if len(leaves) != 2 {
		t.Fatalf("restored %d panes, want 2", len(leaves))
	}
	previewIndex := leaves[0].findTab("Alpha.md")
	if previewIndex < 0 {
		t.Fatal("preview tab was not restored")
	}
	pv := leaves[0].tabs[previewIndex].preview
	if pv == nil || pv.scroll != 17 || pv.cursor != 23 {
		t.Errorf("preview reading position = %+v, want scroll 17 and cursor 23", pv)
	}
	for i, expected := range []*tasksView{board, calendar} {
		idx := leaves[i].findTab(tabTasks)
		if idx < 0 {
			t.Errorf("pane %d: Tasks tab was not restored", i)
			continue
		}
		got, ok := leaves[i].tabs[idx].view.(*tasksView)
		if !ok {
			t.Errorf("pane %d: restored Tasks tab has no Tasks view", i)
			continue
		}
		if got.mode != expected.mode || got.filter != expected.filter || got.groupBy != expected.groupBy || got.day.Format("2006-01-02") != expected.day.Format("2006-01-02") {
			t.Errorf("pane %d: restored Tasks state = (%q, %q, %q, %s), want (%q, %q, %q, %s)", i, got.mode, got.filter, got.groupBy, got.day.Format("2006-01-02"), expected.mode, expected.filter, expected.groupBy, expected.day.Format("2006-01-02"))
		}
	}
}
