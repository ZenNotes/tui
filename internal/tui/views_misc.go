package tui

import (
	"strings"

	"github.com/ZenNotes/tui/internal/vim"
)

// --- Help ---

type helpView struct {
	filter string
	lines  []string
	scroll int
	rows   int
}

func newHelpView() *helpView { return &helpView{} }

func (v *helpView) title() string      { return "Help" }
func (v *helpView) hint(a *App) string { return a.keysHint(v.hintPairs(a)) }

func (v *helpView) refresh(a *App) {
	v.lines = helpLines(a, v.filter)
	if v.scroll > max(0, len(v.lines)-1) {
		v.scroll = max(0, len(v.lines)-1)
	}
}

func (v *helpView) render(a *App, w, h int, focused bool) string {
	th := a.theme
	if v.lines == nil {
		v.refresh(a)
	}
	lines := []string{}
	if v.filter != "" {
		lines = append(lines, padRight(" "+th.Muted.Render("filter: ")+th.Tag.Render(v.filter), w))
	}
	v.rows = h - len(lines)
	for i := v.scroll; i < len(v.lines) && len(lines) < h; i++ {
		line := v.lines[i]
		switch {
		case strings.HasPrefix(line, "## "):
			lines = append(lines, padRight(" "+sectionHeader(th, strings.TrimPrefix(line, "## "), "", w-2), w))
		case strings.HasPrefix(line, "# "):
			lines = append(lines, padRight(th.Title.Render(" "+strings.TrimPrefix(line, "# ")), w))
		case strings.Contains(line, "\t"):
			key, desc, _ := strings.Cut(line, "\t")
			lines = append(lines, padRight("   "+th.KeyHint.Render(padRight(key, 22))+th.Base.Render(truncateCells(desc, w-26)), w))
		default:
			lines = append(lines, padRight(" "+th.Dim.Render(truncateCells(line, w-2)), w))
		}
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (v *helpView) handleKey(a *App, k vim.Key) bool {
	n := len(v.lines)
	cur := listCursor{cursor: v.scroll, scroll: v.scroll}
	if a.listNav(&cur, k, max(1, n-v.rows+1), v.rows) {
		v.scroll = cur.cursor
		return true
	}
	if !a.listKeyAllowed(k) {
		return false
	}
	id, pending := a.resolveAction(k, "nav.filter", "nav.localEx")
	if pending {
		return true
	}
	switch id {
	case "nav.filter":
		a.promptFor("Filter the manual", v.filter, "", func(a *App, text string) {
			v.filter = strings.TrimSpace(text)
			v.scroll = 0
			v.refresh(a)
		})
	case "nav.localEx":
		a.openLocalEx()
	default:
		return false
	}
	return true
}
