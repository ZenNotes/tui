package tui

import (
	"strings"
	"unicode/utf8"
)

// cleanScreen is the last step of every frame: it drops what is not text or a
// colour, so bytes in a note reach the terminal as text and never as control
// sequences. Colours stay because zn draws with them. zn's own cursor and
// picture sequences do not pass through here; they are written apart from the
// frame, in cursorout.go and images.go.
func cleanScreen(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			end, keep := escapeEnd(s, i)
			if keep {
				b.WriteString(s[i:end])
			}
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if isScreenText(r, size, s[i]) {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// isScreenText says a rune may be written: not a C0 or C1 control, apart from
// the newlines and tabs of the frame, and not a raw C1 byte.
func isScreenText(r rune, size int, first byte) bool {
	switch {
	case r == '\n', r == '\t':
		return true
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return false
	case r == utf8.RuneError && size == 1:
		return first < 0x80 || first > 0x9f
	}
	return true
}

// escapeEnd finds the end of the escape sequence at s[i], and says whether it
// is a colour change, the one kind kept.
func escapeEnd(s string, i int) (end int, keep bool) {
	j := i + 1
	if j >= len(s) {
		return j, false
	}
	switch s[j] {
	case '[':
		j++
		params := j
		for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
			j++
		}
		digits := strings.Trim(s[params:j], "0123456789;:") == ""
		mark := j
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j >= len(s) || s[j] < 0x40 || s[j] > 0x7e {
			return j, false
		}
		return j + 1, s[j] == 'm' && digits && j == mark
	case ']', 'P', 'X', '^', '_':
		// A string runs to its terminator. A frame's rows never continue one,
		// so a newline or a new escape also ends it.
		for j++; j < len(s); j++ {
			switch s[j] {
			case 0x07:
				return j + 1, false
			case '\n':
				return j, false
			case 0x1b:
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2, false
				}
				return j, false
			}
		}
		return j, false
	}
	for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
		j++
	}
	if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
		return j + 1, false
	}
	return j, false
}
