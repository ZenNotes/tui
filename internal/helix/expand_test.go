package helix

import "testing"

// The ladder a selection grows through: the word, the paragraph, the
// heading section and the note. Every context holds the one before it, so
// growing and shrinking walk the same rungs.
const expandText = "# Title\n\nfirst paragraph here\nsecond line of it\n\n## Section\n\n- one\n- two\n"

func TestExpandSelectionGrowsThroughTheContexts(t *testing.T) {
	e := New(expandText, DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{2, 6})
	want := []struct {
		start, end Pos
		label      string
	}{
		{Pos{2, 6}, Pos{2, 14}, "word"},
		{Pos{2, 0}, Pos{3, 16}, "paragraph"},
		{Pos{0, 0}, Pos{4, 0}, "section"},
		{Pos{0, 0}, Pos{9, 0}, "note"},
	}
	for i, w := range want {
		e.Feed("<a-o>")
		start, end, _, ok := e.VisualRange()
		if !ok || start != w.start || end != w.end {
			t.Fatalf("step %d: selection %v..%v ok=%t, want %v..%v", i, start, end, ok, w.start, w.end)
		}
		if e.msg != "selection: "+w.label {
			t.Fatalf("step %d: message %q, want %q", i, e.msg, w.label)
		}
	}
	// The note is the top of the ladder: growing again changes nothing.
	e.Feed("<a-o>")
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{9, 0}) {
		t.Fatalf("growing past the note: %v..%v ok=%t", start, end, ok)
	}
}

func TestShrinkSelectionRestoresThePreviousSelection(t *testing.T) {
	e := New(expandText, DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{2, 6})
	// Grow to the word, then to the paragraph.
	e.Feed("<a-o><a-o>")
	// The shrink walks back down the ladder.
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{2, 0}) || end != (Pos{3, 16}) {
		t.Fatalf("grown selection = %v..%v ok=%t", start, end, ok)
	}
	e.Feed("<a-i>")
	start, end, _, ok = e.VisualRange()
	if !ok || start != (Pos{2, 6}) || end != (Pos{2, 14}) {
		t.Fatalf("shrunk selection = %v..%v ok=%t, want the word", start, end, ok)
	}
	// The last step puts the cursor back where the growth started.
	e.Feed("<a-i>")
	if _, _, _, ok := e.VisualRange(); ok {
		t.Fatal("the shrink left a selection behind")
	}
	if e.Cursor() != (Pos{2, 6}) {
		t.Fatalf("cursor after the shrink: %+v", e.Cursor())
	}
}

func TestShrinkSelectionWithoutAStackTakesTheContextInside(t *testing.T) {
	// A selection made by hand has no stack: the shrink falls to the
	// largest context inside it.
	e := New("one two three\n", DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{0, 0})
	e.Feed("v")
	e.Feed("llllll")
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 6}) {
		t.Fatalf("handmade selection = %v..%v ok=%t", start, end, ok)
	}
	// The word at the start of the selection.
	e.Feed("<a-i>")
	start, end, _, ok = e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 2}) {
		t.Fatalf("shrunk to %v..%v ok=%t, want the first word", start, end, ok)
	}
	// Then the character under it.
	e.Feed("<a-i>")
	start, end, _, ok = e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 0}) {
		t.Fatalf("shrunk to %v..%v ok=%t, want the first character", start, end, ok)
	}
}

func TestExpandSelectionInSelectMode(t *testing.T) {
	e := New(expandText, DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{2, 6})
	e.Feed("v")
	e.Feed("<a-o>")
	start, end, mode, ok := e.VisualRange()
	if !ok || mode != ModeSelect || start != (Pos{2, 6}) || end != (Pos{2, 14}) {
		t.Fatalf("grown selection = %v..%v ok=%t mode=%v", start, end, ok, mode)
	}
	// The shrink puts the character under the cursor back.
	e.Feed("<a-i>")
	start, end, _, ok = e.VisualRange()
	if !ok || start != (Pos{2, 6}) || end != (Pos{2, 6}) {
		t.Fatalf("shrunk selection = %v..%v ok=%t", start, end, ok)
	}
}

