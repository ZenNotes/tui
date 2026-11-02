package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/keymaps"
	"github.com/ZenNotes/tui/internal/vim"
)

type probeView struct{ keys []vim.Key }

func (v *probeView) title() string                                { return "probe" }
func (v *probeView) hint(a *App) string                           { return "" }
func (v *probeView) refresh(a *App)                               {}
func (v *probeView) render(a *App, w, h int, focused bool) string { return "" }
func (v *probeView) handleKey(a *App, k vim.Key) bool {
	v.keys = append(v.keys, k)
	return true
}

func tabSequenceApp() (*App, *probeView) {
	a := &App{keymap: keymaps.NewResolver(nil), theme: buildTheme(true), prefs: prefsView{}}
	a.prefs.VimMode = true
	pv := &probeView{}
	p := &pane{tabs: []*tab{{path: "virtual:a", view: pv}, {path: "virtual:b", view: &probeView{}}}}
	a.panes = &paneNode{leaf: p}
	a.activePane = p
	a.focus = focusPane
	return a, pv
}

func TestBufferSequencesCycleTabsFromAView(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]b")
	if a.activePane.active != 1 {
		t.Fatalf("]b from a view: active = %d", a.activePane.active)
	}
	typeKeys(a, "[b")
	if a.activePane.active != 0 {
		t.Fatalf("[b from a view: active = %d", a.activePane.active)
	}
	if len(pv.keys) != 0 {
		t.Fatalf("a completed sequence leaked keys to the view: %v", pv.keys)
	}
}

func TestBracketNotFollowedByBReachesTheView(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]x")
	if a.activePane.active != 0 || len(pv.keys) != 2 {
		t.Fatalf("active=%d keys=%v", a.activePane.active, pv.keys)
	}
}

type closeOverlay struct{}

func (closeOverlay) handleKey(a *App, k vim.Key)    { a.overlay = nil }
func (closeOverlay) render(a *App, w, h int) string { return "" }

func requireDropped(t *testing.T, a *App, pv *probeView) {
	t.Helper()
	if a.activePane.active != 0 {
		t.Fatalf("a stale held key switched tabs: active = %d", a.activePane.active)
	}
	for _, k := range pv.keys {
		if k.IsRune(']') {
			t.Fatalf("a held key was replayed to the view: %v", pv.keys)
		}
	}
}

func TestHeldKeyIsDroppedWhenAnOverlayTakesTheInput(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.overlay = closeOverlay{}
	a.handleKey(vim.R('x'))
	typeKeys(a, "b")
	requireDropped(t, a, pv)
	typeKeys(a, "]b")
	if a.activePane.active != 1 {
		t.Fatalf("a fresh sequence after the drop: active = %d", a.activePane.active)
	}
}

func TestHeldKeyIsDroppedByALeaderSequence(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.handleKey(vim.R(' '))
	a.leader = nil
	typeKeys(a, "b")
	requireDropped(t, a, pv)
}

func TestHeldKeyIsDroppedByThePanePrefix(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.handleKey(vim.Ctrl('w'))
	a.paneKeys = false
	typeKeys(a, "b")
	requireDropped(t, a, pv)
}

func TestHeldKeyIsDroppedByAMouseClick(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	typeKeys(a, "b")
	requireDropped(t, a, pv)
}

func TestHeldKeyIsDroppedWhenFocusMovesToAnEditor(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.openNote("note.md", true)
	note := a.activePane.active
	pv := &probeView{}
	a.activePane.tabs = append(a.activePane.tabs, &tab{path: "virtual:a", view: pv}, &tab{path: "virtual:b", view: &probeView{}})
	view := len(a.activePane.tabs) - 2
	a.activePane.active = view
	a.focus = focusPane
	typeKeys(a, "]")
	a.activePane.active = note
	a.handleKey(vim.KeyEsc)
	a.activePane.active = view
	typeKeys(a, "b")
	if a.activePane.active != view || len(pv.keys) != 1 || !pv.keys[0].IsRune('b') {
		t.Fatalf("active = %d keys = %v", a.activePane.active, pv.keys)
	}
}

func TestEscapeCancelsAHeldKeyWithoutReplay(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.handleKey(vim.KeyEsc)
	if len(pv.keys) != 0 {
		t.Fatalf("Esc replayed the held key: %v", pv.keys)
	}
	typeKeys(a, "b")
	requireDropped(t, a, pv)
}

func TestHeldKeyIsDroppedWhenVimModeTurnsOff(t *testing.T) {
	a, pv := tabSequenceApp()
	typeKeys(a, "]")
	a.prefs.VimMode = false
	a.handleKey(vim.R('x'))
	a.prefs.VimMode = true
	pv.keys = nil
	typeKeys(a, "b")
	requireDropped(t, a, pv)
}

func TestCalendarBracketsChangeTheMonthAtOnce(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	if err := a.openTasksMode("calendar"); err != nil {
		t.Fatal(err)
	}
	tv := a.activeTab().view.(*tasksView)
	month := tv.month
	typeKeys(a, "]")
	if want := month.AddDate(0, 1, 0); !tv.month.Equal(want) {
		t.Fatalf("Tasks calendar ]: month = %v, want %v", tv.month, want)
	}
	a.toggleSidePanel(&a.calendarOpen, focusCalendar)
	month = a.calendar.month
	typeKeys(a, "[")
	if want := month.AddDate(0, -1, 0); !a.calendar.month.Equal(want) {
		t.Fatalf("calendar panel [: month = %v, want %v", a.calendar.month, want)
	}
}

func TestKanbanGChangesTheGroupingAtOnce(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.openNote("note.md", true)
	if err := a.openTasksMode("kanban"); err != nil {
		t.Fatal(err)
	}
	board := a.activePane.active
	tv := a.activeTab().view.(*tasksView)
	before := tv.groupBy
	typeKeys(a, "g")
	if tv.groupBy == before {
		t.Fatalf("g on the board: still grouped by %q", tv.groupBy)
	}
	typeKeys(a, "]b")
	if a.activePane.active == board {
		t.Fatal("]b from the board did not switch tabs")
	}
}

func TestStatusBarShowsAHeldKey(t *testing.T) {
	a, _ := newFeatureTestApp(t)
	a.activePane.tabs = append(a.activePane.tabs, &tab{path: "virtual:a", view: &probeView{}})
	a.activePane.active = len(a.activePane.tabs) - 1
	a.focus = focusPane
	typeKeys(a, "]")
	if !strings.Contains(a.renderStatusBar(), "]") {
		t.Fatalf("the status bar does not show the held key: %q", a.renderStatusBar())
	}
}

func TestTabSequencesNeedVimMode(t *testing.T) {
	a, pv := tabSequenceApp()
	a.prefs.VimMode = false
	typeKeys(a, "]bgt")
	if a.activePane.active != 0 || len(pv.keys) != 4 {
		t.Fatalf("with Vim off: active = %d keys = %v", a.activePane.active, pv.keys)
	}
}
