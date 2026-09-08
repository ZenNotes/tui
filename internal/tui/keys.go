package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZenNotes/tui/internal/vim"
)

// keyFromTea normalizes a Bubble Tea key into the engine's key type.
func keyFromTea(msg tea.KeyMsg) vim.Key {
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return vim.Key{Rune: msg.Runes[0], Alt: msg.Alt}
		}
		return vim.Key{Rune: msg.Runes[0], Alt: msg.Alt}
	case tea.KeySpace:
		return vim.Key{Rune: ' ', Alt: msg.Alt}
	case tea.KeyEnter:
		return vim.Key{Name: "enter", Alt: msg.Alt}
	case tea.KeyEsc:
		return vim.Key{Name: "esc", Alt: msg.Alt}
	case tea.KeyTab:
		return vim.Key{Name: "tab", Alt: msg.Alt}
	case tea.KeyShiftTab:
		return vim.Key{Name: "tab", Shift: true, Alt: msg.Alt}
	case tea.KeyBackspace:
		return vim.Key{Name: "backspace", Alt: msg.Alt}
	case tea.KeyDelete:
		return vim.Key{Name: "delete", Alt: msg.Alt}
	case tea.KeyUp:
		return vim.Key{Name: "up", Alt: msg.Alt}
	case tea.KeyDown:
		return vim.Key{Name: "down", Alt: msg.Alt}
	case tea.KeyLeft:
		return vim.Key{Name: "left", Alt: msg.Alt}
	case tea.KeyRight:
		return vim.Key{Name: "right", Alt: msg.Alt}
	case tea.KeyHome:
		return vim.Key{Name: "home", Alt: msg.Alt}
	case tea.KeyEnd:
		return vim.Key{Name: "end", Alt: msg.Alt}
	case tea.KeyPgUp:
		return vim.Key{Name: "pgup", Alt: msg.Alt}
	case tea.KeyPgDown:
		return vim.Key{Name: "pgdn", Alt: msg.Alt}
	case tea.KeyInsert:
		return vim.Key{Name: "insert", Alt: msg.Alt}
	case tea.KeyShiftUp:
		return vim.Key{Name: "up", Shift: true}
	case tea.KeyShiftDown:
		return vim.Key{Name: "down", Shift: true}
	case tea.KeyShiftLeft:
		return vim.Key{Name: "left", Shift: true}
	case tea.KeyShiftRight:
		return vim.Key{Name: "right", Shift: true}
	case tea.KeyCtrlUp:
		return vim.Key{Name: "up", Ctrl: true}
	case tea.KeyCtrlDown:
		return vim.Key{Name: "down", Ctrl: true}
	case tea.KeyCtrlLeft:
		return vim.Key{Name: "left", Ctrl: true}
	case tea.KeyCtrlRight:
		return vim.Key{Name: "right", Ctrl: true}
	case tea.KeyCtrlHome:
		return vim.Key{Name: "home", Ctrl: true}
	case tea.KeyCtrlEnd:
		return vim.Key{Name: "end", Ctrl: true}
	}
	name := msg.String()
	if strings.HasPrefix(name, "alt+") {
		name = name[4:]
	}
	if strings.HasPrefix(name, "ctrl+") {
		rest := name[5:]
		if r := []rune(rest); len(r) == 1 {
			return vim.Key{Rune: r[0], Ctrl: true, Alt: msg.Alt}
		}
		switch rest {
		case "@":
			return vim.Key{Rune: '@', Ctrl: true}
		case "[":
			return vim.Key{Name: "esc"}
		case "]":
			return vim.Key{Rune: ']', Ctrl: true}
		case "\\":
			return vim.Key{Rune: '\\', Ctrl: true}
		case "^":
			return vim.Key{Rune: '^', Ctrl: true}
		case "_":
			return vim.Key{Rune: '_', Ctrl: true}
		case "?":
			return vim.Key{Name: "backspace"}
		}
	}
	return vim.Key{Name: name, Alt: msg.Alt}
}

// bindingKeys parses a catalog binding like `Alt+H`, `Ctrl+P`, `g t`,
// `Space`, `Alt+ArrowLeft` into a key sequence.
func bindingKeys(binding string) []vim.Key {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return nil
	}
	out := []vim.Key{}
	for _, chord := range strings.Fields(binding) {
		parts := strings.Split(chord, "+")
		k := vim.Key{}
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
func sameKey(a, b vim.Key) bool {
	if a.Name != "" || b.Name != "" {
		return a.Name == b.Name && a.Ctrl == b.Ctrl && a.Alt == b.Alt && a.Shift == b.Shift
	}
	return a.Rune == b.Rune && a.Ctrl == b.Ctrl && a.Alt == b.Alt
}

// matchSequence reports whether typed keys complete or could still
// complete a binding.
func matchSequence(typed, binding []vim.Key) (complete bool, prefix bool) {
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
func keyLabel(k vim.Key) string {
	s := k.String()
	s = strings.TrimPrefix(strings.TrimSuffix(s, ">"), "<")
	switch s {
	case "Space":
		return "␣"
	}
	return s
}