func TestExpandSelectionInCode(t *testing.T) {
	text := "intro\n\n```go\nfunc add(a int) int {\n\treturn a + 1\n}\n```\n\nafter\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{4, 2})
	want := []struct {
		start, end Pos
		label      string
	}{
		{Pos{4, 1}, Pos{4, 6}, "word"},
		{Pos{3, 0}, Pos{5, 0}, "function"},
		{Pos{2, 0}, Pos{6, 2}, "code block"},
		{Pos{0, 0}, Pos{9, 0}, "note"},
	}
	for i, w := range want {
		e.Feed("<a-o>")
		start, end, _, ok := e.VisualRange()
		if !ok || start != w.start || end != w.end {
			t.Fatalf("step %d: selection %v..%v ok=%t, want %v..%v", i, start, end, ok, w.start, w.end)
		}
		if e.msg != "selection: "+w.label {
			t.Fatalf("step %d: message %q, want %q", i, e.msg, w.label)
		}
	}
}

func TestExpandSelectionWithTheArrowKeys(t *testing.T) {
	e := New("one two three\n", DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{0, 0})
	e.Feed("<a-up>")
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 2}) {
		t.Fatalf("alt-up selection = %v..%v ok=%t", start, end, ok)
	}
	// The shrink puts the character under the cursor back.
	e.Feed("<a-down>")
	if _, _, _, ok := e.VisualRange(); ok {
		t.Fatal("alt-down left a selection behind")
	}
	if e.Cursor() != (Pos{0, 0}) {
		t.Fatalf("cursor after alt-down: %+v", e.Cursor())
	}
}

func TestExpandedSelectionCoversWholeLines(t *testing.T) {
	// A grown paragraph selects whole lines, so deleting it takes the
	// lines with it.
	e := New(expandText, DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{2, 6})
	e.Feed("<a-o><a-o>")
	e.Feed("d")
	if got, want := e.Text(), "# Title\n\n\n## Section\n\n- one\n- two\n"; got != want {
		t.Fatalf("text after deleting the paragraph:\n got %q\nwant %q", got, want)
	}
}

func TestWordMotionRunsToTheLineEnd(t *testing.T) {
	text := "one two three\nfour five\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 8})
	// The last word of the line is selected in full.
	e.Feed("w")
	if e.Cursor() != (Pos{0, 12}) {
		t.Fatalf("cursor after w at the line end: %+v", e.Cursor())
	}
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 8}) || end != (Pos{0, 12}) {
		t.Fatalf("selection after w at the line end = %v..%v ok=%t", start, end, ok)
	}
	// The word motion walks the line and stops at its last word.
	e2 := New(text, DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{0, 0})
	e2.Feed("ww")
	if e2.Cursor() != (Pos{0, 8}) {
		t.Fatalf("cursor after two w: %+v", e2.Cursor())
	}
	// The last word of the line is selected too.
	e2.Feed("w")
	start, end, _, ok = e2.VisualRange()
	if !ok || start != (Pos{0, 8}) || end != (Pos{0, 12}) {
		t.Fatalf("selection after the last w = %v..%v ok=%t", start, end, ok)
	}
	if e2.Cursor() != (Pos{0, 12}) {
		t.Fatalf("cursor after the last w: %+v", e2.Cursor())
	}
	// With no word left on the line, the motion crosses to the next line.
	e2.Feed("w")
	if e2.Cursor() != (Pos{1, 0}) {
		t.Fatalf("cursor after the crossing w: %+v", e2.Cursor())
	}
	// The next word on the new line is selected, without its first
	// character.
	e2.Feed("w")
	start, end, _, ok = e2.VisualRange()
	if !ok || start != (Pos{1, 0}) || end != (Pos{1, 4}) {
		t.Fatalf("selection after the w on the next line = %v..%v ok=%t", start, end, ok)
	}
	if e2.Cursor() != (Pos{1, 5}) {
		t.Fatalf("cursor after the w on the next line: %+v", e2.Cursor())
	}
}

func TestBlockCursorStaysInsideTheSelection(t *testing.T) {
	// Helix draws the block cursor on the head side of the selection,
	// inside it: the word a motion lands on is neither selected nor
	// covered by the cursor.
	text := "one two three\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 0})
	e.Feed("w")
	if got, want := e.BlockCursor(), (Pos{0, 3}); got != want {
		t.Fatalf("block cursor after w = %+v, want %+v", got, want)
	}
	start, end, _, _ := e.VisualRange()
	if got, want := e.buf.Range(start, Pos{end.Line, end.Col + 1}), "one "; got != want {
		t.Fatalf("selection after w = %q, want %q", got, want)
	}
	// A backward motion selects the word it lands on, plus the space that
	// follows it, and the cursor sits on its first character.
	e2 := New(text, DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{0, 8})
	e2.Feed("b")
	if got, want := e2.BlockCursor(), (Pos{0, 4}); got != want {
		t.Fatalf("block cursor after b = %+v, want %+v", got, want)
	}
	start, end, _, _ = e2.VisualRange()
	if got, want := e2.buf.Range(start, Pos{end.Line, end.Col + 1}), "two "; got != want {
		t.Fatalf("selection after b = %q, want %q", got, want)
	}
}

