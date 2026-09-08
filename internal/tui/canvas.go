package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// canvas composes styled blocks at absolute positions, one row at a time.
type canvas struct {
	w, h int
	rows []string
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, rows: make([]string, h)}
	for i := range c.rows {
		c.rows[i] = strings.Repeat(" ", w)
	}
	return c
}

// paint writes a block whose lines are already exactly sized.
func (c *canvas) paint(x, y int, block string) {
	for i, line := range strings.Split(block, "\n") {
		row := y + i
		if row < 0 || row >= c.h {
			continue
		}
		c.rows[row] = splice(c.rows[row], x, line, c.w)
	}
}

// set writes a one-cell styled string.
func (c *canvas) set(x, y int, cell string) {
	if y < 0 || y >= c.h || x < 0 || x >= c.w {
		return
	}
	c.rows[y] = splice(c.rows[y], x, cell, c.w)
}

func (c *canvas) fillRow(y int, line string) {
	if y < 0 || y >= c.h {
		return
	}
	c.rows[y] = padRight(line, c.w)
}

// splice overlays text at column x of a styled row, keeping the row w wide.
func splice(row string, x int, text string, w int) string {
	if x >= w {
		return row
	}
	tw := ansi.StringWidth(text)
	left := padRight(truncateAnsi(row, x), x)
	right := ""
	if x+tw < w {
		right = ansi.Cut(row, x+tw, ansi.StringWidth(row))
	}
	return padRight(left+ansi.Truncate(text, w-x, "")+right, w)
}

func truncateAnsi(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "")
}

func (c *canvas) String() string {
	return strings.Join(c.rows, "\n")
}
