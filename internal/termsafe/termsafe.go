// Package termsafe keeps note text from acting on the terminal. A note's bytes
// are data: they may show as text, but never as control sequences.
package termsafe

import (
	"io"
	"strings"
	"unicode/utf8"
)

// kittyPlaceholder is the rune the Kitty protocol draws pictures with, which
// only zn itself may place.
const kittyPlaceholder = 0x10EEEE

// Visible makes untrusted text safe to lay out and draw: each control rune
// becomes a one-cell picture of itself, such as ␛, and an invalid byte U+FFFD.
// Newlines and tabs stay, and so does the line a CRLF ends.
func Visible(s string) string {
	if isVisible(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		next := i + size
		switch {
		case r == '\r' && next < len(s) && s[next] == '\n':
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20:
			b.WriteRune(0x2400 + r)
		case r == 0x7f:
			b.WriteRune(0x2421)
		case r >= 0x80 && r <= 0x9f, r == kittyPlaceholder:
			b.WriteRune(utf8.RuneError)
		default:
			b.WriteRune(r) // an invalid byte decodes to RuneError
		}
		i = next
	}
	return b.String()
}

// isVisible is Visible's quick path, for the usual note with nothing to do.
func isVisible(s string) bool {
	for i, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20, r >= 0x7f && r <= 0x9f, r == kittyPlaceholder:
			return false
		case r == utf8.RuneError && !strings.HasPrefix(s[i:], "�"):
			return false
		}
	}
	return true
}

// Line is Visible for text that must stay on one row, such as a file name: a
// newline or tab shows as a picture too.
func Line(s string) string {
	return strings.NewReplacer("\n", "␤", "\t", "␉", "\r", "␍").Replace(Visible(s))
}

// Clean is the last step before bytes reach a terminal: it keeps text, newlines,
// tabs and colours and drops every other control sequence, with the bidirectional
// overrides that make text read in a different order than it is stored.
func Clean(s string) string {
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
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case keepRune(r):
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// keepRune says a rune may be written: not a C0 or C1 control, apart from the
// newlines and tabs of a frame, and not a bidirectional override or isolate.
func keepRune(r rune) bool {
	switch {
	case r == '\n', r == '\t':
		return true
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return false
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return false
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
			case 0x07, 0x9c:
				return j + 1, false
			case '\n':
				return j, false
			case 0x1b:
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2, false
				}
				return j, false
			case 0xc2:
				if j+1 < len(s) && s[j+1] == 0x9c {
					return j + 2, false
				}
			}
		}
		return j, false
	case ' ':
		return j, false // padding after a stray escape stays padding
	}
	for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
		j++
	}
	if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
		return j + 1, false
	}
	return j, false
}

// Writer passes everything written to it through Clean. Each Write is cleaned on
// its own: a sequence split across two writes loses its escape in the first, so
// the rest arrives as text.
type Writer struct{ W io.Writer }

func (w Writer) Write(p []byte) (int, error) {
	if _, err := io.WriteString(w.W, Clean(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}