func TestWordMotionSelectsTheLastWord(t *testing.T) {
	// The last word of a line can be selected, in normal and select mode.
	text := "one two three\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 8})
	e.Feed("w")
	start, end, mode, ok := e.VisualRange()
	if !ok || mode != ModeNormal || start != (Pos{0, 8}) || end != (Pos{0, 12}) {
		t.Fatalf("selection in normal mode = %v..%v mode=%v ok=%t", start, end, mode, ok)
	}
	e2 := New(text, DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{0, 8})
	e2.Feed("v")
	e2.Feed("w")
	start, end, mode, ok = e2.VisualRange()
	if !ok || mode != ModeSelect || start != (Pos{0, 8}) || end != (Pos{0, 12}) {
		t.Fatalf("selection in select mode = %v..%v mode=%v ok=%t", start, end, mode, ok)
	}
}

func TestBackwardWordSelectsTheWordAndItsSpace(t *testing.T) {
	// A backward word motion selects the word it lands on and the space
	// that follows it. The word the cursor sat in is left out.
	text := "one two three\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 8})
	e.Feed("b")
	start, end, mode, ok := e.VisualRange()
	if !ok || mode != ModeNormal || start != (Pos{0, 4}) || end != (Pos{0, 7}) {
		t.Fatalf("selection after b = %v..%v mode=%v ok=%t", start, end, mode, ok)
	}
	if got, want := e.buf.Range(start, Pos{end.Line, end.Col + 1}), "two "; got != want {
		t.Fatalf("selection after b = %q, want %q", got, want)
	}
	// The same in select mode.
	e2 := New(text, DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{0, 8})
	e2.Feed("v")
	e2.Feed("b")
	start, end, mode, ok = e2.VisualRange()
	if !ok || mode != ModeSelect || start != (Pos{0, 4}) || end != (Pos{0, 7}) {
		t.Fatalf("selection after vb = %v..%v mode=%v ok=%t", start, end, mode, ok)
	}
	// A big word motion behaves the same way: "one.two" is one big word.
	e3 := New("one.two three\n", DefaultOptions(), Hooks{})
	e3.SetViewport(10)
	e3.SetCursor(Pos{0, 8})
	e3.Feed("B")
	start, end, _, ok = e3.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 7}) {
		t.Fatalf("selection after B = %v..%v ok=%t", start, end, ok)
	}
	// A word at the end of a line has no space after it, so only the word
	// is selected.
	e4 := New("one two\n", DefaultOptions(), Hooks{})
	e4.SetViewport(10)
	e4.SetCursor(Pos{0, 6})
	e4.Feed("b")
	start, end, _, ok = e4.VisualRange()
	if !ok || start != (Pos{0, 4}) || end != (Pos{0, 6}) {
		t.Fatalf("selection after b at the line end = %v..%v ok=%t", start, end, ok)
	}
	// A motion with a count covers the words it travelled over.
	e5 := New(text, DefaultOptions(), Hooks{})
	e5.SetViewport(10)
	e5.SetCursor(Pos{0, 8})
	e5.Feed("2b")
	start, end, _, ok = e5.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 7}) {
		t.Fatalf("selection after 2b = %v..%v ok=%t", start, end, ok)
	}
}

