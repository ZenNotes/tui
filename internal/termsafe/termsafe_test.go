package termsafe

import (
	"bytes"
	"strings"
	"testing"
)

func TestCleanDropsControlSequences(t *testing.T) {
	for name, in := range map[string]string{
		"osc bel":        "a\x1b]52;c;aGk=\x07b",
		"osc st":         "a\x1b]52;c;aGk=\x1b\\b",
		"osc c1 st raw":  "a\x1b]52;c;aGk=\x9cb",
		"osc c1 st utf8": "a\x1b]52;c;aGk=\u009cb",
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
		"bidi override":  "a‮b",
		"bidi isolate":   "a⁦b",
	} {
		if got := Clean(in); got != "ab" {
			t.Errorf("%s: %q gives %q, want %q", name, in, got, "ab")
		}
	}
}

func TestCleanEdges(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b]52;c;aGk=\nb":   "a\nb",
		"a\x1b]52;c;aGk=":      "a",
		"a\x1b]52;c;\x1b[31mb": "a\x1b[31mb",
		"a\x1b":                "a",
		"a\x1b[":               "a",
		"a\x1b[12":             "a",
		"a\x1b(":               "a",
		"a\x1b[12\nb":          "a\nb",
		"a\x1b    b":           "a    b",
		"a\xffb":               "a�b",
		"a\x9bb":               "a�b",
	} {
		if got := Clean(in); got != want {
			t.Errorf("%q gives %q, want %q", in, got, want)
		}
	}
}

func TestCleanKeepsTextAndColours(t *testing.T) {
	for _, in := range []string{
		"plain",
		"two\nrows\tand a tab",
		"\x1b[0m\x1b[m\x1b[1;31mred\x1b[39m\x1b[49m",
		"\x1b[38;2;10;20;30mtrue\x1b[48;5;200mcolour\x1b[0m",
		"\x1b[4:3mcurly\x1b[0m",
		"box │ ┌─┐ 中文 ＠ 🚀 ⚠️ 👨‍👩‍👧‍👦 é",
		"\U0010EEEE̅̅ placeholder",
		" icon",
		"right‏to‎left مرحبا",
		"",
	} {
		if got := Clean(in); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}

func TestVisibleShowsControlRunes(t *testing.T) {
	for in, want := range map[string]string{
		"plain\nrows\ttab": "plain\nrows\ttab",
		"a\x1b[8mhidden":   "a␛[8mhidden",
		"a\x07b":           "a␇b",
		"a\x7fb":           "a␡b",
		"a\u009bb":         "a�b",
		"a\xffb":           "a�b",
		"a�b":              "a�b",
		"crlf\r\nnext":     "crlf\nnext",
		"lone\rreturn":     "lone␍return",
		"\U0010EEEE̅ pic":  "�̅ pic",
		"中文 🚀":             "中文 🚀",
	} {
		if got := Visible(in); got != want {
			t.Errorf("%q gives %q, want %q", in, got, want)
		}
	}
}

func TestLineStaysOnOneRow(t *testing.T) {
	if got := Line("a\nb\tc\x1b"); got != "a␤b␉c␛" {
		t.Errorf("got %q", got)
	}
}

func TestWriterCleansEachWrite(t *testing.T) {
	var out bytes.Buffer
	w := Writer{W: &out}
	n, err := w.Write([]byte("a\x1b]52;c;aGk=\x07b"))
	if err != nil || n != len("a\x1b]52;c;aGk=\x07b") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	_, _ = w.Write([]byte("c\x1b]52"))
	_, _ = w.Write([]byte(";c;aGk=\x07d"))
	if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\x07") {
		t.Fatalf("got %q", out.String())
	}
}
