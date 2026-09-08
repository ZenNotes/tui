package tui

import "github.com/charmbracelet/x/ansi"

// stripAnsi drops escape sequences from a styled string.
func stripAnsi(s string) string {
	return ansi.Strip(s)
}
