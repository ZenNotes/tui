package helix

import (
	"strings"
	"testing"
)

func keymapEditor(t *testing.T, text string, raw map[string]any) *Editor {
	t.Helper()
	km, errs := ParseKeymap(raw)
	if len(errs) > 0 {
		t.Fatalf("keymap errors: %v", errs)
	}
	e := New(text, DefaultOptions(), Hooks{})
	e.SetKeymap(km)
	return e
}

func press(e *Editor, keys ...Key) {
	for _, k := range keys {
		e.HandleKey(k)
	}
}

func TestParseHelixKey(t *testing.T) {
	for in, want := range map[string]Key{
		"a":        R('a'),
		"A":        R('A'),
		"%":        R('%'),
		"C-s":      Ctrl('s'),
		"A-x":      Alt('x'),
		"S-a":      R('A'),
		"ret":      KeyEnter,
		"esc":      KeyEsc,
		"space":    R(' '),
		"minus":    R('-'),
		"backtab":  KeyShiftTab,
		"S-tab":    KeyShiftTab,
		"pageup":   KeyPgUp,
		"C-space":  {Rune: ' ', Ctrl: true},
		"A-up":     {Name: "up", Alt: true},
		"C-A-left": {Name: "left", Ctrl: true, Alt: true},
	} {
		got, err := ParseHelixKey(in)
		if err != nil || got != want {
			t.Errorf("ParseHelixKey(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"X-a", "nosuchkey", ""} {
		if _, err := ParseHelixKey(bad); err == nil {
			t.Errorf("ParseHelixKey(%q) should fail", bad)
		}
	}
}

func TestKeymapRebindsACommand(t *testing.T) {
	// swap j and k, the classic test of a remap that must not loop
	e := keymapEditor(t, "one\ntwo\nthree\n", map[string]any{"normal": map[string]any{
		"j": "move_line_up", "k": "move_line_down",
	}})
	press(e, R('k'), R('k'))
	if got := e.Cursor().Line; got != 2 {
		t.Fatalf("k k (move_line_down) = line %d, want 2", got)
	}
	press(e, R('j'))
	if got := e.Cursor().Line; got != 1 {
		t.Fatalf("j (move_line_up) = line %d, want 1", got)
	}
}

func TestKeymapCommandListAndCount(t *testing.T) {
	e := keymapEditor(t, "one\ntwo\nthree\nfour\n", map[string]any{"normal": map[string]any{
		"A-x": []any{"extend_line_below", "delete_selection"},
		"D":   "move_line_down",
	}})
	press(e, Alt('x'))
	if got := e.Text(); got != "two\nthree\nfour\n" {
		t.Fatalf("A-x (extend_line_below, delete_selection) = %q", got)
	}
	// a count typed before a bound key applies to its command
	press(e, R('2'), R('D'))
	if got := e.Cursor().Line; got != 2 {
		t.Fatalf("2D (move_line_down) = line %d, want 2", got)
	}
}

func TestKeymapSequenceKeepsEngineDefaults(t *testing.T) {
	e := keymapEditor(t, "one\ntwo\nthree", map[string]any{"normal": map[string]any{
		"g": map[string]any{"a": "goto_last_line"},
	}})
	press(e, R('g'), R('a'))
	if got := e.Cursor().Line; got != 2 {
		t.Fatalf("g a (goto_last_line) = line %d, want 2", got)
	}
	// g g is not in the table: the engine's own goto_file_start still runs
	press(e, R('g'), R('g'))
	if got := e.Cursor().Line; got != 0 {
		t.Fatalf("g g fell through to line %d, want 0", got)
	}
}

func TestKeymapTypableCommandReachesTheHost(t *testing.T) {
	var ran []string
	e := keymapEditor(t, "one\n", map[string]any{
		"normal": map[string]any{"C-s": ":w"},
		"insert": map[string]any{"C-s": ":w"},
	})
	e.SetHooks(Hooks{ExCommand: func(c ExCommand) (bool, error) { ran = append(ran, c.Name); return true, nil }})
	press(e, Ctrl('s'), R('i'), R('x'), Ctrl('s'))
	if strings.Join(ran, ",") != "w,w" {
		t.Fatalf("host ran %v, want w twice", ran)
	}
	if got := e.Text(); got != "xone\n" {
		t.Fatalf("text %q", got)
	}
}

func TestKeymapLeavesCommandArgumentsAlone(t *testing.T) {
	// j is rebound, but after f it is the character to find
	e := keymapEditor(t, "abjcd\n", map[string]any{"normal": map[string]any{"j": "move_line_up"}})
	press(e, R('f'), R('j'))
	if got := e.Cursor().Col; got != 2 {
		t.Fatalf("f j = col %d, want 2", got)
	}
}

func TestKeymapInsertSequenceFallsBackToText(t *testing.T) {
	e := keymapEditor(t, "\n", map[string]any{"insert": map[string]any{
		"j": map[string]any{"k": "normal_mode"},
	}})
	press(e, R('i'), R('j'), R('x'))
	if got := e.Text(); got != "jx\n" {
		t.Fatalf("j x typed %q, want both characters", got)
	}
	press(e, R('j'), R('k'))
	if e.Mode() != ModeNormal {
		t.Fatalf("j k did not leave insert mode: %v", e.Mode())
	}
	if got := e.Text(); got != "jx\n" {
		t.Fatalf("j k inserted text: %q", got)
	}
}

func TestKeymapNoOpUnbindsAKey(t *testing.T) {
	e := keymapEditor(t, "one\n", map[string]any{"normal": map[string]any{"d": "no_op"}})
	press(e, R('d'))
	if got := e.Text(); got != "one\n" {
		t.Fatalf("d (no_op) changed the text: %q", got)
	}
}

func TestKeymapMacroReplaysWhatRan(t *testing.T) {
	e := keymapEditor(t, "a\nb\nc\nd\n", map[string]any{"normal": map[string]any{"D": "move_line_down"}})
	press(e, R('Q'), R('D'), R('Q'), R('q'))
	if got := e.Cursor().Line; got != 2 {
		t.Fatalf("macro of D replayed to line %d, want 2", got)
	}
}

func TestParseKeymapReportsEachBadBinding(t *testing.T) {
	km, errs := ParseKeymap(map[string]any{
		"normal": map[string]any{"C-s": ":w", "x": "no_such_command", "Z-q": "undo"},
		"visual": map[string]any{},
	})
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want 3", errs)
	}
	if km.modes[ModeNormal].next[Ctrl('s')] == nil {
		t.Fatal("the good binding was dropped")
	}
}

// Every command in the table runs in a fresh editor without leaving the
// engine waiting, except the ones that wait for an argument by design.
func TestHelixCommandsRun(t *testing.T) {
	waits := map[string]bool{
		"find_till_char": true, "find_next_char": true, "till_prev_char": true, "find_prev_char": true,
		"replace": true, "select_textobject_inner": true, "select_textobject_around": true,
		"command_mode": true, "search": true, "rsearch": true,
	}
	for _, name := range HelixCommands() {
		a, err := parseKeyAction(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if a.ex != "" {
			continue
		}
		e := New("one two\nthree four\n(five)\n", DefaultOptions(), Hooks{})
		e.runKeyActions([]keyAction{a})
		_, _, _, cmdActive := e.Cmdline()
		pending := e.Pending() != "" || cmdActive
		if pending != waits[name] {
			t.Errorf("%s: pending=%v %q, want pending=%v", name, pending, e.Pending(), waits[name])
		}
	}
}
