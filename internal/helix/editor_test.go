package helix

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
		{"0", text, Pos{0, 5}, "0", text, Pos{0, 0}, ModeNormal, nil},
		{"$", text, Pos{0, 0}, "$", text, Pos{0, 12}, ModeNormal, nil},
		{"$ then j sticks", text, Pos{0, 0}, "$j", text, Pos{1, 8}, ModeNormal, nil},
		{"^", "   x y", Pos{0, 5}, "^", "   x y", Pos{0, 3}, ModeNormal, nil},
		{"gg", text, Pos{3, 2}, "gg", text, Pos{0, 0}, ModeNormal, nil},
		{"ge last line", text, Pos{0, 0}, "ge", text, Pos{4, 0}, ModeNormal, nil},
		{"G", text, Pos{0, 0}, "G", text, Pos{4, 0}, ModeNormal, nil},
		{"2G", text, Pos{0, 0}, "2G", text, Pos{1, 0}, ModeNormal, nil},
		{"3gg", text, Pos{0, 0}, "3gg", text, Pos{2, 0}, ModeNormal, nil},
		{"g h line start", "  a b", Pos{0, 4}, "gh", "  a b", Pos{0, 0}, ModeNormal, nil},
		{"g l line end", "  a b", Pos{0, 0}, "gl", "  a b", Pos{0, 4}, ModeNormal, nil},
		{"g s first non-blank", "  a b", Pos{0, 4}, "gs", "  a b", Pos{0, 2}, ModeNormal, nil},
		{"f", text, Pos{0, 0}, "ft", text, Pos{0, 4}, ModeNormal, nil},
		{"2f", text, Pos{0, 0}, "2ft", text, Pos{0, 8}, ModeNormal, nil},
		{"t", text, Pos{0, 0}, "tt", text, Pos{0, 3}, ModeNormal, nil},
		{", repeats find backwards", text, Pos{0, 0}, "ft,,", text, Pos{0, 4}, ModeNormal, nil},
		{"f is document wide", "ab\ncd", Pos{0, 0}, "fd", "ab\ncd", Pos{1, 1}, ModeNormal, nil},
		{"F", text, Pos{0, 12}, "Fo", text, Pos{0, 6}, ModeNormal, nil},
		{"T", text, Pos{0, 12}, "To", text, Pos{0, 7}, ModeNormal, nil},
		{"f fail stays", text, Pos{0, 0}, "fz", text, Pos{0, 0}, ModeNormal, nil},
		{"m m bracket", text, Pos{3, 4}, "mm", text, Pos{3, 10}, ModeNormal, nil},
		{"m m back", text, Pos{3, 10}, "mm", text, Pos{3, 4}, ModeNormal, nil},
		{"m m from before", text, Pos{3, 0}, "mm", text, Pos{3, 10}, ModeNormal, nil},
		{"}", text, Pos{0, 0}, "}", text, Pos{2, 0}, ModeNormal, nil},
		{"{", text, Pos{3, 3}, "{", text, Pos{2, 0}, ModeNormal, nil},
		{"| column", text, Pos{0, 0}, "5|", text, Pos{0, 4}, ModeNormal, nil},
		{"enter first nonblank", "a\n  b", Pos{0, 0}, "<CR>", "a\n  b", Pos{1, 2}, ModeNormal, nil},
		{"- up", "  a\nb", Pos{1, 0}, "-", "  a\nb", Pos{0, 2}, ModeNormal, nil},
		{"]] heading", "x\n# A\ny\n## B\n", Pos{0, 0}, "]]", "x\n# A\ny\n## B\n", Pos{1, 0}, ModeNormal, nil},
		{"2]] heading", "x\n# A\ny\n## B\n", Pos{0, 0}, "2]]", "x\n# A\ny\n## B\n", Pos{3, 0}, ModeNormal, nil},
		{"[[ heading", "x\n# A\ny\n## B\n", Pos{3, 0}, "[[", "x\n# A\ny\n## B\n", Pos{1, 0}, ModeNormal, nil},
		{"( sentence", "One. Two. Three.", Pos{0, 12}, "(", "One. Two. Three.", Pos{0, 10}, ModeNormal, nil},
		{") sentence", "One. Two. Three.", Pos{0, 0}, ")", "One. Two. Three.", Pos{0, 5}, ModeNormal, nil},
	})
}

