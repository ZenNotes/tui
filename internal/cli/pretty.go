package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// renderPretty formats a note through Glamour for the terminal: the
// requested style, or dark/light from the terminal background, or the
// plain `notty` style when stdout is a pipe.
func renderPretty(body, style string) (string, error) {
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	width := 100
	if isTTY {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			width = min(w, 120)
		}
	}
	opts := []glamour.TermRendererOption{glamour.WithWordWrap(width), glamour.WithPreservedNewLines()}
	style = strings.TrimSpace(style)
	switch {
	case !isTTY && (style == "" || style == "auto"):
		opts = append(opts, glamour.WithStandardStyle(styles.NoTTYStyle))
	case style == "" || style == "auto":
		if lipgloss.HasDarkBackground() {
			opts = append(opts, glamour.WithStandardStyle(styles.DarkStyle))
		} else {
			opts = append(opts, glamour.WithStandardStyle(styles.LightStyle))
		}
	default:
		if _, ok := styles.DefaultStyles[strings.ToLower(style)]; ok {
			opts = append(opts, glamour.WithStandardStyle(strings.ToLower(style)))
		} else {
			if _, err := os.Stat(style); err != nil {
				return "", fmt.Errorf("unknown style %q: use a Glamour style name (dark, light, dracula, tokyo-night, pink, ascii, notty) or a JSON file", style)
			}
			opts = append(opts, glamour.WithStylesFromJSONFile(style))
		}
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return "", err
	}
	return r.Render(body)
}
