package vim

import "testing"

func TestDisplayRowBoundariesRespectCountsAndLogicalMode(t *testing.T) {
	opts := DefaultOptions()
	opts.WrappedLineMotions = true
	ed := New("abcdefghij\nsecond", opts, Hooks{DisplayRowBounds: func(p Pos) (int, int) {
		if p.Line == 0 {
			if p.Col < 5 {
				return 0, 5
			}
			return 5, 10
		}
		return 0, 6
	}})
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 4 {
		t.Fatal(ed.Cursor())
	}
	ed.SetCursor(Pos{})
	ed.HandleKey(R('A'))
	if ed.Cursor().Col != 5 {
		t.Fatal(ed.Cursor())
	}
	ed.HandleKey(Special("esc"))
	ed.SetCursor(Pos{0, 7})
	ed.HandleKey(R('I'))
	if ed.Cursor().Col != 5 {
		t.Fatal(ed.Cursor())
	}
	ed.HandleKey(Special("esc"))
	ed.SetCursor(Pos{})
	ed.HandleKey(R('2'))
	ed.HandleKey(R('$'))
	if ed.Cursor() != (Pos{1, 5}) {
		t.Fatal(ed.Cursor())
	}
	opts.WrappedLineMotions = false
	ed.SetOptions(opts)
	ed.SetCursor(Pos{})
	ed.HandleKey(R('$'))
	if ed.Cursor().Col != 9 {
		t.Fatal(ed.Cursor())
	}
}
