package vim

import (
	"strings"
	"testing"
)

type keyCase struct {
	name   string
	text   string
	cursor Pos
	keys   string
	want   string
	wantC  Pos
	mode   Mode
	setup  func(o *Options)
}

func run(t *testing.T, cases []keyCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.TextReplacementsEnabled = false
			if c.setup != nil {
				c.setup(&opts)
			}
			e := New(c.text, opts, Hooks{})
			e.SetViewport(10)
			e.SetCursor(c.cursor)
			e.Feed(c.keys)
			if got := e.Text(); got != c.want {
				t.Fatalf("text:\n got %q\nwant %q", got, c.want)
			}
			if e.Cursor() != c.wantC {
				t.Fatalf("cursor: got %+v want %+v", e.Cursor(), c.wantC)
			}
			if e.Mode() != c.mode {
				t.Fatalf("mode: got %v want %v", e.Mode(), c.mode)
			}
		})
	}
}

func TestMotions(t *testing.T) {
	text := "one two three\nfour five\n\nsix (seven) eight\n"
	run(t, []keyCase{
		{"l", text, Pos{0, 0}, "l", text, Pos{0, 1}, ModeNormal, nil},
		{"3l", text, Pos{0, 0}, "3l", text, Pos{0, 3}, ModeNormal, nil},
		{"l stops at end", text, Pos{0, 12}, "l", text, Pos{0, 12}, ModeNormal, nil},
		{"h", text, Pos{0, 3}, "h", text, Pos{0, 2}, ModeNormal, nil},
		{"j keeps column", text, Pos{0, 5}, "j", text, Pos{1, 5}, ModeNormal, nil},
		{"j clamps then restores", text, Pos{0, 10}, "jj", text, Pos{2, 0}, ModeNormal, nil},
		{"jjj restores desired col", text, Pos{0, 10}, "jjj", text, Pos{3, 10}, ModeNormal, nil},
		{"k", text, Pos{1, 2}, "k", text, Pos{0, 2}, ModeNormal, nil},
		{"w", text, Pos{0, 0}, "w", text, Pos{0, 4}, ModeNormal, nil},
		{"2w", text, Pos{0, 0}, "2w", text, Pos{0, 8}, ModeNormal, nil},
		{"w crosses lines", text, Pos{0, 8}, "w", text, Pos{1, 0}, ModeNormal, nil},
		{"w to blank line", text, Pos{1, 5}, "w", text, Pos{2, 0}, ModeNormal, nil},
		{"W", "a.b c", Pos{0, 0}, "W", "a.b c", Pos{0, 4}, ModeNormal, nil},
		{"w punctuation", "a.b c", Pos{0, 0}, "w", "a.b c", Pos{0, 1}, ModeNormal, nil},
		{"b", text, Pos{0, 8}, "b", text, Pos{0, 4}, ModeNormal, nil},
		{"b crosses lines", text, Pos{1, 0}, "b", text, Pos{0, 8}, ModeNormal, nil},
		{"e", text, Pos{0, 0}, "e", text, Pos{0, 2}, ModeNormal, nil},
		{"e from end of word", text, Pos{0, 2}, "e", text, Pos{0, 6}, ModeNormal, nil},
		{"ge", text, Pos{0, 5}, "ge", text, Pos{0, 2}, ModeNormal, nil},
		{"0", text, Pos{0, 5}, "0", text, Pos{0, 0}, ModeNormal, nil},
		{"$", text, Pos{0, 0}, "$", text, Pos{0, 12}, ModeNormal, nil},
		{"$ then j sticks", text, Pos{0, 0}, "$j", text, Pos{1, 8}, ModeNormal, nil},
		{"^", "   x y", Pos{0, 5}, "^", "   x y", Pos{0, 3}, ModeNormal, nil},
		{"gg", text, Pos{3, 2}, "gg", text, Pos{0, 0}, ModeNormal, nil},
		{"G", text, Pos{0, 0}, "G", text, Pos{4, 0}, ModeNormal, nil},
		{"2G", text, Pos{0, 0}, "2G", text, Pos{1, 0}, ModeNormal, nil},
		{"3gg", text, Pos{0, 0}, "3gg", text, Pos{2, 0}, ModeNormal, nil},
		{"f", text, Pos{0, 0}, "ft", text, Pos{0, 4}, ModeNormal, nil},
		{"2f", text, Pos{0, 0}, "2ft", text, Pos{0, 8}, ModeNormal, nil},
		{"t", text, Pos{0, 0}, "tt", text, Pos{0, 3}, ModeNormal, nil},
		{";", text, Pos{0, 0}, "ft;", text, Pos{0, 8}, ModeNormal, nil},
		{",", text, Pos{0, 0}, "ft;,", text, Pos{0, 4}, ModeNormal, nil},
		{"F", text, Pos{0, 12}, "Fo", text, Pos{0, 6}, ModeNormal, nil},
		{"T", text, Pos{0, 12}, "To", text, Pos{0, 7}, ModeNormal, nil},
		{"f fail stays", text, Pos{0, 0}, "fz", text, Pos{0, 0}, ModeNormal, nil},
		{"%", text, Pos{3, 4}, "%", text, Pos{3, 10}, ModeNormal, nil},
		{"% back", text, Pos{3, 10}, "%", text, Pos{3, 4}, ModeNormal, nil},
		{"% from before", text, Pos{3, 0}, "%", text, Pos{3, 10}, ModeNormal, nil},
		{"}", text, Pos{0, 0}, "}", text, Pos{2, 0}, ModeNormal, nil},
		{"{", text, Pos{3, 3}, "{", text, Pos{2, 0}, ModeNormal, nil},
		{"| column", text, Pos{0, 0}, "5|", text, Pos{0, 4}, ModeNormal, nil},
		{"enter first nonblank", "a\n  b", Pos{0, 0}, "<CR>", "a\n  b", Pos{1, 2}, ModeNormal, nil},
		{"- up", "  a\nb", Pos{1, 0}, "-", "  a\nb", Pos{0, 2}, ModeNormal, nil},
		{"]] heading", "x\n# A\ny\n## B\n", Pos{0, 0}, "]]", "x\n# A\ny\n## B\n", Pos{1, 0}, ModeNormal, nil},
		{"2]] heading", "x\n# A\ny\n## B\n", Pos{0, 0}, "2]]", "x\n# A\ny\n## B\n", Pos{3, 0}, ModeNormal, nil},
		{"[[ heading", "x\n# A\ny\n## B\n", Pos{3, 0}, "[[", "x\n# A\ny\n## B\n", Pos{1, 0}, ModeNormal, nil},
		{"marks", text, Pos{0, 4}, "majj`a", text, Pos{0, 4}, ModeNormal, nil},
		{"mark line", text, Pos{0, 4}, "majj'a", text, Pos{0, 0}, ModeNormal, nil},
		{"( sentence", "One. Two. Three.", Pos{0, 12}, "(", "One. Two. Three.", Pos{0, 10}, ModeNormal, nil},
		{") sentence", "One. Two. Three.", Pos{0, 0}, ")", "One. Two. Three.", Pos{0, 5}, ModeNormal, nil},
	})
}

