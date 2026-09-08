// Package vim is a Vim editing engine over a plain text buffer: modes,
// counts, registers, motions, operators, text objects, visual selections,
// undo, dot-repeat, macros, marks, search and substitute, an ex command
// line, and heading folds. It knows nothing about terminals; the terminal
// UI feeds it keys and draws what it exposes.
package vim

import (
	"strings"
	"unicode"
)

// Key is one keystroke, normalized. Printable keys carry a Rune; special
// keys carry a Name (esc, enter, tab, backspace, delete, up, down, left,
// right, home, end, pgup, pgdn, insert).
type Key struct {
	Rune  rune
	Name  string
	Ctrl  bool
	Alt   bool
	Shift bool
}

// R is a plain rune key.
func R(r rune) Key { return Key{Rune: r} }

// Ctrl is a control chord like Ctrl+d.
func Ctrl(r rune) Key { return Key{Rune: unicode.ToLower(r), Ctrl: true} }

// Alt is an alt chord.
func Alt(r rune) Key { return Key{Rune: r, Alt: true} }

// Special is a named key.
func Special(name string) Key { return Key{Name: name} }

var (
	KeyEsc       = Special("esc")
	KeyEnter     = Special("enter")
	KeyTab       = Special("tab")
	KeyShiftTab  = Key{Name: "tab", Shift: true}
	KeyBackspace = Special("backspace")
	KeyDelete    = Special("delete")
	KeyUp        = Special("up")
	KeyDown      = Special("down")
	KeyLeft      = Special("left")
	KeyRight     = Special("right")
	KeyHome      = Special("home")
	KeyEnd       = Special("end")
	KeyPgUp      = Special("pgup")
	KeyPgDn      = Special("pgdn")
)

// IsRune is true for the plain printable rune r.
func (k Key) IsRune(r rune) bool {
	return k.Name == "" && !k.Ctrl && !k.Alt && k.Rune == r
}

// IsCtrl is true for the control chord Ctrl+r.
func (k Key) IsCtrl(r rune) bool {
	return k.Ctrl && !k.Alt && k.Rune == unicode.ToLower(r)
}

// Is is true for a special key by name (shift-insensitive unless the
// name is "shift+tab").
func (k Key) Is(name string) bool {
	if name == "shift+tab" {
		return k.Name == "tab" && k.Shift
	}
	return k.Name == name && !k.Ctrl && !k.Alt && !(name == "tab" && k.Shift)
}

// Printable is true for a plain rune the user could type into text.
func (k Key) Printable() bool {
	return k.Name == "" && !k.Ctrl && !k.Alt && k.Rune != 0
}

var specialNotation = map[string]string{
	"esc": "Esc", "enter": "CR", "tab": "Tab", "backspace": "BS", "delete": "Del",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home",
	"end": "End", "pgup": "PageUp", "pgdn": "PageDown", "insert": "Insert",
}

// String renders the key in Vim's notation: `a`, `<Esc>`, `<C-d>`, `<CR>`.
func (k Key) String() string {
	var b strings.Builder
	if k.Name != "" {
		b.WriteString("<")
		if k.Ctrl {
			b.WriteString("C-")
		}
		if k.Alt {
			b.WriteString("A-")
		}
		if k.Shift {
			b.WriteString("S-")
		}
		if n, ok := specialNotation[k.Name]; ok {
			b.WriteString(n)
		} else {
			b.WriteString(k.Name)
		}
		b.WriteString(">")
		return b.String()
	}
	if k.Ctrl || k.Alt {
		b.WriteString("<")
		if k.Ctrl {
			b.WriteString("C-")
		}
		if k.Alt {
			b.WriteString("A-")
		}
		if k.Rune == ' ' {
			b.WriteString("Space")
		} else {
			b.WriteRune(k.Rune)
		}
		b.WriteString(">")
		return b.String()
	}
	if k.Rune == ' ' {
		return "<Space>"
	}
	if k.Rune == '<' {
		return "<lt>"
	}
	return string(k.Rune)
}

var notationSpecial = map[string]Key{
	"esc": KeyEsc, "cr": KeyEnter, "enter": KeyEnter, "return": KeyEnter, "tab": KeyTab,
	"s-tab": KeyShiftTab, "bs": KeyBackspace, "backspace": KeyBackspace, "del": KeyDelete,
	"delete": KeyDelete, "up": KeyUp, "down": KeyDown, "left": KeyLeft, "right": KeyRight,
	"home": KeyHome, "end": KeyEnd, "pageup": KeyPgUp, "pagedown": KeyPgDn, "space": R(' '),
	"lt": R('<'), "bar": R('|'), "bslash": R('\\'), "nl": KeyEnter,
}

// ParseKeys turns Vim notation into keys: `dw<Esc>ihello<CR>`, `<C-d>`.
// An unterminated `<` is a literal less-than.
func ParseKeys(s string) []Key {
	out := []Key{}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '<' {
			end := -1
			for j := i + 1; j < len(runes) && j < i+24; j++ {
				if runes[j] == '>' {
					end = j
					break
				}
			}
			if end > i+1 {
				name := strings.ToLower(string(runes[i+1 : end]))
				if k, ok := parseNotation(name); ok {
					out = append(out, k)
					i = end
					continue
				}
			}
		}
		out = append(out, R(r))
	}
	return out
}

func parseNotation(name string) (Key, bool) {
	if k, ok := notationSpecial[name]; ok {
		return k, true
	}
	if strings.HasPrefix(name, "c-") && len(name) > 2 {
		rest := name[2:]
		if k, ok := notationSpecial[rest]; ok {
			k.Ctrl = true
			return k, true
		}
		if r := []rune(rest); len(r) == 1 {
			return Ctrl(r[0]), true
		}
	}
	if strings.HasPrefix(name, "a-") || strings.HasPrefix(name, "m-") {
		rest := name[2:]
		if k, ok := notationSpecial[rest]; ok {
			k.Alt = true
			return k, true
		}
		if r := []rune(rest); len(r) == 1 {
			return Alt(r[0]), true
		}
	}
	return Key{}, false
}

// KeysString renders a key sequence in notation.
func KeysString(keys []Key) string {
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k.String())
	}
	return b.String()
}
