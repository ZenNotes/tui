package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
)

var calStripSubs = []struct {
	name string
	sub  calSub
}{{"month", calMonth}, {"week", calWeek}, {"day", calDay}}

const calStripNote = "- [ ] aaa strip\n- [ ] bbb strip\n- [ ] ccc strip\n"

func calStripTask(id, content string) vault.Task {
	return vault.Task{ID: id, SourcePath: "s.md", Content: content}
}

func calDatedTask() vault.Task {
	return vault.Task{ID: "d#0", SourcePath: "d.md", Content: "dated job", Due: "2026-11-18"}
}

func requireFocus(t *testing.T, tv *tasksView, want calFocus, context string) {
	t.Helper()
	if got := tv.calc().focus; got != want {
		t.Errorf("%s: focus = %v, want %v", context, got, want)
	}
}

func requireSelected(t *testing.T, tv *tasksView, want, context string) {
	t.Helper()
	got := tv.selectedTask()
	if got == nil || got.Content != want {
		t.Errorf("%s: selected = %+v, want %q", context, got, want)
	}
}

// calReload runs the reload every task change triggers: the tasks are scanned
// again and the open views rebuilt from them.
func calReload(t *testing.T, a *App) {
	t.Helper()
	msg, ok := a.loadTasksCmd()().(tasksLoadedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("task reload failed: %+v", msg)
	}
	a.Update(msg)
}

// calGiveDue sets a due date through the prompt that `d` opens.
func calGiveDue(t *testing.T, a *App, tv *tasksView, day string) {
	t.Helper()
	calPress(a, tv, vim.R('d'))
	if _, ok := a.overlay.(*prompt); !ok {
		t.Fatalf("overlay after d = %T, want the due-date prompt", a.overlay)
	}
	for _, r := range day {
		a.overlay.handleKey(a, vim.R(r))
	}
	a.overlay.handleKey(a, vim.KeyEnter)
	calReload(t, a)
}

func calManyUndated(n int) []vault.Task {
	var out []vault.Task
	for i := 0; i < n; i++ {
		out = append(out, calStripTask(fmt.Sprintf("u#%d", i), fmt.Sprintf("loose item %02d", i)))
	}
	return out
}

func TestTabCyclesGridItemsStrip(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		for _, s := range calStripSubs {
			a, tv := calTestApp(t, []vault.Task{calDatedTask(), calStripTask("u#0", "loose end")})
			a.prefs.VimMode = vimOn
			calToSub(t, a, tv, s.sub)
			ctx := fmt.Sprintf("%s vim=%v", s.name, vimOn)
			calPress(a, tv, vim.KeyTab)
			requireFocus(t, tv, calFocusItems, ctx+" first Tab")
			calPress(a, tv, vim.KeyTab)
			requireFocus(t, tv, calFocusStrip, ctx+" second Tab")
			requireSelected(t, tv, "loose end", ctx+" second Tab")
			calPress(a, tv, vim.KeyTab)
			requireFocus(t, tv, calFocusGrid, ctx+" third Tab")
			requireDay(t, tv, "2026-11-18", ctx)
			if tv.calc().sub != s.sub {
				t.Errorf("%s: Tab changed the sub-mode to %v", ctx, tv.calc().sub)
			}
		}
	}
}

func TestTabSkipsEmptyStrip(t *testing.T) {
	for _, s := range calStripSubs {
		a, tv := calTestApp(t, []vault.Task{calDatedTask()})
		calToSub(t, a, tv, s.sub)
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusItems, s.name+" first Tab")
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusGrid, s.name+" Tab from the items skips the empty strip")
	}
}

func TestTabSkipsEmptyItemPanel(t *testing.T) {
	for _, s := range calStripSubs {
		a, tv := calTestApp(t, []vault.Task{calStripTask("u#0", "loose end")})
		calToSub(t, a, tv, s.sub)
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusStrip, s.name+" Tab on an empty day skips the item panel")
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusGrid, s.name+" Tab from the strip")
	}
}

func TestTabStaysOnGridWhenBothEmpty(t *testing.T) {
	for _, s := range calStripSubs {
		a, tv := calTestApp(t, nil)
		calToSub(t, a, tv, s.sub)
		calPress(a, tv, vim.KeyTab)
		requireFocus(t, tv, calFocusGrid, s.name+" Tab with nothing to focus")
		requireDay(t, tv, "2026-11-18", s.name)
	}
}

