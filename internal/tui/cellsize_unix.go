//go:build !windows

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

// cellPixelSize asks the terminal for its cell size; 10x20 is the usual
// fallback when the terminal (or tmux) does not report pixels.
func cellPixelSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err == nil && ws.Col > 0 && ws.Row > 0 && ws.Xpixel > 0 && ws.Ypixel > 0 {
		return max(4, int(ws.Xpixel)/int(ws.Col)), max(8, int(ws.Ypixel)/int(ws.Row))
	}
	return 10, 20
}
