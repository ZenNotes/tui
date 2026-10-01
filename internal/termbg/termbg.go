// Package termbg decides the terminal's background color for Lip Gloss
// before Bubble Tea can ask the terminal itself.
//
// Bubble Tea v1 queries the terminal in a package init (tea_init.go: "make
// sure Lip Gloss and Termenv query the terminal before any Bubble Tea
// Program runs") whenever stdout is a terminal. Termenv waits up to
// OSCTimeout, five seconds, for the answer. Terminals that never answer
// OSC 11 (the Linux console, some IDE and serial terminals, a few
// multiplexer setups) therefore stalled every zn command, `zn --version`
// included, for five seconds, and the belated answer could land in the
// next thing reading stdin, such as the `zn setup` prompt.
//
// Go initializes packages in import-path order among those whose imports
// are ready, and github.com/ZenNotes sorts before github.com/charmbracelet,
// so this package's init runs after Lip Gloss's and before Bubble Tea's.
// Presetting the answer here makes that init a no-op; the two places that
// want the real answer, the terminal app's auto theme and `zn read
// --pretty`, call Detect and ask the terminal deliberately.
package termbg

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() {
	lipgloss.SetHasDarkBackground(Guess(os.Getenv("COLORFGBG")))
}

// Guess is the answer without asking the terminal: what COLORFGBG says
// (rxvt, Konsole and others export it as "fg;bg"), else dark, the same
// fallback termenv uses when a query goes unanswered.
func Guess(colorFGBG string) bool {
	parts := strings.Split(strings.TrimSpace(colorFGBG), ";")
	if len(parts) < 2 {
		return true
	}
	bg, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || bg < 0 || bg > 15 {
		return true
	}
	_, _, lightness := termenv.ConvertToRGB(termenv.ANSIColor(bg)).Hsl()
	return lightness < 0.5
}

var (
	detectOnce sync.Once
	detected   bool
)

// Detect asks the terminal on stdout once and remembers the answer, also
// for Lip Gloss's default renderer. It must run before Bubble Tea owns the
// terminal; it is the one place zn pays for the query, and only when the
// caller needs the real background.
func Detect() bool {
	detectOnce.Do(func() {
		detected = lipgloss.NewRenderer(os.Stdout).HasDarkBackground()
		lipgloss.SetHasDarkBackground(detected)
	})
	return detected
}
