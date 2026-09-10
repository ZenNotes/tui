//go:build windows

package tui

// cellPixelSize has no console query on Windows; the usual cell size
// stands in, and the terminal scales pictures into that box.
func cellPixelSize() (int, int) { return 10, 20 }
