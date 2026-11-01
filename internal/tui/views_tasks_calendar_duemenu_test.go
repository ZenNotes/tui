package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ZenNotes/tui/internal/vim"
)

const calLooseNote = "- [ ] loose end\n"

// calDueMenu focuses the item panel on 2026-11-18 (today fixed to 2026-10-29)
// and presses T.
func calDueMenu(t *testing.T, files map[string]string, vimOn bool) (*App, *tasksView, string, *menu) {
	t.Helper()
	calFixToday(t, calDate(2026, time.October, 29))
	a, tv, root := calVaultApp(t, files)
	a.prefs.VimMode = vimOn
	calPress(a, tv, vim.KeyEnter)
	calPress(a, tv, vim.R('T'))
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay after T on an item = %T, want the due-date menu", a.overlay)
	}
	return a, tv, root, m
}

func calMenuIndex(t *testing.T, m *menu, word string) int {
	t.Helper()
	for i, it := range m.items {
		if strings.Contains(strings.ToLower(it.label), word) {
			return i
		}
	}
	t.Fatalf("menu has no item containing %q: %+v", word, m.items)
	return -1
}

// calPick moves the cursor to the item and presses Enter.
func calPick(t *testing.T, a *App, m *menu, word string) {
	t.Helper()
	want := calMenuIndex(t, m, word)
	for i := 0; i < len(m.items) && m.cursor != want; i++ {
		a.overlay.handleKey(a, vim.KeyDown)
	}
	if m.cursor != want {
		t.Fatalf("cursor stuck at %d, want %d", m.cursor, want)
	}
	a.overlay.handleKey(a, vim.KeyEnter)
}

func TestDueMenuOpensFromItemPanel(t *testing.T) { // AC88
	_, tv, _, _ := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	requireDay(t, tv, "2026-11-18", "after T")
	if tv.calc().sub != calMonth {
		t.Errorf("sub-mode = %v, want month", tv.calc().sub)
	}
	requireFocus(t, tv, calFocusItems, "after T")
}

func TestDueMenuListsChoicesInOrder(t *testing.T) { // AC89
	_, _, _, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	if len(m.items) != 6 {
		t.Fatalf("menu has %d items, want 6: %+v", len(m.items), m.items)
	}
	for i, w := range []string{"today", "tomorrow", "next week", "2026-11-18", "clear", "other"} {
		if !strings.Contains(strings.ToLower(m.items[i].label), w) {
			t.Errorf("item %d = %q, want it to mention %q", i, m.items[i].label, w)
		}
	}
}

func TestDueMenuArrowsAndEnterRunChoice(t *testing.T) { // AC90
	a, _, root, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	a.overlay.handleKey(a, vim.KeyDown)
	if m.cursor != 1 {
		t.Fatalf("cursor after Down = %d, want 1", m.cursor)
	}
	a.overlay.handleKey(a, vim.KeyEnter)
	calReload(t, a)
	if a.overlay != nil {
		t.Errorf("overlay after Enter = %T, want none", a.overlay)
	}
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "alpha job due:2026-10-30") {
		t.Errorf("Down then Enter did not pick tomorrow:\n%s", got)
	}
}

func TestDueMenuLettersRunNothing(t *testing.T) { // AC91
	a, _, root, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	for _, it := range m.items {
		if it.key != "" {
			t.Errorf("item %q has shortcut key %q, want none", it.label, it.key)
		}
	}
	for _, r := range "tTnwscodxa" {
		a.overlay.handleKey(a, vim.R(r))
	}
	if got := calReadFile(t, root, "work.md"); got != calWorkNote {
		t.Errorf("letters changed the note:\n%s", got)
	}
}

