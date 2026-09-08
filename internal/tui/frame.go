package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Pane chrome: a tab strip on the first row, then a rounded frame whose
// top border carries a small lowercase title, the way a terminal
// multiplexer labels its panes. The focused pane's frame is a step
// brighter and its title bold; the frame never takes the accent color.

// frameInset is the chrome around a framed pane's content: the strip row
// and the top border above, the bottom border below, and a border plus one
// cell of padding on each side.
const (
	frameTop    = 2
	frameBottom = 1
	frameSide   = 2
)

// framed reports whether a pane gets its frame: never in zen mode, and
// not when the pane is too small to show anything inside one.
func (a *App) framed(p *pane) bool {
	if a.zen {
		return false
	}
	return p.rect.w >= 12 && p.rect.h >= 6
}

// contentRect is the screen rectangle a pane's content is drawn in, which
// is what every click, hint and menu anchor is measured against.
func (a *App) contentRect(p *pane) rect {
	r := p.rect
	strip := a.tabBarHeight()
	if a.framed(p) {
		top := strip + frameTop - 1
		return rect{r.x + frameSide, r.y + top, max(1, r.w-2*frameSide), max(1, r.h-top-frameBottom)}
	}
	return rect{r.x, r.y + strip, max(1, r.w), max(1, r.h-strip)}
}

// frameBox draws body inside a rounded border of exactly w x h cells with
// title set into the top edge. body must already be sized to the inside:
// w-4 wide (one cell of padding beside each border) and h-2 tall.
func frameBox(th Theme, title, body string, w, h int, focused bool) string {
	border := th.Border
	if focused {
		border = th.BorderFocus
	}
	inner := max(0, w-2)
	var top string
	if title != "" {
		label := " " + truncateCells(title, max(0, inner-4)) + " "
		top = border.Render("╭─") + frameTitleStyle(th, focused).Render(label) + border.Render(strings.Repeat("─", max(0, inner-1-cellWidth(label)))+"╮")
	} else {
		top = border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	lines := []string{top}
	bodyLines := strings.Split(body, "\n")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(bodyLines) {
			line = bodyLines[i]
		}
		lines = append(lines, border.Render("│")+" "+padRight(line, max(0, w-4))+" "+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(lines, "\n")
}

func frameTitleStyle(th Theme, focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Foreground(th.Fg).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(th.FgMuted)
}

// paneFrameTitle names what a pane is showing, in the frame's top edge.
func (a *App) paneFrameTitle(p *pane) string {
	t := p.activeTab()
	if t == nil {
		return "zennotes"
	}
	if t.view != nil {
		switch v := t.view.(type) {
		case *databaseView:
			if v.doc != nil && v.isBoard() {
				return "board"
			}
			return "database"
		case *tasksView:
			return "tasks"
		case *tagsView:
			return "tags"
		case *homeView:
			return "home"
		case *helpView:
			return "manual"
		case *settingsView:
			return "settings"
		case *noteListView:
			return strings.ToLower(v.title())
		}
		return strings.ToLower(t.view.title())
	}
	switch t.mode {
	case modePreview:
		return "preview"
	case modeSplit:
		return "editor · preview"
	}
	return "editor"
}
