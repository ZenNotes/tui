package tui

import (
	"strings"
	"testing"
)

func TestCleanScreenDropsControlSequences(t *testing.T) {
	for name, in := range map[string]string{
		"osc bel":        "a\x1b]52;c;aGk=\x07b",
		"osc st":         "a\x1b]52;c;aGk=\x1b\\b",
		"osc title":      "a\x1b]0;name\x07b",
		"osc link":       "a\x1b]8;;https://example.com\x1b\\b",
		"dcs":            "a\x1bPq#0\x1b\\b",
		"apc":            "a\x1b_Gi=1\x1b\\b",
		"pm":             "a\x1b^x\x1b\\b",
		"sos":            "a\x1bXx\x1b\\b",
		"csi cursor":     "a\x1b[2;2Hb",
		"csi erase":      "a\x1b[2Jb",
		"csi mode":       "a\x1b[?1049lb",
		"csi cursor set": "a\x1b[6 qb",
		"csi report":     "a\x1b[6nb",
		"csi keyboard":   "a\x1b[>4;2mb",
		"reset":          "a\x1bcb",
		"save cursor":    "a\x1b7b",
		"charset":        "a\x1b(0b",
		"bell":           "a\x07b",
		"backspace":      "a\x08b",
		"return":         "a\rb",
		"delete":         "a\x7fb",
		"c1 utf8":        "a\u009bb",
		"c1 byte":        "a\x9bb",
	} {
		if got := cleanScreen(in); got != "ab" {
			t.Errorf("%s: %q gives %q, want %q", name, in, got, "ab")
		}
	}
}

// A sequence with no end stops at the row's end, so the rows after it stay.
func TestCleanScreenUnterminatedSequences(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b]52;c;aGk=\nb":   "a\nb",
		"a\x1b]52;c;aGk=":      "a",
		"a\x1b]52;c;\x1b[31mb": "a\x1b[31mb",
		"a\x1b":                "a",
		"a\x1b[":               "a",
		"a\x1b[12":             "a",
		"a\x1b(":               "a",
		"a\x1b[12\nb":          "a\nb",
	} {
		if got := cleanScreen(in); got != want {
			t.Errorf("%q gives %q, want %q", in, got, want)
		}
	}
}

func TestCleanScreenKeepsTextAndColours(t *testing.T) {
	for _, in := range []string{
		"plain",
		"two\nrows\tand a tab",
		"\x1b[0m\x1b[m\x1b[1;31mred\x1b[39m\x1b[49m",
		"\x1b[38;2;10;20;30mtrue\x1b[48;5;200mcolour\x1b[0m",
		"\x1b[4:3mcurly\x1b[0m",
		"box │ ┌─┐ 中文 ＠ 🚀 ⚠️ 👨‍👩‍👧‍👦 é",
		"\U0010EEEE̅̅ placeholder",
		"\ue0a0 icon",
		"",
	} {
		if got := cleanScreen(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}

// The real frame: a note's bytes are text on screen wherever they are drawn,
// in the editor and in both reading view styles, with the colours left alone.
func TestViewSendsNoControlSequencesFromANote(t *testing.T) {
	body := "# Title \x1b]0;name\x07\n\nline \x1b]52;c;aGk=\x07 and \x1b[2J more\n\n```\ncode \x1b]52;c;aGk=\x07\n```\n\n| a \x1b]52;c;aGk=\x07 | b |\n|---|---|\n| 1 | 2 |\n"
	for name, style := range map[string]string{"auto": "auto", "zen": "zen"} {
		out := viewOfNote(t, body, style)
		for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: the frame holds %q", name, bad)
			}
		}
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("%s: the frame lost its colours", name)
		}
		if !strings.Contains(stripAnsi(out), "line") {
			t.Errorf("%s: the frame lost the note's text", name)
		}
	}
}

// viewOfNote draws a note in the editor and in the reading view, as one frame
// each, and returns them joined.
func viewOfNote(t *testing.T, body, style string) string {
	t.Helper()
	a, _ := cursorApp(t, body, 100)
	a.prefs.PreviewStyle = style
	editor := a.View()
	a.setPaneMode(modePreview)
	return editor + a.View()
}
