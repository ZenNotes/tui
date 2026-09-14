package tui

import (
	"github.com/ZenNotes/tui/internal/vim"
	"strings"
)

// textReader keeps long threads and operation reports fully accessible.
type textReader struct {
	title, body         string
	scroll, rows, total int
}

func (r *textReader) handleKey(a *App, k vim.Key) {
	switch {
	case k.Is("esc"), k.IsCtrl('c'), k.IsRune('q'):
		a.overlay = nil
	case k.Is("up"), k.IsRune('k'):
		r.scroll--
	case k.Is("down"), k.IsRune('j'):
		r.scroll++
	case k.Is("pgup"), k.IsCtrl('u'):
		r.scroll -= max(1, r.rows/2)
	case k.Is("pgdn"), k.IsCtrl('d'), k.IsRune(' '):
		r.scroll += max(1, r.rows/2)
	case k.Is("home"), k.IsRune('g'):
		r.scroll = 0
	case k.Is("end"), k.IsRune('G'):
		r.scroll = max(0, r.total-r.rows)
	}
	r.scroll = max(0, min(max(0, r.total-r.rows), r.scroll))
}
func (r *textReader) render(a *App, w, h int) string {
	width := max(1, min(100, w-6))
	lines := wrapCommentText(r.body, width)
	r.total = len(lines)
	r.rows = max(1, h-8)
	r.scroll = max(0, min(max(0, r.total-r.rows), r.scroll))
	content := []string{a.theme.OverlayTitle.Render(truncateCells(r.title, width)), "↑/↓ scroll · Home/End · Esc close", ""}
	content = append(content, lines[r.scroll:min(len(lines), r.scroll+r.rows)]...)
	return a.theme.Overlay.Width(width).Render(strings.Join(content, "\n"))
}