func TestDueMenuRelativeChoicesSetDue(t *testing.T) { // AC92
	for _, c := range []struct{ word, due string }{
		{"today", "2026-10-29"}, {"tomorrow", "2026-10-30"}, {"next week", "2026-11-05"},
	} {
		a, _, root, m := calDueMenu(t, map[string]string{"plan.md": calPlanNote}, true)
		calPick(t, a, m, c.word)
		body := calReadFile(t, root, "plan.md")
		if !strings.Contains(body, "due: "+c.due) && !strings.Contains(body, "due:"+c.due) {
			t.Errorf("%s: due is not %s:\n%s", c.word, c.due, body)
		}
		if !strings.Contains(body, "scheduled: 2026-11-16") {
			t.Errorf("%s: scheduled changed:\n%s", c.word, body)
		}
	}
}

func TestDueMenuSelectedDaySetsDue(t *testing.T) { // AC93
	a, _, root, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	calPick(t, a, m, "2026-11-18")
	if got := calReadFile(t, root, "work.md"); !strings.Contains(got, "alpha job due:2026-11-18") {
		t.Errorf("due is not the selected day:\n%s", got)
	}
}

func TestDueMenuClearRemovesDue(t *testing.T) { // AC94
	a, _, root, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	calPick(t, a, m, "clear")
	got := calReadFile(t, root, "work.md")
	if strings.Contains(got, "alpha job due:") || !strings.Contains(got, "alpha job") {
		t.Errorf("alpha job should lose its due date and stay:\n%s", got)
	}
	if !strings.Contains(got, "bravo job due:2026-11-18") {
		t.Errorf("another task changed:\n%s", got)
	}
}

func TestDueMenuOtherOpensDuePrompt(t *testing.T) { // AC95
	a, _, _, m := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
	calPick(t, a, m, "other")
	p, ok := a.overlay.(*prompt)
	if !ok {
		t.Fatalf("overlay = %T, want the due-date prompt", a.overlay)
	}
	if !strings.Contains(p.title, "Due") {
		t.Errorf("prompt title = %q, want the due-date prompt", p.title)
	}
}

func TestDueMenuDismissLeavesTaskUnchanged(t *testing.T) { // AC96
	for _, k := range []vim.Key{vim.KeyEsc, vim.R('q'), vim.Ctrl('c')} {
		a, _, root, _ := calDueMenu(t, map[string]string{"work.md": calWorkNote}, true)
		a.overlay.handleKey(a, k)
		if a.overlay != nil {
			t.Errorf("%v: overlay = %T, want none", k, a.overlay)
		}
		if got := calReadFile(t, root, "work.md"); got != calWorkNote {
			t.Errorf("%v: note changed:\n%s", k, got)
		}
	}
}

func TestDueMenuOpensFromStrip(t *testing.T) { // AC97
	calFixToday(t, calDate(2026, time.October, 29))
	a, tv, root := calVaultApp(t, map[string]string{"work.md": calWorkNote, "loose.md": calLooseNote})
	calPress(a, tv, vim.KeyTab, vim.KeyTab)
	requireFocus(t, tv, calFocusStrip, "second Tab")
	calPress(a, tv, vim.R('T'))
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay after T on the strip = %T, want the due-date menu", a.overlay)
	}
	calPick(t, a, m, "tomorrow")
	if got := calReadFile(t, root, "loose.md"); !strings.Contains(got, "loose end due:2026-10-30") {
		t.Errorf("strip task did not get the due date:\n%s", got)
	}
}

func TestDueMenuWithoutVimMode(t *testing.T) { // AC112
	calFixToday(t, calDate(2026, time.October, 29))
	a, tv, root := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	a.prefs.VimMode = false
	calPress(a, tv, vim.KeyEnter)
	requireFocus(t, tv, calFocusItems, "after Enter")
	calPress(a, tv, calRunes("Tcdw/e")...)
	if a.overlay != nil {
		t.Errorf("letters on a focused task opened %T with Vim off", a.overlay)
	}
	if got := calReadFile(t, root, "work.md"); got != calWorkNote {
		t.Errorf("letters on a focused task changed the note with Vim off:\n%s", got)
	}
	requireFocus(t, tv, calFocusItems, "after the letters")
}
