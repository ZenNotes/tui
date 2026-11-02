package helix

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Keymap is the user's key bindings on top of the engine's own keys, in the
// format of Helix's config.toml: for each mode (normal, select, insert) a
// key maps to a command name, a typable command (":w"), a list of those, or
// a table of further keys for a sequence:
//
//	[keys.normal]
//	C-s = ":w"
//	"A-x" = ["extend_line_below", "delete_selection"]
//	[keys.normal.g]
//	a = "goto_last_accessed_file"
//
// Keys left unbound keep the engine's behaviour, so a table only lists what
// changes. A sequence that matches no binding is replayed to the engine.
type Keymap struct {
	modes map[Mode]*keyNode
}

type keyNode struct {
	actions []keyAction // set on a bound key
	next    map[Key]*keyNode
}

type keyAction struct {
	keys []Key  // engine keys that run the command
	ex   string // a typable command, without the ':'
}

// keymapModes are the config table names for each mode.
var keymapModes = map[string]Mode{"normal": ModeNormal, "select": ModeSelect, "insert": ModeInsert}

// ParseKeymap reads the [keys] table of a config file. It returns the
// keymap built from every binding it understood and one error per binding
// it did not, so one typo does not drop the rest.
func ParseKeymap(raw map[string]any) (*Keymap, []error) {
	km := &Keymap{modes: map[Mode]*keyNode{}}
	var errs []error
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode, ok := keymapModes[name]
		if !ok {
			errs = append(errs, fmt.Errorf("keys.%s: unknown mode (normal, select or insert)", name))
			continue
		}
		table, ok := raw[name].(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("keys.%s must be a table", name))
			continue
		}
		root := &keyNode{next: map[Key]*keyNode{}}
		errs = append(errs, parseKeyTable(root, table, "keys."+name)...)
		km.modes[mode] = root
	}
	return km, errs
}

func parseKeyTable(node *keyNode, table map[string]any, path string) []error {
	var errs []error
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		where := path + "." + name
		k, err := ParseHelixKey(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", where, err))
			continue
		}
		child := &keyNode{}
		switch v := table[name].(type) {
		case map[string]any:
			child.next = map[Key]*keyNode{}
			errs = append(errs, parseKeyTable(child, v, where)...)
		case string:
			a, err := parseKeyAction(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %v", where, err))
				continue
			}
			child.actions = []keyAction{a}
		case []any:
			child.actions = []keyAction{}
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					errs = append(errs, fmt.Errorf("%s: a command list holds strings", where))
					continue
				}
				a, err := parseKeyAction(s)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %v", where, err))
					continue
				}
				child.actions = append(child.actions, a)
			}
		default:
			errs = append(errs, fmt.Errorf("%s: expected a command, a list of commands or a table", where))
			continue
		}
		node.next[k] = child
	}
	return errs
}

func parseKeyAction(s string) (keyAction, error) {
	s = strings.TrimSpace(s)
	if ex, ok := strings.CutPrefix(s, ":"); ok {
		if strings.TrimSpace(ex) == "" {
			return keyAction{}, fmt.Errorf("empty typable command")
		}
		return keyAction{ex: strings.TrimSpace(ex)}, nil
	}
	native, ok := helixCommands[s]
	if !ok {
		return keyAction{}, fmt.Errorf("unknown or unsupported command %q", s)
	}
	if ex, isEx := strings.CutPrefix(native, ":"); isEx && ex != "" {
		return keyAction{ex: ex}, nil
	}
	return keyAction{keys: ParseKeys(native)}, nil
}

// helixNamedKeys are Helix's key names, mapped to the engine's keys.
var helixNamedKeys = map[string]Key{
	"backspace": KeyBackspace, "space": R(' '), "ret": KeyEnter, "enter": KeyEnter,
	"esc": KeyEsc, "del": KeyDelete, "tab": KeyTab, "backtab": KeyShiftTab,
	"left": KeyLeft, "right": KeyRight, "up": KeyUp, "down": KeyDown,
	"home": KeyHome, "end": KeyEnd, "pageup": KeyPgUp, "pagedown": KeyPgDn,
	"minus": R('-'),
}

// ParseHelixKey reads one key in Helix's notation: a character ("a", "A",
// "%"), a name ("ret", "esc", "space", "pageup", "minus"), with C-, A- and
// S- modifiers ("C-s", "A-x", "S-tab").
func ParseHelixKey(s string) (Key, error) {
	var ctrl, alt, shift bool
	rest := s
	for len(rest) > 2 && rest[1] == '-' {
		switch rest[0] {
		case 'C':
			ctrl = true
		case 'A':
			alt = true
		case 'S':
			shift = true
		default:
			return Key{}, fmt.Errorf("unknown modifier in %q (C-, A- or S-)", s)
		}
		rest = rest[2:]
	}
	var k Key
	if r := []rune(rest); len(r) == 1 {
		k = R(r[0])
		if shift {
			k.Rune = unicode.ToUpper(k.Rune)
		}
		if ctrl {
			k.Rune = unicode.ToLower(k.Rune)
		}
	} else if named, ok := helixNamedKeys[strings.ToLower(rest)]; ok {
		k = named
		if shift && k.Name != "" {
			k.Shift = true
		}
	} else {
		return Key{}, fmt.Errorf("unknown key %q", s)
	}
	k.Ctrl = k.Ctrl || ctrl
	k.Alt = k.Alt || alt
	return k, nil
}

// SetKeymap installs the user's bindings; nil removes them.
func (e *Editor) SetKeymap(km *Keymap) {
	e.keymap = km
	e.keymapSeq = nil
}

