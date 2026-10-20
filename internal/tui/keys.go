package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/helix"
)

// keyFromTea normalizes a Bubble Tea key into the engine's key type.
func keyFromTea(msg tea.KeyMsg) helix.Key {
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return helix.Key{Rune: msg.Runes[0], Alt: msg.Alt}
		}
		return helix.Key{Rune: msg.Runes[0], Alt: msg.Alt}
	case tea.KeySpace:
		return helix.Key{Rune: ' ', Alt: msg.Alt}
	case tea.KeyF2:
		return helix.Special("f2")
	case tea.KeyEnter:
		return helix.Key{Name: "enter", Alt: msg.Alt}
	case tea.KeyEsc:
		return helix.Key{Name: "esc", Alt: msg.Alt}
	case tea.KeyTab:
		return helix.Key{Name: "tab", Alt: msg.Alt}
	case tea.KeyShiftTab:
		return helix.Key{Name: "tab", Shift: true, Alt: msg.Alt}
	case tea.KeyBackspace:
		return helix.Key{Name: "backspace", Alt: msg.Alt}
	case tea.KeyDelete:
		return helix.Key{Name: "delete", Alt: msg.Alt}
	case tea.KeyUp:
		return helix.Key{Name: "up", Alt: msg.Alt}
	case tea.KeyDown:
		return helix.Key{Name: "down", Alt: msg.Alt}
	case tea.KeyLeft:
		return helix.Key{Name: "left", Alt: msg.Alt}
	case tea.KeyRight:
		return helix.Key{Name: "right", Alt: msg.Alt}
	case tea.KeyHome:
		return helix.Key{Name: "home", Alt: msg.Alt}
	case tea.KeyEnd:
		return helix.Key{Name: "end", Alt: msg.Alt}
	case tea.KeyPgUp:
		return helix.Key{Name: "pgup", Alt: msg.Alt}
	case tea.KeyPgDown:
		return helix.Key{Name: "pgdn", Alt: msg.Alt}
	case tea.KeyInsert:
		return helix.Key{Name: "insert", Alt: msg.Alt}
	case tea.KeyShiftUp:
		return helix.Key{Name: "up", Shift: true}
	case tea.KeyShiftDown:
		return helix.Key{Name: "down", Shift: true}
	case tea.KeyShiftLeft:
		return helix.Key{Name: "left", Shift: true}
	case tea.KeyShiftRight:
		return helix.Key{Name: "right", Shift: true}
	case tea.KeyCtrlUp:
		return helix.Key{Name: "up", Ctrl: true}
	case tea.KeyCtrlDown:
		return helix.Key{Name: "down", Ctrl: true}
	case tea.KeyCtrlLeft:
		return helix.Key{Name: "left", Ctrl: true}
	case tea.KeyCtrlRight:
		return helix.Key{Name: "right", Ctrl: true}
	case tea.KeyCtrlHome:
		return helix.Key{Name: "home", Ctrl: true}
	case tea.KeyCtrlEnd:
		return helix.Key{Name: "end", Ctrl: true}
	}
	name := msg.String()
	if strings.HasPrefix(name, "alt+") {
		name = name[4:]
	}
	if strings.HasPrefix(name, "ctrl+") {
		rest := name[5:]
		if r := []rune(rest); len(r) == 1 {
			return helix.Key{Rune: r[0], Ctrl: true, Alt: msg.Alt}
		}
		switch rest {
		case "@":
			return helix.Key{Rune: '@', Ctrl: true}
		case "[":
			return helix.Key{Name: "esc"}
		case "]":
			return helix.Key{Rune: ']', Ctrl: true}
		case "\\":
			return helix.Key{Rune: '\\', Ctrl: true}
		case "^":
			return helix.Key{Rune: '^', Ctrl: true}
		case "_":
			return helix.Key{Rune: '_', Ctrl: true}
		case "?":
			return helix.Key{Name: "backspace"}
		}
	}
	return helix.Key{Name: name, Alt: msg.Alt}
}

// bindingKeys parses a catalog binding like `Alt+H`, `Ctrl+P`, `g t`,
// `Space`, `Alt+ArrowLeft` into a key sequence.
func bindingKeys(binding string) []helix.Key {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return nil
	}
	out := []helix.Key{}
	for _, chord := range strings.Fields(binding) {
		parts := strings.Split(chord, "+")
		k := helix.Key{}
		base := parts[len(parts)-1]
		for _, mod := range parts[:len(parts)-1] {
			switch strings.ToLower(mod) {
			case "ctrl", "mod":
				k.Ctrl = true
			case "alt", "option", "meta":
				k.Alt = true
			case "shift":
				k.Shift = true
			}
		}
		switch strings.ToLower(base) {
		case "space":
			k.Rune = ' '
		case "enter", "return":
			k.Name = "enter"
		case "esc", "escape":
			k.Name = "esc"
		case "tab":
			k.Name = "tab"
		case "arrowleft", "left":
			k.Name = "left"
		case "arrowright", "right":
			k.Name = "right"
		case "arrowup", "up":
			k.Name = "up"
		case "arrowdown", "down":
			k.Name = "down"
		case "backspace":
			k.Name = "backspace"
		case "delete":
			k.Name = "delete"
		default:
			r := []rune(base)
			if len(r) != 1 {
				return nil
			}
			if k.Ctrl || k.Alt {
				k.Rune = unicode.ToLower(r[0])
				if k.Shift && !k.Ctrl {
					k.Rune = unicode.ToUpper(r[0])
				}
				if k.Ctrl {
					k.Shift = false
				}
			} else {
				k.Rune = r[0]
				if k.Shift {
					k.Rune = unicode.ToUpper(r[0])
					k.Shift = false
				}
			}
		}
		out = append(out, k)
	}
	return out
}

// sameKey compares keys ignoring the shift flag on printable runes.
func sameKey(a, b helix.Key) bool {
	if a.Name != "" || b.Name != "" {
		return a.Name == b.Name && a.Ctrl == b.Ctrl && a.Alt == b.Alt && a.Shift == b.Shift
	}
	return a.Rune == b.Rune && a.Ctrl == b.Ctrl && a.Alt == b.Alt
}

// matchSequence reports whether typed keys complete or could still
// complete a binding.
func matchSequence(typed, binding []helix.Key) (complete bool, prefix bool) {
	if len(binding) == 0 || len(typed) > len(binding) {
		return false, false
	}
	for i, k := range typed {
		if !sameKey(k, binding[i]) {
			return false, false
		}
	}
	return len(typed) == len(binding), len(typed) < len(binding)
}

// keyLabel renders a key for hint overlays.
func keyLabel(k helix.Key) string {
	s := k.String()
	s = strings.TrimPrefix(strings.TrimSuffix(s, ">"), "<")
	switch s {
	case "Space":
		return "␣"
	}
	return s
}
