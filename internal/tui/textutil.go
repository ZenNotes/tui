package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// runeWidth is the cell width of a rune; tabs count as tabSize cells here
// only through wrapLine, which expands them.
func runeWidth(r rune) int {
	if r == '\t' {
		return 4
	}
	w := runewidth.RuneWidth(r)
	if w == 0 && r >= 0x20 {
		return 1
	}
	return w
}

// segment is one display row of a wrapped line: rune indexes [start, end).
type segment struct {
	start, end int
}

// wrapLine splits a line into display rows no wider than width cells,
// breaking at spaces when one is near enough, otherwise mid-word.
func wrapLine(line []rune, width int) []segment {
	if width <= 0 {
		return []segment{{0, len(line)}}
	}
	if len(line) == 0 {
		return []segment{{0, 0}}
	}
	segs := []segment{}
	start := 0
	for start < len(line) {
		cells := 0
		i := start
		lastSpace := -1
		for i < len(line) {
			w := runeWidth(line[i])
			if cells+w > width {
				break
			}
			cells += w
			if line[i] == ' ' {
				lastSpace = i
			}
			i++
		}
		if i >= len(line) {
			segs = append(segs, segment{start, len(line)})
			break
		}
		end := i
		if lastSpace > start && i-lastSpace < width/2 {
			end = lastSpace + 1
		}
		if end == start {
			end = start + 1
		}
		segs = append(segs, segment{start, end})
		start = end
	}
	return segs
}

// cellWidth is the display width of a plain string.
func cellWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// padRight pads a styled string to width cells, truncating when longer.
func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}

// truncateCells shortens a plain string to width cells with an ellipsis.
func truncateCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if cellWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	out := []rune{}
	cells := 0
	for _, r := range s {
		w := runeWidth(r)
		if cells+w > width-1 {
			break
		}
		cells += w
		out = append(out, r)
	}
	return string(out) + "…"
}

// fitBlock forces text to exactly width x height cells.
func fitBlock(text string, width, height int) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, padRight(lines[i], width))
		} else {
			out = append(out, strings.Repeat(" ", width))
		}
	}
	return strings.Join(out, "\n")
}

// centerLine centers a styled line in width cells.
func centerLine(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return ansi.Truncate(s, width, "")
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-w-left)
}

// overlayOn draws a block centered over a base screen.
func overlayOn(base, block string, screenW, screenH int) string {
	out, _ := overlayPlace(base, block, screenW, screenH)
	return out
}

// overlayPlace draws a block centered over the base and reports where it
// landed, so mouse clicks can be mapped back to it.
func overlayPlace(base, block string, screenW, screenH int) (string, rect) {
	baseLines := strings.Split(base, "\n")
	blockLines := strings.Split(block, "\n")
	bw := 0
	for _, l := range blockLines {
		bw = max(bw, ansi.StringWidth(l))
	}
	bh := len(blockLines)
	top := max(0, (screenH-bh)/3)
	left := max(0, (screenW-bw)/2)
	return paintBlock(baseLines, blockLines, left, top, bw, bh)
}

// overlayPlaceAt draws a block with its top-left corner at a screen cell,
// nudged back on screen when it would overflow: a context menu opens next
// to the pointer or the selected row, above it when there is no room below.
func overlayPlaceAt(base, block string, x, y, screenW, screenH int) (string, rect) {
	baseLines := strings.Split(base, "\n")
	blockLines := strings.Split(block, "\n")
	bw := 0
	for _, l := range blockLines {
		bw = max(bw, ansi.StringWidth(l))
	}
	bh := len(blockLines)
	usableH := screenH - 2 // above the status bar and the help line
	left, top := x, y
	if left+bw > screenW {
		left = max(0, screenW-bw)
	}
	if top+bh > usableH {
		top = max(0, y-bh)
		if top+bh > usableH {
			top = max(0, usableH-bh)
		}
	}
	return paintBlock(baseLines, blockLines, left, top, bw, bh)
}

func paintBlock(baseLines, blockLines []string, left, top, bw, bh int) (string, rect) {
	for i, bl := range blockLines {
		row := top + i
		if row >= len(baseLines) {
			break
		}
		orig := baseLines[row]
		leftPart := ansi.Truncate(orig, left, "")
		leftPart = padRight(leftPart, left)
		rightStart := left + ansi.StringWidth(bl)
		rightPart := ""
		if rightStart < ansi.StringWidth(orig) {
			rightPart = ansi.Cut(orig, rightStart, ansi.StringWidth(orig))
		}
		baseLines[row] = leftPart + bl + rightPart
	}
	return strings.Join(baseLines, "\n"), rect{x: left, y: top, w: bw, h: bh}
}

var _ = lipgloss.Width

// joinHorizontal places blocks side by side, top-aligned.
func joinHorizontal(blocks ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

// sectionHeader draws a titled rule: "─ Title · detail ─────".
func sectionHeader(th Theme, title, detail string, width int) string {
	label := th.Bold.Foreground(th.Accent).Render(title)
	if detail != "" {
		label += th.Muted.Render(" · " + detail)
	}
	used := cellWidth(title) + 3
	if detail != "" {
		used += cellWidth(detail) + 3
	}
	return th.Border.Render("─ ") + label + " " + th.Border.Render(strings.Repeat("─", max(0, width-used-1)))
}

// withBackground paints a background under a styled line. Nested styles end
// with a full reset, which also drops any background set around them, so a
// bar built from several segments ends up with holes; this re-applies the
// background after every reset and default-background sequence while
// leaving segments that set their own background alone.
func withBackground(line string, bg lipgloss.Color) string {
	seq := backgroundSeq(bg)
	if seq == "" {
		return line
	}
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+seq)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+seq)
	line = strings.ReplaceAll(line, "\x1b[49m", seq)
	return seq + line + "\x1b[0m"
}

// backgroundSeq is the SGR prefix the active color profile uses for bg.
func backgroundSeq(bg lipgloss.Color) string {
	probe := lipgloss.NewStyle().Background(bg).Render("x")
	i := strings.Index(probe, "x")
	if i <= 0 {
		return ""
	}
	return probe[:i]
}