func TestOperators(t *testing.T) {
	run(t, []keyCase{
		{"x", "abc", Pos{0, 1}, "x", "ac", Pos{0, 1}, ModeNormal, nil},
		{"3x", "abcdef", Pos{0, 1}, "3x", "aef", Pos{0, 1}, ModeNormal, nil},
		{"x at end clamps", "abc", Pos{0, 2}, "x", "ab", Pos{0, 1}, ModeNormal, nil},
		{"X", "abc", Pos{0, 2}, "X", "ac", Pos{0, 1}, ModeNormal, nil},
		{"dw", "one two three", Pos{0, 0}, "dw", "two three", Pos{0, 0}, ModeNormal, nil},
		{"dw last word", "one two", Pos{0, 4}, "dw", "one ", Pos{0, 3}, ModeNormal, nil},
		{"dw end of line keeps next line", "one two\nthree", Pos{0, 4}, "dw", "one \nthree", Pos{0, 3}, ModeNormal, nil},
		{"d2w", "one two three four", Pos{0, 0}, "d2w", "three four", Pos{0, 0}, ModeNormal, nil},
		{"2dw", "one two three four", Pos{0, 0}, "2dw", "three four", Pos{0, 0}, ModeNormal, nil},
		{"de", "one two", Pos{0, 0}, "de", " two", Pos{0, 0}, ModeNormal, nil},
		{"db", "one two", Pos{0, 4}, "db", "two", Pos{0, 0}, ModeNormal, nil},
		{"dd", "a\nb\nc", Pos{1, 0}, "dd", "a\nc", Pos{1, 0}, ModeNormal, nil},
		{"2dd", "a\nb\nc\nd", Pos{1, 0}, "2dd", "a\nd", Pos{1, 0}, ModeNormal, nil},
		{"dd last line", "a\nb", Pos{1, 0}, "dd", "a", Pos{0, 0}, ModeNormal, nil},
		{"dd only line", "abc", Pos{0, 1}, "dd", "", Pos{0, 0}, ModeNormal, nil},
		{"dj", "a\nb\nc", Pos{0, 0}, "dj", "c", Pos{0, 0}, ModeNormal, nil},
		{"dk", "a\nb\nc", Pos{2, 0}, "dk", "a", Pos{0, 0}, ModeNormal, nil},
		{"dG", "a\nb\nc", Pos{1, 0}, "dG", "a", Pos{0, 0}, ModeNormal, nil},
		{"dgg", "a\nb\nc", Pos{1, 0}, "dgg", "c", Pos{0, 0}, ModeNormal, nil},
		{"d$", "one two", Pos{0, 3}, "d$", "one", Pos{0, 2}, ModeNormal, nil},
		{"D", "one two", Pos{0, 3}, "D", "one", Pos{0, 2}, ModeNormal, nil},
		{"d0", "one two", Pos{0, 4}, "d0", "two", Pos{0, 0}, ModeNormal, nil},
		{"d^", "  one two", Pos{0, 6}, "d^", "  two", Pos{0, 2}, ModeNormal, nil},
		{"dfx", "abcxdef", Pos{0, 0}, "dfx", "def", Pos{0, 0}, ModeNormal, nil},
		{"dtx", "abcxdef", Pos{0, 0}, "dtx", "xdef", Pos{0, 0}, ModeNormal, nil},
		{"d}", "a\nb\n\nc", Pos{0, 0}, "d}", "\nc", Pos{0, 0}, ModeNormal, nil},
		{"d} mid line charwise", "one two\nb\n\nc", Pos{0, 4}, "d}", "one \n\nc", Pos{0, 3}, ModeNormal, nil},
		{"diw", "one two three", Pos{0, 5}, "diw", "one  three", Pos{0, 4}, ModeNormal, nil},
		{"daw", "one two three", Pos{0, 5}, "daw", "one three", Pos{0, 4}, ModeNormal, nil},
		{"daw end", "one two", Pos{0, 5}, "daw", "one", Pos{0, 2}, ModeNormal, nil},
		{"ciw", "one two three", Pos{0, 5}, "ciwX<Esc>", "one X three", Pos{0, 4}, ModeNormal, nil},
		{"di(", "f(a, b) c", Pos{0, 3}, "di(", "f() c", Pos{0, 2}, ModeNormal, nil},
		{"da(", "f(a, b) c", Pos{0, 3}, "da(", "f c", Pos{0, 1}, ModeNormal, nil},
		{"di( on bracket", "f(a, b) c", Pos{0, 1}, "di(", "f() c", Pos{0, 2}, ModeNormal, nil},
		{"di[ nested", "[a [b] c]", Pos{0, 4}, "di[", "[a [] c]", Pos{0, 4}, ModeNormal, nil},
		{"2di[", "[a [b] c]", Pos{0, 4}, "2di[", "[]", Pos{0, 1}, ModeNormal, nil},
		{"di{ multiline", "{\n  a\n  b\n}", Pos{1, 1}, "di{", "{\n}", Pos{1, 0}, ModeNormal, nil},
		{"di\"", `say "hello world" now`, Pos{0, 7}, `di"`, `say "" now`, Pos{0, 5}, ModeNormal, nil},
		{"da\"", `say "hello world" now`, Pos{0, 7}, `da"`, `say now`, Pos{0, 4}, ModeNormal, nil},
		{"ci\" before quotes", `say "hello" now`, Pos{0, 0}, `ci"x<Esc>`, `say "x" now`, Pos{0, 5}, ModeNormal, nil},
		{"dit", "<b>bold</b> x", Pos{0, 4}, "dit", "<b></b> x", Pos{0, 3}, ModeNormal, nil},
		{"dat", "<b>bold</b> x", Pos{0, 4}, "dat", " x", Pos{0, 0}, ModeNormal, nil},
		{"dip", "a\nb\n\nc", Pos{0, 0}, "dip", "\nc", Pos{0, 0}, ModeNormal, nil},
		{"dap", "a\nb\n\nc", Pos{0, 0}, "dap", "c", Pos{0, 0}, ModeNormal, nil},
		{"cc keeps indent", "  abc", Pos{0, 3}, "ccx<Esc>", "  x", Pos{0, 2}, ModeNormal, nil},
		{"S", "abc\ndef", Pos{1, 1}, "Sq<Esc>", "abc\nq", Pos{1, 0}, ModeNormal, nil},
		{"cw is ce", "one two", Pos{0, 0}, "cwX<Esc>", "X two", Pos{0, 0}, ModeNormal, nil},
		{"cw on space", "one  two", Pos{0, 3}, "cw-<Esc>", "one-two", Pos{0, 3}, ModeNormal, nil},
		{"C", "one two", Pos{0, 3}, "C!<Esc>", "one!", Pos{0, 3}, ModeNormal, nil},
		{"s", "abc", Pos{0, 1}, "sXY<Esc>", "aXYc", Pos{0, 2}, ModeNormal, nil},
		{"r", "abc", Pos{0, 1}, "rx", "axc", Pos{0, 1}, ModeNormal, nil},
		{"3r", "abcd", Pos{0, 1}, "3rx", "axxx", Pos{0, 3}, ModeNormal, nil},
		{"r too many", "abc", Pos{0, 1}, "5rx", "abc", Pos{0, 1}, ModeNormal, nil},
		{"r enter", "ab cd", Pos{0, 2}, "r<CR>", "ab\ncd", Pos{1, 0}, ModeNormal, nil},
		{"~", "abc", Pos{0, 0}, "~", "Abc", Pos{0, 1}, ModeNormal, nil},
		{"3~", "abc", Pos{0, 0}, "3~", "ABC", Pos{0, 2}, ModeNormal, nil},
		{"gUw", "one two", Pos{0, 0}, "gUw", "ONE two", Pos{0, 0}, ModeNormal, nil},
		{"guu", "ONE TWO", Pos{0, 3}, "guu", "one two", Pos{0, 0}, ModeNormal, nil},
		{"g~~", "One", Pos{0, 0}, "g~~", "oNE", Pos{0, 0}, ModeNormal, nil},
		{"J", "a\n  b\nc", Pos{0, 0}, "J", "a b\nc", Pos{0, 1}, ModeNormal, nil},
		{"3J", "a\nb\nc\nd", Pos{0, 0}, "3J", "a b c\nd", Pos{0, 3}, ModeNormal, nil},
		{"gJ", "a\n  b", Pos{0, 0}, "gJ", "a  b", Pos{0, 1}, ModeNormal, nil},
		{">>", "a\nb", Pos{0, 0}, ">>", "    a\nb", Pos{0, 4}, ModeNormal, nil},
		{"2>>", "a\nb", Pos{0, 0}, "2>>", "    a\n    b", Pos{0, 4}, ModeNormal, nil},
		{"<<", "    a", Pos{0, 4}, "<<", "a", Pos{0, 0}, ModeNormal, nil},
		{">j", "a\nb\nc", Pos{0, 0}, ">j", "    a\n    b\nc", Pos{0, 4}, ModeNormal, nil},
		{"ctrl-a", "x 41 y", Pos{0, 0}, "<C-a>", "x 42 y", Pos{0, 3}, ModeNormal, nil},
		{"5 ctrl-x", "x 41 y", Pos{0, 0}, "5<C-x>", "x 36 y", Pos{0, 3}, ModeNormal, nil},
		{"ctrl-a negative", "x -1 y", Pos{0, 0}, "2<C-a>", "x 1 y", Pos{0, 2}, ModeNormal, nil},
		{"o list continues", "- item", Pos{0, 3}, "onext<Esc>", "- item\n- next", Pos{1, 5}, ModeNormal, nil},
		{"o numbered continues", "1. item", Pos{0, 3}, "ox<Esc>", "1. item\n2. x", Pos{1, 3}, ModeNormal, nil},
		{"o checkbox continues", "- [x] done", Pos{0, 3}, "ox<Esc>", "- [x] done\n- [ ] x", Pos{1, 6}, ModeNormal, nil},
		{"O above", "  abc", Pos{0, 3}, "Ox<Esc>", "  x\n  abc", Pos{0, 2}, ModeNormal, nil},
		{"gq joins paragraph", "one\ntwo\n\nthree", Pos{0, 0}, "gqip", "one two\n\nthree", Pos{0, 0}, ModeNormal, nil},
		{"gq keeps headings", "# H\none\ntwo", Pos{1, 0}, "gqG", "# H\none two", Pos{1, 0}, ModeNormal, nil},
	})
}