func TestSelectionOperations(t *testing.T) {
	run(t, []keyCase{
		// A collapsed selection is one character: d deletes it, like vim x.
		{"d deletes a char", "abc", Pos{0, 1}, "d", "ac", Pos{0, 1}, ModeNormal, nil},
		{"3d deletes three", "abcdef", Pos{0, 1}, "3d", "acdef", Pos{0, 1}, ModeNormal, nil},
		{"d at end clamps", "abc", Pos{0, 2}, "d", "ab", Pos{0, 1}, ModeNormal, nil},
		{"w d deletes a word", "one two three", Pos{0, 0}, "wd", "two three", Pos{0, 0}, ModeNormal, nil},
		{"e d deletes to word end", "one two", Pos{0, 0}, "ed", " two", Pos{0, 0}, ModeNormal, nil},
		{"w d twice", "one two three", Pos{0, 0}, "wdd", "wo three", Pos{0, 0}, ModeNormal, nil},
		// x selects the line; d then deletes it.
		{"x d deletes the line", "a\nb\nc", Pos{1, 0}, "xd", "a\nc", Pos{1, 0}, ModeNormal, nil},
		{"x x d deletes two lines", "a\nb\nc\nd", Pos{1, 0}, "xxd", "a\nd", Pos{1, 0}, ModeNormal, nil},
		{"x d last line", "a\nb", Pos{1, 0}, "xd", "a", Pos{0, 0}, ModeNormal, nil},
		{"x d only line", "abc", Pos{0, 1}, "xd", "", Pos{0, 0}, ModeNormal, nil},
		// X extends to line bounds.
		{"X d line bounds", "one two", Pos{0, 3}, "Xd", "", Pos{0, 0}, ModeNormal, nil},
		// Selecting with motions then operating.
		{"v l l d", "abcdef", Pos{0, 1}, "vlld", "aef", Pos{0, 1}, ModeNormal, nil},
		{"v $ d", "abc\ndef", Pos{0, 1}, "v$d", "a\ndef", Pos{0, 0}, ModeNormal, nil},
		{"v j d", "a\nb\nc", Pos{0, 0}, "vjd", "\nc", Pos{0, 0}, ModeNormal, nil},
		{"V j d", "a\nb\nc", Pos{0, 0}, "Vjd", "c", Pos{0, 0}, ModeNormal, nil},
		{"v e c", "one two", Pos{0, 0}, "vecX<Esc>", "X two", Pos{0, 0}, ModeNormal, nil},
		{"v l l y P", "abcdef", Pos{0, 1}, "vllyP", "abcdbcdef", Pos{0, 3}, ModeNormal, nil},
		{"v o swaps", "abcdef", Pos{0, 2}, "vlohd", "aef", Pos{0, 1}, ModeNormal, nil},
		{"v U uppercase", "ABc", Pos{0, 0}, "vl`", "abc", Pos{0, 0}, ModeNormal, nil},
		{"v ~ toggles", "Abc", Pos{0, 0}, "vl~", "aBc", Pos{0, 0}, ModeNormal, nil},
		{"v r", "abc", Pos{0, 0}, "vlrx", "xxc", Pos{0, 0}, ModeNormal, nil},
		{"V j J", "a\nb\nc", Pos{0, 0}, "VjJ", "a b\nc", Pos{0, 1}, ModeNormal, nil},
		{"V j >", "a\nb", Pos{0, 0}, "Vj>", "    a\n    b", Pos{0, 4}, ModeNormal, nil},
		{"v R replaces with yank", "one two", Pos{0, 0}, "miwyvR", "onene two", Pos{0, 2}, ModeSelect, nil},
		{"; collapses", "abcdef", Pos{0, 1}, "vll;d", "abcef", Pos{0, 3}, ModeNormal, nil},
		{"% selects all", "a\nb", Pos{0, 0}, "%d", "", Pos{0, 0}, ModeNormal, nil},
		{"gv reselects", "abcdef", Pos{0, 1}, "vl<Esc>0gvd", "adef", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-v block delete", "abc\ndef\nghi", Pos{0, 1}, "<C-v>jld", "a\nd\nghi", Pos{0, 0}, ModeNormal, nil},
		{"ctrl-v block I", "abc\ndef", Pos{0, 1}, "<C-v>jIX<Esc>", "aXbc\ndXef", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-v block A", "abc\ndef", Pos{0, 1}, "<C-v>jAX<Esc>", "abXc\ndeXf", Pos{0, 2}, ModeNormal, nil},
		{"ctrl-v $A", "ab\ndefg", Pos{0, 0}, "<C-v>j$AX<Esc>", "abX\ndefgX", Pos{0, 2}, ModeNormal, nil},
		{"ctrl-v block y p", "abc\ndef", Pos{0, 0}, "<C-v>jly$p", "abcab\ndefde", Pos{0, 3}, ModeNormal, nil},
		{"esc leaves select", "abc", Pos{0, 0}, "vl<Esc>", "abc", Pos{0, 1}, ModeNormal, nil},
		{"V then v narrows", "abc", Pos{0, 1}, "Vvd", "ac", Pos{0, 1}, ModeNormal, nil},
		{"v m i p", "a\nb\n\nc", Pos{0, 0}, "vmipd", "\nc", Pos{0, 0}, ModeNormal, nil},
		// Match mode text objects select, then d deletes.
		{"m i w d", "one two three", Pos{0, 5}, "miwd", "one  three", Pos{0, 4}, ModeNormal, nil},
		{"m a w d", "one two three", Pos{0, 5}, "mawd", "one three", Pos{0, 4}, ModeNormal, nil},
		{"m a w at end", "one two", Pos{0, 5}, "mawd", "one", Pos{0, 2}, ModeNormal, nil},
		{"m i ( d", "f(a, b) c", Pos{0, 3}, "mi(d", "f() c", Pos{0, 2}, ModeNormal, nil},
		{"m a ( d", "f(a, b) c", Pos{0, 3}, "ma(d", "f c", Pos{0, 1}, ModeNormal, nil},
		{"m i ( on bracket", "f(a, b) c", Pos{0, 1}, "mi(d", "f() c", Pos{0, 2}, ModeNormal, nil},
		{"m i [ nested", "[a [b] c]", Pos{0, 4}, "mi[d", "[a [] c]", Pos{0, 4}, ModeNormal, nil},
		{"2 m i [", "[a [b] c]", Pos{0, 4}, "2mi[d", "[a [] c]", Pos{0, 4}, ModeNormal, nil},
		{"m i { multiline", "{\n  a\n  b\n}", Pos{1, 1}, "mi{d", "{\n}", Pos{1, 0}, ModeNormal, nil},
		{"m i quote", `say "hello world" now`, Pos{0, 7}, `mi"d`, `say "" now`, Pos{0, 5}, ModeNormal, nil},
		{"m a quote", `say "hello world" now`, Pos{0, 7}, `ma"d`, `say now`, Pos{0, 4}, ModeNormal, nil},
		{"m i t", "<b>bold</b> x", Pos{0, 4}, "mitd", "<b></b> x", Pos{0, 3}, ModeNormal, nil},
		{"m a t", "<b>bold</b> x", Pos{0, 4}, "matd", " x", Pos{0, 0}, ModeNormal, nil},
		{"m i p", "a\nb\n\nc", Pos{0, 0}, "mipd", "\nc", Pos{0, 0}, ModeNormal, nil},
		{"m a p", "a\nb\n\nc", Pos{0, 0}, "mapd", "c", Pos{0, 0}, ModeNormal, nil},
		{"m i w c", "one two three", Pos{0, 5}, "miwcX<Esc>", "one X three", Pos{0, 4}, ModeNormal, nil},
		{"c changes selection", "one two", Pos{0, 0}, "wcX<Esc>", "Xtwo", Pos{0, 0}, ModeNormal, nil},
		{"C changes to line end", "one two", Pos{0, 3}, "C!<Esc>", "one!", Pos{0, 3}, ModeNormal, nil},
		{"r replaces the selection", "abc", Pos{0, 1}, "rx", "axc", Pos{0, 1}, ModeNormal, nil},
		{"3r replaces three", "abcd", Pos{0, 1}, "3rx", "axcd", Pos{0, 1}, ModeNormal, nil},
		{"3r too short", "abc", Pos{0, 1}, "5rx", "axc", Pos{0, 1}, ModeNormal, nil},
		{"r enter", "ab cd", Pos{0, 2}, "r<CR>", "ab cd", Pos{0, 2}, ModeNormal, nil},
		{"~ toggles the selection", "abc", Pos{0, 0}, "~", "Abc", Pos{0, 0}, ModeNormal, nil},
		{">> indents the line", "a\nb", Pos{0, 0}, ">>", "        a\nb", Pos{0, 8}, ModeNormal, nil},
		{"2>> indents two lines", "a\nb", Pos{0, 0}, "2>>", "        a\nb", Pos{0, 8}, ModeNormal, nil},
		{"<< outdents", "    a", Pos{0, 4}, "<<", "a", Pos{0, 0}, ModeNormal, nil},
		{"J joins lines", "a\n  b\nc", Pos{0, 0}, "J", "a b\nc", Pos{0, 1}, ModeNormal, nil},
		{"3J joins three", "a\nb\nc\nd", Pos{0, 0}, "3J", "a b\nc\nd", Pos{0, 1}, ModeNormal, nil},
		{"ctrl-a", "x 41 y", Pos{0, 0}, "<C-a>", "x 42 y", Pos{0, 3}, ModeNormal, nil},
		{"5 ctrl-x", "x 41 y", Pos{0, 0}, "5<C-x>", "x 36 y", Pos{0, 3}, ModeNormal, nil},
		{"ctrl-a negative", "x -1 y", Pos{0, 0}, "2<C-a>", "x 1 y", Pos{0, 2}, ModeNormal, nil},
		{"o list continues", "- item", Pos{0, 3}, "onext<Esc>", "- item\n- next", Pos{1, 5}, ModeNormal, nil},
		{"o numbered continues", "1. item", Pos{0, 3}, "ox<Esc>", "1. item\n2. x", Pos{1, 3}, ModeNormal, nil},
		{"o checkbox continues", "- [x] done", Pos{0, 3}, "ox<Esc>", "- [x] done\n- [ ] x", Pos{1, 6}, ModeNormal, nil},
		{"O above", "  abc", Pos{0, 3}, "Ox<Esc>", "  x\n  abc", Pos{0, 2}, ModeNormal, nil},
		{"g q joins paragraph", "one\ntwo\n\nthree", Pos{0, 0}, "miqgq", "one\ntwo\n\nthree", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestPutSelection(t *testing.T) {
	run(t, []keyCase{
		{"y i w P", "one two", Pos{0, 0}, "miwyP", "oneone two", Pos{0, 2}, ModeNormal, nil},
		{"y i w p", "one two", Pos{0, 0}, "miwyp", "oonene two", Pos{0, 3}, ModeNormal, nil},
		{"x y p pastes below", "a\nb", Pos{1, 0}, "xyp", "a\nb\nb", Pos{2, 0}, ModeNormal, nil},
		{"x y P pastes above", "a\nb", Pos{1, 0}, "xyP", "a\nb\nb", Pos{1, 0}, ModeNormal, nil},
		{"x d p moves the line", "a\nb\nc", Pos{0, 0}, "xdp", "b\na\nc", Pos{1, 0}, ModeNormal, nil},
		{"named register", "one two", Pos{0, 0}, "x\"ayp", "one two\none two", Pos{1, 0}, ModeNormal, nil},
		{"append register", "one two", Pos{0, 0}, "x\"ay", "one two", Pos{0, 0}, ModeNormal, nil},
		{"0 register survives delete", "one two", Pos{0, 0}, "miwyxwd\"0P", "one", Pos{0, 2}, ModeNormal, nil},
		{"blackhole", "one two", Pos{0, 0}, "x\"_dp", "", Pos{0, 0}, ModeNormal, nil},
		{"R replaces with yank", "one two", Pos{0, 0}, "miwyR", "onene two", Pos{0, 2}, ModeNormal, nil},
		{"p multi-line charwise", "ab\ncd", Pos{0, 0}, "vjy$p", "abab\nc\ncd", Pos{0, 2}, ModeNormal, nil},
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
		{"ctrl-o one command", "abc def", Pos{0, 0}, "i<C-o>$X<Esc>", "abc defX", Pos{0, 7}, ModeNormal, nil},
		{"ctrl-r register", "abc", Pos{0, 0}, "miwyA <C-r>\"<Esc>", "abc abc", Pos{0, 6}, ModeNormal, nil},
		{"ctrl-n completion", "hello help\nhe", Pos{1, 1}, "A<C-n><Esc>", "hello help\nhello", Pos{1, 4}, ModeNormal, nil},
		{"ctrl-n twice", "hello help\nhe", Pos{1, 1}, "A<C-n><C-n><Esc>", "hello help\nhelp", Pos{1, 3}, ModeNormal, nil},
		{"ctrl-t indents", "ab", Pos{0, 1}, "i<C-t><Esc>", "    ab", Pos{0, 4}, ModeNormal, nil},
		{"shift-tab outdents", "    ab", Pos{0, 5}, "i<S-Tab><Esc>", "ab", Pos{0, 0}, ModeNormal, nil},
		{"tab indents list item", "- ab", Pos{0, 2}, "i<Tab><Esc>", "    - ab", Pos{0, 5}, ModeNormal, nil},
		{"arrows in insert", "abc", Pos{0, 0}, "i<Right><Right>X<Esc>", "abXc", Pos{0, 2}, ModeNormal, nil},
		{"3ix", "", Pos{0, 0}, "3ix<Esc>", "x", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestSurround(t *testing.T) {
	run(t, []keyCase{
		{"m s ) wraps the char", "abc", Pos{0, 1}, "ms)", "a(b)c", Pos{0, 1}, ModeNormal, nil},
		{"m s } wraps a line selection", "abc", Pos{0, 0}, "xms}", "{abc}", Pos{0, 0}, ModeNormal, nil},
		{"m s quote", "abc", Pos{0, 1}, `ms"`, `a"b"c`, Pos{0, 1}, ModeNormal, nil},
		{"m d ) deletes the pair", "a(b)c", Pos{0, 2}, "md)", "abc", Pos{0, 1}, ModeNormal, nil},
		{"m d quote", `a"b"c`, Pos{0, 2}, `md"`, "abc", Pos{0, 1}, ModeNormal, nil},
		{"m r ) ] replaces the pair", "a(b)c", Pos{0, 2}, "mr)]", "a[b]c", Pos{0, 1}, ModeNormal, nil},
	})
}

func TestUndoRedoDot(t *testing.T) {
	run(t, []keyCase{
		{"u", "abc", Pos{0, 0}, "du", "abc", Pos{0, 0}, ModeNormal, nil},
		{"u insert session", "abc", Pos{0, 0}, "ixyz<Esc>u", "abc", Pos{0, 0}, ModeNormal, nil},
		{"redo", "abc", Pos{0, 0}, "du<C-r>", "bc", Pos{0, 0}, ModeNormal, nil},
		{"U redo", "abc", Pos{0, 0}, "duU", "bc", Pos{0, 0}, ModeNormal, nil},
		{"2u", "abcd", Pos{0, 0}, "dd2u", "abcd", Pos{0, 0}, ModeNormal, nil},
		{"dot d", "abcdef", Pos{0, 0}, "d.", "cdef", Pos{0, 0}, ModeNormal, nil},
		{"dot insert", "", Pos{0, 0}, "ix<Esc>.", "xx", Pos{0, 0}, ModeNormal, nil},
		{"dot A", "a\nb", Pos{0, 0}, "A!<Esc>j.", "a!\nb!", Pos{1, 1}, ModeNormal, nil},
		{"dot with count", "abcdef", Pos{0, 0}, "dd.", "def", Pos{0, 0}, ModeNormal, nil},
		{"dot o", "a", Pos{0, 0}, "ob<Esc>.", "a\nb\nb", Pos{2, 0}, ModeNormal, nil},
		{"Q records, q replays", "a\nb\nc", Pos{0, 0}, "QA!<Esc>jQjq", "a!\nb\nc!", Pos{2, 1}, ModeNormal, nil},
		{"macro count", "a\nb\nc\nd", Pos{0, 0}, "QA!<Esc>jQ2q", "a!\nb!\nc!\nd", Pos{3, 0}, ModeNormal, nil},
		{"macro register prefix", "a\nb", Pos{0, 0}, "\"zQA!<Esc>jQ\"zq", "a!\nb!", Pos{1, 1}, ModeNormal, nil},
	})
}

func TestSearchAndEx(t *testing.T) {
	run(t, []keyCase{
		{"search", "one two\nthree two", Pos{0, 0}, "/two<CR>", "one two\nthree two", Pos{0, 4}, ModeNormal, nil},
		{"search n", "one two\nthree two", Pos{0, 0}, "/two<CR>n", "one two\nthree two", Pos{1, 8}, ModeNormal, nil},
		{"search wraps", "one two\nthree two", Pos{0, 0}, "/two<CR>nn", "one two\nthree two", Pos{0, 6}, ModeNormal, nil},
		{"search backward", "one two\nthree two", Pos{0, 0}, "?two<CR>", "one two\nthree two", Pos{1, 6}, ModeNormal, nil},
		{"search N", "one two\nthree two", Pos{0, 0}, "/two<CR>N", "one two\nthree two", Pos{1, 8}, ModeNormal, nil},
		{"search esc restores", "one two", Pos{0, 0}, "/two<Esc>", "one two", Pos{0, 0}, ModeNormal, nil},
		{"star selects the word then searches", "foo bar foo", Pos{0, 0}, "miw*", "foo bar foo", Pos{0, 2}, ModeSelect, nil},
		{"star searches the selection", "foo foobar foo", Pos{0, 0}, "miw*", "foo foobar foo", Pos{0, 2}, ModeSelect, nil},
		{"hash searches the word back", "foo bar foo", Pos{0, 8}, "#", "foo bar foo", Pos{0, 0}, ModeNormal, nil},
		{"search vim word boundary", "cat concat cat", Pos{0, 0}, "/\\<cat\\><CR>", "cat concat cat", Pos{0, 11}, ModeNormal, nil},
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
		{":pu", "a\nb", Pos{0, 0}, "xy:pu<CR>", "a\na\nb", Pos{1, 0}, ModeNormal, nil},
		{"ctrl-r on cmdline", "word", Pos{0, 0}, "miwy:s/<C-r>\"/x/<CR>", "x", Pos{0, 0}, ModeNormal, nil},
	})
}

func TestCursorFollowsPaste(t *testing.T) {
	// p on a collapsed selection pastes after the character under the
	// cursor, the Helix behaviour.
	e := New("ab", DefaultOptions(), Hooks{})
	e.Feed("xp")
	if e.Text() != "ab" || e.Cursor() != (Pos{0, 1}) {
		t.Fatalf("p after the cursor: %q %+v", e.Text(), e.Cursor())
	}
}

func TestFolds(t *testing.T) {
	text := "# A\na1\na2\n# B\nb1"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.Feed("jzf")
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
	e.Feed("miwy")
	if reg, ok := e.RegisterText('0'); !ok || reg.Text != "hello" {
		t.Fatalf("yank register = %+v", reg)
	}
	e.Feed("xd")
	if reg, _ := e.RegisterText('1'); !reg.Linewise || reg.Text != "hello world\n" {
		t.Fatalf("delete register = %+v", reg)
	}
	e.Feed("u")
	e.Feed("wd")
	if reg, _ := e.RegisterText('-'); reg.Text != "d" && reg.Text != "hello " {
		t.Fatalf("small delete register = %+v", reg)
	}
}

func TestPlainMode(t *testing.T) {
	opts := DefaultOptions()
	opts.HelixEnabled = false
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
	// Helix view mode: z c centres without moving the cursor.
	e.Feed("ggzc")
	if e.Cursor().Line != 0 {
		t.Fatalf("zc moved the cursor to %d", e.Cursor().Line)
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

func TestSelectionKindAndRange(t *testing.T) {
	e := New("abc\ndef", DefaultOptions(), Hooks{})
	if k := e.SelectionKind(); k != "v" {
		t.Fatalf("collapsed selection kind = %q", k)
	}
	e.Feed("x")
	if k := e.SelectionKind(); k != "V" {
		t.Fatalf("x selection kind = %q", k)
	}
	start, end, mode, ok := e.VisualRange()
	if !ok || mode != ModeSelect || start != (Pos{0, 0}) || end != (Pos{0, 2}) {
		t.Fatalf("x range = %+v..%+v %v %v", start, end, mode, ok)
	}
	e.Feed("<C-v>")
	if k := e.SelectionKind(); k != "^V" {
		t.Fatalf("ctrl-v selection kind = %q", k)
	}
}

func TestReplaceAtCursorJoinsTheInsert(t *testing.T) {
	opts := DefaultOptions()
	opts.TextReplacementsEnabled = false
	e := New("see  end", opts, Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 3})
	e.Feed("a#pro")
	e.ReplaceAtCursor(4, 0, "#project")
	if got, cur := e.Text(), e.Cursor(); got != "see #project end" || cur != (Pos{0, 12}) || e.Mode() != ModeInsert {
		t.Fatalf("after the completion: %q %+v %v", got, cur, e.Mode())
	}
	// The runes right of the cursor go too, clamped to the line.
	e.ReplaceAtCursor(0, 99, "!")
	if got := e.Text(); got != "see #project!" {
		t.Fatalf("replacing to the end: %q", got)
	}
	// One undo takes the typing and both replacements.
	e.Feed("<Esc>u")
	if got := e.Text(); got != "see  end" {
		t.Fatalf("after undo: %q", got)
	}
}

func TestSelectorMotions(t *testing.T) {
	// Helix motions are selection-first: word, find and search motions
	// replace the selection with the span they traverse, so actions work on
	// the selection ("wd" deletes a word). Plain character and line moves
	// collapse the selection onto their target.
	run(t, []keyCase{
		{"w d deletes a word", "one two three", Pos{0, 0}, "wd", "two three", Pos{0, 0}, ModeNormal, nil},
		{"2w d", "one two three", Pos{0, 0}, "2wd", "three", Pos{0, 0}, ModeNormal, nil},
		{"w d w continues", "one two three", Pos{0, 0}, "wdw", "two three", Pos{0, 4}, ModeNormal, nil},
		{"b d deletes backwards", "one two", Pos{0, 4}, "bd", "two", Pos{0, 0}, ModeNormal, nil},
		{"e d deletes the word", "one two", Pos{0, 0}, "ed", " two", Pos{0, 0}, ModeNormal, nil},
		{"f d selects through the char", "banana", Pos{0, 0}, "fad", "nana", Pos{0, 0}, ModeNormal, nil},
		{"; collapses then d deletes one", "hello world", Pos{0, 0}, "w;d", "hello orld", Pos{0, 6}, ModeNormal, nil},
		{"l still moves one char", "abcdef", Pos{0, 0}, "l", "abcdef", Pos{0, 1}, ModeNormal, nil},
		{"w then j collapses", "a\nb", Pos{0, 0}, "wj", "a\nb", Pos{1, 0}, ModeNormal, nil},
		{"i inserts before the selection", "hello world", Pos{0, 0}, "wiX<Esc>", "Xhello world", Pos{0, 0}, ModeNormal, nil},
		{"a appends after the selection", "hello world", Pos{0, 0}, "waX<Esc>", "hello wXorld", Pos{0, 7}, ModeNormal, nil},
		{"v keeps a selector selection", "hello world", Pos{0, 0}, "wvd", "orld", Pos{0, 0}, ModeNormal, nil},
	})

	// `w` leaves a spanning selection that renders even in normal mode.
	e := New("hello world foo", DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{0, 0})
	e.Feed("w")
	start, end, mode, ok := e.VisualRange()
	if !ok || mode != ModeNormal || start != (Pos{0, 0}) || end != (Pos{0, 5}) {
		t.Fatalf("w selection = %v..%v ok=%t mode=%v", start, end, ok, mode)
	}
	// repeated w re-anchors from the head: one hop at a time
	e.Feed("w")
	start, end, _, _ = e.VisualRange()
	if start != (Pos{0, 6}) || end != (Pos{0, 11}) {
		t.Fatalf("second w selection = %v..%v", start, end)
	}
	// `;` collapses onto the cursor and clears the highlight
	e.Feed(";")
	if _, _, _, ok := e.VisualRange(); ok {
		t.Fatal("; left a selection behind")
	}
	// `n` selects the whole match
	e2 := New("cat dog cat", DefaultOptions(), Hooks{})
	e2.SetViewport(20)
	e2.Feed("/cat<CR>n")
	start, end, _, ok = e2.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 2}) || e2.Cursor() != (Pos{0, 2}) {
		t.Fatalf("n selection = %v..%v ok=%t cursor=%+v", start, end, ok, e2.Cursor())
	}
}
