// Package themes ports the desktop app's color schemes to the terminal: the
// registry of built-in themes, the --z-* tokens each one defines, and the
// custom themes users drop under <config>/themes. It knows nothing about
// how the terminal UI paints; it only resolves a selection to a palette.
package themes

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// RGB is one opaque color.
type RGB struct{ R, G, B uint8 }

// Hex formats the color the way lipgloss takes it.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

var (
	hex3Re    = regexp.MustCompile(`^#?([0-9a-fA-F]{3})$`)
	hex6Re    = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)
	rgbFuncRe = regexp.MustCompile(`(?i)^rgba?\(([^)]+)\)$`)
	splitRe   = regexp.MustCompile(`[\s,/]+`)
)

// ParseColor reads `#rgb`, `#rrggbb`, `rgb(r, g, b)` or the bare `r g b`
// triplet the desktop's tokens are written in.
func ParseColor(input string) (RGB, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return RGB{}, false
	}
	if m := hex3Re.FindStringSubmatch(s); m != nil {
		h := m[1]
		return hexColor(string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]}))
	}
	if m := hex6Re.FindStringSubmatch(s); m != nil {
		return hexColor(m[1])
	}
	body := s
	if m := rgbFuncRe.FindStringSubmatch(s); m != nil {
		body = m[1]
	}
	parts := []string{}
	for _, p := range splitRe.Split(strings.TrimSpace(body), -1) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 3 {
		return RGB{}, false
	}
	var ch [3]uint8
	for i := range 3 {
		n, err := strconv.ParseFloat(parts[i], 64)
		if err != nil || math.IsNaN(n) || n < 0 || n > 255 {
			return RGB{}, false
		}
		ch[i] = uint8(math.Round(n))
	}
	return RGB{ch[0], ch[1], ch[2]}, true
}

func hexColor(h string) (RGB, bool) {
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGB{}, false
	}
	return RGB{uint8(n >> 16), uint8(n >> 8), uint8(n)}, true
}

// Mix blends linearly from a to b: t=0 is a, t=1 is b.
func Mix(a, b RGB, t float64) RGB {
	ch := func(x, y uint8) uint8 {
		v := math.Round(float64(x) + (float64(y)-float64(x))*t)
		return uint8(math.Max(0, math.Min(255, v)))
	}
	return RGB{ch(a.R, b.R), ch(a.G, b.G), ch(a.B, b.B)}
}

// Luminance is the WCAG relative luminance.
func Luminance(c RGB) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// Contrast is the WCAG contrast ratio of two colors, from 1 to 21.
func Contrast(a, b RGB) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// EnsureContrast nudges fg toward anchor until it reads against bg at the
// wanted ratio. Palettes tuned for a backlit app window keep their hue; only
// the colors a terminal would render too faint move, and only as far as
// needed.
func EnsureContrast(fg, bg, anchor RGB, ratio float64) RGB {
	if Contrast(fg, bg) >= ratio {
		return fg
	}
	for step := 1; step <= 20; step++ {
		c := Mix(fg, anchor, float64(step)/20)
		if Contrast(c, bg) >= ratio {
			return c
		}
	}
	return anchor
}