// remapKey runs k through the user's bindings. It reports whether it took
// the key: a bound key runs its commands, a key that starts a bound
// sequence waits for the next one, and a sequence that matches nothing is
// replayed to the engine.
func (e *Editor) remapKey(k Key) bool {
	if e.keymap == nil || e.remapping || e.replaying || e.macroDepth > 0 {
		return false
	}
	root := e.keymap.modes[e.mode]
	if root == nil || !e.remapReady() {
		e.keymapSeq = nil
		return false
	}
	seq := append(e.keymapSeq, k)
	node := root
	for _, sk := range seq {
		node = node.next[sk]
		if node == nil {
			break
		}
	}
	switch {
	case node != nil && node.actions != nil:
		e.keymapSeq = nil
		e.runKeyActions(node.actions)
		return true
	case node != nil && node.next != nil:
		e.keymapSeq = seq
		return true
	case len(e.keymapSeq) == 0:
		return false
	}
	// no binding: the engine gets the keys as typed
	e.keymapSeq = nil
	e.feedNative(seq)
	return true
}

// remapReady is false while the engine waits for the rest of a command of
// its own, such as the character after f or r: those keys are arguments,
// not commands. A count or register prefix still lets a binding run.
func (e *Editor) remapReady() bool {
	switch e.mode {
	case ModeInsert:
		return !e.ins.ctrlR && !e.ins.ctrlV && !e.ins.ctrlO
	case ModeNormal, ModeSelect:
		if len(e.pending) == 0 {
			return true
		}
		if _, digits := e.PendingCount(); digits {
			return true
		}
		_, _, _, rest, st := splitPrefix(e.pending)
		return st == parseComplete && len(rest) == 0
	}
	return false
}

func (e *Editor) runKeyActions(actions []keyAction) {
	for _, a := range actions {
		if a.ex != "" {
			e.ExecuteEx(a.ex)
			continue
		}
		e.feedNative(a.keys)
	}
}

func (e *Editor) feedNative(keys []Key) {
	e.remapping = true
	defer func() { e.remapping = false }()
	for _, k := range keys {
		e.HandleKey(k)
	}
}

// helixCommands maps Helix command names to the engine keys that run them.
// Only commands the engine implements are listed; a name it lacks is an
// error in the keymap rather than a key that silently does nothing. A
// value starting with ':' is a command the host runs.
var helixCommands = map[string]string{
	"no_op": "",

	// movement
	"move_char_left":            "h",
	"move_char_right":           "l",
	"move_line_down":            "j",
	"move_line_up":              "k",
	"move_visual_line_down":     "j",
	"move_visual_line_up":       "k",
	"move_next_word_start":      "w",
	"move_prev_word_start":      "b",
	"move_next_word_end":        "e",
	"move_next_long_word_start": "W",
	"move_prev_long_word_start": "B",
	"move_next_long_word_end":   "E",
	"find_till_char":            "t",
	"find_next_char":            "f",
	"till_prev_char":            "T",
	"find_prev_char":            "F",
	"repeat_last_motion":        "<a-.>",
	"page_down":                 "<c-f>",
	"page_up":                   "<c-b>",
	"half_page_down":            "<c-d>",
	"half_page_up":              "<c-u>",
	"page_cursor_half_down":     "<c-d>",
	"page_cursor_half_up":       "<c-u>",
	"scroll_down":               "<c-e>",
	"scroll_up":                 "<c-y>",

	// goto mode
	"goto_file_start":          "gg",
	"goto_last_line":           "ge",
	"goto_line_start":          "gh",
	"goto_line_end":            "gl",
	"goto_first_nonwhitespace": "gs",
	"goto_window_top":          "gt",
	"goto_window_center":       "gc",
	"goto_window_bottom":       "gb",
	"goto_definition":          "gd",
	"goto_next_buffer":         ":bn",
	"goto_previous_buffer":     ":bp",

	// view mode
	"align_view_center": "zz",
	"align_view_top":    "zt",
	"align_view_bottom": "zb",

	// selection
	"select_mode":              "v",
	"extend_line_below":        "x",
	"extend_to_line_bounds":    "X",
	"select_all":               "%",
	"collapse_selection":       ";",
	"expand_selection":         "<a-o>",
	"shrink_selection":         "<a-i>",
	"match_brackets":           "mm",
	"select_textobject_inner":  "mi",
	"select_textobject_around": "ma",
	"search_selection":         "*",
	"normal_mode":              "<esc>",
	"exit_select_mode":         "<esc>",

	// changes
	"insert_mode":          "i",
	"append_mode":          "a",
	"insert_at_line_start": "I",
	"insert_at_line_end":   "A",
	"open_below":           "o",
	"open_above":           "O",
	"delete_selection":     "d",
	"change_selection":     "c",
	"yank":                 "y",
	"paste_after":          "p",
	"paste_before":         "P",
	"replace_with_yanked":  "R",
	"replace":              "r",
	"indent":               ">",
	"unindent":             "<lt>",
	"switch_case":          "~",
	"switch_to_lowercase":  "`",
	"join_selections":      "J",
	"undo":                 "u",
	"redo":                 "U",
	"increment":            "<c-a>",
	"decrement":            "<c-x>",
	"repeat_last_insert":   ".",
	"record_macro":         "Q",
	"replay_macro":         "q",
	"command_mode":         ":",
	"search":               "/",
	"rsearch":              "?",
	"search_next":          "n",
	"search_prev":          "N",
}

// HelixCommands lists the supported command names, sorted.
func HelixCommands() []string {
	out := make([]string, 0, len(helixCommands))
	for name := range helixCommands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
