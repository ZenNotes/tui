package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/vim"
)

// readingApp opens a note in the reading view and draws it once, as a key press
// in the real app always follows a draw.
func readingApp(t *testing.T, body string) (*App, *previewState) {
	t.Helper()
	a, _ := linkTestApp(t, map[string]string{"tall.md": body})
	a.openNote("tall.md", true)
	a.setPaneMode(modePreview)
	a.View()
	tb := a.activeTab()
	if tb == nil || tb.preview == nil || tb.preview.cache == nil || tb.mode != modePreview {
		t.Fatal("the reading view did not draw")
	}
	return a, tb.preview
}

func tallNote(n int) string {
	var b strings.Builder
	b.WriteString("# Tall\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "\nLine number %d\n", i)
	}
	return b.String()
}

func pressAndDraw(a *App, keys ...vim.Key) {
	for _, k := range keys {
		a.handleKey(k)
		a.View()
	}
}

func TestReadingViewScrollsOnTheFirstPress(t *testing.T) {
	for name, down := range map[string]vim.Key{"j": {Rune: 'j'}, "down": vim.KeyDown} {
		t.Run(name, func(t *testing.T) {
			a, pv := readingApp(t, tallNote(100))
			pressAndDraw(a, down)
			if pv.scroll != 1 || pv.cursor != 1 {
				t.Errorf("after one press scroll %d cursor %d, want both 1", pv.scroll, pv.cursor)
			}
			if pressAndDraw(a, vim.KeyUp); pv.scroll != 0 || pv.cursor != 0 {
				t.Errorf("after going back scroll %d cursor %d, want both 0", pv.scroll, pv.cursor)
			}
		})
	}
}

func TestReadingViewHalfPageMovesTheViewAtOnce(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	half := pv.rows / 2
	pressAndDraw(a, vim.Ctrl('d'))
	if pv.scroll != half || pv.cursor != half {
		t.Errorf("Ctrl+D: scroll %d cursor %d, want both %d", pv.scroll, pv.cursor, half)
	}
	pressAndDraw(a, vim.Ctrl('u'))
	if pv.scroll != 0 || pv.cursor != 0 {
		t.Errorf("Ctrl+U: scroll %d cursor %d, want both 0", pv.scroll, pv.cursor)
	}
}

// The first and last screens can't scroll further, so the cursor carries on, and
// no row is out of its reach: a link or a task anywhere can still be used.
func TestReadingViewCursorReachesEveryRow(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	n := len(pv.cache.lines)
	last := n - pv.rows
	for row := 1; row < n; row++ {
		pressAndDraw(a, vim.Key{Rune: 'j'})
		if pv.cursor != row {
			t.Fatalf("press %d: cursor %d", row, pv.cursor)
		}
		if want := min(row, last); pv.scroll != want {
			t.Fatalf("press %d: scroll %d, want %d", row, pv.scroll, want)
		}
	}
	pressAndDraw(a, vim.Key{Rune: 'j'})
	if pv.cursor != n-1 || pv.scroll != last {
		t.Errorf("past the end: cursor %d scroll %d", pv.cursor, pv.scroll)
	}
	pressAndDraw(a, vim.Key{Rune: 'k'})
	if pv.cursor != n-2 || pv.scroll != last-1 {
		t.Errorf("up from the end: cursor %d scroll %d, the view should move", pv.cursor, pv.scroll)
	}
	pressAndDraw(a, vim.Key{Rune: 'g'}, vim.Key{Rune: 'g'})
	if pv.cursor != 0 || pv.scroll != 0 {
		t.Errorf("gg: cursor %d scroll %d", pv.cursor, pv.scroll)
	}
	pressAndDraw(a, vim.Key{Rune: 'G'})
	if pv.cursor != n-1 || pv.scroll != last {
		t.Errorf("G: cursor %d scroll %d", pv.cursor, pv.scroll)
	}
}

func TestReadingViewShortNoteMovesOnlyTheCursor(t *testing.T) {
	a, pv := readingApp(t, "# Short\n\none\n\ntwo\n")
	pressAndDraw(a, vim.Key{Rune: 'j'})
	if pv.cursor != 1 || pv.scroll != 0 {
		t.Errorf("cursor %d scroll %d, want 1 and 0", pv.cursor, pv.scroll)
	}
}

func TestReadingViewCursorStillFollowsLinksAndTogglesTasks(t *testing.T) {
	a, pv := readingApp(t, tallNote(50)+"\n- [ ] the task\n\n[[Other]]\n")
	n := len(pv.cache.lines)
	taskRow, linkRow := -1, -1
	for i, l := range pv.cache.lines {
		if l.task {
			taskRow = i
		}
		if len(l.links) > 0 {
			linkRow = i
		}
	}
	if taskRow < 0 || linkRow < 0 {
		t.Fatal("no task or link row")
	}
	for pv.cursor < taskRow {
		pressAndDraw(a, vim.KeyDown)
	}
	pressAndDraw(a, vim.Key{Rune: 'x'})
	if buf := a.activeBuffer(); !strings.Contains(buf.ed.Text(), "- [x] the task") {
		t.Errorf("x did not toggle the task under the cursor:\n%s", buf.ed.Text())
	}
	if pv.scroll < 1 || pv.scroll > max(0, n-pv.rows) {
		t.Errorf("scroll %d", pv.scroll)
	}
	for pv.cursor < linkRow {
		pressAndDraw(a, vim.KeyDown)
	}
	if got := pv.cache.lines[pv.cursor].links; len(got) != 1 || got[0].target != "Other" {
		t.Errorf("the cursor's row holds links %v", got)
	}
}

func TestReadingViewWheelMovesTheView(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	r := a.contentRect(a.activePane)
	a.Update(tea.MouseMsg{X: r.x + 2, Y: r.y + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if pv.scroll != 3 || pv.cursor != 3 {
		t.Errorf("wheel: scroll %d cursor %d, want both 3", pv.scroll, pv.cursor)
	}
}

// An anchor near the end used to leave the view past the last full screen, and the
// next key jumped it back.
func TestReadingViewJumpNearTheEndFillsThePane(t *testing.T) {
	a, pv := readingApp(t, tallNote(100))
	pv.jumpLine, pv.jumpPending = 200, true
	a.View()
	if want := len(pv.cache.lines) - pv.rows; pv.scroll != want {
		t.Errorf("scroll %d, want %d", pv.scroll, want)
	}
	before := pv.scroll
	pressAndDraw(a, vim.Key{Rune: 'k'})
	if pv.scroll != before-1 {
		t.Errorf("k moved the view from %d to %d", before, pv.scroll)
	}
}
