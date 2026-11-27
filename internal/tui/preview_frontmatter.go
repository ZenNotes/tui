package tui

import (
	"fmt"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// How the reading view shows a note's frontmatter. [terminal]
// preview_frontmatter picks the resting state, and a tab flips its own with
// Enter on the summary row or Space z f. A note that keeps fifteen fields
// above its first heading otherwise opens on a screen of YAML.
const (
	frontRaw       = "raw"
	frontCollapsed = "collapsed"
	frontExpanded  = "expanded"
	frontHidden    = "hidden"
)

// frontmatterDisplay is what a tab draws: the setting, or its opposite once
// the tab has flipped it. A flipped hidden block opens under a summary row,
// so there is a row to close it from again.
func frontmatterDisplay(pref string, flipped bool) string {
	switch pref {
	case frontCollapsed, frontHidden:
		if flipped {
			return frontExpanded
		}
		return pref
	}
	if flipped {
		return frontCollapsed
	}
	return frontRaw
}

// frontmatterEnd is the index of the line that closes a frontmatter block
// opened on the first line, or -1 when the note has none.
func frontmatterEnd(lines []string) int {
	if len(lines) == 0 || lines[0] != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" || lines[i] == "..." {
			return i
		}
	}
	return -1
}

// frontmatterRows draws the block that ends on line end, in the display the
// current render asked for. The summary row answers for the opening line, so
// a preview that follows the editor has a row for a cursor inside the block.
func (a *App) frontmatterRows(lines []string, end, width int) []previewLine {
	th := a.theme
	out := []previewLine{}
	display := a.previewFront
	if display == frontHidden {
		return out
	}
	if display == frontCollapsed || display == frontExpanded {
		n := len(vault.ParseFrontmatterFields(strings.Join(lines[1:end], "\n")))
		label := fmt.Sprintf("%d fields", n)
		if n == 1 {
			label = "1 field"
		}
		glyph := "▸ "
		if display == frontExpanded {
			glyph = "▾ "
		}
		out = append(out, previewLine{text: th.Frontmatter.Render(truncateCells(glyph+label, width)), srcLine: 0, front: true})
		if display == frontCollapsed {
			return out
		}
	}
	for i := 1; i < end; i++ {
		out = append(out, previewLine{text: th.Frontmatter.Render(truncateCells(lines[i], width)), srcLine: i})
	}
	return append(out, previewLine{text: th.Muted.Render(strings.Repeat("╌", min(width, 24))), srcLine: end})
}

// togglePreviewFrontmatter opens or closes the frontmatter of the active
// note's preview. The row under the cursor keeps its place on the screen.
func (a *App) togglePreviewFrontmatter() {
	t := a.activeTab()
	buf := a.activeBuffer()
	if t == nil || t.view != nil || buf == nil {
		a.notify("Open a note first")
		return
	}
	if _, _, ok := vault.Frontmatter(buf.ed.Text()); !ok {
		a.notify("This note has no frontmatter")
		return
	}
	if t.preview == nil {
		t.preview = &previewState{}
	}
	pv := t.preview
	if pv.cache != nil && pv.cursor < len(pv.cache.lines) {
		pv.restoreLine = pv.cache.lines[pv.cursor].srcLine
		pv.restoreOffset = pv.cursor - pv.scroll
		pv.restorePending = true
	}
	pv.frontFlipped = !pv.frontFlipped
	switch frontmatterDisplay(a.prefs.PreviewFrontmatter, pv.frontFlipped) {
	case frontHidden:
		a.notify("Frontmatter hidden")
	case frontCollapsed:
		a.notify("Frontmatter collapsed")
	default:
		a.notify("Frontmatter shown")
	}
}