func TestStripJKMovesAndScrolls(t *testing.T) {
	a, tv := calTestApp(t, calManyUndated(3))
	a.prefs.VimMode = true
	calPress(a, tv, vim.KeyTab)
	requireSelected(t, tv, "loose item 00", "strip entered")
	calPress(a, tv, vim.R('j'), vim.R('j'), vim.R('k'))
	requireSelected(t, tv, "loose item 01", "j j k")
	calPress(a, tv, vim.R('k'), vim.R('k'))
	requireSelected(t, tv, "loose item 00", "k stops at the first")
	calPress(a, tv, vim.R('j'), vim.R('j'), vim.R('j'), vim.R('j'))
	requireSelected(t, tv, "loose item 02", "j stops at the last")

	for _, s := range calStripSubs {
		a, tv := calTestApp(t, calManyUndated(30))
		a.prefs.VimMode = true
		calToSub(t, a, tv, s.sub)
		calPress(a, tv, vim.KeyTab)
		if text := calScreenText(calScreen(a, tv, 140, 40)); strings.Contains(text, "loose item 29") {
			t.Fatalf("%s: the strip shows all 30 tasks, so it cannot scroll:\n%s", s.name, text)
		}
		for i := 0; i < 29; i++ {
			calPress(a, tv, vim.R('j'))
		}
		requireSelected(t, tv, "loose item 29", s.name+" after 29 j")
		if text := calScreenText(calScreen(a, tv, 140, 40)); !strings.Contains(text, "loose item 29") {
			t.Errorf("%s: the strip did not scroll to the selected task:\n%s", s.name, text)
		}
		for i := 0; i < 29; i++ {
			calPress(a, tv, vim.R('k'))
		}
		if text := calScreenText(calScreen(a, tv, 140, 40)); !strings.Contains(text, "loose item 00") {
			t.Errorf("%s: the strip did not scroll back to the first task:\n%s", s.name, text)
		}
	}
}

func TestStripArrowsWithoutVimMode(t *testing.T) {
	a, tv := calTestApp(t, calManyUndated(3))
	a.prefs.VimMode = false
	calPress(a, tv, vim.KeyTab)
	requireFocus(t, tv, calFocusStrip, "Tab without Vim mode")
	calPress(a, tv, vim.Special("down"), vim.Special("down"), vim.Special("up"))
	requireSelected(t, tv, "loose item 01", "Down Down Up")
}

func TestStripKeysActOnSelectedTask(t *testing.T) {
	stripApp := func(t *testing.T) (*App, *tasksView, string) {
		a, tv, root := calVaultApp(t, map[string]string{"work.md": calStripNote})
		calPress(a, tv, vim.KeyTab, vim.R('j'))
		requireFocus(t, tv, calFocusStrip, "setup")
		requireSelected(t, tv, "bbb strip", "setup")
		return a, tv, root
	}

	for _, k := range []vim.Key{vim.KeyEnter, vim.R('o')} {
		a, tv, _ := stripApp(t)
		calPress(a, tv, k)
		if tab := a.activeTab(); tab == nil || tab.path != "work.md" {
			t.Errorf("%s: active tab = %+v, want work.md", k, tab)
		}
	}

	a, tv, root := stripApp(t)
	calPress(a, tv, vim.R('x'))
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "- [x] bbb strip") || strings.Contains(got, "- [x] aaa") {
		t.Errorf("x did not toggle the selected strip task:\n%s", got)
	}

	a, tv, _ = stripApp(t)
	keys := bindingKeys(a.keymap.Binding("nav.contextMenu"))
	if len(keys) != 1 {
		t.Fatalf("nav.contextMenu binding = %v, want one key", keys)
	}
	calPress(a, tv, keys[0])
	if _, ok := a.overlay.(*menu); !ok {
		t.Errorf("overlay after the context-menu key = %T, want the task menu", a.overlay)
	}

	for _, r := range "pdesy" {
		a, tv, _ := stripApp(t)
		if !tv.handleKey(a, vim.R(r)) {
			t.Errorf("%c was not handled with strip focus", r)
		}
		switch r {
		case 'p':
			if _, ok := a.overlay.(*menu); !ok {
				t.Errorf("p: overlay = %T, want the priority menu", a.overlay)
			}
		case 'd', 'e', 's':
			if _, ok := a.overlay.(*prompt); !ok {
				t.Errorf("%c: overlay = %T, want a prompt", r, a.overlay)
			}
		}
		requireFocus(t, tv, calFocusStrip, string(r))
	}

	for _, r := range "wc/" {
		a, tv, root := stripApp(t)
		calPress(a, tv, vim.R(r))
		got := calReadFile(t, root, "work.md")
		if got == calStripNote {
			t.Errorf("%c left the note unchanged", r)
		}
		want := strings.Split(calStripNote, "\n")
		for i, l := range strings.Split(got, "\n") {
			if i != 1 && (i >= len(want) || l != want[i]) {
				t.Errorf("%c changed line %d (%q), which is not the selected task", r, i+1, l)
			}
		}
	}

	for _, r := range "><" {
		a, tv, root := stripApp(t)
		calPress(a, tv, vim.R(r))
		got := calReadFile(t, root, "work.md")
		if !strings.Contains(got, "bbb strip due:") || strings.Contains(got, "aaa strip due:") || strings.Contains(got, "ccc strip due:") {
			t.Errorf("%c did not give the selected strip task a due date:\n%s", r, got)
		}
	}
}

