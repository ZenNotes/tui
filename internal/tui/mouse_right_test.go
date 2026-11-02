package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A right-click measures from the pane's content rectangle, like a left-click,
// so a framed pane's border and padding do not shift it.
func TestRightClickMeasuresFromContentRect(t *testing.T) {
	a, tv, _ := calVaultApp(t, map[string]string{"work.md": calWorkNote})
	calToSub(t, a, tv, calMonth)
	cr := a.contentRect(a.activePane)
	lines := strings.Split(stripAnsi(tv.render(a, cr.w, cr.h, true)), "\n")
	r := tv.calc().layout.items
	var x, y int
	found := false
	for ly := r.y; ly < r.y+r.h && ly+1 < len(lines); ly++ {
		if i := strings.Index(lines[ly+1], "bravo job"); i >= 0 {
			x, y, found = lipgloss.Width(lines[ly+1][:i]), ly+1, true
			break
		}
	}
	if !found {
		t.Fatalf("bravo job is not listed in the item panel:\n%s", strings.Join(lines, "\n"))
	}
	a.handleMouse(tea.MouseMsg{X: cr.x + x, Y: cr.y + y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	m, ok := a.overlay.(*menu)
	if !ok {
		t.Fatalf("overlay = %T, want the task menu", a.overlay)
	}
	if !strings.Contains(m.title, "bravo job") {
		t.Fatalf("menu title = %q, want the bravo job task", m.title)
	}
}