func TestForwardWordSelectsTheWordAndItsSpace(t *testing.T) {
	// A forward word motion covers the word it moves over, whole, plus
	// the space that follows it. The word it lands on stays out of the
	// selection.
	text := "one two three\n"
	e := New(text, DefaultOptions(), Hooks{})
	e.SetViewport(10)
	// From the middle of a word the selection still covers the word.
	e.SetCursor(Pos{0, 1})
	e.Feed("w")
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 3}) {
		t.Fatalf("selection after w in a word = %v..%v ok=%t", start, end, ok)
	}
	if got, want := e.buf.Range(start, Pos{end.Line, end.Col + 1}), "one "; got != want {
		t.Fatalf("selection after w in a word = %q, want %q", got, want)
	}
	// From a blank the selection is the blank itself: the first character
	// of the next word is never part of it.
	e2 := New(text, DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{0, 3})
	e2.Feed("w")
	start, end, _, ok = e2.VisualRange()
	if !ok || start != (Pos{0, 3}) || end != (Pos{0, 3}) {
		t.Fatalf("selection after w on a blank = %v..%v ok=%t", start, end, ok)
	}
	if got, want := e2.BlockCursor(), (Pos{0, 3}); got != want {
		t.Fatalf("block cursor after w on a blank = %+v, want %+v", got, want)
	}
	// The last word of a line has no space after it, so only the word is
	// selected.
	e3 := New(text, DefaultOptions(), Hooks{})
	e3.SetViewport(10)
	e3.SetCursor(Pos{0, 8})
	e3.Feed("w")
	start, end, _, ok = e3.VisualRange()
	if !ok || start != (Pos{0, 8}) || end != (Pos{0, 12}) {
		t.Fatalf("selection after w at the line end = %v..%v ok=%t", start, end, ok)
	}
}

func TestWordMotionDoesNotJoinLines(t *testing.T) {
	// `wd` on the last word of a line must not eat the line break.
	e := New("one two\nthree four\n", DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{0, 4})
	e.Feed("wd")
	if got, want := e.Text(), "one \nthree four\n"; got != want {
		t.Fatalf("text after wd at the line end:\n got %q\nwant %q", got, want)
	}
}

func TestBackwardWordMotionOntoAnEmptyLine(t *testing.T) {
	// An empty line is a word stop like any other: the motion lands on
	// the line break and the selection stays empty.
	e := New("one\n\nthree\n", DefaultOptions(), Hooks{})
	e.SetViewport(10)
	e.SetCursor(Pos{2, 0})
	e.Feed("b")
	if e.Cursor() != (Pos{1, 0}) {
		t.Fatalf("cursor after b onto an empty line: %+v", e.Cursor())
	}
	if _, _, _, ok := e.VisualRange(); ok {
		t.Fatal("b onto an empty line left a selection behind")
	}
	// A count over several empty lines lands on the last one.
	e2 := New("one two\n\n\nsix\n", DefaultOptions(), Hooks{})
	e2.SetViewport(10)
	e2.SetCursor(Pos{3, 0})
	e2.Feed("2b")
	if e2.Cursor() != (Pos{1, 0}) {
		t.Fatalf("cursor after 2b over empty lines: %+v", e2.Cursor())
	}
	if _, _, _, ok := e2.VisualRange(); ok {
		t.Fatal("2b over empty lines left a selection behind")
	}
}

func TestExpandSelectionGrowsFromAPartialSelection(t *testing.T) {
	// A selection that is no rung of the ladder — a word motion's or a
	// handmade one — grows to the next context around it, not straight
	// to the note.
	e := New("one two three\n\nnext para\n", DefaultOptions(), Hooks{})
	e.SetViewport(20)
	e.SetCursor(Pos{0, 5})
	e.Feed("w")
	start, end, _, ok := e.VisualRange()
	if !ok || start != (Pos{0, 4}) || end != (Pos{0, 7}) {
		t.Fatalf("selection after w = %v..%v ok=%t", start, end, ok)
	}
	e.Feed("<a-o>")
	start, end, _, ok = e.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 12}) {
		t.Fatalf("grown selection = %v..%v ok=%t, want the paragraph", start, end, ok)
	}
	if e.msg != "selection: paragraph" {
		t.Fatalf("message %q, want %q", e.msg, "selection: paragraph")
	}
	// The same for a handmade selection in select mode.
	e2 := New("one two three\n\nnext para\n", DefaultOptions(), Hooks{})
	e2.SetViewport(20)
	e2.SetCursor(Pos{0, 4})
	e2.Feed("v6l")
	e2.Feed("<a-o>")
	start, end, _, ok = e2.VisualRange()
	if !ok || start != (Pos{0, 0}) || end != (Pos{0, 12}) {
		t.Fatalf("grown handmade selection = %v..%v ok=%t, want the paragraph", start, end, ok)
	}
	// Shrinking a word motion's selection falls to the word inside it.
	e3 := New("one two three\n\nnext para\n", DefaultOptions(), Hooks{})
	e3.SetViewport(20)
	e3.SetCursor(Pos{0, 5})
	e3.Feed("w<a-i>")
	start, end, _, ok = e3.VisualRange()
	if !ok || start != (Pos{0, 4}) || end != (Pos{0, 6}) {
		t.Fatalf("shrunk w selection = %v..%v ok=%t, want the word", start, end, ok)
	}
}