func TestStripSelectionMovesOnWhenTaskGetsDue(t *testing.T) {
	a, tv, root := calVaultApp(t, map[string]string{"work.md": calStripNote})
	calPress(a, tv, vim.KeyTab, vim.R('j'))
	requireSelected(t, tv, "bbb strip", "before")
	calGiveDue(t, a, tv, "2026-11-18")
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "bbb strip due:2026-11-18") {
		t.Fatalf("d did not set the due date:\n%s", got)
	}
	requireFocus(t, tv, calFocusStrip, "after the reload")
	requireSelected(t, tv, "ccc strip", "after the second of three leaves")
	if text := calScreenText(calScreen(a, tv, 140, 40)); !strings.Contains(text, "bbb strip") {
		t.Errorf("the task is not on its day after the reload:\n%s", text)
	}
}

func TestStripLastTaskLeavingSelectsNewLast(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calStripNote})
	calPress(a, tv, vim.KeyTab, vim.R('j'), vim.R('j'))
	requireSelected(t, tv, "ccc strip", "before")
	calGiveDue(t, a, tv, "2026-11-18")
	requireFocus(t, tv, calFocusStrip, "after the reload")
	requireSelected(t, tv, "bbb strip", "after the last of three leaves")
}

func TestStripEmptyingReturnsFocusToGrid(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": "- [ ] only strip\n"})
	calPress(a, tv, vim.KeyTab)
	requireFocus(t, tv, calFocusStrip, "before")
	requireSelected(t, tv, "only strip", "before")
	calGiveDue(t, a, tv, "2026-11-18")
	requireFocus(t, tv, calFocusGrid, "after the only strip task leaves")
	if got := tv.selectedTask(); got != nil {
		t.Errorf("selected = %+v on the grid, want none", got)
	}
	requireDay(t, tv, "2026-11-18", "after the reload")
	if tv.calc().sub != calMonth {
		t.Errorf("sub-mode = %v, want it unchanged", tv.calc().sub)
	}
}

func TestStripEscReturnsToGrid(t *testing.T) {
	for _, vimOn := range []bool{true, false} {
		for _, s := range calStripSubs {
			a, tv := calTestApp(t, calManyUndated(2))
			a.prefs.VimMode = vimOn
			calToSub(t, a, tv, s.sub)
			calPress(a, tv, vim.KeyTab)
			requireFocus(t, tv, calFocusStrip, "setup")
			calPress(a, tv, vim.KeyEsc)
			ctx := fmt.Sprintf("%s vim=%v", s.name, vimOn)
			requireFocus(t, tv, calFocusGrid, ctx+" after Esc")
			if tv.calc().sub != s.sub {
				t.Errorf("%s: Esc changed the sub-mode to %v", ctx, tv.calc().sub)
			}
			requireDay(t, tv, "2026-11-18", ctx)
		}
	}
}

func TestStripColonOpensExLine(t *testing.T) {
	a, tv := calTestApp(t, calManyUndated(2))
	a.prefs.VimMode = true
	calPress(a, tv, vim.KeyTab)
	requireFocus(t, tv, calFocusStrip, "setup")
	calPress(a, tv, vim.R(':'))
	if _, ok := a.overlay.(*exPrompt); !ok {
		t.Errorf(": on a strip task opened %T, want the ex line", a.overlay)
	}
}
