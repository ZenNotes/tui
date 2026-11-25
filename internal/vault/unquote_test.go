package vault

import "testing"

func TestUnquoteReadsYAMLEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"Lab \"A\""`, `Lab "A"`},
		{`"a\\b"`, `a\b`},
		{`"one\ntwo"`, "one\ntwo"},
		{`"a\tb"`, "a\tb"},
		{`"café"`, "café"},
		{`"\\n"`, `\n`},
		{`"a\rb"`, "a\rb"},
		{`"x\u0041"`, "xA"},
		{`"caf\u00e9"`, "café"},
		{`"\ud83d\ude00!"`, "\U0001F600!"},
		// An unknown or unfinished escape stays as written.
		{`"a\qb"`, `a\qb`},
		{`"a\u12"`, `a\u12`},
		{`"\u12zz"`, `\u12zz`},
		{`"\ud83d x"`, `\ud83d x`},
		{`"\ude00"`, `\ude00`},
		{`'it''s'`, `it's`},
		{`'a\nb'`, `a\nb`},
		// A plain value does not change.
		{`a \"b\" ''c''`, `a \"b\" ''c''`},
		{`  "x"  `, `x`},
		{`"`, `"`},
		{`""`, ``},
	}
	for _, c := range cases {
		if got := Unquote(c.in); got != c.want {
			t.Errorf("Unquote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnquoteReadsWhatYAMLValueWrites(t *testing.T) {
	for _, v := range []string{"Lab \"A\"", `a\b`, "one\ntwo\r\tend", "bell\x07", "café 😀"} {
		if got := Unquote(YAMLValue(v)); got != v {
			t.Errorf("Unquote(YAMLValue(%q)) = %q", v, got)
		}
	}
}

func TestParseFrontmatterFieldsReadsEscapesInEveryForm(t *testing.T) {
	fm := ParseFrontmatterFields("title: \"Lab \\\"A\\\"\"\ntags:\n  - 'it''s'\nlist: [\"a\\tb\", 'c']\n")
	if got := fm["title"][0]; got != `Lab "A"` {
		t.Errorf("title = %q", got)
	}
	if got := fm["tags"][0]; got != "it's" {
		t.Errorf("tags = %q", got)
	}
	if got := fm["list"]; len(got) != 2 || got[0] != "a\tb" || got[1] != "c" {
		t.Errorf("list = %q", got)
	}
}