func TestInsertMode(t *testing.T) {
	run(t, []keyCase{
		{"i", "abc", Pos{0, 1}, "iX<Esc>", "aXbc", Pos{0, 1}, ModeNormal, nil},
		{"a", "abc", Pos{0, 1}, "aX<Esc>", "abXc", Pos{0, 2}, ModeNormal, nil},
		{"A", "abc", Pos{0, 0}, "AX<Esc>", "abcX", Pos{0, 3}, ModeNormal, nil},
		{"I", "  abc", Pos{0, 4}, "IX<Esc>", "  Xabc", Pos{0, 2}, ModeNormal, nil},
		{"esc at col 0 stays", "abc", Pos{0, 0}, "i<Esc>", "abc", Pos{0, 0}, ModeNormal, nil},
		{"enter splits", "abcd", Pos{0, 2}, "i<CR><Esc>", "ab\ncd", Pos{1, 0}, ModeNormal, nil},
		{"enter autoindent", "  ab", Pos{0, 3}, "a<CR>x<Esc>", "  ab\n  x", Pos{1, 2}, ModeNormal, nil},
		{"enter continues list", "- ab", Pos{0, 3}, "a<CR>x<Esc>", "- ab\n- x", Pos{1, 2}, ModeNormal, nil},
		{"enter on empty item ends list", "- ab\n- ", Pos{1, 1}, "a<CR>x<Esc>", "- ab\nx", Pos{1, 0}, ModeNormal, nil},
		{"enter renumbers", "1. a\n2. b", Pos{0, 3}, "a<CR>c<Esc>", "1. a\n2. c\n3. b", Pos{1, 3}, ModeNormal, nil},
		{"backspace", "abc", Pos{0, 2}, "i<BS><Esc>", "ac", Pos{0, 0}, ModeNormal, nil},
		{"backspace joins", "ab\ncd", Pos{1, 0}, "i<BS><Esc>", "abcd", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-w", "one two", Pos{0, 6}, "a<C-w><Esc>", "one ", Pos{0, 3}, ModeNormal, nil},
		{"ctrl-u", "one two", Pos{0, 6}, "a<C-u><Esc>", "", Pos{0, 0}, ModeNormal, nil},
		{"tab spaces", "ab", Pos{0, 1}, "i<Tab><Esc>", "a   b", Pos{0, 3}, ModeNormal, nil},
		{"autopair", "", Pos{0, 0}, "i(<Esc>", "()", Pos{0, 0}, ModeNormal, nil},
		{"autopair skip", "", Pos{0, 0}, "i(a)<Esc>", "(a)", Pos{0, 2}, ModeNormal, nil},
		{"autopair backspace", "", Pos{0, 0}, "i(<BS><Esc>", "", Pos{0, 0}, ModeNormal, nil},
		{"no autopair before word", "x", Pos{0, 0}, "i(<Esc>", "(x", Pos{0, 0}, ModeNormal, nil},
		{"insert escape jk", "", Pos{0, 0}, "iajk", "a", Pos{0, 0}, ModeNormal, func(o *Options) { o.InsertEscape = "jk" }},
		{"insert escape j alone", "", Pos{0, 0}, "iaj<Esc>", "aj", Pos{0, 1}, ModeNormal, func(o *Options) { o.InsertEscape = "jk" }},
		{"text replacement", "", Pos{0, 0}, "ia->b<Esc>", "a→b", Pos{0, 2}, ModeNormal, func(o *Options) { o.TextReplacementsEnabled = true }},
		{"R replace", "abcd", Pos{0, 1}, "Rxy<Esc>", "axyd", Pos{0, 2}, ModeNormal, nil},
		{"R replace past end", "ab", Pos{0, 1}, "Rxyz<Esc>", "axyz", Pos{0, 3}, ModeNormal, nil},
		{"R backspace restores", "abcd", Pos{0, 1}, "Rxy<BS><Esc>", "axcd", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-o one command", "abc def", Pos{0, 0}, "i<C-o>$X<Esc>", "abc defX", Pos{0, 7}, ModeNormal, nil},
		{"ctrl-r register", "abc", Pos{0, 0}, "yiwA <C-r>\"<Esc>", "abc abc", Pos{0, 6}, ModeNormal, nil},
		{"ctrl-n completion", "hello help\nhe", Pos{1, 1}, "A<C-n><Esc>", "hello help\nhello", Pos{1, 4}, ModeNormal, nil},
		{"ctrl-n twice", "hello help\nhe", Pos{1, 1}, "A<C-n><C-n><Esc>", "hello help\nhelp", Pos{1, 3}, ModeNormal, nil},
		{"ctrl-t indents", "ab", Pos{0, 1}, "i<C-t><Esc>", "    ab", Pos{0, 4}, ModeNormal, nil},
		{"shift-tab outdents", "    ab", Pos{0, 5}, "i<S-Tab><Esc>", "ab", Pos{0, 0}, ModeNormal, nil},
		{"tab indents list item", "- ab", Pos{0, 2}, "i<Tab><Esc>", "    - ab", Pos{0, 5}, ModeNormal, nil},
		{"arrows in insert", "abc", Pos{0, 0}, "i<Right><Right>X<Esc>", "abXc", Pos{0, 2}, ModeNormal, nil},
		{"3ix", "", Pos{0, 0}, "3ix<Esc>", "x", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestYankPut(t *testing.T) {
	run(t, []keyCase{
		{"yyp", "a\nb", Pos{0, 0}, "yyp", "a\na\nb", Pos{1, 0}, ModeNormal, nil},
		{"yyP", "a\nb", Pos{1, 0}, "yyP", "a\nb\nb", Pos{1, 0}, ModeNormal, nil},
		{"3yyP count", "a\nb\nc", Pos{0, 0}, "yy2P", "a\na\na\nb\nc", Pos{0, 0}, ModeNormal, nil},
		{"yw p", "one two", Pos{0, 0}, "ywP", "one one two", Pos{0, 3}, ModeNormal, nil},
		{"yiw p after", "one two", Pos{0, 0}, "yiwp", "oonene two", Pos{0, 3}, ModeNormal, nil},
		{"dd p", "a\nb\nc", Pos{0, 0}, "ddp", "b\na\nc", Pos{1, 0}, ModeNormal, nil},
		{"xp swap", "ab", Pos{0, 0}, "xp", "ba", Pos{0, 1}, ModeNormal, nil},
		{"named register", "one two", Pos{0, 0}, "\"ayiww\"aP", "one onetwo", Pos{0, 6}, ModeNormal, nil},
		{"append register", "one two", Pos{0, 0}, "\"ayiww\"Ayiw$\"ap", "one twoonetwo", Pos{0, 12}, ModeNormal, nil},
		{"yank 0 register survives delete", "one two", Pos{0, 0}, "yiwwdiw\"0P", "oneone ", Pos{0, 5}, ModeNormal, nil},
		{"blackhole", "one two", Pos{0, 0}, "yiww\"_dwP", "oneone ", Pos{0, 5}, ModeNormal, nil},
		{"gp cursor after", "a\nb", Pos{0, 0}, "yygp", "a\na\nb", Pos{2, 0}, ModeNormal, nil},
		{"Y is yy", "ab\ncd", Pos{0, 1}, "Yp", "ab\nab\ncd", Pos{1, 0}, ModeNormal, nil},
		{"p multi-line charwise", "ab\ncd", Pos{0, 0}, "vjy$p", "abab\nc\ncd", Pos{0, 2}, ModeNormal, nil},
	})
}

func TestVisual(t *testing.T) {
	run(t, []keyCase{
		{"vd", "abcdef", Pos{0, 1}, "vlld", "aef", Pos{0, 1}, ModeNormal, nil},
		{"vy", "abcdef", Pos{0, 1}, "vllyP", "abcdbcdef", Pos{0, 3}, ModeNormal, nil},
		{"Vd", "a\nb\nc", Pos{0, 0}, "Vjd", "c", Pos{0, 0}, ModeNormal, nil},
		{"vc", "one two", Pos{0, 0}, "vecX<Esc>", "X two", Pos{0, 0}, ModeNormal, nil},
		{"viw", "one two three", Pos{0, 5}, "viwd", "one  three", Pos{0, 4}, ModeNormal, nil},
		{"v o swap", "abcdef", Pos{0, 2}, "vlohd", "aef", Pos{0, 1}, ModeNormal, nil},
		{"v$", "abc\ndef", Pos{0, 1}, "v$d", "a\ndef", Pos{0, 0}, ModeNormal, nil},
		{"vU", "abc", Pos{0, 0}, "vlU", "ABc", Pos{0, 0}, ModeNormal, nil},
		{"vr", "abc", Pos{0, 0}, "vlrx", "xxc", Pos{0, 0}, ModeNormal, nil},
		{"vJ", "a\nb\nc", Pos{0, 0}, "VjJ", "a b\nc", Pos{0, 1}, ModeNormal, nil},
		{"v>", "a\nb", Pos{0, 0}, "Vj>", "    a\n    b", Pos{0, 4}, ModeNormal, nil},
		{"vp replaces", "one two", Pos{0, 0}, "yiwwviwp", "one one", Pos{0, 6}, ModeNormal, nil},
		{"gv", "abcdef", Pos{0, 1}, "vl<Esc>0gvd", "adef", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-v block delete", "abc\ndef\nghi", Pos{0, 1}, "<C-v>jld", "a\nd\nghi", Pos{0, 0}, ModeNormal, nil},
		{"ctrl-v block I", "abc\ndef", Pos{0, 1}, "<C-v>jIX<Esc>", "aXbc\ndXef", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-v block A", "abc\ndef", Pos{0, 1}, "<C-v>jAX<Esc>", "abXc\ndeXf", Pos{0, 2}, ModeNormal, nil},
		{"ctrl-v $A", "ab\ndefg", Pos{0, 0}, "<C-v>j$AX<Esc>", "abX\ndefgX", Pos{0, 2}, ModeNormal, nil},
		{"ctrl-v block y p", "abc\ndef", Pos{0, 0}, "<C-v>jly$p", "abcab\ndefde", Pos{0, 3}, ModeNormal, nil},
		{"v esc", "abc", Pos{0, 0}, "vl<Esc>", "abc", Pos{0, 1}, ModeNormal, nil},
		{"V then v", "abc", Pos{0, 1}, "Vvd", "ac", Pos{0, 1}, ModeNormal, nil},
		{"visual ip", "a\nb\n\nc", Pos{0, 0}, "vipd", "\nc", Pos{0, 0}, ModeNormal, nil},
		{"visual toggle checkbox", "a\nb", Pos{0, 0}, "Vj", "a\nb", Pos{1, 0}, ModeVisualLine, nil},
	})
}

func TestUndoRedoDot(t *testing.T) {
	run(t, []keyCase{
		{"u", "abc", Pos{0, 0}, "xu", "abc", Pos{0, 0}, ModeNormal, nil},
		{"u insert session", "abc", Pos{0, 0}, "ixyz<Esc>u", "abc", Pos{0, 0}, ModeNormal, nil},
		{"redo", "abc", Pos{0, 0}, "xu<C-r>", "bc", Pos{0, 0}, ModeNormal, nil},
		{"2u", "abcd", Pos{0, 0}, "xx2u", "abcd", Pos{0, 0}, ModeNormal, nil},
		{"dot x", "abcdef", Pos{0, 0}, "x.", "cdef", Pos{0, 0}, ModeNormal, nil},
		{"dot dw", "a b c d", Pos{0, 0}, "dw.", "c d", Pos{0, 0}, ModeNormal, nil},
		{"dot insert", "", Pos{0, 0}, "ix<Esc>.", "xx", Pos{0, 0}, ModeNormal, nil},
		{"dot A", "a\nb", Pos{0, 0}, "A!<Esc>j.", "a!\nb!", Pos{1, 1}, ModeNormal, nil},
		{"dot with count", "abcdef", Pos{0, 0}, "x3.", "ef", Pos{0, 0}, ModeNormal, nil},
		{"dot ciw", "one two", Pos{0, 0}, "ciwX<Esc>w.", "X X", Pos{0, 2}, ModeNormal, nil},
		{"dot o", "a", Pos{0, 0}, "ob<Esc>.", "a\nb\nb", Pos{2, 0}, ModeNormal, nil},
		{"dot dd count", "a\nb\nc\nd\ne", Pos{0, 0}, "2dd.", "e", Pos{0, 0}, ModeNormal, nil},
		{"macro", "a\nb\nc", Pos{0, 0}, "qaA!<Esc>jq@a", "a!\nb!\nc", Pos{2, 0}, ModeNormal, nil},
		{"macro count", "a\nb\nc\nd", Pos{0, 0}, "qaA!<Esc>jq2@a", "a!\nb!\nc!\nd", Pos{3, 0}, ModeNormal, nil},
		{"macro @@", "a\nb\nc\nd", Pos{0, 0}, "qaA!<Esc>jq@a@@", "a!\nb!\nc!\nd", Pos{3, 0}, ModeNormal, nil},
		{"U undo", "abc", Pos{0, 0}, "xU", "abc", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestSearchAndEx(t *testing.T) {
	run(t, []keyCase{
		{"search", "one two\nthree two", Pos{0, 0}, "/two<CR>", "one two\nthree two", Pos{0, 4}, ModeNormal, nil},
		{"search n", "one two\nthree two", Pos{0, 0}, "/two<CR>n", "one two\nthree two", Pos{1, 6}, ModeNormal, nil},
		{"search wraps", "one two\nthree two", Pos{0, 0}, "/two<CR>nn", "one two\nthree two", Pos{0, 4}, ModeNormal, nil},
		{"search backward", "one two\nthree two", Pos{0, 0}, "?two<CR>", "one two\nthree two", Pos{1, 6}, ModeNormal, nil},
		{"search N", "one two\nthree two", Pos{0, 0}, "/two<CR>N", "one two\nthree two", Pos{1, 6}, ModeNormal, nil},
		{"star", "foo bar foo", Pos{0, 0}, "*", "foo bar foo", Pos{0, 8}, ModeNormal, nil},
		{"star word boundary", "foo foobar foo", Pos{0, 0}, "*", "foo foobar foo", Pos{0, 11}, ModeNormal, nil},
		{"hash", "foo bar foo", Pos{0, 8}, "#", "foo bar foo", Pos{0, 0}, ModeNormal, nil},
		{"d/", "one two three", Pos{0, 0}, "d/three<CR>", "three", Pos{0, 0}, ModeNormal, nil},
		{"c/", "one two three", Pos{0, 0}, "c/three<CR>X <Esc>", "X three", Pos{0, 1}, ModeNormal, nil},
		{"y?", "one two three", Pos{0, 8}, "y?two<CR>P", "one two two three", Pos{0, 7}, ModeNormal, nil},
		{"d/ dot", "a x b x c x", Pos{0, 0}, "d/x<CR>..", "x", Pos{0, 0}, ModeNormal, nil},
		{"search vim word boundary", "cat concat cat", Pos{0, 0}, "/\\<cat\\><CR>", "cat concat cat", Pos{0, 11}, ModeNormal, nil},
		{"search esc restores", "one two", Pos{0, 0}, "/two<Esc>", "one two", Pos{0, 0}, ModeNormal, nil},
		{":s", "one two one", Pos{0, 0}, ":s/one/1/<CR>", "1 two one", Pos{0, 0}, ModeNormal, nil},
		{":s g", "one two one", Pos{0, 0}, ":s/one/1/g<CR>", "1 two 1", Pos{0, 0}, ModeNormal, nil},
		{":%s", "a\nb\na", Pos{0, 0}, ":%s/a/x/<CR>", "x\nb\nx", Pos{2, 0}, ModeNormal, nil},
		{":s groups", "john smith", Pos{0, 0}, ":s/\\(\\w\\+\\) \\(\\w\\+\\)/\\2 \\1/<CR>", "smith john", Pos{0, 0}, ModeNormal, nil},
		{":s ampersand", "abc", Pos{0, 0}, ":s/b/[&]/<CR>", "a[b]c", Pos{0, 0}, ModeNormal, nil},
		{":s case", "Abc abc", Pos{0, 0}, ":s/abc/x/gi<CR>", "x x", Pos{0, 0}, ModeNormal, nil},
		{":s newline", "a,b", Pos{0, 0}, ":s/,/\\r/<CR>", "a\nb", Pos{0, 0}, ModeNormal, nil},
		{":g/d", "keep\ndrop\nkeep2\ndrop", Pos{0, 0}, ":g/drop/d<CR>", "keep\nkeep2", Pos{1, 0}, ModeNormal, nil},
		{":v/d", "keep\ndrop\nkeep2", Pos{0, 0}, ":v/keep/d<CR>", "keep\nkeep2", Pos{1, 0}, ModeNormal, nil},
		{":d range", "a\nb\nc\nd", Pos{0, 0}, ":2,3d<CR>", "a\nd", Pos{1, 0}, ModeNormal, nil},
		{":N goto", "a\nb\nc", Pos{0, 0}, ":3<CR>", "a\nb\nc", Pos{2, 0}, ModeNormal, nil},
		{":m 0", "a\nb\nc", Pos{2, 0}, ":m 0<CR>", "c\na\nb", Pos{0, 0}, ModeNormal, nil},
		{":m $", "a\nb\nc", Pos{0, 0}, ":m $<CR>", "b\nc\na", Pos{2, 0}, ModeNormal, nil},
		{":t", "a\nb", Pos{0, 0}, ":t.<CR>", "a\na\nb", Pos{1, 0}, ModeNormal, nil},
		{":sort", "c\na\nb", Pos{0, 0}, ":sort<CR>", "a\nb\nc", Pos{0, 0}, ModeNormal, nil},
		{":sort!", "c\na\nb", Pos{0, 0}, ":sort!<CR>", "c\nb\na", Pos{0, 0}, ModeNormal, nil},
		{":normal", "a\nb", Pos{0, 0}, ":%norm A;<CR>", "a;\nb;", Pos{1, 1}, ModeNormal, nil},
		{":>", "a", Pos{0, 0}, ":><CR>", "    a", Pos{0, 4}, ModeNormal, nil},
		{":j", "a\nb\nc", Pos{0, 0}, ":j<CR>", "a b\nc", Pos{0, 1}, ModeNormal, nil},
		{":'<,'>", "a\nb\nc", Pos{0, 0}, "Vj:d<CR>", "c", Pos{0, 0}, ModeNormal, nil},
		{"& repeat", "aa\naa", Pos{0, 0}, ":s/a/x/<CR>j&", "xa\nxa", Pos{1, 0}, ModeNormal, nil},
		{":pu", "a\nb", Pos{0, 0}, "yy:pu<CR>", "a\na\nb", Pos{1, 0}, ModeNormal, nil},
		{"ctrl-r on cmdline", "word", Pos{0, 0}, "yiw:s/<C-r>\"/x/<CR>", "x", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestFolds(t *testing.T) {
	text := "# A\na1\na2\n# B\nb1"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.Feed("jzc")
	if end, ok := e.IsFoldStart(0); !ok || end != 2 {
		t.Fatalf("expected fold 0..2, got %d %v", end, ok)
	}
	if e.Cursor().Line != 0 {
		t.Fatalf("cursor should sit on fold start, got %d", e.Cursor().Line)
	}
	e.Feed("j")
	if e.Cursor().Line != 3 {
		t.Fatalf("j should skip the fold, got %d", e.Cursor().Line)
	}
	e.Feed("k")
	if e.Cursor().Line != 0 {
		t.Fatalf("k should land on fold start, got %d", e.Cursor().Line)
	}
	e.Feed("zo")
	if _, ok := e.IsFoldStart(0); ok {
		t.Fatal("zo should open")
	}
	e.Feed("zM")
	if len(e.Folds()) != 2 {
		t.Fatalf("zM should fold both sections, got %v", e.Folds())
	}
	e.Feed("zR")
	if len(e.Folds()) != 0 {
		t.Fatal("zR should open all")
	}
}

func TestRegistersAndMarks(t *testing.T) {
	e := New("hello world", DefaultOptions(), Hooks{})
	e.Feed("yiw")
	if reg, ok := e.RegisterText('0'); !ok || reg.Text != "hello" {
		t.Fatalf("yank register = %+v", reg)
	}
	e.Feed("dd")
	if reg, _ := e.RegisterText('1'); !reg.Linewise || reg.Text != "hello world\n" {
		t.Fatalf("delete register = %+v", reg)
	}
	e.Feed("u")
	e.Feed("dw")
	if reg, _ := e.RegisterText('-'); reg.Text != "hello " {
		t.Fatalf("small delete register = %+v", reg)
	}
}

func TestPlainMode(t *testing.T) {
	opts := DefaultOptions()
	opts.VimEnabled = false
	e := New("ab", opts, Hooks{})
	if e.Mode() != ModeInsert {
		t.Fatal("plain mode starts inserting")
	}
	e.Feed("x")
	e.HandleKey(KeyRight)
	e.Feed("y")
	if e.Text() != "xayb" {
		t.Fatalf("text = %q", e.Text())
	}
	e.HandleKey(KeyEsc)
	if e.Mode() != ModeInsert {
		t.Fatal("esc must not leave insert in plain mode")
	}
	if e.HandleKey(Ctrl('p')) {
		t.Fatal("unused keys should be reported unhandled")
	}
	e.HandleKey(Ctrl('z'))
	if e.Text() != "xa" && e.Text() != "xab" && e.Text() != "ab" {
		t.Fatalf("undo = %q", e.Text())
	}
}

func TestToggleCheckbox(t *testing.T) {
	e := New("plain\n- bullet\n- [ ] open\n- [x] done\n- [/] doing", DefaultOptions(), Hooks{})
	for i := 0; i < 5; i++ {
		e.SetCursor(Pos{i, 0})
		e.ToggleCheckboxAtCursor()
	}
	want := "- [ ] plain\n- [ ] bullet\n- [x] open\n- [ ] done\n- [x] doing"
	if e.Text() != want {
		t.Fatalf("got %q", e.Text())
	}
}

func TestScrolling(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	e := New(strings.Join(lines, "\n"), DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.Feed("<C-d>")
	if e.Cursor().Line != 5 || e.ScrollTop() != 5 {
		t.Fatalf("ctrl-d: cursor %d top %d", e.Cursor().Line, e.ScrollTop())
	}
	e.Feed("G")
	if e.ScrollTop() != 40 {
		t.Fatalf("G scroll top = %d", e.ScrollTop())
	}
	e.Feed("zt")
	if e.ScrollTop() != 49 {
		t.Fatalf("zt top = %d", e.ScrollTop())
	}
	e.Feed("gg<C-f>")
	if e.ScrollTop() != 8 {
		t.Fatalf("ctrl-f top = %d", e.ScrollTop())
	}
	e.Feed("gg")
	e.Feed("L")
	if e.Cursor().Line != 9 {
		t.Fatalf("L = %d", e.Cursor().Line)
	}
	e.Feed("M")
	if e.Cursor().Line != 4 {
		t.Fatalf("M = %d", e.Cursor().Line)
	}
}

func TestHostExCommand(t *testing.T) {
	var got ExCommand
	e := New("x", DefaultOptions(), Hooks{ExCommand: func(cmd ExCommand) (bool, error) {
		got = cmd
		return true, nil
	}})
	e.Feed(":wq!<CR>")
	if got.Name != "wq" || !got.Bang {
		t.Fatalf("host got %+v", got)
	}
	e.Feed(":move inbox/Work<CR>")
	if got.Name != "move" || got.Args != "inbox/Work" {
		t.Fatalf("host got %+v", got)
	}
	e.Feed("ZZ")
	if got.Name != "wq" {
		t.Fatalf("ZZ got %+v", got)
	}
}
